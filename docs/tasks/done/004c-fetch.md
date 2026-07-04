# TASK 004c (Phase 3c): Fetch API

## Why

SPAs and modern websites load critical state via `fetch('/api/...')` at boot. With no Fetch API, those calls throw `ReferenceError`, the JS aborts, and the agent sees a half-rendered DOM. This TASK lands the minimum: a `fetch(url, opts)` global that performs the HTTP request synchronously through the engine's network client and returns a pre-resolved Promise carrying a Response object. Callers may use `.then(...)` chains or `await` inside an async function. After the script returns, V8 microtasks are drained so awaiting code runs before `Eval` returns.

## Acceptance

- `fetch(url)` and `fetch(url, opts)` available as a global function. Returns a Promise.
- Promise resolves to a Response with: `status`, `ok`, `statusText`, `url`, `headers` (plain object), `text()` -> Promise<string>, `json()` -> Promise<any>, `ok` boolean.
- `opts.method`, `opts.headers`, `opts.body` (string only) supported.
- Network errors reject the Promise.
- HTTP errors (4xx, 5xx) resolve normally with `ok=false` (per Fetch spec).
- Microtask checkpoint runs after every `Eval` and every inline script so awaited calls observe their fulfilled values.
- `make build`, `make vet`, `make test`, `go test -race ./js ./engine` all green.

- [x] mark active, write detail
- [x] `js/fetch.go` (FetchFunc, FetchRequest, FetchResponse, installFetch, Response object with status / ok / statusText / url / headers / text() / json())
- [x] `js.NewContext` takes `ContextOpts{Console, Fetch}` (breaking change to old `(doc, console)` signature; updated all call sites in tests and engine)
- [x] V8 default microtask policy (`kAuto`) drains the queue at script end, so promise chains resolve between successive Evals (no manual `PerformMicrotaskCheckpoint` needed - method does not exist in v8go 0.9.0)
- [x] `engine.Engine.jsFetchFunc` adapts `network.HTTPClient.Do` into `js.FetchFunc`; passed on every NewContext
- [x] tests: `text().then`, `await fetch().json()`, 404 ok=false, rejected on transport error, headers + body POST round-trip via JSON.stringify, fetch-disabled throws
- [x] engine integration test: page calls `fetch(apiURL)` against a second httptest server, renders JSON result via createElement+appendChild, `page.Markdown()` reflects the API content
- [x] all green: `make build`, `make vet`, `make test`, `go test -race ./js ./engine`
- [x] FLUSH documentation.md
- [x] archive

## Notes

The Fetch callback blocks the V8 thread for the duration of the HTTP request. V8 is single-threaded per isolate so this is safe but it serializes fetch calls. A future TASK adds a goroutine pool + cross-thread Promise resolution for parallel fetches; this requires a v8 microtask scheduler hook because `*Promise.Resolve` must run on the isolate thread. Out of scope for 004c.

`v8go.Isolate.PerformMicrotaskCheckpoint()` flushes the microtask queue. Called by `Context.Eval` after `RunScript` and by `engine.runInlineScripts` after each script. Without this, `await` in a Promise chain leaves continuations queued and the caller does not see their effects.

Reference: internal design notes.

`js.NewContext` signature changed from `(doc, console)` to `(doc, ContextOpts)` to make room for the optional Fetch field without future churn. All in-tree call sites updated in this TASK; embedders should pass `js.ContextOpts{Console: ..., Fetch: ...}` going forward.

`PerformMicrotaskCheckpoint` does not exist in v8go 0.9.0. Initially planned to call it after every Eval; instead we rely on V8's default `kAuto` microtask policy which runs queued microtasks at the end of each top-level script. Practical consequence: promise continuations are observable between successive Eval calls, but not within the same Eval expression. Tests use a two-eval pattern (`var x; ...promise chain...` followed by `x`).

Headers passed via `opts.headers` plain object are extracted by JSON.stringify round-trip (v8go does not expose Object enumeration). For most use cases this works since header values are scalar strings. Headers WebAPI type with proper enumeration arrives in TASK 004d.

The Fetch callback runs synchronously on the V8 thread, blocking it for the duration of the HTTP request. Adequate for current single-page agent workloads. True parallelism requires a goroutine pool plus cross-thread Promise resolution, deferred to TASK 004c2.

## Deviations

`js.NewContext` API changed: documented above.
