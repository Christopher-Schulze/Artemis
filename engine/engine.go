package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"

	"github.com/Christopher-Schulze/Artemis/agent"
	"github.com/Christopher-Schulze/Artemis/diagnostics"
	artemisdownload "github.com/Christopher-Schulze/Artemis/download"
	"github.com/Christopher-Schulze/Artemis/js"
	"github.com/Christopher-Schulze/Artemis/network"
	"github.com/Christopher-Schulze/Artemis/parser"
	"github.com/Christopher-Schulze/Artemis/webapi"
)

// FetchOpts customizes a single Fetch call. The zero value is valid.
type FetchOpts struct {
	// Method overrides the HTTP method (default GET).
	Method string
	// Body is the request body for POST / PUT / PATCH.
	Body []byte
	// ContentType is the value of the Content-Type header for non-GET
	// requests with a body. If empty and Body is non-nil, defaults to
	// "application/x-www-form-urlencoded".
	ContentType string
	// Headers are merged into the outgoing request, after the Engine's
	// default User-Agent has been set.
	Headers http.Header
	// MaxBodyBytes overrides Config.MaxBodyBytes for this request when
	// non-zero.
	MaxBodyBytes int64
	// RunScripts executes <script> tags (inline AND external) in document
	// order against the page's JS context after parsing. External scripts
	// are fetched via the engine's HTTP client and cached per URL.
	RunScripts bool
	// RunInlineScripts is the old name; prefer RunScripts. Kept as alias.
	// When either is true, scripts execute. External scripts always load
	// in this build; if you want inline-only, parse the page yourself.
	RunInlineScripts bool
	// Console captures console.* output from JS executed against the
	// page. nil means drop all output.
	Console js.Console
	// Navigator overrides navigator.* values seen by JS.
	Navigator js.NavigatorConfig
	// OnRequest is invoked with the outbound request before it leaves
	// the engine. Returning a non-nil response short-circuits the
	// network call (mock/cache); returning nil + nil error proceeds
	// normally. Errors are propagated.
	OnRequest func(*RequestInfo) (*ResponseInfo, error)
	// AsyncFetch routes JS fetch() calls through goroutines so multiple
	// concurrent fetch() calls run in parallel. Use Page.WaitIdle to
	// block until pending fetches settle.
	AsyncFetch bool
}

// RequestInfo describes an outbound request handed to OnRequest hooks.
type RequestInfo struct {
	Method  string
	URL     string
	Headers http.Header
	Body    []byte
}

// ResponseInfo describes a response returned by an OnRequest hook to
// short-circuit the network call.
type ResponseInfo struct {
	Status   int
	Headers  http.Header
	Body     []byte
	FinalURL string
}

// ErrRobotsDisallowed is returned when ObeyRobots is on and the page's
// URL is forbidden by robots.txt for the configured UserAgent.
var ErrRobotsDisallowed = network.ErrRobotsDisallowed

// Engine is the top-level handle for performing fetches and producing
// pages. It is safe for concurrent use.
type Engine struct {
	cfg         Config
	client      *network.HTTPClient
	policy      *network.Policy
	jsRT        *js.Runtime
	downloadMu  sync.Mutex
	downloads   map[string]*artemisdownload.DownloadManager
	session     *sessionBudgetController
	diagnostics *diagnostics.Store
}

// Download is the verified metadata for a committed session download.
type Download = artemisdownload.Download

// New creates an Engine using cfg. The returned engine must be Closed.
func New(cfg Config) (*Engine, error) {
	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if err := cfg.SessionBudget.validate(); err != nil {
		return nil, err
	}
	diagnosticStore, err := diagnostics.NewStore(cfg.Diagnostics)
	if err != nil {
		return nil, fmt.Errorf("engine: diagnostics: %w", err)
	}
	resourceSink := func(sessionID string, usage SessionUsage) error {
		return diagnosticStore.AppendResource(diagnostics.ResourceUsage{
			Scope: "renderless", SessionRef: diagnostics.HashSession(sessionID), Requests: usage.Requests,
			ResponseBytes: usage.ResponseBytes, DiskBytes: usage.DiskBytes,
			ActiveTabs: usage.ActiveTabs, Concurrent: usage.Concurrent, SessionStopped: usage.Cancelled,
		})
	}
	session := newSessionBudgetController(cfg.SessionBudget, cfg.SessionID, resourceSink)
	policy, err := network.NewPolicy(cfg.PolicyConfig, nil, func(decision network.Decision) error {
		return diagnosticStore.AppendPolicy(diagnostics.PolicyDecision{
			Operation: string(decision.Kind), Transport: decision.Scheme, Host: decision.Host,
			Port: decision.Port, Result: string(decision.Action), ReasonCode: decision.Reason,
			SessionRef: diagnostics.HashSession(decision.SessionID),
		})
	})
	if err != nil {
		return nil, errors.Join(fmt.Errorf("engine: build network policy: %w", err), session.close())
	}
	cfg.PolicyConfig = policy.Config()
	client, err := network.NewHTTPClient(network.HTTPClientConfig{
		UserAgent:        cfg.UserAgent,
		ProxyURL:         cfg.ProxyURL,
		Timeout:          cfg.Timeout,
		MaxBodyBytes:     cfg.MaxBodyBytes,
		Policy:           policy,
		SessionID:        cfg.SessionID,
		RequestLifecycle: session,
	})
	if err != nil {
		return nil, errors.Join(fmt.Errorf("engine: build http client: %w", err), session.close())
	}
	var rt *js.Runtime
	switch {
	case cfg.JSContextPoolSize > 0 && cfg.JSContextPoolWarm:
		rt, err = js.NewRuntimeWithWarmPool(cfg.JSContextPoolSize)
		if err != nil {
			return nil, errors.Join(fmt.Errorf("engine: warm pool: %w", err), client.Close(), session.close())
		}
	case cfg.JSContextPoolSize > 0:
		rt = js.NewRuntimeWithPool(cfg.JSContextPoolSize)
	default:
		rt = js.NewRuntime()
	}
	return &Engine{cfg: cfg, client: client, policy: policy, jsRT: rt, session: session, diagnostics: diagnosticStore}, nil
}

// Config returns a copy of the active configuration.
func (e *Engine) Config() Config { return e.cfg }

// HTTPClient exposes the underlying network client. Phase 1 callers
// use it for cookie inspection.
func (e *Engine) HTTPClient() *network.HTTPClient { return e.client }

// SessionUsage returns an atomic snapshot of the active hard-budget counters.
func (e *Engine) SessionUsage() SessionUsage { return e.session.usage() }

// Diagnostics returns a redacted snapshot of the bounded audit ledger.
func (e *Engine) Diagnostics() ([]diagnostics.Record, error) { return e.diagnostics.Snapshot() }

// Download fetches raw content under TargetDownload policy and atomically
// commits it to this engine session's owned download directory.
func (e *Engine) Download(ctx context.Context, rawURL, filename string) (*Download, error) {
	if ctx == nil {
		return nil, errors.New("engine: download context required")
	}
	ctx, release := e.session.mergeContext(ctx)
	defer release()
	resp, err := e.client.DoTarget(ctx, network.Request{Method: http.MethodGet, URL: rawURL}, network.TargetDownload)
	if err != nil {
		return nil, fmt.Errorf("engine: download %s: %w", rawURL, err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("engine: download %s: status %d", rawURL, resp.StatusCode)
	}
	if strings.TrimSpace(filename) == "" {
		filename = artemisdownload.SuggestedFilename(resp.FinalURL, resp.Headers.Get("Content-Disposition"))
	}
	sessionID := network.SessionID(ctx, e.cfg.SessionID)
	manager, err := e.downloadManager(sessionID)
	if err != nil {
		return nil, err
	}
	download, err := manager.Store(filename, resp.Headers.Get("Content-Type"), resp.Body)
	if err != nil {
		return nil, err
	}
	return e.recordDownload(sessionID, manager, download)
}

// Fetch performs an HTTP request on rawURL and returns a Page. The page
// is fully fetched and parsed before return. Method defaults to GET.
func (e *Engine) Fetch(ctx context.Context, rawURL string, opts FetchOpts) (result *Page, resultErr error) {
	if ctx == nil {
		return nil, errors.New("engine: fetch context required")
	}
	if opts.MaxBodyBytes < 0 {
		return nil, errors.New("engine: maximum body bytes override must not be negative")
	}
	ctx, release := e.session.mergeContext(ctx)
	defer release()
	sessionID := network.SessionID(ctx, e.cfg.SessionID)
	method := opts.Method
	if method == "" {
		method = http.MethodGet
	} else {
		method = strings.ToUpper(method)
	}
	headers := opts.Headers.Clone()
	if len(opts.Body) > 0 && method != http.MethodGet {
		if headers == nil {
			headers = http.Header{}
		}
		ct := opts.ContentType
		if ct == "" {
			ct = "application/x-www-form-urlencoded"
		}
		if headers.Get("Content-Type") == "" {
			headers.Set("Content-Type", ct)
		}
	}
	// Robots check (best-effort)
	if e.cfg.ObeyRobots && method == http.MethodGet {
		if u, perr := url.Parse(rawURL); perr == nil && u.Host != "" {
			if policy, perr := e.client.FetchRobots(ctx, u); perr == nil {
				if !policy.Allowed(e.cfg.UserAgent, u.Path) {
					return nil, fmt.Errorf("engine: %w: %s", ErrRobotsDisallowed, rawURL)
				}
			}
		}
	}
	// OnRequest interception
	if opts.OnRequest != nil {
		mock, err := opts.OnRequest(&RequestInfo{
			Method:  method,
			URL:     rawURL,
			Headers: headers,
			Body:    opts.Body,
		})
		if err != nil {
			return nil, fmt.Errorf("engine: OnRequest: %w", err)
		}
		if mock != nil {
			bodyLimit := opts.MaxBodyBytes
			if bodyLimit == 0 {
				bodyLimit = e.cfg.MaxBodyBytes
			}
			if int64(len(mock.Body)) > bodyLimit {
				return nil, fmt.Errorf("engine: OnRequest response body exceeds limit of %d bytes", bodyLimit)
			}
			if mock.Status < 100 || mock.Status > 599 {
				return nil, fmt.Errorf("engine: OnRequest response status %d is invalid", mock.Status)
			}
			if err := e.session.admitSynthetic(int64(len(mock.Body))); err != nil {
				return nil, err
			}
			if err := e.session.acquireTab(sessionID); err != nil {
				return nil, err
			}
			tabOwned := true
			defer func() {
				if tabOwned {
					resultErr = errors.Join(resultErr, e.session.releaseTab(sessionID))
				}
			}()
			doc, err := parser.ParseHTML(bytes.NewReader(mock.Body), mock.FinalURL)
			if err != nil {
				return nil, fmt.Errorf("engine: parse mock: %w", err)
			}
			finalURL := mock.FinalURL
			if finalURL == "" {
				finalURL = rawURL
			}
			jsCtx, err := e.jsRT.NewContext(doc, js.ContextOpts{
				Console:        opts.Console,
				Fetch:          e.jsFetchFunc(),
				AsyncFetch:     opts.AsyncFetch,
				Navigator:      opts.Navigator,
				LoadStylesheet: e.stylesheetLoader(finalURL),
				LoadIFrame:     e.iframeLoader(finalURL),
				Policy:         e.policy,
				SessionID:      sessionID,
			})
			if err != nil {
				return nil, fmt.Errorf("engine: js context: %w", err)
			}
			page := &Page{
				url:        finalURL,
				statusCode: mock.Status,
				headers:    mock.Headers.Clone(),
				document:   doc,
				rawBody:    append([]byte(nil), mock.Body...),
				jsCtx:      jsCtx,
				download: func(filename, contentType string, content []byte) (*Download, error) {
					return e.storePageDownload(sessionID, filename, contentType, content)
				},
				session:   e.session,
				sessionID: sessionID,
			}
			tabOwned = false
			if opts.RunInlineScripts || opts.RunScripts {
				if err := e.runScripts(ctx, jsCtx, doc, finalURL); err != nil {
					_ = page.Close()
					return nil, err
				}
			}
			return page, nil
		}
	}

	var body io.Reader
	if len(opts.Body) > 0 {
		body = bytes.NewReader(opts.Body)
	}
	req := network.Request{
		Method:       method,
		URL:          rawURL,
		Body:         body,
		Headers:      headers,
		MaxBodyBytes: opts.MaxBodyBytes,
	}
	resp, err := e.client.Do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("engine: fetch %s: %w", rawURL, err)
	}
	doc, err := parser.ParseHTML(bytes.NewReader(resp.Body), resp.FinalURL)
	if err != nil {
		return nil, fmt.Errorf("engine: parse %s: %w", resp.FinalURL, err)
	}

	jsCtx, err := e.jsRT.NewContext(doc, js.ContextOpts{
		Console:        opts.Console,
		Fetch:          e.jsFetchFunc(),
		AsyncFetch:     opts.AsyncFetch,
		Navigator:      opts.Navigator,
		GetCookie:      e.cookieGetter(resp.FinalURL),
		SetCookie:      e.cookieSetter(resp.FinalURL),
		LoadStylesheet: e.stylesheetLoader(resp.FinalURL),
		LoadIFrame:     e.iframeLoader(resp.FinalURL),
		Policy:         e.policy,
		SessionID:      sessionID,
	})
	if err != nil {
		return nil, fmt.Errorf("engine: js context: %w", err)
	}
	if err := e.session.acquireTab(sessionID); err != nil {
		jsCtx.Close()
		return nil, err
	}

	page := &Page{
		url:        resp.FinalURL,
		statusCode: resp.StatusCode,
		headers:    resp.Headers.Clone(),
		document:   doc,
		rawBody:    append([]byte(nil), resp.Body...),
		jsCtx:      jsCtx,
		download: func(filename, contentType string, content []byte) (*Download, error) {
			return e.storePageDownload(sessionID, filename, contentType, content)
		},
		session:   e.session,
		sessionID: sessionID,
	}

	if opts.RunInlineScripts || opts.RunScripts {
		if err := e.runScripts(ctx, jsCtx, doc, resp.FinalURL); err != nil {
			_ = page.Close()
			return nil, err
		}
	}

	return page, nil
}

func (e *Engine) downloadManager(sessionID string) (*artemisdownload.DownloadManager, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, errors.New("engine: session ID required for downloads")
	}
	e.downloadMu.Lock()
	defer e.downloadMu.Unlock()
	if manager := e.downloads[sessionID]; manager != nil {
		return manager, nil
	}
	manager, err := artemisdownload.NewDownloadManager(artemisdownload.DownloadConfig{
		RootDir:      e.cfg.DownloadRoot,
		SessionID:    sessionID,
		MaxDiskBytes: e.cfg.MaxDownloadDiskBytes,
		MinFreeBytes: e.cfg.MinDownloadFreeBytes,
		Policy:       e.policy,
	})
	if err != nil {
		return nil, err
	}
	if e.downloads == nil {
		e.downloads = make(map[string]*artemisdownload.DownloadManager)
	}
	e.downloads[sessionID] = manager
	return manager, nil
}

func (e *Engine) storePageDownload(sessionID, filename, contentType string, content []byte) (*Download, error) {
	manager, err := e.downloadManager(sessionID)
	if err != nil {
		return nil, err
	}
	download, err := manager.Store(filename, contentType, content)
	if err != nil {
		return nil, err
	}
	return e.recordDownload(sessionID, manager, download)
}

// ReleaseSession removes runtime-only download synchronization state after all
// work for sessionID has stopped. Committed files remain under the owned store.
func (e *Engine) ReleaseSession(sessionID string) {
	e.downloadMu.Lock()
	delete(e.downloads, sessionID)
	e.downloadMu.Unlock()
}

func (e *Engine) recordDownload(sessionID string, manager *artemisdownload.DownloadManager, download *Download) (*Download, error) {
	usage, err := manager.DiskUsage()
	if err != nil {
		removeErr := os.Remove(download.Path)
		return nil, errors.Join(fmt.Errorf("engine: measure download disk usage: %w", err), removeErr)
	}
	if err := e.session.setDiskUsage(sessionID, usage); err != nil {
		removeErr := os.Remove(download.Path)
		return nil, errors.Join(err, removeErr)
	}
	return download, nil
}

// Submit performs a form submission via the engine's HTTP client and
// returns the resulting page. The submission's URL, Method, Body, and
// ContentType override the corresponding FetchOpts fields.
func (e *Engine) Submit(ctx context.Context, sub agent.FormSubmission, opts FetchOpts) (*Page, error) {
	opts.Method = sub.Method
	opts.Body = sub.Body
	if sub.ContentType != "" {
		opts.ContentType = sub.ContentType
	}
	return e.Fetch(ctx, sub.URL, opts)
}

// Close releases engine-held resources.
func (e *Engine) Close() error {
	var firstErr error
	if e.session != nil {
		firstErr = e.session.close()
	}
	if e.client != nil {
		if err := e.client.Close(); err != nil {
			firstErr = errors.Join(firstErr, err)
		}
	}
	if e.jsRT != nil {
		e.jsRT.Close()
	}
	return firstErr
}

// runScripts walks the document and executes every <script> in document
// order. External scripts (`src=...`) are fetched via the engine's HTTP
// client, cached per URL, and executed against the same JS context.
// Ordinary script errors do not abort the page; session-budget failures are
// always propagated because they cancel the owning engine.
func (e *Engine) runScripts(ctx context.Context, jsCtx *js.Context, doc *webapi.Document, baseURL string) error {
	root := doc.Root()
	if root == nil {
		return nil
	}
	cache := map[string][]byte{}
	var runErr error
	webapi.Walk(root, func(n *webapi.Node) webapi.WalkAction {
		if n.Type() != webapi.NodeElement || n.Tag() != "script" {
			return webapi.WalkContinue
		}
		// Skip non-JS script types (e.g. application/ld+json)
		if t, ok := n.Attr("type"); ok && t != "" {
			lower := strings.ToLower(t)
			if lower != "text/javascript" && lower != "application/javascript" && lower != "module" {
				return webapi.WalkContinue
			}
		}
		var code string
		if src, ok := n.Attr("src"); ok && src != "" {
			absURL := src
			if base, err := url.Parse(baseURL); err == nil && base != nil {
				if ref, err := url.Parse(src); err == nil {
					absURL = base.ResolveReference(ref).String()
				}
			}
			if cached, ok := cache[absURL]; ok {
				code = string(cached)
			} else {
				resp, err := e.client.Do(ctx, network.Request{Method: http.MethodGet, URL: absURL})
				if err != nil {
					if budgetErr := e.session.err(); budgetErr != nil {
						runErr = budgetErr
						return webapi.WalkStop
					}
					return webapi.WalkContinue
				}
				if resp.StatusCode != 200 {
					return webapi.WalkContinue
				}
				cache[absURL] = resp.Body
				code = string(resp.Body)
			}
		} else {
			code = n.Text()
		}
		if strings.TrimSpace(code) == "" {
			return webapi.WalkContinue
		}
		_, _ = jsCtx.Eval(ctx, code)
		if budgetErr := e.session.err(); budgetErr != nil {
			runErr = budgetErr
			return webapi.WalkStop
		}
		return webapi.WalkContinue
	})
	return runErr
}

// cookieGetter returns a function that serializes the cookies for
// rawURL as `name=value; name2=value2`.
func (e *Engine) cookieGetter(rawURL string) func() string {
	return func() string {
		jar := e.client.CookieJar()
		if jar == nil {
			return ""
		}
		u, err := url.Parse(rawURL)
		if err != nil || u == nil {
			return ""
		}
		cs := jar.Cookies(u)
		parts := make([]string, 0, len(cs))
		for _, c := range cs {
			parts = append(parts, c.Name+"="+c.Value)
		}
		return strings.Join(parts, "; ")
	}
}

// cookieSetter returns a function that ingests a Set-Cookie-style line
// and stores the cookie in the jar against rawURL.
func (e *Engine) cookieSetter(rawURL string) func(string) {
	return func(line string) {
		jar := e.client.CookieJar()
		if jar == nil {
			return
		}
		u, err := url.Parse(rawURL)
		if err != nil || u == nil {
			return
		}
		// Parse via http.ReadResponse-style parsing: build a fake header
		// with Set-Cookie: <line> and use http.Response.Cookies.
		resp := &http.Response{Header: http.Header{}}
		resp.Header.Add("Set-Cookie", line)
		jar.SetCookies(u, resp.Cookies())
	}
}

// iframeLoader returns a function that fetches an iframe's HTML body
// resolved against pageURL.
func (e *Engine) iframeLoader(pageURL string) js.IFrameLoader {
	return func(href string) ([]byte, error) {
		abs := href
		if base, err := url.Parse(pageURL); err == nil && base != nil {
			if ref, err := url.Parse(href); err == nil {
				abs = base.ResolveReference(ref).String()
			}
		}
		resp, err := e.client.Do(context.Background(), network.Request{
			Method: http.MethodGet,
			URL:    abs,
		})
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("iframe %s: status %d", abs, resp.StatusCode)
		}
		return resp.Body, nil
	}
}

// stylesheetLoader returns a function that fetches a CSS stylesheet
// resolved against pageURL. Used for `<link rel=stylesheet>` external
// loads at Context init.
func (e *Engine) stylesheetLoader(pageURL string) js.StylesheetLoader {
	return func(href string) ([]byte, error) {
		abs := href
		if base, err := url.Parse(pageURL); err == nil && base != nil {
			if ref, err := url.Parse(href); err == nil {
				abs = base.ResolveReference(ref).String()
			}
		}
		resp, err := e.client.Do(context.Background(), network.Request{
			Method: http.MethodGet,
			URL:    abs,
		})
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("stylesheet %s: status %d", abs, resp.StatusCode)
		}
		return resp.Body, nil
	}
}

// jsFetchFunc adapts the engine's network client into a js.FetchFunc.
func (e *Engine) jsFetchFunc() js.FetchFunc {
	return func(ctx context.Context, req js.FetchRequest) (*js.FetchResponse, error) {
		hdrs := make(http.Header, len(req.Headers))
		for k, v := range req.Headers {
			hdrs.Set(k, v)
		}
		var body io.Reader
		if len(req.Body) > 0 {
			body = bytes.NewReader(req.Body)
		}
		resp, err := e.client.Do(ctx, network.Request{
			Method:  req.Method,
			URL:     req.URL,
			Body:    body,
			Headers: hdrs,
		})
		if err != nil {
			return nil, err
		}
		return &js.FetchResponse{
			Status:     resp.StatusCode,
			StatusText: http.StatusText(resp.StatusCode),
			Headers:    resp.Headers,
			Body:       resp.Body,
			URL:        resp.FinalURL,
		}, nil
	}
}
