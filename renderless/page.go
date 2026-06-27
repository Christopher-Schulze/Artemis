package renderless

import (
	"fmt"
	"net/http"
	"time"
)

// page.go (spec L4022: renderless/page.go - shared Page extraction
// surface).
//
// In-process no-render JS browser path: shared Page extraction
// surface. The Page represents a fetched page with DOM access,
// text extraction, and structured data extraction.
//
// Ref: research/artemis/engine/page.go:1-65

// Page represents a fetched page in the renderless engine
// (spec L4022: shared Page extraction surface).
type Page struct {
	URL        string
	StatusCode int
	Headers    http.Header
	RawBody    []byte
	FetchedAt  time.Time
	Engine     *Engine
}

// NewPage creates a new Page
// (spec L4022: shared Page extraction surface).
func NewPage(url string, statusCode int, body []byte) *Page {
	return &Page{
		URL:        url,
		StatusCode: statusCode,
		Headers:    make(http.Header),
		RawBody:    body,
		FetchedAt:  time.Now(),
	}
}

// ContentLength returns the content length in bytes
// (spec L4022: shared Page extraction surface).
func (p *Page) ContentLength() int {
	return len(p.RawBody)
}

// IsHTML reports whether the page content is HTML
// (spec L4022: shared Page extraction surface).
func (p *Page) IsHTML() bool {
	ct := p.Headers.Get("Content-Type")
	return contains(ct, "text/html")
}

// IsJSON reports whether the page content is JSON
// (spec L4022: shared Page extraction surface).
func (p *Page) IsJSON() bool {
	ct := p.Headers.Get("Content-Type")
	return contains(ct, "application/json")
}

// IsSuccess reports whether the status code is 2xx
// (spec L4022: shared Page extraction surface).
func (p *Page) IsSuccess() bool {
	return p.StatusCode >= 200 && p.StatusCode < 300
}

// IsRedirect reports whether the status code is 3xx
// (spec L4022: shared Page extraction surface).
func (p *Page) IsRedirect() bool {
	return p.StatusCode >= 300 && p.StatusCode < 400
}

// IsError reports whether the status code is 4xx or 5xx
// (spec L4022: shared Page extraction surface).
func (p *Page) IsError() bool {
	return p.StatusCode >= 400
}

// SetHeader sets a header on the page
// (spec L4022: shared Page extraction surface).
func (p *Page) SetHeader(key, value string) {
	if p.Headers == nil {
		p.Headers = make(http.Header)
	}
	p.Headers.Set(key, value)
}

// GetHeader retrieves a header from the page
// (spec L4022: shared Page extraction surface).
func (p *Page) GetHeader(key string) string {
	if p.Headers == nil {
		return ""
	}
	return p.Headers.Get(key)
}

// Age returns the age of the page since fetch
// (spec L4022: shared Page extraction surface).
func (p *Page) Age() time.Duration {
	return time.Since(p.FetchedAt)
}

// String returns a diagnostic summary.
func (p *Page) String() string {
	return fmt.Sprintf("Page{url:%s status:%d bytes:%d}", p.URL, p.StatusCode, p.ContentLength())
}

// contains is a case-insensitive substring check
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		(len(s) > len(substr) && (indexOf(s, substr) >= 0)))
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if equalFold(s[i:i+len(substr)], substr) {
			return i
		}
	}
	return -1
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 32
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 32
		}
		if ca != cb {
			return false
		}
	}
	return true
}
