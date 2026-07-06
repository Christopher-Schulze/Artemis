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
	"time"
)

// HTTPClientConfig configures an HTTPClient.
type HTTPClientConfig struct {
	// UserAgent sets the User-Agent header on every outgoing request.
	UserAgent string
	// ProxyURL routes outbound requests through the given proxy. When
	// empty the client honors HTTP_PROXY / HTTPS_PROXY environment
	// variables.
	ProxyURL string
	// Timeout is the per-request timeout (including dial, TLS, and body
	// read). A zero value disables the deadline.
	Timeout time.Duration
	// MaxBodyBytes caps the response body. Zero means unlimited.
	MaxBodyBytes int64
}

// HTTPClient performs HTTP requests on behalf of the engine.
type HTTPClient struct {
	cfg    HTTPClientConfig
	client *http.Client
	jar    http.CookieJar
	robots *robotsCache
}

// NewHTTPClient builds an HTTPClient.
func NewHTTPClient(cfg HTTPClientConfig) (*HTTPClient, error) {
	// Pool tuned for crawler-style workloads: lots of subresources from
	// a small set of hosts, parallel fetches via the async-runtime.
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
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
		transport.Proxy = http.ProxyURL(u)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("cookie jar: %w", err)
	}
	return &HTTPClient{
		cfg: cfg,
		client: &http.Client{
			Transport: transport,
			Jar:       jar,
			Timeout:   cfg.Timeout,
		},
		jar: jar,
	}, nil
}

// Close releases resources. It is safe to call multiple times.
func (c *HTTPClient) Close() error {
	if t, ok := c.client.Transport.(*http.Transport); ok {
		t.CloseIdleConnections()
	}
	return nil
}

// CookieJar returns the underlying cookie jar.
func (c *HTTPClient) CookieJar() http.CookieJar { return c.jar }

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
	if r.URL == "" {
		return nil, errors.New("request URL is empty")
	}
	method := r.Method
	if method == "" {
		method = http.MethodGet
	}
	req, err := http.NewRequestWithContext(ctx, method, r.URL, r.Body)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if c.cfg.UserAgent != "" && req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", c.cfg.UserAgent)
	}
	for k, vs := range r.Headers {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request %s %s: %w", method, r.URL, err)
	}
	defer resp.Body.Close()

	limit := r.MaxBodyBytes
	if limit == 0 {
		limit = c.cfg.MaxBodyBytes
	}
	body, err := readLimited(resp.Body, limit)
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
