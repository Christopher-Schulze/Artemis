// Package network implements the HTTP client and related network
// primitives used by the engine. Phase 1 covers a stdlib-backed client
// with proxy, timeout, and body-limit controls. Cookies, IpFilter,
// robots.txt, and network interception land in their respective TASKs.
package network

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"runtime"
	"strings"
	"time"
)

// HTTPClientConfig configures an HTTPClient.
type HTTPClientConfig struct {
	// UserAgent sets the User-Agent header on every outgoing request.
	UserAgent string
	// ChromeLike fills in a coherent desktop-Chrome header set
	// (Accept, Accept-Language, Sec-CH-UA*, Sec-Fetch-*, Upgrade-Insecure-Requests)
	// for any header the caller did not set. The generated set matches
	// the User-Agent's Chrome version and ChromeOS — without it a
	// renderless fetch is trivially distinguishable from a browser.
	ChromeLike bool
	// ChromeOS selects the header-identity OS ("windows"|"macos"|"linux");
	// empty resolves to the host OS.
	ChromeOS string
	// ProxyURL routes outbound requests through the given proxy. When
	// empty the client honors HTTP_PROXY / HTTPS_PROXY environment
	// variables.
	ProxyURL string
	// Timeout is the per-request timeout (including dial, TLS, and body
	// read). A zero value disables the deadline.
	Timeout time.Duration
	// MaxBodyBytes caps the response body. Zero means unlimited.
	MaxBodyBytes int64
	// Policy is the mandatory outbound network security boundary. Nil uses
	// the deny-private default policy.
	Policy *Policy
	// SessionID correlates redacted policy decisions without exposing URLs.
	SessionID string
	// RequestLifecycle optionally enforces session-wide request admission,
	// cancellation, concurrency, and response-byte accounting.
	RequestLifecycle RequestLifecycle
}

// HTTPClient performs HTTP requests on behalf of the engine.
type HTTPClient struct {
	cfg           HTTPClientConfig
	client        *http.Client
	jar           http.CookieJar
	robots        *robotsCache
	chromeHeaders http.Header
}

// NewHTTPClient builds an HTTPClient.
func NewHTTPClient(cfg HTTPClientConfig) (*HTTPClient, error) {
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
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           policy.DialContextFor(cfg.SessionID),
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
	if cfg.ProxyURL != "" {
		u, err := url.Parse(cfg.ProxyURL)
		if err != nil {
			return nil, fmt.Errorf("parse proxy url %q: %w", cfg.ProxyURL, err)
		}
		validationCtx, cancel := context.WithTimeout(context.Background(), policy.Config().DialTimeout)
		_, validationErr := policy.ResolveURL(validationCtx, u.String(), TargetProxy, cfg.SessionID)
		cancel()
		if validationErr != nil {
			return nil, fmt.Errorf("validate proxy url: %w", validationErr)
		}
		transport.Proxy = http.ProxyURL(u)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("cookie jar: %w", err)
	}
	return &HTTPClient{
		cfg:           cfg,
		chromeHeaders: chromeHeaders,
		client: &http.Client{
			Transport: transport,
			Jar:       jar,
			Timeout:   cfg.Timeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= policy.Config().MaxRedirects {
					return fmt.Errorf("%w: redirect_limit", ErrPolicyDenied)
				}
				if len(via) > 0 && requestCanCarrySecretBody(req) && !sameOrigin(via[0].URL, req.URL) {
					return fmt.Errorf("%w: cross_origin_body_redirect", ErrPolicyDenied)
				}
				return policy.ValidateRequest(req.Context(), req.URL.String(), req.Method, req.Header.Get("Content-Type"), policyRequestContentLength(req), TargetRedirect, SessionID(req.Context(), cfg.SessionID))
			},
		},
		jar: jar, robots: newRobotsCache(),
	}, nil
}

func requestCanCarrySecretBody(req *http.Request) bool {
	return req != nil && req.Method != http.MethodGet && req.Method != http.MethodHead
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
	if t, ok := c.client.Transport.(*http.Transport); ok {
		t.CloseIdleConnections()
	}
	return nil
}

// CookieJar returns the underlying cookie jar.
func (c *HTTPClient) CookieJar() http.CookieJar {
	if c == nil {
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
	StatusCode int
	Headers    http.Header
	Body       []byte
	FinalURL   string
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
	if c == nil || c.client == nil || c.cfg.Policy == nil {
		return nil, errors.New("network: initialized HTTP client required")
	}
	requestCtx, finish, err := c.beginRequest(ctx)
	if err != nil {
		return nil, err
	}
	var responseBytes int64
	defer func() {
		if finishErr := finish(responseBytes); finishErr != nil {
			result = nil
			resultErr = errors.Join(resultErr, finishErr)
		}
	}()
	if r.URL == "" {
		return nil, errors.New("request URL is empty")
	}
	method := r.Method
	if method == "" {
		method = http.MethodGet
	}
	req, err := http.NewRequestWithContext(requestCtx, method, r.URL, r.Body)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if c.cfg.UserAgent != "" && req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", c.cfg.UserAgent)
	}
	for k, vs := range c.chromeHeaders {
		if _, callerSet := r.Headers[k]; callerSet {
			continue
		}
		for _, v := range vs {
			req.Header.Set(k, v)
		}
	}
	for k, vs := range r.Headers {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	if validateErr := c.cfg.Policy.ValidateRequest(requestCtx, req.URL.String(), req.Method, req.Header.Get("Content-Type"), policyRequestContentLength(req), kind, SessionID(requestCtx, c.cfg.SessionID)); validateErr != nil {
		return nil, fmt.Errorf("validate request: %w", validateErr)
	}

	resp, err := c.client.Do(req)
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
	body, err := readLimited(countedBody, limit)
	responseBytes = countedBody.bytes
	if err != nil {
		return nil, err
	}

	finalURL := r.URL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}

	return &Response{
		StatusCode: resp.StatusCode,
		Headers:    resp.Header.Clone(),
		Body:       body,
		FinalURL:   finalURL,
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
