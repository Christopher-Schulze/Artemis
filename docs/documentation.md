# Artemis Documentation

Single source of truth for project-level documentation. Code-level details live inline; TASK history lives in `docs/tasks/done/`.

## Table of Contents

- [Project Overview](#project-overview)
- [License](#license)
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
- [Hybrid Execution Router](#hybrid-execution-router)
- [Browser Provider Switch](#browser-provider-switch)
- [CDP Bridge](#cdp-bridge)
- [Bridge Actions](#bridge-actions)
- [CDP Operations](#cdp-operations)
- [Tab Management](#tab-management)
- [Renderless Engine](#renderless-engine)
- [Stealth Layer](#stealth-layer)
- [Observation Layer](#observation-layer)
- [Solver / CAPTCHA Pipeline](#solver--captcha-pipeline)
- [Human-like Input](#human-like-input)
- [Security Layer](#security-layer)
- [Profile / Session Management](#profile--session-management)
- [Scraper Subsystem](#scraper-subsystem)
- [Prompts System](#prompts-system)
- [Actions / Auto-Login](#actions--auto-login)
- [Platform Capabilities](#platform-capabilities)
- [Steering Server](#steering-server)
- [Telemetry](#telemetry)

## Project Overview

Artemis is a hybrid headless browser engine written in Go, designed for AI agents and web automation. It runs a fast renderless V8 path (loads HTML, runs JavaScript via V8, exposes a DOM and WebAPI surface, CSS cascade and computed style, and produces structured output: DOM dump, Markdown, extracted data) for pages that do not need real rendering, and escalates via a deterministic execution router to a real Chromium browser controlled over CDP (`chromedp`/`cdproto`) for layout, screenshots, canvas/media, CAPTCHA and hardened sites. It offers a Go library API, a CLI, and a custom JSON-over-WebSocket steering protocol; it does not ship a Model Context Protocol (MCP) endpoint.

## License

License: [MIT](../LICENSE). Artemis is original Go code.

## Repository Layout

```
artemis/
  cmd/
    artemis/           CLI entry point
    artemis-snapshot/  V8 startup snapshot baker (run via `make snapshot`)
    artemis-smoke/     standalone smoke harness (TASK-2325)
  engine/              top-level Engine handle: Fetch, Submit, Page, Config
  js/                  V8 isolate + Context lifecycle, native bindings, JS bootstraps
    snapshot.bin       embedded V8 startup snapshot (regenerated on demand)
  webapi/              DOM (Document, Node, Walk), HTML5 element subclasses
  parser/              HTML parser shim around golang.org/x/net/html
  agent/               extraction layer: Markdown, Text, Links, StructuredData,
                       SemanticTree, Forms, ClickByText, Type
  network/             HTTP client, robots.txt, IP filter, cookie jar
  css/                 CSS parser + inline cascade engine
  bridge/              CDP Bridge: Chrome control, context hierarchy, state machine,
                       provider registry, execution router, code mode, batcher
    actions/           high-level actions: click, type, form, scroll, selector resolution
    cdpops/            low-level CDP ops: element queries, box model, navigation, pointer
    tabs/              multi-tab management: registry, executor, dialog handler, locking
  renderless/          in-process no-render JS browser path: engine, runtime, context pool,
                       webapi registry, script router, intercept handler, CSS parser,
                       capability profile
  stealth/             anti-detection: launch flags, emulation, UA, worker parity,
                       fingerprint, geo-presets, 3-level stealth (default/stealth/paranoid)
  observe/             page observation: AX tree, network ring buffer, console capture,
                       metrics, output formatters (HAR, NDJSON)
  solver/              challenge/CAPTCHA pipeline: detector, vision solver, metrics store
  input/               human-like input: Bezier mouse, keystroke timing, touch events
  security/            browser security: SSRF, IDPI, ad blocking, navigation policy,
                       secret redaction, consent management
  profile/             enterprise session/profile mgmt: encrypted credentials, cookies,
                       sessions, storage, identity, proxy profiles
  scraper/             Scrapling-inspired scraping: adaptive selectors, AI finder,
                       structured extract, concurrent engine, diff re-scrape, HAR streaming
    parsers/           parse-phase worker pool with LockOSThread pinning
  prompts/             9 AgentScope browser agent prompt templates + template executor
  actions/             auto-login: form detection, login flow, post-login verification
  platform/            platform capability detection (GPU, CPU, memory, fonts)
  serve/               WS steering server (JSON over WebSocket)
  telemetry/           OpenTelemetry hooks + anonymous counters
  internal/            non-exported helpers (pool, provenance guard)
    provenance/        provenance guard: forbids derivation-attributing references
    docs/              doc-coverage guard: documentation.md drift checker
  third_party/v8go/    vendored fork of rogchap.com/v8go with SnapshotCreator,
                       NewIsolateFromSnapshot, Object.SetMany bindings
  docs/                this directory
    documentation.md   you are here
    tasks.md           TASK overview
    tasks/done/        archived TASK detail files
  testdata/            test fixtures (smoke scenarios, etc.)
  scripts/             tooling scripts (added on demand)
  LICENSE              MIT
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

The extraction layer is backed by `parser.ParseHTML` (HTML parser shim around `golang.org/x/net/html`) and `css.Cascade` (CSS parser + inline cascade engine). `agent.SemanticTree` returns the hierarchical semantic tree; `agent.Markdown`, `agent.Text`, `agent.Links`, `agent.StructuredData`, `agent.Forms` are the other direct converters from a `*webapi.Document`.

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

## Hybrid Execution Router

The `bridge.ExecutionRouter` deterministically escalates scrape routes on failure. It is a simple state machine: `RouteStatic` → `RouteRendered`. The renderless V8 path handles `RouteStatic` (no-JS pages, APIs, RSS) and `RouteRendered` (JS-generated DOM without layout). When the renderless path encounters an unsupported feature (layout, canvas, WebAuthn, CAPTCHA), the router escalates to `RouteRendered` which triggers the CDP/Chromium fallback.

```go
router := bridge.NewExecutionRouter()
next, err := router.Escalate(bridge.RouteStatic, err)
// RouteStatic -> RouteRendered on failure
// RouteRendered -> error (no further route)
```

| Symbol | Kind | Purpose |
|---|---|---|
| `bridge.RouteStatic` | `RouteKind` | no-JS / API / RSS pages |
| `bridge.RouteRendered` | `RouteKind` | JS-generated DOM, Chromium fallback |
| `bridge.NewExecutionRouter()` | func | create router |
| `router.Escalate(current, err)` | method | compute next route on failure |

## Browser Provider Switch

The `bridge.ProviderRegistry` holds available browser providers and selects one based on configuration. The `BrowserProvider` interface abstracts multi-backend browser control: local headless Chromium (default), Camofox REST backend, or cloud providers (Browserbase, Firecrawl — deferred P7).

```go
registry := bridge.NewProviderRegistry()
registry.Register("chrome", &bridge.LocalChromeProvider{})
provider, config, err := registry.SelectFromConfig()
```

| Symbol | Kind | Purpose |
|---|---|---|
| `bridge.BrowserProvider` | interface | `Name()`, `Launch(ctx, config)`, `Close()`, `Healthy()` |
| `bridge.ProviderRegistry` | struct | provider registry with `Register`, `Get`, `Default`, `SelectFromConfig` |
| `bridge.LocalChromeProvider` | struct | local headless Chromium provider |
| `bridge.CamofoxProvider` | struct | Camofox REST backend provider |
| `bridge.ProviderConfig` | struct | provider launch configuration |
| `bridge.BrowserSession` | struct | active browser session handle |
| `bridge.SelectBrowserProvider(candidates, preferred)` | func | select provider from candidates |

## CDP Bridge

The `bridge` package implements Chrome control via CDP (`chromedp` + `cdproto`). It manages the context hierarchy (`AllocCtx → BrowserCtx → TabCtx`), the bridge state machine, code mode execution, CDP pipelining/batching, and the event filter.

| Symbol | Kind | Purpose |
|---|---|---|
| `bridge.BridgeState` | type | bridge state machine states |
| `bridge.BridgeStateMachine` | struct | state machine with `IsActiveState`, `IsTerminalState` |
| `bridge.ContextKind` | type | context hierarchy kinds (Alloc, Browser, Tab) |
| `bridge.CDPContextNode` / `bridge.CDPContextTree` | struct | context hierarchy nodes |
| `bridge.Batcher` | struct | CDP command batcher for pipelining |
| `bridge.CDPPipeline` | struct | CDP command pipeline |
| `bridge.CDPTask` / `bridge.CDPTaskResult` | struct | batched task + result |
| `bridge.MandatoryBatchType` | type | mandatory batch types (boxModel+style, navigate+wait+snapshot, etc.) |
| `bridge.EventFilter` | struct | CDP domain enable/disable manager |
| `bridge.CodeModeExecutor` | struct | Code Mode: LLM-generated JS async arrow functions |
| `bridge.BridgeInitializer` | struct | bridge initialization with `BridgeInitConfig` |
| `bridge.ChromeDiscovery` | struct | system Chromium auto-detection |
| `bridge.TabRegistry` | struct | tab registry (bridge-level) |
| `bridge.NetworkRequestBuffer` | struct | network ring buffer (max 50 entries/tab) |
| `bridge.RefRegistry` / `bridge.RefHandle` | struct | ARIA ref-ID registry for snapshots |
| `bridge.CircuitBreaker` | struct | per-domain circuit breaker (5 failures → 60s pause) |
| `bridge.IdleWatchdog` | struct | idle tab watchdog with `IdleWatchdogConfig` |
| `bridge.InactivityMonitor` | struct | inactivity monitor with `InactivityConfig` |
| `bridge.ConfigHasher` / `bridge.ConfigHash` | struct | config-hash for session isolation |
| `bridge.PolicyHook` / `bridge.PolicyEngine` | interface | navigation policy hooks |
| `bridge.PrivacyRoutingHook` | struct | privacy routing hook |
| `bridge.AuditHook` | struct | OCSF audit hook |
| `bridge.RecoveryAction` | interface | recovery action interface |
| `bridge.PageLoadStrategy` | struct | page load strategy (auto-wait) |
| `bridge.ScreenshotFormat` | type | screenshot format (PNG, JPEG) |
| `bridge.JPEGPipeline` | struct | JPEG screenshot pipeline |
| `bridge.CDPConnectMode` | type | CDP connection mode |

## Bridge Actions

The `bridge/actions` package implements high-level browser actions: click with human-like movement, text input with keystroke timing, form fill/select/check/submit, scroll with easeInOut, and unified selector resolution.

| Symbol | Kind | Purpose |
|---|---|---|
| `actions.NewClickAction(ref)` | func | create click action for ARIA ref |
| `actions.ClickWithMovement(ctx, ref, start, target)` | func | click with Bezier mouse path |
| `actions.NewTypeAction(ref, text)` | func | create type action |
| `actions.NewFormFill(ref, value)` | func | form fill action |
| `actions.NewFormSelect(ref, value)` | func | form select action |
| `actions.NewFormCheck(ref)` | func | form check action |
| `actions.NewFormSubmit(ref)` | func | form submit action |
| `actions.FormBatch(ctx, actions)` | func | batch form actions |
| `actions.NewScrollAction(direction, amount)` | func | scroll action |
| `actions.ResolveSelector(selector)` | func | resolve ARIA ref / CSS / XPath |
| `actions.ResolveAndValidate(ctx, selector)` | func | resolve + validate selector |
| `actions.GenerateClickPath(start, end)` | func | Bezier click path |
| `actions.ComputeScrollSteps(total, steps)` | func | scroll step distribution |
| `actions.EaseInOut(t)` | func | easeInOut interpolation |

## CDP Operations

The `bridge/cdpops` package implements low-level CDP operations: element queries, box model, coordinate transforms, page navigation + wait, and mouse/touch events.

| Symbol | Kind | Purpose |
|---|---|---|
| `cdpops.GetBoxModel(ref)` | func | element box model |
| `cdpops.GetElementCenter(box)` | func | element center coordinates |
| `cdpops.IsElementVisible(box)` | func | visibility check |
| `cdpops.IsElementClickable(info)` | func | clickability check |
| `cdpops.FilterVisible(elements)` | func | filter visible elements |
| `cdpops.FindByRef(elements, ref)` | func | find element by ARIA ref |
| `cdpops.GenerateBezierPath(start, end, steps)` | func | Bezier curve path |
| `cdpops.AddJitter(p, maxJitter)` | func | add jitter to point |
| `cdpops.CSSToDevicePixels(p, scale)` | func | CSS → device pixel transform |
| `cdpops.DeviceToCSSPixels(p, scale)` | func | device → CSS pixel transform |
| `cdpops.NewNavigator()` | func | page navigation manager |
| `cdpops.NewPointerDispatcher()` | func | pointer event dispatcher |
| `cdpops.Point` / `cdpops.Rect` / `cdpops.Quad` / `cdpops.BoxModel` | struct | geometry types |
| `cdpops.ElementInfo` | struct | element metadata |
| `cdpops.MouseEvent` / `cdpops.MouseButton` | type | mouse event types |

## Tab Management

The `bridge/tabs` package implements multi-tab management: tab registry + lifecycle, concurrent tab execution, alert/confirm/prompt dialog handling, and per-tab locking.

| Symbol | Kind | Purpose |
|---|---|---|
| `tabs.NewTabRegistry()` | func | create tab registry |
| `tabs.NewTabExecutor(registry, maxConcurrent)` | func | concurrent tab executor |
| `tabs.NewTabLock()` | func | per-tab lock |
| `tabs.NewDialogHandler()` | func | dialog handler |
| `tabs.NewAlertDialog(message, url)` | func | alert dialog |
| `tabs.NewConfirmDialog(message, url)` | func | confirm dialog |
| `tabs.NewPromptDialog(message, url, defaultPrompt)` | func | prompt dialog |
| `tabs.Tab` | struct | tab handle |
| `tabs.TabState` | type | tab state (Open, Closed, Loading) |
| `tabs.DialogAction` / `tabs.DialogType` | type | dialog action/type enums |

## Renderless Engine

The `renderless` package is the in-process no-render JS browser path. It provides a standalone engine that runs V8 with DOM/WebAPI globals, inline + external script execution, fetch/XHR bridge, cookie jar, OnRequest mock/intercept, robots/private-IP guard, CSS parse/cascade/computed style, and a generated `RenderlessCapabilityProfile`. It never owns layout/paint/CDP/profile/server semantics and escalates to `bridge/` via the execution router.

| Symbol | Kind | Purpose |
|---|---|---|
| `renderless.NewEngine(cfg)` | func | create renderless engine |
| `renderless.Engine` | struct | renderless engine handle |
| `renderless.EngineConfig` | struct | engine configuration |
| `renderless.Page` | struct | renderless page |
| `renderless.NewPage(url, status, body)` | func | create page from raw HTML |
| `renderless.ContextPool` | struct | V8 context pool |
| `renderless.NewContextPool(maxSize)` | func | create context pool |
| `renderless.IsolatePool` | struct | V8 isolate pool |
| `renderless.NewIsolatePool(maxSize)` | func | create isolate pool |
| `renderless.RuntimeContext` | struct | runtime context |
| `renderless.ScriptRouter` | struct | script execution router |
| `renderless.NewScriptRouter()` | func | create script router |
| `renderless.InterceptHandler` | struct | request intercept handler |
| `renderless.NewInterceptHandler()` | func | create intercept handler |
| `renderless.NewMockResponse(status, body)` | func | mock response for intercept |
| `renderless.CSSParser` / `renderless.CSSRule` | struct | CSS parser + rule |
| `renderless.ComputedStyle` | struct | computed style engine |
| `renderless.RenderlessCapabilityProfile` | struct | generated capability profile |
| `renderless.GenerateCapabilityProfile(cfg, registry)` | func | generate capability profile |
| `renderless.WebAPIRegistry` / `renderless.WebAPIGlobal` | struct | WebAPI registry + global |
| `renderless.BuilderPool` | struct | HTML builder pool |
| `renderless.InterceptAction` | type | intercept action (continue, mock, fail) |
| `renderless.ScriptType` | type | script type (inline, external) |

## Stealth Layer

The `stealth` package provides anti-detection with 3 levels (Default, Stealth, Paranoid), 27 zero-cost patches bundled into one script via `go:embed`, 60 launch flags, geo-presets, worker thread parity, fingerprint spoofing, and HTTP/2 fingerprint validation.

| Symbol | Kind | Purpose |
|---|---|---|
| `stealth.StealthDefault` | `StealthLevel` | local/LAN: zero stealth |
| `stealth.StealthStealth` | `StealthLevel` | public sites: 27 patches + 60 flags |
| `stealth.StealthParanoid` | `StealthLevel` | hardened sites: 29 patches + 60 flags |
| `stealth.DetermineStealthLevel(targetURL, policy, lookup)` | func | determine level from URL + policy |
| `stealth.SuggestEscalation(current, signals)` | func | suggest escalation from bot signals |
| `stealth.Profile` | struct | stealth profile (viewport, UA, vendor, platform, languages, timezone, etc.) |
| `stealth.Script(p)` | func | generate stealth script for profile |
| `stealth.BundledScript()` | func | bundled 27-patch script (~25KB) |
| `stealth.ParanoidScript()` | func | paranoid-mode +2 on-demand patches |
| `stealth.Quick()` | func | quick stealth script |
| `stealth.ContextHash(opts)` | func | session-isolation hash |
| `stealth.BuiltinGeoPresets()` | func | 8 built-in geo presets |
| `stealth.ValidatePreset(p)` | func | validate geo preset |
| `stealth.ValidateManifest(dir)` | func | validate stealth manifest |
| `stealth.LaunchFlagCount()` / `stealth.LaunchFlagCountFor(level)` | func | launch flag counts |
| `stealth.PatchCount()` / `stealth.PatchCountFor(level)` | func | patch counts |
| `stealth.ReferrerForDomain(rawURL, mem)` | func | referrer from domain memory |
| `stealth.IsSwiftShader(renderer)` | func | detect SwiftShader |
| `stealth.IsChromiumFingerprint(config)` | func | validate H2 fingerprint |
| `stealth.ValidateH2Settings(frame)` | func | validate H2 settings frame |
| `stealth.DeriveEffectiveType(rtt, downlink)` | func | derive network effective type |
| `stealth.ParseChromeVersion(ua)` | func | parse Chrome version from UA |
| `stealth.STEALTH_ARGS` | var | stealth launch arguments |
| `stealth.BasePatchCount` / `stealth.ParanoidPatchCount` | const | patch count constants |

## Observation Layer

The `observe` package implements page observation: accessibility tree extraction + diff, network ring buffer + subscriber pattern, console log capture (ring buffer 1000), performance metrics, and output formatters (HAR, NDJSON).

| Symbol | Kind | Purpose |
|---|---|---|
| `observe.NewAnnotator(viewportWidth, viewportHeight)` | func | create annotator |
| `observe.AXNode` / `observe.AXTreeNode` | struct | accessibility tree node |
| `observe.DiffAXTrees(before, after)` | func | diff AX trees (edit distance) |
| `observe.MyersDiff(before, after)` | func | Myers diff algorithm |
| `observe.SnapshotInteractive(tree)` | func | snapshot interactive elements |
| `observe.SnapshotByRole(tree, role)` | func | snapshot by ARIA role |
| `observe.DedupRoleSnapshot(nodes)` | func | deduplicate role snapshot |
| `observe.FormatHAR(entries)` | func | format HAR output |
| `observe.FormatNDJSON(events)` | func | format NDJSON output |
| `observe.FormatConsoleNDJSON(entries)` | func | format console NDJSON |
| `observe.FormatOutput(format, events)` | func | format output |
| `observe.TruncateContent(content, maxLen)` | func | truncate content |
| `observe.ContentRoles` / `observe.InteractiveRoles` / `observe.StructuralRoles` | var | role classification maps |
| `observe.DefaultConsoleBufferSize` | const | 1000 entries |
| `observe.DefaultViewportWidth` / `observe.DefaultViewportHeight` | const | 1280×720 |

## Solver / CAPTCHA Pipeline

The `solver` package implements a 2-stage challenge/CAPTCHA pipeline: vision solve (screenshot → LLM vision → instruction → execute) → user fallback. Challenge types are classified by `ChallengeType`.

| Symbol | Kind | Purpose |
|---|---|---|
| `solver.NewChallengeDetector()` | func | create challenge detector |
| `solver.ChallengeDetector` | struct | challenge detector |
| `solver.ChallengeType` | type | challenge type enum |
| `solver.TypeNone` | `ChallengeType` | no challenge |
| `solver.TypeCloudflare` | `ChallengeType` | Cloudflare challenge |
| `solver.TypeRecaptcha` | `ChallengeType` | reCAPTCHA |
| `solver.TypeHCaptcha` | `ChallengeType` | hCaptcha |
| `solver.TypeGeneric` | `ChallengeType` | generic challenge |
| `solver.InferenceHub` | interface | LLM inference hub interface |
| `solver.NewInferenceHubHook(hub)` | func | create inference hub hook |
| `solver.InferenceHubRequest` / `solver.InferenceHubResponse` | struct | inference request/response |
| `solver.PipelineResult` | struct | pipeline result |
| `solver.PipelineStage` | type | pipeline stage (vision, fallback) |
| `solver.PipelineStats` | struct | pipeline statistics |
| `solver.MetricsStore` | struct | SQLite metrics store |
| `solver.OpenMetricsStore(path)` | func | open metrics store |
| `solver.FormatChallengePrompt(challengeType, context)` | func | format challenge prompt |
| `solver.PageSignals` | struct | page signals for detection |
| `solver.ChallengeInfo` | struct | detected challenge info |
| `solver.DefaultVisionModel` | const | `qwen3.6-vision` |

## Human-like Input

The `input` package implements human-like input: Bezier curve mouse movement with jitter, keystroke timing with Gaussian distribution, and touch events for mobile emulation.

| Symbol | Kind | Purpose |
|---|---|---|
| `input.GenerateMousePath(start, end, cfg, rng)` | func | Bezier mouse path |
| `input.MoveMouse(start, end)` | func | simple mouse move |
| `input.BezierCurve(p0, p1, p2, t)` | func | Bezier curve point |
| `input.BezierPath(start, end, steps, rng)` | func | Bezier path with jitter |
| `input.ClickGaussianOffset(sigma, rng)` | func | Gaussian click offset |
| `input.TypingDelays(text, cfg, rng)` | func | keystroke delays |
| `input.TypingRhythm(text, cfg, rng)` | func | keystroke rhythm + keys |
| `input.GaussianJitter(baseMs, sigmaPct, rng)` | func | Gaussian jitter |
| `input.EaseInOutScroll(totalDistance, steps)` | func | easeInOut scroll steps |
| `input.Scroll(distance, steps, hasIO, hasSL)` | func | scroll with infinite-scroll detection |
| `input.DetectInfiniteScroll(hasIO, hasSL)` | func | detect infinite scroll |
| `input.NewMouseClick(point, button)` | func | mouse click event |
| `input.DefaultMouseMoveConfig()` | func | default mouse move config |
| `input.MousePath` / `input.MousePoint` / `input.MouseClick` | struct | mouse types |
| `input.TypingConfig` | struct | typing configuration |
| `input.ScrollResult` / `input.ScrollStrategy` | type | scroll result/strategy |
| `input.TouchEvent` / `input.TouchEventType` | type | touch event types |

## Security Layer

The `security` package implements browser security: SSRF prevention (private IP blocking), indirect prompt injection (IDPI) defense, ad/tracker blocking (40+ patterns), navigation policy (domain allowlist), secret redaction (3-point pipeline), and consent management.

| Symbol | Kind | Purpose |
|---|---|---|
| `security.IsPrivateIP(ip)` | func | private IP check (RFC1918, loopback, link-local) |
| `security.NewAdBlocker()` | func | create ad blocker |
| `security.AdBlocker` | struct | ad/tracker blocker |
| `security.AdBlockResult` / `security.AdBlockCategory` | type | ad block result/category |
| `security.NewRedactor()` | func | create secret redactor |
| `security.Redactor` | struct | secret redaction pipeline |
| `security.CheckResult` | struct | redaction check result |
| `security.ContainsInvisibleChars(s)` | func | detect invisible characters (IDPI) |
| `security.DetectLoginFlow(text)` | func | detect login flow |
| `security.NewNavigationPolicy()` | func | create navigation policy |
| `security.NavigationPolicy` | struct | domain allowlist policy |
| `security.ConsentPage` / `security.ConsentProfile` | struct | consent management types |
| `security.ConsentMode` / `security.ConsentAction` | type | consent mode/action enums |
| `security.AutoConfirm(ctx, page, profile)` | func | auto-confirm consent dialog |
| `security.ResolveConsentMode(raw)` | func | resolve consent mode |
| `security.DefaultConsentAction()` | func | default consent action |

## Profile / Session Management

The `profile` package implements the enterprise browser profile system: encrypted credential store (AES-256-GCM), multi-profile manager, auto-login, session health check, cookie + storage management, per-profile fingerprint identity, and session-level proxy with hybrid geo-modes.

| Symbol | Kind | Purpose |
|---|---|---|
| `profile.BrowserProfile` | struct | browser profile |
| `profile.ProfileManager` | struct | multi-profile manager |
| `profile.CredentialStore` | struct | encrypted credential store (AES-256-GCM) |
| `profile.SessionManager` | struct | session health + auto-relogin |
| `profile.CookieJar` | struct | cookie persistence + expiry |
| `profile.StorageManager` | struct | localStorage/sessionStorage/IndexedDB |
| `profile.IdentityManager` | struct | per-profile fingerprint identity |
| `profile.GenerateUUID5(namespace, name)` | func | deterministic UUID5 generation |
| `profile.ProxyProfileConfig` | struct | session-level proxy config |
| `profile.ResolvedProxyConfig` | struct | resolved proxy with geo-mode |
| `profile.GeoMode` | type | geo-mode enum (explicit_wins, proxy_locked) |
| `profile.KeychainProvider` | struct | in-memory keychain (test) |
| `profile.SecretProvider` | interface | pluggable keychain abstraction |

## Scraper Subsystem

The `scraper` package implements the Scrapling-inspired scraping engine: adaptive selectors (SQLite, survives redesigns), AI element finding via Inference Hub, structured data extraction (JSON-LD, OpenGraph, Twitter Cards, Microdata), concurrent scraping engine (static + renderless + browser pools), differential re-scrape with conditional GET + region hashing, HAR streaming, pagination/infinite-scroll detection, anti-scraping recovery (backoff + escalation), and server impact detection.

| Symbol | Kind | Purpose |
|---|---|---|
| `scraper.NewFinder()` | func | create adaptive element finder |
| `scraper.Finder` | struct | adaptive element locator (CSS → XPath → Text → Attribute → Structural) |
| `scraper.Result` | struct | find result with strategy + confidence |
| `scraper.ExtractedPage` | struct | extracted page result |
| `scraper.StructuredRecord` | struct | structured data record |
| `scraper.ExtractJSONLD(doc)` | func | extract JSON-LD |
| `scraper.ExtractOpenGraph(doc)` | func | extract Open Graph |
| `scraper.AdaptiveSelectorCache` | struct | SQLite adaptive selector cache |
| `scraper.DomainRateLimiter` | struct | per-domain rate limiter (token bucket) |
| `scraper.ConcurrentEngine` | struct | concurrent scraping engine |
| `scraper.HARStreamer` | struct | HAR streaming writer |
| `scraper.DiffEngine` | struct | differential re-scrape engine |
| `scraper.PaginationDetector` | struct | pagination + infinite scroll detector |
| `scraper.RecoveryChain` | struct | anti-scraping recovery chain |
| `scraper.ImpactDetector` | struct | server impact detector |
| `scraper.BloomFilter` | struct | URL dedup bloom filter |

## Prompts System

The `prompts` package provides the 9 AgentScope browser agent prompt templates as Go string constants, plus a `TemplateExecutor` that integrates template selection with browser skill execution.

| Symbol | Kind | Purpose |
|---|---|---|
| `prompts.SystemPrompt()` | func | base behavior prompt |
| `prompts.DecomposePrompt()` | func | task decomposition prompt |
| `prompts.DecomposeReflectionPrompt()` | func | decomposition reflection |
| `prompts.ObservePrompt()` | func | chunked observation prompt |
| `prompts.PureReasoningPrompt()` | func | pure reasoning prompt |
| `prompts.FormFillingPrompt()` | func | form filling prompt |
| `prompts.FileDownloadPrompt()` | func | file download prompt |
| `prompts.SummarizePrompt()` | func | task summarization prompt |
| `prompts.GetPrompt(pt)` | func | get prompt by type |
| `prompts.AllTemplates()` | func | list all templates |
| `prompts.RenderTemplate(template, variables)` | func | render template with variables |
| `prompts.EstimateTokens(text)` | func | estimate token count |
| `prompts.ShouldUsePureReasoning(situation, tokens)` | func | pure reasoning decision |
| `prompts.PromptType` | type | prompt type enum |
| `prompts.PromptTemplate` | struct | prompt template descriptor |

## Actions / Auto-Login

The `actions` package implements auto-login: form detection, login flow identification, and post-login verification.

| Symbol | Kind | Purpose |
|---|---|---|
| `actions.DetectLoginForm(ctx, page)` | func | detect login form |
| `actions.LoginDetection` | struct | login detection result |
| `actions.LoginForm` / `actions.LoginField` | struct | login form + field |
| `actions.FormIntent` | struct | form intent for batch fill |
| `actions.FormField` | struct | form field spec |
| `actions.VerifyPostLogin(page)` | func | verify post-login state |
| `actions.LoginPage` / `actions.PostLoginPage` | struct | login page interfaces |

## Platform Capabilities

The `platform` package detects platform capabilities (GPU, CPU, memory, fonts) for stealth fingerprint consistency.

| Symbol | Kind | Purpose |
|---|---|---|
| `platform.Detect()` | func | detect platform capabilities |
| `platform.PlatformCapabilities` | struct | detected capabilities (GPU, CPU, memory, fonts) |

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
| 100 pages with scripts | ~97 ms / 7.4 MB / 153k allocs | **~18.4 ms / 6.5 MB / 100k allocs** | **~18.9 ms / 6.5 MB / 100k allocs** | **5.3x** |

That brings wall time to ~0.184 ms / page (excluding network), 2.7x faster than the ~0.5 ms / page published for comparable renderless engines, despite running through cgo to V8.

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

## TASK-2344 Performance Optimizations

The excellence reality-gate (TASK-2344) profiled every hot path, committed per-hot-path benchmarks, and eliminated redundancy/allocation/contention across 4 rounds. Key optimizations:

### network/easylist.go — IsAdTrackerDomain label-walk

Replaced linear scan over 48 patterns (with per-call `strings.ToLower` on each pattern) with a pre-built `map[string]struct{}` pattern set + label-walk (check domain, drop leftmost label, repeat). The miss case (production hot path — most domains are NOT ad/tracker) went from 1809ns to 57ns (31x faster). `FilterAdTrackerDomains` on a 100-domain batch went from 94969ns to 5861ns (16x faster).

### js/context_pool.go + js/helpers.go — buffer pre-allocation

`jsonStringLiteral` and `jsStringLit` now pre-allocate their output buffers to `len(s)+2` (the minimum output: 2 quotes + input unchanged), eliminating reallocation growth. `jsonStringLiteral`: 282ns→232ns (-18%), 3→2 allocs. `jsStringLit`: 228ns→167ns (-27%), 4→1 allocs.

### serve/streaming.go — Broadcast fast paths

The `Broadcast` method now fast-paths the common cases: 0 subscribers (skip slice alloc + timestamp, still count broadcast) and 1 subscriber (direct call, no goroutine, no WaitGroup). 0-subscriber: 114ns→43ns (-62%). 1-subscriber: 714ns→118ns (-83%). 2+ subscribers keep the goroutine fan-out.

### webapi/node.go + mutation.go — Attr/Tag fast paths

`Tag()` now checks `DataAtom != 0` (known tag) and returns `Data` directly (already lowercase from atom string), skipping `strings.ToLower` for the common case. `Attr()` fast-paths exact key match (`a.Key == name`) on the first loop (parser lowercases keys), with `EqualFold` fallback on a second loop for user-injected attributes. `GetElementById` and `GetElementsByClassName` inline the attribute lookup to avoid the method call overhead. `GetElementById`: 7003ns→6279ns (-10%). `GetElementsByClassName`: 15255ns→14296ns (-6%).

### Benchmark suite

72 benchmarks across 14 packages (was 51, +21 new). The benchmark harness scorecard (`benchmark/results/scorecard.md`) shows avg 0.87ms per scenario (was 2.42ms, 2.8x improvement). Artemis beats the published competitor number (0.184ms/page vs 0.5ms/page, 2.7x faster).
