# Artemis Documentation

Single source of truth for project-level documentation. Code-level details live inline; TASK history lives in `docs/tasks/done/`; target architecture lives in `docs/spec.md`.

## Table of Contents

- [Project Overview](#project-overview)
- [License and Lineage](#license-and-lineage)
- [Repository Layout](#repository-layout)
- [Build and Run](#build-and-run)
- [Configuration](#configuration)
- [CLI](#cli)
- [Library API](#library-api)
- [JavaScript Execution](#javascript-execution)
- [Fetch API](#fetch-api)
- [Browser Globals](#browser-globals)
- [Agent Extraction](#agent-extraction)
- [Forms and Actions](#forms-and-actions)
- [Network Stack](#network-stack)
- [Additional Web Globals](#additional-web-globals)
- [Steering Server](#steering-server)
- [Telemetry](#telemetry)

## Project Overview

Artemis is a headless browser engine written in Go, designed for AI agents and web automation. It loads HTML, runs JavaScript via V8, exposes a DOM and WebAPI surface, executes user scripts, and produces structured output (DOM dump, Markdown, extracted data) for an embedding agent. It does not render: no layout engine, no compositor, no paint pipeline. It does not ship Chromium DevTools Protocol (CDP) or Model Context Protocol (MCP) endpoints; instead it offers a Go library API, a CLI, and a custom JSON-over-WebSocket steering protocol.

## License and Lineage

License: [AGPL-3.0-only](../LICENSE).

Artemis is an independent re-implementation in Go inspired by the architecture of [Lightpanda Browser](https://github.com/lightpanda-io/browser) (Zig, AGPL-3.0). The original Lightpanda source is preserved under `research/lightpanda/` as a reference. Artemis code is original; module structure, WebAPI coverage, and Page/Session/Frame design follow Lightpanda where useful.

## Repository Layout

```
artemis/
  cmd/
    artemis/           CLI entry point
    artemis-snapshot/  V8 startup snapshot baker (run via `make snapshot`)
  engine/              top-level Engine handle: Fetch, Submit, Page, Config
  js/                  V8 isolate + Context lifecycle, native bindings, JS bootstraps
    snapshot.bin       embedded V8 startup snapshot (regenerated on demand)
  webapi/              DOM (Document, Node, Walk), HTML5 element subclasses
  parser/              HTML parser shim around golang.org/x/net/html
  agent/               extraction layer: Markdown, Text, Links, StructuredData,
                       SemanticTree, Forms, ClickByText, Type
  network/             HTTP client, robots.txt, IP filter, cookie jar
  css/                 CSS parser + inline cascade engine
  serve/               WS steering server (JSON over WebSocket)
  telemetry/           OpenTelemetry hooks
  internal/            non-exported helpers
  third_party/v8go/    vendored fork of rogchap.com/v8go with SnapshotCreator,
                       NewIsolateFromSnapshot, Object.SetMany bindings
  docs/                this directory
    documentation.md   you are here
    spec.md            target architecture, module map
    tasks.md           TASK overview
    tasks/done/        archived TASK detail files
  research/lightpanda/ reference source (read-only mirror, do not edit)
  scripts/             tooling scripts (added on demand)
  LICENSE              AGPL-3.0
  Makefile             build / test / fmt / vet / snapshot / bench
  go.mod               Go module (with `replace rogchap.com/v8go => ./third_party/v8go`)
  README.md            landing page
```

## Build and Run

Requires Go 1.26+. Cgo toolchain (clang/clang++) is required for the vendored v8go fork.

```sh
make build       # produces ./artemis
make run         # go run ./cmd/artemis
make test        # all tests
make test-race   # tests with -race
make bench       # all benchmarks
make snapshot    # regenerate js/snapshot.bin (after touching any js/ bootstrap source)
make vet         # static analysis
make fmt         # gofmt -s -w
make tidy        # go mod tidy
make clean       # remove ./artemis, bin/, dist/, and Go build/test caches
```

The V8 startup snapshot (`js/snapshot.bin`) is checked into the repo and embedded via `go:embed`. Regenerate it whenever you change a snapshot-eligible JS bootstrap (anything in `js.BootstrapSources()`); the snapshot tool runs in deterministic mode (`v8.SetFlags("--predictable")`) so identical sources produce byte-identical output.

## Configuration

`engine.Config` controls runtime behavior. The zero value is valid; defaults are applied by `engine.New`.

| Field | Default | Purpose |
|---|---|---|
| `UserAgent` | `Artemis/0.0.1 (...) AppleWebKit/537.36` | sent on every outbound request |
| `ProxyURL` | empty (uses `HTTP_PROXY` / `HTTPS_PROXY`) | proxy URL |
| `Timeout` | `30s` | per-request timeout |
| `MaxBodyBytes` | `50 MiB` | response body cap; `network.ErrBodyTooLarge` on overflow |
| `ObeyRobots` | `false` | per-host robots.txt fetched + cached; disallowed URLs return `engine.ErrRobotsDisallowed` |
| `BlockPrivateIPs` | `false` | reject loopback / RFC1918 / link-local / multicast / CGNAT hosts (SSRF guard) |
| `JSContextPoolSize` | `0` (disabled) | size of the v8.Context pool. When > 0, `Page.Close` returns the underlying v8.Context to the pool and the next `Fetch(... RunScripts=true)` reuses it via JS-side `__artemis_reset(url)`. Skips ~30% of NewContext CPU cost. See [v8.Context pool](#v8context-pool) for caveats. |
| `JSContextPoolWarm` | `false` | when paired with `JSContextPoolSize > 0`, pre-builds all N v8.Contexts at engine.New time so the first Fetch hits the pool fast path immediately. |

`engine.FetchOpts` overrides per-call:

| Field | Purpose |
|---|---|
| `Headers http.Header` | request headers (merged with engine defaults) |
| `MaxBodyBytes int64` | per-call response body cap |
| `RunScripts bool` | run inline `<script>` tags after parse, plus any external scripts whose URLs are reachable |
| `RunInlineScripts bool` | run inline `<script>` tags only (no external script fetch) |
| `Console js.Console` | sink for `console.*`; defaults to discard |
| `Navigator js.NavigatorConfig` | overrides `navigator.userAgent`/`language`/`languages`/`platform` for this Page |
| `AsyncFetch bool` | route JS `fetch()` through a goroutine pool so multiple concurrent fetches run in parallel; the V8 thread drains results between scripts and during `WaitIdle` |
| `OnRequest func(*RequestInfo) (*ResponseInfo, error)` | request interception; non-nil response short-circuits the network call with a mock |

## CLI

```
artemis <command> [flags] [args]
```

| Command | Purpose |
|---|---|
| `version` | print version |
| `help` | print top-level usage |
| `fetch <url>` | fetch URL, optionally run scripts, dump html / markdown / text / title / links / structured / semantic |
| `serve` | start the JSON-over-WebSocket steering server |

`fetch` flags: `--dump {html\|markdown\|text\|title\|links\|structured\|semantic}` (default `markdown`), `--user-agent`, `--proxy`, `--timeout`, `--max-body-bytes`, `--header k=v` (repeatable), `--run-scripts`, `--eval <expr>`, `--console`. Exit 0 on success; 1 on runtime error; 2 on argument error.

`serve` flags: `--host`, `--port`, `--obey-robots`, `--block-private-ips`. See [Steering Server](#steering-server).

## Library API

```go
import (
    "context"
    "time"
    "artemis/engine"
)

eng, err := engine.New(engine.Config{
    Timeout:           10 * time.Second,
    JSContextPoolSize: 8,
    JSContextPoolWarm: true,
})
defer eng.Close()

page, err := eng.Fetch(context.Background(), "https://example.test/", engine.FetchOpts{
    RunScripts: true,
})
defer page.Close()

title := page.Title()
md    := page.Markdown()
text  := page.Text()
html  := page.HTML()

v, _ := page.Eval(context.Background(), `document.querySelector('h1').textContent`)
println(v.String())
```

`engine.Page` exposes `URL`, `StatusCode`, `Headers`, `Document`, `RawBody`, `HTML`, `Text`, `Markdown`, `Title`, `Eval`, `WaitIdle`, `Click`, and `Close`. The DOM lives in package `webapi` (`*webapi.Document`, `*webapi.Node`) with `QuerySelector` / `QuerySelectorAll` (cascadia-backed), `Tag`, `Attr`, `Children`, `Text`, and tree walking via `webapi.Walk`. `agent.HTML`, `agent.Text`, `agent.Markdown`, `agent.Title` are direct converters from a `*webapi.Document`. Form/action helpers (`agent.Forms`, `agent.FindForm`, `agent.ClickByText`, `agent.Type`, `engine.Engine.Submit`) cover form-driven workflows.

## JavaScript Execution

Each fetched `*engine.Page` owns a V8 execution context (via a locally vendored fork of `rogchap.com/v8go`, stock V8). The context is created at parse time and released by `page.Close()`. The engine's V8 isolate is shared across pages and released by `engine.Close()`.

The isolate is initialised from a precompiled V8 startup snapshot embedded as `js/snapshot.bin`. The snapshot bakes the parse + first-run state of every snapshot-eligible bootstrap (DOM bridge, AbortController, MutationObserver, URL/URLSearchParams, TextEncoder, WebSocket, MouseEvent and friends, navigator extras, getComputedStyle wrapper, window EventTarget plumbing). Each new Context deserialises this state in microseconds; only the closure-chained crypto wrappers and the per-document on*-attribute compiler are re-run per Context. Regenerate after any bootstrap source change with `go run ./cmd/artemis-snapshot/`.

The vendored v8go lives at `third_party/v8go` (replace directive in `go.mod`). The fork adds a `SnapshotCreator` C binding, `Isolate-from-snapshot` constructor, and a single `v8-snapshot.h` header patch to remove a stale `const` qualifier that mismatched the prebuilt `libv8.a` ABI.

```go
page, _ := eng.Fetch(ctx, "https://example.test/", engine.FetchOpts{
    RunInlineScripts: true, // execute inline <script> tags after parse
    Console:          js.FuncConsole(func(level, msg string) { /* ... */ }),
})
defer page.Close()

v, err := page.Eval(ctx, `document.querySelector('h1').textContent`)
println(v.String())
```

`engine.FetchOpts.RunScripts` runs every inline `<script>` tag in document order at the end of `Fetch`, plus any external `<script src="...">` whose URL is reachable through the engine's network client. `RunInlineScripts` is the inline-only variant (skips network for external scripts). `engine.FetchOpts.Console` (`js.Console` interface) captures `console.log/warn/error/info/debug`; `js.DiscardConsole`, `js.FuncConsole`, and `js.CollectConsole` are provided.

DOM bindings exposed on the JS global `document` and on element objects (live, handle-backed - JS-side mutations propagate to the Go DOM, so `page.HTML()` / `page.Markdown()` after script execution reflect the mutated tree):

`document`:

| Member | Kind |
|---|---|
| `title`, `URL` | string getters |
| `body`, `head`, `documentElement` | element getters |
| `querySelector(sel)`, `querySelectorAll(sel)` | cascadia CSS3 |
| `getElementById(id)` | tree walk |
| `getElementsByTagName(tag)`, `getElementsByClassName(cls)` | array of elements |
| `createElement(tag)`, `createTextNode(text)` | constructor methods |

Element prototype:

| Member | Kind |
|---|---|
| `tagName`, `nodeType`, `nodeName` | getter |
| `textContent` | getter + setter |
| `innerHTML` | getter + setter (re-parses) |
| `outerHTML` | getter |
| `parentNode`, `parentElement` | getter |
| `firstChild`, `lastChild`, `nextSibling`, `previousSibling` | getter |
| `childNodes` (all), `children` (elements only) | array getter |
| `setAttribute(k,v)`, `getAttribute(k)`, `removeAttribute(k)`, `hasAttribute(k)` | mutation |
| `appendChild`, `removeChild`, `insertBefore`, `cloneNode(deep)` | mutation |
| `querySelector`, `querySelectorAll` | cascadia |

`console.log/warn/error/info/debug` route to the configured `js.Console`. Standard whatwg `nodeType` values are exposed (Element=1, Text=3, Comment=8, Document=9, DocumentType=10).

**Caveat for multi-step Eval**: V8 `RunScript` evaluates each call as a top-level script in the same global. Successive evals that re-declare the same `const`/`let` identifier collide. Wrap multi-step JS in an IIFE `(() => { ... })()` or a block `{ ... }`.

Events:

| Member | Notes |
|---|---|
| `new Event(type, {bubbles, cancelable})` | constructor |
| `event.{type, target, currentTarget, bubbles, cancelable, defaultPrevented}` | properties |
| `event.preventDefault()`, `event.stopPropagation()` | methods |
| `element.addEventListener(type, fn)`, `element.removeEventListener(type, fn)` | EventTarget |
| `element.dispatchEvent(event)` returns `!event.defaultPrevented` | EventTarget |
| `element.click()` | shortcut: dispatches `Event('click', {bubbles:true})` |
| `document.addEventListener / removeEventListener / dispatchEvent` | mirrors element EventTarget on `documentElement` |

Listener storage is JS-side, so reference equality of the registered function works for removeEventListener. Bubbling honored by walking `parentNode`. Capture phase + boolean `useCapture` shorthand + `{capture: true}` option are honored; `AT_TARGET` fires both capture and bubble registrations on the target itself. `{once: true}` auto-removes after the first fire; `{passive, signal}` are accepted but `signal`-driven removal is not yet wired.

## Fetch API

`fetch(url, opts)` is exposed on the JS global. It performs the HTTP request through the engine's network client and returns a Promise carrying a Response. By default the request is synchronous on the V8 thread; set `engine.FetchOpts.AsyncFetch = true` to route through a goroutine pool for parallel concurrent fetches.

| Member | Notes |
|---|---|
| `fetch(url)` | GET, returns Promise<Response> |
| `fetch(url, {method, headers, body, signal})` | method (string), headers (plain object or `Headers`), body (string or `FormData`), signal (`AbortSignal`) |
| `response.status`, `response.ok` (200-299), `response.statusText`, `response.url` | properties |
| `response.headers` | plain object, `Content-Type: text/plain` etc. |
| `response.text()` | Promise<string> |
| `response.json()` | Promise<any> via V8 `JSON.parse` |
| transport error | rejects the Promise |
| 4xx / 5xx | resolves normally with `ok=false` (per Fetch spec) |
| `signal.aborted` (pre-abort) | rejects with `DOMException('AbortError')` immediately |
| `body` is `FormData` | auto-encoded as `application/x-www-form-urlencoded`, `Content-Type` set when absent |

V8 `kAuto` microtask policy drains promise continuations at the end of every top-level script. Within a single `Eval` body the continuations have not yet run; use a two-eval pattern (register the chain, then read the captured global) for inspection. Inline `<script>` tags work fine because each script is a separate task and microtasks drain between them.

The Fetch callback runs synchronously on the V8 thread by default and blocks it for the request duration. Set `engine.FetchOpts.AsyncFetch = true` to route through a goroutine pool so multiple `fetch()` calls run in parallel; the V8 thread drains pending results at script boundaries and inside `Page.WaitIdle`.

`fetch()` honors `AbortController`/`AbortSignal`: a pre-aborted signal rejects the Promise immediately with a DOMException-shaped `AbortError`. `FormData` passed as the body is auto-encoded as `application/x-www-form-urlencoded` and the matching `Content-Type` is set when not present.

CLI entry: `artemis fetch --eval "<expr>" <url>` prints the result; `--run-scripts` enables inline-script execution; `--console` forwards JS console.* to stderr.

## Browser Globals

| Global | Notes |
|---|---|
| `window` | identical to `globalThis` |
| `window.document` | inherited from JS Execution section |
| `window.location` | `href`, `protocol`, `host`, `hostname`, `port`, `pathname`, `search`, `hash`, `origin`. Built from the page's URL at Context creation. The `history` API mutates these in-process; setters / `assign` / `replace` / `reload` are no-op. |
| `window.navigator` | `userAgent`, `language`, `languages` (array-like with `length`), `platform`, `onLine`, `cookieEnabled`, `webdriver` (false), `doNotTrack` (null), `plugins` / `mimeTypes` (empty array-likes), `userAgentData` (NavigatorUAData reduced-UA shape), `clipboard`, `geolocation` (rejects with permission-denied), `permissions.query` (always `denied`), `serviceWorker` (NotSupportedError on register), `hardwareConcurrency` (4), `deviceMemory` (4), `maxTouchPoints` (0). Configurable per Page via `engine.FetchOpts.Navigator`. Defaults: `Mozilla/5.0 (Artemis/0.0.1) AppleWebKit/537.36`, `en-US`, `Linux x86_64`. |
| `window.localStorage`, `window.sessionStorage` | in-memory per Context. `getItem`, `setItem`, `removeItem`, `clear`, `key(i)` work. `length` is a snapshot at install (use `lengthOf()` for live length - v8go limitation). Both stores are independent. |
| `setTimeout(fn, ms)`, `clearTimeout(id)` | callbacks queue and fire at the end of every `Eval` and every inline `<script>`. Delays are not simulated: ordering follows queue order. Chained timers (a callback that schedules another) run too, up to 64 rounds. |
| `setInterval`, `clearInterval` | aliased to `setTimeout` / `clearTimeout`; the interval callback fires once, not on a wall clock. Repeated firing on real time intervals is intentionally not modelled — agent flows do not benefit from real-time scheduling. |

## Agent Extraction

Pure-Go extractors that turn a `*webapi.Document` into agent-shaped output. Work on the static DOM, identical to the post-script DOM after `RunInlineScripts`.

| Method | Returns | Notes |
|---|---|---|
| `page.Links()` | `[]agent.Link` | every `<a href>` with absolute URL; skips empty / `javascript:` / `mailto:` / `tel:` / `data:` / fragment-only |
| `page.LinksAll()` | `[]agent.Link` | unfiltered |
| `page.StructuredData()` | `agent.StructuredData` | `JSONLD` (object or array), `OpenGraph` (`og:*`), `Twitter` (`twitter:*`), `Meta` (any `name`/`property`/`http-equiv`), `Title` |
| `page.SemanticTree()` | `*agent.SemanticNode` | hierarchical view; headings nest; paragraphs/lists/quotes/code/images/links inline; nav/footer/aside/script/style/template skipped |
| `agent.SemanticString(node)` | `string` | indented Markdown-ish render of the tree |

CLI: `artemis fetch --dump {links|structured|semantic}` prints TSV / JSON / Markdown-ish.

## Forms and Actions

| Symbol | Purpose |
|---|---|
| `agent.Forms(doc) []*Form` | every `<form>` on the page |
| `agent.FindForm(doc, selector) *Form` | first form matching selector or its nearest ancestor form |
| `(*Form).Fields() []FormField` | scan inputs/textareas/selects under the form (Name, Type, Value, Checked, Options) |
| `(*Form).Set(name, value)` | write the field value (value attribute or selected option or textarea text) |
| `(*Form).Toggle(name, checked bool)` | check/uncheck a checkbox or radio |
| `(*Form).Submit() (FormSubmission, error)` | resolve action against doc URL, encode URL-encoded body, return URL/Method/ContentType/Body |
| `engine.Engine.Submit(ctx, sub, opts) (*Page, error)` | perform the submission, return the next page |
| `agent.ClickByText(doc, text) (*Node, bool)` | first matching button / anchor / input[submit] (case-insensitive) |
| `engine.Page.Click(ctx, node) error` | dispatch click in JS so listeners fire |
| `agent.Type(doc, selector, text) error` | set value attribute / textarea text content |

URL-encoded form submissions are supported; multipart/file-upload bodies are not. `agent.Type` writes the value attribute directly; React-style controlled inputs that listen on the `input` event need an explicit `page.Eval(...)` to dispatch a synthetic event after the value mutation.

## Network Stack

| Setting / hook | Effect |
|---|---|
| `engine.Config.ObeyRobots` | per-host robots.txt fetched and cached; disallowed URLs return `engine.ErrRobotsDisallowed` |
| `engine.Config.BlockPrivateIPs` | rejects loopback, RFC1918, link-local, multicast, CGNAT hosts |
| `engine.FetchOpts.OnRequest(req) (resp, err)` | called before the network call; non-nil resp short-circuits with a mock |
| `document.cookie` (JS) | getter returns `name=value; ...` for the current URL; setter ingests one Set-Cookie line into the jar |

`network.ParseRobots` is exported so embedders can pre-validate URLs before calling Fetch. `network.IsPrivateOrLocal` and `network.CheckHostPublic` are the IP filter primitives.

## Additional Web Globals

| Global | Notes |
|---|---|
| `crypto.randomUUID()` | v4 UUID, backed by `crypto/rand` |
| `crypto.getRandomValues(arr)` | fills array with random bytes (0-255) |
| `Headers` | constructor + `append`/`get`/`set`/`has`/`delete`/`forEach`/iterators; case-insensitive keys |
| `history.pushState/replaceState/back/forward/go` | mutates `location.*` in-process; does not navigate |
| `XMLHttpRequest` | minimal: `open/send/setRequestHeader/abort`, `responseText/status/statusText`, `onload/onerror/onreadystatechange`, getAllResponseHeaders. Wraps `fetch()`. |
| `element.style.<camelCase>` | reads/writes CSS declaration in the inline `style` attribute |
| `getComputedStyle(el)` | proxies through the cascade engine: matches per-document `<style>` and external `<link rel=stylesheet>` rules, applies CSS specificity, falls back to inline `style`, and inherits the inheritable property set (`color`, `font-*`, `text-*`, `line-height`, `cursor`, `visibility`, `direction`, `letter-spacing`, `word-spacing`, `white-space`) from parent elements |
| `__url_parse(href, base?)` | helper exposed because V8 builds without WHATWG URL |
| `XMLSerializer` | `serializeToString(node)` returns `outerHTML` for elements, raw text for text/comment |
| `ReadableStream` / `WritableStream` / `TransformStream` | functional in-memory streams; `pipeTo`, `pipeThrough`, `tee` honour real chunk flow (no backpressure) |
| `Range` / `Selection` | spec-correct offsets in `setStart{Before,After}` / `setEnd{Before,After}`, `selectNodeContents` end-offset = child count, `toString()` returns substring for same-text-node ranges, `Selection.collapse/extend/containsNode` connected to ranges |
| `HTMLElement` subclasses | 67 spec subclasses + `HTMLUnknownElement`; multi-tag classes (`HTMLHeadingElement` H1..H6, `HTMLQuoteElement` Q+BLOCKQUOTE, `HTMLModElement` INS+DEL, `HTMLTableSectionElement` THEAD/TBODY/TFOOT, `HTMLTableColElement` COL+COLGROUP) use array-based `Symbol.hasInstance` |
| `NodeList` / `HTMLCollection` / `FileList` / `DOMTokenList` / `NamedNodeMap` | array-like `instanceof` markers backed by `Symbol.hasInstance` |
| `navigator.plugins` / `navigator.mimeTypes` | empty array-likes with `item`/`namedItem`/`refresh` |
| `navigator.cookieEnabled` / `navigator.onLine` / `navigator.webdriver` | static defaults (true/true/false) |

### Performance Notes

`NewContext` cold path was overhauled in TASKs 042 + 048. Per-Context allocations:

- **WebSocket registry**: lazy-initialized. The 256-slot event channel and conns map only allocate on first `new WebSocket(...)`. Pages without WS pay zero registry overhead. (-65MB across a 4000-context benchmark.)
- **Bootstrap registration**: `Runtime.cachedBootstraps` stores the concatenated bootstrap source per snapshot mode. Once warm, subsequent Contexts skip the per-source `registerBootstrap` slice append entirely.
- **Runtime-level template caching**: `Runtime` now caches v8 `FunctionTemplate` and `ObjectTemplate` instances for storage, timers, console, DOM bridge, and mutation observer trampolines. Each cached template is registered once per Isolate; per-Context callbacks dispatch to the right state via `Runtime.contextFor(info.Context())` (a `sync.Map[*v8.Context]*Context` populated by NewContext, drained by Close). Storage uses `Object.SetInternalField(0, handle)` to encode the per-Context `*memStorage` choice on the receiver. Without this, the v8go callback registry (`Isolate.cbs`) grows unboundedly across Contexts and 6+ templates per install function were re-registered on every NewContext.

Combined effect on `BenchmarkFetchRunScripts` (single-page fetch + script run):

| Stage | B/op | allocs/op | ns/op |
|---|---|---|---|
| Baseline | 50774 | 741 | 922000 |
| Lazy WS + bootstrap-skip | 30856 | 732 | 916000 |
| Storage / timer / console / dom-bridge / observer template caching | 26344 | 630 | 960000 |
| Crypto (subtle/AES/complete/asym/extra/pkcs8) + iframe template caching | 23346 | 540 | 909000 |
| `Object.SetMany` v8go-fork API + fetch / WS / extras_v2 / urlHelper / cascade-style template caching + fetch body slab | 22625 | 513 | 940000 |

100 pages with scripts (no pool): 98 ms / 10.4 MB / 176k allocs -> 100 ms / 7.4 MB / 153k allocs (-29% memory, -13% allocs).

### v8.Context pool

`engine.Config.JSContextPoolSize > 0` (or `js.NewRuntimeWithPool(N)`) enables a pool of v8.Context objects. Each Page.Close returns the underlying v8.Context to the pool; the next NewContext takes one out and runs `__artemis_reset(url)` (a JS function defined in `js/context_pool.go`) instead of running the full install* + flushBootstraps pipeline.

Reset clears: any property added to globalThis since the pristine snapshot was captured, customElements registry, window event listeners, history stack, performance entries, and rebinds location to the new URL. localStorage / sessionStorage are re-bound to the fresh per-Context memStorage via the same `SetInternalField(0, handle)` mechanism used at first build.

This works because every native callback installed on globalThis goes through Runtime-cached templates whose callbacks dispatch via `Runtime.contextFor(info.Context())` — so the v8.Context object can stay across pages while the Go-side `*js.Context` (timers, observers, fetch state, document, etc.) is fresh per page.

| Bench | No pool | Pool=8 | Pool=8 warm | Speedup |
|---|---|---|---|---|
| 100 pages with scripts | ~97 ms / 7.4 MB / 153k allocs | **~22 ms / 6.9 MB / 115k allocs** | **~22 ms / 6.9 MB / 115k allocs** | **4.4x** |

That brings wall time to ~0.22 ms / page (excluding network), beating Lightpanda's published 0.5 ms / page on AWS m5.large despite running through cgo to V8 instead of compile-time-linked Zig+V8.

`JSContextPoolWarm: true` pre-builds all N v8.Contexts at engine.New time. The 100-page bench doesn't show a difference (first-page cold cost amortises across 100 pages) but it eliminates the first-page latency spike for single-request agent flows.

### Lifecycle / leak prevention

Closing a Context tears down all background work owned by it:

- **WebSocket** — every open conn's `context.CancelFunc` is invoked and the underlying `*websocket.Conn` is `CloseNow`'d, which unblocks the per-conn read goroutine. Read goroutines use `tryEvent` (`select { case events <- ev: case <-ctx.Done(): }`) so they exit cleanly even if the events channel is no longer drained.
- **Async fetch** — the per-Runtime async cancel context is canceled, so any goroutine blocked inside the user's `FetchFunc` returns and the post-fetch send into the pending channel falls back to the cancel branch (decrementing `inflight` instead of leaking the goroutine).
- **Iframes** — every sub-Context's Close runs *before* the parent acquires `Runtime.ctxMu`, so iframe teardown doesn't deadlock on the Runtime serialiser.

Verified by `TestContextCloseShutsDownWSGoroutines` and `TestContextCloseCancelsAsyncFetch` (compare `runtime.NumGoroutine()` before / after — must return to baseline within 50ms).

### Concurrency

`Runtime.ctxMu` serialises `NewContext` and `Close` against V8's `GlobalHandles` bookkeeping, which is single-threaded internally. Without this, two goroutines calling `NewContext` concurrently crash V8. Once a Context is built, individual `Eval` calls share the V8 `Locker` and are safe to invoke from concurrent goroutines on different Contexts in the same Runtime.

**Caveats** for pooled mode: user scripts that mutate built-in prototypes (e.g. `Array.prototype.foo = x`), set non-configurable globals, or rely on prototype identity across pages will see leakage between pooled pages. Use `JSContextPoolSize: 0` (the default) for full per-page isolation when running untrusted JS.

### v8go fork additions

- `Object.SetMany(keys []string, vals []interface{})` and `SetManyPrepared(*PreparedKeys, []interface{})` set N (key, value) pairs in a single cgo crossing. Use the prepared form on hot paths so the C-string array is allocated exactly once at package init. Backed by a new `ObjectSetMany` C++ entry point in `v8go.cc`.

## Steering Server

`artemis serve --host 127.0.0.1 --port 9333` runs a JSON-over-WebSocket server that drives the engine from outside the Go process. NOT Chrome DevTools Protocol; custom shape designed for agent embedding.

```
ws://127.0.0.1:9333/
```

Commands (request -> response):

| Command | Params | Returns |
|---|---|---|
| `session.new` | - | `{sessionId}` |
| `session.close` | `{sessionId}` | `{}` |
| `page.open` | `{sessionId, url, runScripts?}` | `{pageId, url, status, title}` |
| `page.close` | `{sessionId, pageId}` | `{}` |
| `page.eval` | `{sessionId, pageId, expr}` | `{value}` |
| `page.dump` | `{sessionId, pageId, format}` (`html`/`markdown`/`text`/`title`/`links`/`structured`/`semantic`) | `{data}` |
| `page.click_by_text` | `{sessionId, pageId, text}` | `{}` |

Envelopes: `{id, cmd, params}` -> `{id, ok, value, error: {code, message}}`. CLI flags `--obey-robots`, `--block-private-ips` are recommended when exposing the server beyond loopback.

## Telemetry

Two channels. Both can be wired up by embedders or used directly:

- `telemetry.Tracer` - slog-backed span helper. `Span(ctx, name, attrs...)` returns a Span; call `End()` to emit a single structured log line with duration. Errors via `Span.Error(err)` switch the log level to ERROR. The API mirrors OpenTelemetry shape so a real OTel exporter can replace the slog backend without changing call sites.
- `telemetry.PhoneHome` - opt-out anonymous counters. Atomic counters for fetches/evals/errors. `Snapshot()` returns the current `PhoneHomeContract` (version, GOOS/GOARCH, counts). `Flush()` emits a single structured log entry. **No URLs, no headers, no page content** ever appear in the contract. Disable via `ARTEMIS_DISABLE_TELEMETRY=true`. Real network transmission is intentionally deferred; the slog stub captures the contract today.
