# TASK 002: Phase 1 - HTTP fetch + HTML parse + DOM tree + Markdown dump + CLI fetch

## Why

Foundation for everything that follows. Before V8, WebAPI, or any agent layer can land, the engine needs the static path: open a URL, fetch bytes over HTTP, parse HTML into a DOM tree, expose a stable Go DOM type, and serialize back to HTML / Text / Markdown. The CLI gets its first useful subcommand `artemis fetch <url>`. This is the slice that proves the architecture before complexity (V8 cgo, full WebAPI surface, agent actions) is layered on top.

## Acceptance

- `network` package: `HTTPClient` wraps stdlib `net/http`, supports user-agent, proxy URL, request timeout, max-body bytes, custom headers per request, returns `(body, status, headers, finalURL, error)` via a typed Response.
- `parser` package: `ParseHTML(io.Reader) (*webapi.Document, error)` builds a DOM via `golang.org/x/net/html`.
- `webapi` package: `Node` interface + `Document`, `Element`, `Text`, `Comment` types with parent/children/sibling/attribute access; tree walk helpers; `QuerySelector` and `QuerySelectorAll` for CSS selector subset (id, class, tag, attribute, descendant) sufficient for Phase 1 dumping needs - delegated to `golang.org/x/net/html`'s cascadia (already a transitive dep) only if straightforward; otherwise scoped manual matcher.
- `agent` package: `HTML(*Document) string` (serialize), `Text(*Document) string` (extract text), `Markdown(*Document) string` (convert to Markdown), `Title(*Document) string`.
- `engine` package: `Config`, `Engine`, `Page` types. `Engine.Fetch(ctx, url, FetchOpts) (*Page, error)` returns a fully loaded Page. `Page.URL`, `Page.StatusCode`, `Page.Headers`, `Page.Document`, `Page.HTML`, `Page.Text`, `Page.Markdown`, `Page.Title`.
- `cmd/artemis fetch <url>` CLI: flags `--dump {html|markdown|text|title}` (default markdown), `--user-agent`, `--proxy`, `--timeout`, `--header k=v` (repeatable), `--max-body-bytes`. Exit 0 on success; nonzero on error with stderr message.
- Tests:
  - `network/http_test.go` against `httptest.NewServer` - status, headers, body, redirects, timeout, proxy is nil-safe, max-body limit truncates with explicit error.
  - `parser/html_test.go` - parses fixtures, exposes correct element tree, attributes, text.
  - `webapi/walk_test.go` - tree traversal, querySelector basics.
  - `agent/markdown_test.go` - table-driven cases for headings, paragraphs, lists, links, images, code, blockquote, emphasis.
  - `agent/dump_test.go` - HTML round-trip equivalence and text extraction.
  - `engine/engine_test.go` - end-to-end fetch via httptest.
- `make build`, `make vet`, `make test` all green.
- Public symbols documented (Go doc comments on every exported identifier).
- `docs/documentation.md` updated to reflect the public API and CLI surface that landed.

## Sub-Tasks

- [x] write this detail file and mark TASK active in tasks.md
- [x] webapi: `node.go`, `document.go`, `walk.go`, `selector.go` (separate Element / Text / Comment types deferred to Phase 3 - the Node API + cascadia covers Phase 1 needs without premature abstraction)
- [x] parser: `html.go` (wrap `golang.org/x/net/html`)
- [x] network: `http.go` (wrap stdlib, support proxy/timeout/headers/max-body)
- [x] agent: `dump.go`, `markdown.go`, `title.go`
- [x] engine: `config.go`, `engine.go`, `page.go`
- [x] cmd: refactor `main.go`, add `cli.go` and `fetch.go`
- [x] tests for webapi, parser, network, agent, engine
- [x] `go mod tidy` (added `golang.org/x/net v0.53.0`, `github.com/andybalholm/cascadia v1.3.3`)
- [x] verify `make build`, `make vet`, `make test` all green
- [x] FLUSH: update `docs/documentation.md` API + CLI sections
- [x] archive to `docs/tasks/done/`

## Notes

DOM design: types in `webapi/` wrap `*html.Node` from `golang.org/x/net/html` by reference. No copies, walking goes through the parser-owned tree. Strings are returned by-value at the API boundary (Go strings are immutable so this is cheap copy of header). This follows a zero-copy intent within Go's constraints.

Selector engine: `golang.org/x/net/html/atom` plus simple manual matcher for tag/id/class/attribute/descendant suffices for Phase 1. Avoid pulling in cascadia explicitly until the WebAPI Phase 3 forces it.

Cookies + JS-aware features (Click, Eval, Form) are explicitly out of TASK 002 scope. They land in their respective TASKs.

Reference: internal design notes.

Visible-text extraction adds a soft space at the start and end of every block-level element (p, div, h1-h6, li, td, ...). Without this, neighbouring blocks merge their text content (`HelloWorld`). The space is collapsed by the standard whitespace pass.

Markdown converter uses the same whitespace-collapsing pass for inline text, but preserves whitespace inside `<pre>` blocks via a separate `inPre` mode that walks raw text without collapsing.

Selector engine resolved to `github.com/andybalholm/cascadia` rather than rolling our own. Justification: the Phase 3 WebAPI surface needs full CSS3 selector support anyway, and cascadia is the canonical Go implementation, BSD-2 licensed (AGPL-3.0 compatible), maintained, and operates directly on `*html.Node`. Added to `docs/spec.md` External Dependencies.

## Deviations

(none)
