# TASK 006 (Phase 5a): Agent extraction

## Why

The agent embedding Artemis cares less about how the page rendered and more about what's on it: links to follow, structured data for product/article semantics, and a clean hierarchical view of the content for LLM consumption. This TASK delivers the extractors that turn a fully-rendered `*webapi.Document` into agent-shaped output. No JS dependency: extractors operate on the static DOM, which is identical to the post-script DOM thanks to TASK 004.

## Acceptance

- `agent/links.go`: `Links(doc) []Link` returns every `<a href>` with absolute URL (resolved against `doc.URL()`), inner text, `rel`, `title`. Skip empty hrefs and `javascript:`/`mailto:`/`tel:` schemes by default but expose them in `LinksAll`.
- `agent/structured.go`: `StructuredData(doc) StructuredData` returns:
  - `JSONLD []map[string]any` parsed from every `<script type="application/ld+json">`
  - `OpenGraph map[string]string` from `<meta property="og:*">`
  - `Twitter map[string]string` from `<meta name="twitter:*">`
  - `Meta map[string]string` from arbitrary `<meta name|property|http-equiv>`
- `agent/semantic.go`: `SemanticTree(doc) *SemanticNode` returns a hierarchical tree where each node has `{Kind, Text, Level, Children, URL}`. Kinds: `heading`, `paragraph`, `list`, `listItem`, `link`, `quote`, `code`, `image`, `section`. Skipped: nav, footer, aside, script, style. Headings establish hierarchy: h1 makes a section, h2 nests under nearest h1, etc. Output is a single root `Section` with `Level=0`.
- `engine.Page` gains: `Links()`, `LinksAll()`, `StructuredData()`, `SemanticTree()`.
- CLI `artemis fetch --dump {links|structured|semantic}` prints the extracted output: links as TSV, structured as JSON, semantic as indented Markdown-ish tree.
- `make build`, `make vet`, `make test`, `go test -race ./js ./engine` all green.

- [x] mark active, write detail
- [x] `agent/links.go` (`Link`, `Links`, `LinksAll`, base-URL resolution, scheme + fragment filter) + tests
- [x] `agent/structured.go` (`StructuredData`, JSON-LD object + array, OpenGraph, Twitter, generic meta, malformed-tolerant) + tests
- [x] `agent/semantic.go` (`SemanticNode`, `SemanticKind`, `Semantic`, `SemanticString`; nests headings, skips nav/footer/aside/script/style; lists, blockquotes, pre/code, images) + tests
- [x] `engine/page.go` exposes `Links`, `LinksAll`, `StructuredData`, `SemanticTree`
- [x] `cmd/artemis fetch --dump {links|structured|semantic}` (TSV / JSON / Markdown-ish)
- [x] CLI smoke: product page with og:* + JSON-LD + nav/footer correctly classified
- [x] all green: build, vet, test, race
- [x] FLUSH documentation.md
- [x] archive

## Notes

JSON-LD parsing is via `encoding/json` directly. Schema validation is out of scope; the agent decides what to do with the parsed map.

URL resolution uses `net/url.URL.ResolveReference` against the document URL. If the document URL is empty (loaded from a bare reader) the link href stays as-is.

SemanticTree intentionally drops nav/footer/aside chrome. Configurable filter list arrives if a user reports a regression where an article uses unconventional structure.

Forms and Actions live in a sibling TASK 006b - they need different plumbing (mutation, event firing, optional follow-up fetch) and would crowd this scope.

Reference: internal design notes.

## Deviations

(none yet)
