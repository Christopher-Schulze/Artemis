package engine

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/Christopher-Schulze/Artemis/agent"
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
	cfg    Config
	client *network.HTTPClient
	jsRT   *js.Runtime
}

// New creates an Engine using cfg. The returned engine must be Closed.
func New(cfg Config) (*Engine, error) {
	cfg.applyDefaults()
	client, err := network.NewHTTPClient(network.HTTPClientConfig{
		UserAgent:    cfg.UserAgent,
		ProxyURL:     cfg.ProxyURL,
		Timeout:      cfg.Timeout,
		MaxBodyBytes: cfg.MaxBodyBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("engine: build http client: %w", err)
	}
	var rt *js.Runtime
	switch {
	case cfg.JSContextPoolSize > 0 && cfg.JSContextPoolWarm:
		rt, err = js.NewRuntimeWithWarmPool(cfg.JSContextPoolSize)
		if err != nil {
			return nil, fmt.Errorf("engine: warm pool: %w", err)
		}
	case cfg.JSContextPoolSize > 0:
		rt = js.NewRuntimeWithPool(cfg.JSContextPoolSize)
	default:
		rt = js.NewRuntime()
	}
	return &Engine{cfg: cfg, client: client, jsRT: rt}, nil
}

// Config returns a copy of the active configuration.
func (e *Engine) Config() Config { return e.cfg }

// HTTPClient exposes the underlying network client. Phase 1 callers
// use it for cookie inspection.
func (e *Engine) HTTPClient() *network.HTTPClient { return e.client }

// Fetch performs an HTTP request on rawURL and returns a Page. The page
// is fully fetched and parsed before return. Method defaults to GET.
func (e *Engine) Fetch(ctx context.Context, rawURL string, opts FetchOpts) (*Page, error) {
	method := opts.Method
	if method == "" {
		method = http.MethodGet
	} else {
		method = strings.ToUpper(method)
	}
	headers := opts.Headers
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
	// IP filter
	if e.cfg.BlockPrivateIPs {
		if u, perr := url.Parse(rawURL); perr == nil {
			if perr := network.CheckHostPublic(u); perr != nil {
				return nil, fmt.Errorf("engine: %w", perr)
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
			})
			if err != nil {
				return nil, fmt.Errorf("engine: js context: %w", err)
			}
			page := &Page{
				url:        finalURL,
				statusCode: mock.Status,
				headers:    mock.Headers,
				document:   doc,
				rawBody:    mock.Body,
				jsCtx:      jsCtx,
			}
			if opts.RunInlineScripts || opts.RunScripts {
				e.runScripts(ctx, jsCtx, doc, finalURL)
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
	})
	if err != nil {
		return nil, fmt.Errorf("engine: js context: %w", err)
	}

	page := &Page{
		url:        resp.FinalURL,
		statusCode: resp.StatusCode,
		headers:    resp.Headers,
		document:   doc,
		rawBody:    resp.Body,
		jsCtx:      jsCtx,
	}

	if opts.RunInlineScripts || opts.RunScripts {
		e.runScripts(ctx, jsCtx, doc, resp.FinalURL)
	}

	return page, nil
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
	if e.client != nil {
		if err := e.client.Close(); err != nil {
			firstErr = err
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
// Errors during script execution are logged via the page's console hook
// but do not abort the page.
func (e *Engine) runScripts(ctx context.Context, jsCtx *js.Context, doc *webapi.Document, baseURL string) {
	root := doc.Root()
	if root == nil {
		return
	}
	cache := map[string][]byte{}
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
				if err != nil || resp.StatusCode != 200 {
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
		return webapi.WalkContinue
	})
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
