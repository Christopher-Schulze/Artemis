// Package scraper implements the Artemis static HTTP fetcher (spec ss28.12b.4).
package scraper

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Christopher-Schulze/Artemis/network"
)

// StaticFetcher performs policy-safe HTTP fetches without a browser.
type StaticFetcher struct {
	client      *network.HTTPClient
	rateLimiter <-chan time.Time
	maxRetries  int
	retryDelay  time.Duration
}

// StaticFetchOpts customizes a static fetch.
type StaticFetchOpts struct {
	Method       string
	Body         []byte
	ContentType  string
	Headers      http.Header
	MaxBodyBytes int64
}

// StaticResult is the outcome of a static fetch.
type StaticResult struct {
	StatusCode  int
	Headers     http.Header
	Body        []byte
	FinalURL    string
	ContentType string
	Charset     string
}

// NewStaticFetcher creates a fetcher with rate limiting and retry semantics.
func NewStaticFetcher(client *network.HTTPClient, rps float64, maxRetries int) *StaticFetcher {
	var ticker <-chan time.Time
	if rps > 0 {
		ticker = time.NewTicker(time.Duration(float64(time.Second) / rps)).C
	}
	return &StaticFetcher{
		client:      client,
		rateLimiter: ticker,
		maxRetries:  maxRetries,
		retryDelay:  time.Second,
	}
}

// Fetch performs a static HTTP fetch with retries and rate limiting.
func (f *StaticFetcher) Fetch(ctx context.Context, rawURL string, opts StaticFetchOpts) (*StaticResult, error) {
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

		return &StaticResult{
			StatusCode:  resp.StatusCode,
			Headers:     resp.Headers,
			Body:        resp.Body,
			FinalURL:    resp.FinalURL,
			ContentType: ct,
			Charset:     charset,
		}, nil
	}
	return nil, fmt.Errorf("static fetch failed after %d retries: %w", f.maxRetries, lastErr)
}

// ShouldRetry determines if a response warrants a retry.
func ShouldRetry(status int) bool {
	return status == 429 || status == 503 || status == 502 || status == 504
}

// ContentTypeCategory groups common content types.
type ContentTypeCategory string

const (
	CategoryHTML    ContentTypeCategory = "html"
	CategoryJSON    ContentTypeCategory = "json"
	CategoryXML     ContentTypeCategory = "xml"
	CategoryText    ContentTypeCategory = "text"
	CategoryBinary  ContentTypeCategory = "binary"
	CategoryUnknown ContentTypeCategory = "unknown"
)

// ClassifyContentType categorizes a Content-Type header.
func ClassifyContentType(ct string) ContentTypeCategory {
	lower := strings.ToLower(ct)
	switch {
	case strings.Contains(lower, "html"):
		return CategoryHTML
	case strings.Contains(lower, "json"):
		return CategoryJSON
	case strings.Contains(lower, "xml"):
		return CategoryXML
	case strings.Contains(lower, "text"):
		return CategoryText
	default:
		return CategoryBinary
	}
}

func extractCharset(ct string) string {
	idx := strings.Index(ct, "charset=")
	if idx == -1 {
		return ""
	}
	cs := strings.TrimSpace(ct[idx+len("charset="):])
	cs = strings.Trim(cs, `"'`)
	return cs
}
