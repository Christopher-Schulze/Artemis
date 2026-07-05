package scraper

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Christopher-Schulze/Artemis/network"
	"golang.org/x/net/html/charset"
)

// static_detail.go (spec L4400: Static fetcher full detail).
//
// Response encoding from curl_cffi response.encoding fallback utf-8.
// Cookie persistence via session/client. The static fetcher must:
//  1. Detect response encoding from Content-Type charset, then <meta> tags,
//     then BOM, then fallback to UTF-8 (matching curl_cffi behavior).
//  2. Persist cookies across requests via a cookie jar.
//  3. Decode the body to UTF-8 text for downstream parsing.

// CookieJar is a simple thread-safe cookie store for the static fetcher
// (spec L4400: cookie persistence via session/client).
type CookieJar struct {
	mu      sync.Mutex
	cookies map[string][]*http.Cookie // keyed by host
}

// NewCookieJar creates an empty cookie jar.
func NewCookieJar() *CookieJar {
	return &CookieJar{cookies: make(map[string][]*http.Cookie)}
}

// SetCookies stores cookies for a host.
func (j *CookieJar) SetCookies(host string, cookies []*http.Cookie) {
	if j == nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	j.cookies[host] = append(j.cookies[host], cookies...)
}

// Cookies returns cookies for a host.
func (j *CookieJar) Cookies(host string) []*http.Cookie {
	if j == nil {
		return nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]*http.Cookie, len(j.cookies[host]))
	copy(out, j.cookies[host])
	return out
}

// CookieCount returns the total number of cookies for a host.
func (j *CookieJar) CookieCount(host string) int {
	if j == nil {
		return 0
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	return len(j.cookies[host])
}

// Clear removes all cookies.
func (j *CookieJar) Clear() {
	if j == nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	j.cookies = make(map[string][]*http.Cookie)
}

// AllCookies returns a copy of all cookies across all hosts.
func (j *CookieJar) AllCookies() map[string][]*http.Cookie {
	if j == nil {
		return nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make(map[string][]*http.Cookie, len(j.cookies))
	for host, cookies := range j.cookies {
		cp := make([]*http.Cookie, len(cookies))
		copy(cp, cookies)
		out[host] = cp
	}
	return out
}

// ToHeader converts stored cookies for a host into a Cookie header value.
func (j *CookieJar) ToHeader(host string) string {
	cookies := j.Cookies(host)
	if len(cookies) == 0 {
		return ""
	}
	var parts []string
	for _, c := range cookies {
		parts = append(parts, c.Name+"="+c.Value)
	}
	return strings.Join(parts, "; ")
}

// StaticDetailFetcher extends StaticFetcher with response encoding detection
// (charset fallback to UTF-8) and cookie persistence (spec L4400).
type StaticDetailFetcher struct {
	client      *network.HTTPClient
	jar         *CookieJar
	rateLimiter <-chan time.Time
	maxRetries  int
	retryDelay  time.Duration
}

// NewStaticDetailFetcher creates a detail fetcher with a cookie jar.
func NewStaticDetailFetcher(client *network.HTTPClient, jar *CookieJar, rps float64, maxRetries int) *StaticDetailFetcher {
	var ticker <-chan time.Time
	if rps > 0 {
		ticker = time.NewTicker(time.Duration(float64(time.Second) / rps)).C
	}
	if jar == nil {
		jar = NewCookieJar()
	}
	return &StaticDetailFetcher{
		client:      client,
		jar:         jar,
		rateLimiter: ticker,
		maxRetries:  maxRetries,
		retryDelay:  time.Second,
	}
}

// StaticDetailResult is the outcome of a detail fetch with decoded text.
type StaticDetailResult struct {
	StatusCode  int
	Headers     http.Header
	Body        []byte // raw bytes
	Text        string // decoded UTF-8 text
	FinalURL    string
	ContentType string
	Charset     string // detected charset
	Encoding    string // encoding used for decoding (may differ from charset if fallback)
}

// FetchDetail performs a static fetch with encoding detection and cookie
// persistence (spec L4400).
func (f *StaticDetailFetcher) FetchDetail(ctx context.Context, rawURL string, opts StaticFetchOpts) (*StaticDetailResult, error) {
	if f.rateLimiter != nil {
		select {
		case <-f.rateLimiter:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	method := opts.Method
	if method == "" {
		method = http.MethodGet
	}

	// Inject stored cookies into the request headers (spec L4400: cookie
	// persistence via session/client).
	host := extractHost(rawURL)
	if f.jar != nil && opts.Headers == nil {
		opts.Headers = http.Header{}
	}
	if f.jar != nil {
		cookieHeader := f.jar.ToHeader(host)
		if cookieHeader != "" {
			existing := opts.Headers.Get("Cookie")
			if existing != "" {
				opts.Headers.Set("Cookie", existing+"; "+cookieHeader)
			} else {
				opts.Headers.Set("Cookie", cookieHeader)
			}
		}
	}

	var body io.Reader
	if len(opts.Body) > 0 {
		body = strings.NewReader(string(opts.Body))
	}

	var lastErr error
	for attempt := 0; attempt <= f.maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(f.retryDelay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		req := network.Request{
			Method:       method,
			URL:          rawURL,
			Body:         body,
			Headers:      opts.Headers,
			MaxBodyBytes: opts.MaxBodyBytes,
		}
		resp, err := f.client.Do(ctx, req)
		if err != nil {
			lastErr = err
			continue
		}

		ct := resp.Headers.Get("Content-Type")
		charset := extractCharset(ct)

		// If no charset in Content-Type, try <meta> charset detection (spec L4400:
		// response encoding from curl_cffi response.encoding fallback utf-8).
		encoding := charset
		if encoding == "" {
			encoding = detectMetaCharset(resp.Body)
		}
		if encoding == "" {
			encoding = detectBOM(resp.Body)
		}
		if encoding == "" {
			encoding = "utf-8"
		}

		// Decode body to UTF-8 text.
		text, decodeErr := decodeBody(resp.Body, encoding)
		if decodeErr != nil {
			// Fallback to UTF-8 if the detected encoding fails to decode.
			text = string(resp.Body)
			encoding = "utf-8"
		}

		// Persist response cookies (spec L4400: cookie persistence).
		if f.jar != nil {
			respCookies := parseSetCookie(resp.Headers)
			if len(respCookies) > 0 {
				f.jar.SetCookies(host, respCookies)
			}
		}

		return &StaticDetailResult{
			StatusCode:  resp.StatusCode,
			Headers:     resp.Headers,
			Body:        resp.Body,
			Text:        text,
			FinalURL:    resp.FinalURL,
			ContentType: ct,
			Charset:     charset,
			Encoding:    encoding,
		}, nil
	}
	return nil, fmt.Errorf("static detail fetch failed after %d retries: %w", f.maxRetries, lastErr)
}

// detectMetaCharset scans the first 1024 bytes of an HTML body for a
// <meta charset="..."> or <meta http-equiv="Content-Type" content="...; charset=...">
// declaration (spec L4400: encoding detection fallback).
func detectMetaCharset(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	scan := body
	if len(scan) > 1024 {
		scan = scan[:1024]
	}
	lower := strings.ToLower(string(scan))

	// <meta charset="utf-8">
	idx := strings.Index(lower, `<meta charset="`)
	if idx != -1 {
		start := idx + len(`<meta charset="`)
		end := strings.IndexByte(lower[start:], '"')
		if end != -1 {
			return lower[start : start+end]
		}
	}

	// <meta http-equiv="content-type" content="text/html; charset=utf-8">
	idx = strings.Index(lower, `http-equiv="content-type"`)
	if idx != -1 {
		contentIdx := strings.Index(lower[idx:], `content="`)
		if contentIdx != -1 {
			start := idx + contentIdx + len(`content="`)
			end := strings.IndexByte(lower[start:], '"')
			if end != -1 {
				content := lower[start : start+end]
				cs := extractCharset(content)
				if cs != "" {
					return cs
				}
			}
		}
	}

	return ""
}

// detectBOM checks for a Byte Order Mark that indicates the encoding
// (spec L4400: encoding detection fallback).
func detectBOM(body []byte) string {
	if len(body) >= 3 && body[0] == 0xEF && body[1] == 0xBB && body[2] == 0xBF {
		return "utf-8"
	}
	if len(body) >= 2 && body[0] == 0xFF && body[1] == 0xFE {
		return "utf-16le"
	}
	if len(body) >= 2 && body[0] == 0xFE && body[1] == 0xFF {
		return "utf-16be"
	}
	return ""
}

// decodeBody decodes a byte slice from the given encoding to a UTF-8 string.
// Uses golang.org/x/net/html/charset for encoding name resolution.
func decodeBody(body []byte, encoding string) (string, error) {
	reader, err := charset.NewReaderLabel(encoding, strings.NewReader(string(body)))
	if err != nil {
		return "", fmt.Errorf("decode: unknown encoding %q: %w", encoding, err)
	}
	decoded, err := io.ReadAll(reader)
	if err != nil {
		return "", fmt.Errorf("decode: read: %w", err)
	}
	return string(decoded), nil
}

// extractHost extracts the host from a URL string.
func extractHost(rawURL string) string {
	// Strip scheme.
	if idx := strings.Index(rawURL, "://"); idx != -1 {
		rawURL = rawURL[idx+3:]
	}
	// Strip path.
	if idx := strings.IndexByte(rawURL, '/'); idx != -1 {
		rawURL = rawURL[:idx]
	}
	// Strip port.
	if idx := strings.IndexByte(rawURL, ':'); idx != -1 {
		rawURL = rawURL[:idx]
	}
	return strings.ToLower(rawURL)
}

// parseSetCookie extracts cookies from Set-Cookie response headers.
func parseSetCookie(headers http.Header) []*http.Cookie {
	if headers == nil {
		return nil
	}
	rawCookies := headers["Set-Cookie"]
	if len(rawCookies) == 0 {
		// Try canonical header key.
		rawCookies = headers.Values("Set-Cookie")
	}
	var cookies []*http.Cookie
	for _, raw := range rawCookies {
		parts := strings.SplitN(raw, ";", 2)
		if len(parts) == 0 {
			continue
		}
		nameValue := strings.SplitN(parts[0], "=", 2)
		if len(nameValue) != 2 {
			continue
		}
		cookies = append(cookies, &http.Cookie{
			Name:  strings.TrimSpace(nameValue[0]),
			Value: strings.TrimSpace(nameValue[1]),
		})
	}
	return cookies
}
