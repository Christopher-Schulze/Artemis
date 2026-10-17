// Package network owns policy-bound HTTP, target validation, cookies,
// robots, response decoding and browser network primitives.
package network

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Christopher-Schulze/Artemis/network/cookiejar"
	"github.com/Christopher-Schulze/Artemis/platform"

	"golang.org/x/net/publicsuffix"
)

// HTTPClientConfig configures an HTTPClient.
type HTTPClientConfig struct {
	// UserAgent sets the User-Agent header on every outgoing request.
	UserAgent string
	// WebBotAuth signs every outgoing request (including each redirect hop)
	// with the operator's Web Bot Auth identity. Nil or disabled sends
	// unsigned requests.
	WebBotAuth *WebBotAuth
	// ChromeLike supplies generated desktop-Chrome-style header defaults
	// (Accept, Accept-Language, Sec-CH-UA*, Sec-Fetch-*, Upgrade-Insecure-Requests)
	// for headers omitted by the caller, plus the vendored TLS preset.
	// Caller overrides may change identity; complete browser wire parity
	// is not established by these generated defaults.
	ChromeLike bool
	// ChromeOS selects the header-identity OS ("windows"|"macos"|"linux");
	// empty resolves to the host OS.
	ChromeOS string
	// TLSInsecureSkipVerify disables certificate verification on the
	// Chrome-fingerprinted TLS path. Test/private-fixture only — never set
	// this for real traffic; it removes MITM protection.
	TLSInsecureSkipVerify bool
	// ProxyURL explicitly routes outbound requests through an HTTP(S) proxy.
	// Empty uses direct policy-bound connections; environment proxies are ignored.
	// ChromeLike currently supports HTTP upstream proxies only.
	ProxyURL string
	// Timeout is the per-request timeout (including dial, TLS, and body
	// read). A zero value disables the deadline.
	Timeout time.Duration
	// MaxBodyBytes caps the response body within the mandatory policy limit.
	MaxBodyBytes int64
	// Policy is the mandatory outbound network security boundary. Nil uses
	// the deny-private default policy.
	Policy *Policy
	// SessionID owns the default cookie partition and redacted policy identity.
	SessionID string
	// RequestLifecycle optionally enforces session-wide request admission,
	// cancellation, concurrency, and response-byte accounting.
	RequestLifecycle RequestLifecycle
	// DisableTCPFastOpen keeps normal connect semantics even when Linux admits
	// the optimization. Proxies and body-bearing/non-GET/HEAD requests always
	// retain normal connect semantics.
	DisableTCPFastOpen bool
	MemoryAdmission    MemoryAdmission
}

// HTTPClient performs HTTP requests on behalf of the engine.
type HTTPClient struct {
	cfg           HTTPClientConfig
	client        *http.Client
	jar           http.CookieJar
	robots        *robotsCache
	chromeHeaders http.Header
	sessionMu     sync.Mutex
	sessionJars   map[string]http.CookieJar
	closed        bool
	mmap          platform.ProbeResult
	proxyURL      *url.URL
}

// NewHTTPClient builds an HTTPClient.
func NewHTTPClient(cfg HTTPClientConfig) (*HTTPClient, error) {
	if cfg.Timeout < 0 || cfg.MaxBodyBytes < 0 {
		return nil, errors.New("network: invalid HTTP client limit")
	}
	if cfg.MemoryAdmission != nil {
		value := reflect.ValueOf(cfg.MemoryAdmission)
		switch value.Kind() {
		case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
			if value.IsNil() {
				return nil, errors.New("network: nil memory admission owner")
			}
		}
	}
	policy := cfg.Policy
	if policy == nil {
		var err error
		policy, err = NewPolicy(DefaultPolicyConfig(), nil, nil)
		if err != nil {
			return nil, fmt.Errorf("default network policy: %w", err)
		}
	}
	cfg.Policy = policy
	var chromeHeaders http.Header
	if cfg.ChromeLike {
		os := cfg.ChromeOS
		if os == "" {
			os = HostOS(runtime.GOOS)
		}
		chromeHeaders = HeaderGenerator{OS: os}.AllHeaders()
	}
	// Pool tuned for crawler-style workloads: lots of subresources from
	// a small set of hosts, parallel fetches via the async-runtime.
	// A client needs only the TCP Fast Open and mmap probes; the remaining
	// platform probes (affinity, memory, container, codec) are not per-client work.
	dialContext := policy.httpDialContextFor(cfg.SessionID, platform.ProbeTCPFastOpen(), !cfg.DisableTCPFastOpen && cfg.ProxyURL == "")
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           dialContext,
		MaxIdleConns:          512,
		MaxIdleConnsPerHost:   64,
		MaxConnsPerHost:       0, // unlimited (HTTP/2 multiplex needs only one)
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 500 * time.Millisecond,
		ResponseHeaderTimeout: 30 * time.Second,
		ForceAttemptHTTP2:     true,
		DisableCompression:    false,
		WriteBufferSize:       64 * 1024,
		ReadBufferSize:        64 * 1024,
	}
	var proxyURL *url.URL
	if cfg.ProxyURL != "" {
		u, err := parsePolicyURL(cfg.ProxyURL)
		if err != nil || u.Scheme != "http" && u.Scheme != "https" || u.Path != "" && u.Path != "/" || u.RawQuery != "" || u.Fragment != "" {
			return nil, errors.New("network: invalid HTTP(S) proxy URL")
		}
		if cfg.ChromeLike && u.Scheme == "https" {
			return nil, errors.New("network: HTTPS upstream proxy unsupported by Chrome-like transport")
		}
		validationCtx, cancel := context.WithTimeout(context.Background(), policy.Config().DialTimeout)
		_, validationErr := policy.ResolveURL(validationCtx, u.String(), TargetProxy, cfg.SessionID)
		cancel()
		if validationErr != nil {
			return nil, fmt.Errorf("validate proxy url: %w", validationErr)
		}
		transport.Proxy = http.ProxyURL(u)
		proxyURL = u
	}
	jar, err := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	if err != nil {
		return nil, fmt.Errorf("cookie jar: %w", err)
	}
	// Spec L4148: with an upstream proxy, target DNS comes from the proxy.
	validateTarget := policy.ValidateRequest
	if proxyURL != nil {
		validateTarget = policy.ValidateProxiedRequest
	}
	var roundTripper http.RoundTripper = transport
	if cfg.ChromeLike {
		chromeRT := newChromeTransport(dialContext, proxyURL, cfg.TLSInsecureSkipVerify)
		roundTripper = &schemeRouter{https: chromeRT, fallback: transport}
	}
	roundTripper = NewWebBotAuthTransport(roundTripper, cfg.WebBotAuth)
	httpClient := &HTTPClient{
		cfg:           cfg,
		mmap:          platform.ProbeMmap(),
		chromeHeaders: chromeHeaders,
		jar:           jar, robots: newRobotsCache(), proxyURL: proxyURL,
	}
	httpClient.client = &http.Client{
		Transport: roundTripper,
		Jar:       jar,
		Timeout:   cfg.Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= policy.Config().MaxRedirects {
				return fmt.Errorf("%w: redirect_limit", ErrPolicyDenied)
			}
			if len(via) > 0 && requestCanCarrySecretBody(req) && !sameOrigin(via[0].URL, req.URL) {
				return fmt.Errorf("%w: cross_origin_body_redirect", ErrPolicyDenied)
			}
			if err := validateTarget(req.Context(), req.URL.String(), req.Method, req.Header.Get("Content-Type"), policyRequestContentLength(req), TargetRedirect, SessionID(req.Context(), cfg.SessionID)); err != nil {
				return err
			}
			// Robots-governed crawls admit every hop, not only the first URL.
			if agent, governed := robotsAgent(req.Context()); governed {
				return httpClient.CheckRobots(req.Context(), req.URL, agent)
			}
			return nil
		},
	}
	return httpClient, nil
}

// validateTarget admits a target; with an upstream proxy DNS stays with the
// proxy (spec L4148), otherwise the policy resolves and checks every address.
func (c *HTTPClient) validateTarget(ctx context.Context, rawURL, method, contentType string, contentLength int64, kind TargetKind, sessionID string) error {
	if c.proxyURL != nil {
		return c.cfg.Policy.ValidateProxiedRequest(ctx, rawURL, method, contentType, contentLength, kind, sessionID)
	}
	return c.cfg.Policy.ValidateRequest(ctx, rawURL, method, contentType, contentLength, kind, sessionID)
}

// ProxyURL returns a copy of the validated upstream proxy, or nil. Every other
// session transport (for example renderless WebSockets) must route through it.
func (c *HTTPClient) ProxyURL() *url.URL {
	if c == nil || c.proxyURL == nil {
		return nil
	}
	proxy := *c.proxyURL
	if c.proxyURL.User != nil {
		user := *c.proxyURL.User
		proxy.User = &user
	}
	return &proxy
}

func requestCanCarrySecretBody(req *http.Request) bool {
	return req != nil && (req.Body != nil && req.Body != http.NoBody || req.ContentLength != 0)
}

func sameOrigin(first, next *url.URL) bool {
	if first == nil || next == nil {
		return false
	}
	return strings.EqualFold(first.Scheme, next.Scheme) && strings.EqualFold(first.Host, next.Host)
}

// Close releases resources. It is safe to call multiple times.
func (c *HTTPClient) Close() error {
	if c == nil || c.client == nil {
		return nil
	}
	c.sessionMu.Lock()
	c.closed = true
	clear(c.sessionJars)
	c.sessionMu.Unlock()
	if c.robots != nil {
		c.robots.close()
	}
	if t, ok := c.client.Transport.(interface{ CloseIdleConnections() }); ok {
		t.CloseIdleConnections()
	}
	return nil
}

// CookieJar returns the underlying cookie jar.
func (c *HTTPClient) CookieJar() http.CookieJar {
	if c == nil {
		return nil
	}
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	if c.closed {
		return nil
	}
	return c.jar
}

// Request describes a single HTTP request.
type Request struct {
	Method       string
	URL          string
	Body         io.Reader
	Headers      http.Header
	MaxBodyBytes int64
}

// Response is the result of executing a Request.
type Response struct {
	StatusCode   int
	Headers      http.Header
	Body         []byte
	FinalURL     string
	InputMemory  platform.ProbeResult
	release      func() error
	operationCtx context.Context
}

// ErrBodyTooLarge is returned when the response body exceeds
// MaxBodyBytes.
var ErrBodyTooLarge = errors.New("response body exceeds configured limit")

// Do executes a Request and returns the Response. The body is fully read
// up to MaxBodyBytes before return.
func (c *HTTPClient) Do(ctx context.Context, r Request) (*Response, error) {
	return c.DoTarget(ctx, r, TargetNavigation)
}

// DoTarget executes a request under the policy identity of kind.
func (c *HTTPClient) DoTarget(ctx context.Context, r Request, kind TargetKind) (result *Response, resultErr error) {
	return c.doTarget(ctx, r, kind, false)
}

func (c *HTTPClient) doTarget(ctx context.Context, r Request, kind TargetKind, scoped bool) (result *Response, resultErr error) {
	if c == nil || c.client == nil || c.cfg.Policy == nil {
		return nil, errors.New("network: initialized HTTP client required")
	}
	if r.MaxBodyBytes < 0 {
		return nil, errors.New("network: invalid request response limit")
	}
	requestCtx, finish, err := c.beginRequest(ctx)
	if err != nil {
		return nil, err
	}
	var responseBytes int64
	var releaseInput func() error
	defer func() {
		if finishErr := finish(responseBytes); finishErr != nil {
			result = nil
			resultErr = errors.Join(resultErr, finishErr)
		}
		if scoped && resultErr == nil && result != nil {
			result.release = releaseInput
			return
		}
		if releaseInput != nil {
			resultErr = errors.Join(resultErr, releaseInput())
		}
		if resultErr != nil {
			result = nil
		}
	}()
	if r.URL == "" {
		return nil, errors.New("request URL is empty")
	}
	method := strings.ToUpper(strings.TrimSpace(r.Method))
	if method == "" {
		method = http.MethodGet
	}
	req, err := http.NewRequestWithContext(requestCtx, method, r.URL, r.Body)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if err := c.applyRequestHeaders(req, r.Headers); err != nil {
		return nil, err
	}
	// Preserve net/http's default gzip negotiation, but make the scoped
	// decoder explicit so its workspace cannot bypass the supplied owner.
	if scoped && c.cfg.MemoryAdmission != nil && req.Header.Get("Accept-Encoding") == "" && req.Header.Get("Range") == "" && method != http.MethodHead {
		req.Header.Set("Accept-Encoding", "gzip")
	}
	if validateErr := c.validateTarget(requestCtx, req.URL.String(), req.Method, req.Header.Get("Content-Type"), policyRequestContentLength(req), kind, SessionID(requestCtx, c.cfg.SessionID)); validateErr != nil {
		return nil, fmt.Errorf("validate request: %w", validateErr)
	}

	jar, err := c.CookieJarForSession(SessionID(requestCtx, c.cfg.SessionID))
	if err != nil {
		return nil, err
	}
	client := *c.client
	client.Jar = jar
	eligible := (method == http.MethodGet || method == http.MethodHead) && (req.Body == nil || req.Body == http.NoBody)
	req = req.WithContext(context.WithValue(req.Context(), fastOpenRequestKey{}, eligible))
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request %s %s: %w", method, r.URL, err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			result = nil
			resultErr = errors.Join(resultErr, fmt.Errorf("close response body: %w", closeErr))
		}
	}()

	limit := r.MaxBodyBytes
	if limit == 0 {
		limit = c.cfg.MaxBodyBytes
	}
	policyLimit := c.cfg.Policy.Config().MaxResponseBodyBytes
	if kind == TargetDownload {
		policyLimit = c.cfg.Policy.Config().MaxDownloadBytes
	}
	if limit <= 0 || policyLimit < limit {
		limit = policyLimit
	}
	countedBody := &countingReader{reader: resp.Body}
	inputSize := resp.ContentLength
	if resp.Body == http.NoBody {
		inputSize = 0
	}
	body, releaseInput, inputMemory, err := c.readScopedBody(requestCtx, countedBody, inputSize, limit, scoped)
	responseBytes = countedBody.bytes
	if err != nil {
		return nil, err
	}
	headers := resp.Header.Clone()
	if encoding := headers.Get("Content-Encoding"); encoding != "" && resp.Body != http.NoBody {
		if scoped {
			var releaseDecoded func() error
			body, releaseDecoded, err = c.decodeScopedResponse(requestCtx, body, encoding, limit)
			releaseInput = releaseResponseInputs(releaseDecoded, releaseInput)
		} else {
			body, err = decodeResponseBody(body, encoding, limit)
		}
		if int64(len(body)) > responseBytes {
			responseBytes = int64(len(body))
		}
		if err != nil {
			return nil, fmt.Errorf("decode response body: %w", err)
		}
		headers.Del("Content-Encoding")
		headers.Del("Content-Length")
	}

	finalURL := r.URL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}

	return &Response{
		StatusCode:   resp.StatusCode,
		Headers:      headers,
		Body:         body,
		FinalURL:     finalURL,
		InputMemory:  inputMemory,
		operationCtx: ctx,
	}, nil
}

func policyRequestContentLength(request *http.Request) int64 {
	if request.Body != nil && request.Body != http.NoBody && request.ContentLength == 0 {
		return -1
	}
	return request.ContentLength
}

type countingReader struct {
	reader io.Reader
	bytes  int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.bytes += int64(n)
	return n, err
}

func readLimited(r io.Reader, limit int64) ([]byte, error) {
	if limit <= 0 {
		buf, err := io.ReadAll(r)
		if err != nil {
			return nil, fmt.Errorf("read body: %w", err)
		}
		return buf, nil
	}
	buf, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if int64(len(buf)) > limit {
		return nil, ErrBodyTooLarge
	}
	return buf, nil
}
