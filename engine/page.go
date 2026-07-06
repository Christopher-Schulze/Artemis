package engine

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/Christopher-Schulze/Artemis/agent"
	"github.com/Christopher-Schulze/Artemis/js"
	"github.com/Christopher-Schulze/Artemis/webapi"
)

// Page is a fetched HTML document. All accessors are read-only.
type Page struct {
	url        string
	statusCode int
	headers    http.Header
	document   *webapi.Document
	rawBody    []byte
	jsCtx      *js.Context
}

// URL returns the final URL of the page after redirects.
func (p *Page) URL() string { return p.url }

// StatusCode returns the HTTP status code of the response.
func (p *Page) StatusCode() int { return p.statusCode }

// Headers returns the response headers.
func (p *Page) Headers() http.Header { return p.headers }

// Document returns the parsed DOM document.
func (p *Page) Document() *webapi.Document { return p.document }

// RawBody returns the raw response body as fetched, before parsing.
func (p *Page) RawBody() []byte { return p.rawBody }

// HTML returns the page as serialized HTML.
func (p *Page) HTML() string { return agent.HTML(p.document) }

// Text returns the visible text of the page.
func (p *Page) Text() string { return agent.Text(p.document) }

// Markdown returns a Markdown rendering of the page body.
func (p *Page) Markdown() string { return agent.Markdown(p.document) }

// Title returns the document <title>.
func (p *Page) Title() string { return p.document.Title() }

// Links returns navigable anchors with absolute URLs.
func (p *Page) Links() []agent.Link { return agent.Links(p.document) }

// LinksAll returns every <a href>, including javascript:/mailto:/tel:/data:.
func (p *Page) LinksAll() []agent.Link { return agent.LinksAll(p.document) }

// StructuredData extracts JSON-LD scripts, OpenGraph, Twitter, and meta tags.
func (p *Page) StructuredData() agent.StructuredData { return agent.Structured(p.document) }

// SemanticTree returns a hierarchical agent-friendly view of the document body.
func (p *Page) SemanticTree() *agent.SemanticNode { return agent.Semantic(p.document) }

// Click dispatches a click event on n via the JS context. Listeners
// registered with addEventListener fire and may mutate the DOM.
func (p *Page) Click(ctx context.Context, n *webapi.Node) error {
	if p.jsCtx == nil {
		return errors.New("page has no JS context")
	}
	if n == nil {
		return errors.New("nil node")
	}
	id := p.jsCtx.HandleFor(n)
	if id == 0 {
		return errors.New("could not register node handle")
	}
	_, err := p.jsCtx.Eval(ctx, fmt.Sprintf("__wrap(%d).click()", id))
	return err
}

// JSContext returns the JavaScript execution context bound to this page.
// It is owned by the page and freed by Close.
func (p *Page) JSContext() *js.Context { return p.jsCtx }

// Eval evaluates a JavaScript expression in the page's JS context and
// returns the result.
func (p *Page) Eval(ctx context.Context, expr string) (*js.Value, error) {
	if p.jsCtx == nil {
		return nil, errors.New("page has no JS context")
	}
	return p.jsCtx.Eval(ctx, expr)
}

// WaitIdle blocks until any in-flight async fetches have settled and
// their .then continuations have run. Returns ctx.Err() on cancellation.
func (p *Page) WaitIdle(ctx context.Context) error {
	if p.jsCtx == nil {
		return nil
	}
	return p.jsCtx.WaitIdle(ctx)
}

// Close releases page-held resources, including the JS context.
func (p *Page) Close() error {
	if p.jsCtx != nil {
		p.jsCtx.Close()
		p.jsCtx = nil
	}
	return nil
}
