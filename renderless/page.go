package renderless

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	artemisengine "github.com/Christopher-Schulze/Artemis/engine"
	"github.com/Christopher-Schulze/Artemis/js"
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
	backend    *artemisengine.Page
	closeOnce  sync.Once
	closeErr   error
}

// NewPage creates a detached page value for extraction/status helpers. It is
// not an execution owner; fetched pages are created only by Engine.Fetch.
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

func newPageFromEngine(owner *Engine, page *artemisengine.Page) *Page {
	if page == nil {
		return nil
	}
	return &Page{
		URL:        page.URL(),
		StatusCode: page.StatusCode(),
		Headers:    page.Headers().Clone(),
		RawBody:    append([]byte(nil), page.RawBody()...),
		FetchedAt:  time.Now(),
		Engine:     owner,
		backend:    page,
	}
}

// RealPage returns the production engine page owned by this facade.
func (p *Page) RealPage() *artemisengine.Page {
	if p == nil {
		return nil
	}
	return p.backend
}

// Eval executes JavaScript against the fetched page's real DOM context.
func (p *Page) Eval(ctx context.Context, expr string) (*js.Value, error) {
	if p == nil || p.backend == nil {
		return nil, errors.New("renderless: page has no production execution owner")
	}
	ctx, cancel := p.Engine.withScriptTimeout(ctx)
	defer cancel()
	return p.backend.Eval(ctx, expr)
}

// WaitIdle waits for all asynchronous fetch work owned by the page.
func (p *Page) WaitIdle(ctx context.Context) error {
	if p == nil || p.backend == nil {
		return errors.New("renderless: page has no production execution owner")
	}
	ctx, cancel := p.Engine.withScriptTimeout(ctx)
	defer cancel()
	return p.backend.WaitIdle(ctx)
}

// Close releases the production page and its JavaScript context.
func (p *Page) Close() error {
	if p == nil || p.backend == nil {
		return nil
	}
	p.closeOnce.Do(func() { p.closeErr = p.backend.Close() })
	return p.closeErr
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
