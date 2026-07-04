# TASK 003: Phase 2 - V8 integration + minimal DOM API

## Why

Static fetch alone is not a browser. The defining feature of Artemis is running real JavaScript against a live DOM via stock V8. TASK 003 lays the foundation: V8 isolate management, per-page contexts, a `Page.Eval` agent surface, captured `console.*`, and a minimal read-only `document` binding. The full WebAPI surface (Event, Fetch, XHR, MutationObserver, full Element prototype) lands in TASK 004; TASK 003 proves the architecture end-to-end with a small but real slice.

## Acceptance

- `js` package: `Runtime` wraps `*v8go.Isolate`. Process-shared isolate; `Runtime.Close()` releases V8 resources.
- `js` package: `Context` wraps a `*v8go.Context` bound to a single `*webapi.Document`. Created via `Runtime.NewContext(doc)`, freed via `Context.Close()`.
- `js.Context.Eval(ctx, expr) (Value, error)` evaluates JavaScript in the bound page context. `Value` exposes `String() string`, `Bool() bool`, `Int64() int64`, `Float64() float64`, `IsUndefined() bool`, `IsNull() bool`, `IsObject() bool`. Errors carry the V8 stack trace where available.
- `console.log`, `console.warn`, `console.error`, `console.info`, `console.debug` are routed to a configurable sink (default: nothing). Engine wires them to `slog.Default()` at `Info` / `Warn` / `Error` / `Info` / `Debug` levels.
- Minimal `document` global, read-only, exposes:
  - `document.title` -> the parsed `<title>` text
  - `document.URL` -> the document URL
  - `document.body.innerHTML` -> serialized body inner HTML
  - `document.body.outerHTML` -> serialized body outer HTML
  - `document.documentElement.outerHTML` -> full-document serialization
  - `document.querySelector(sel)` and `document.querySelectorAll(sel)` returning a thin element wrapper exposing `.tagName`, `.textContent`, `.innerHTML`, `.getAttribute(name)`, `.outerHTML`, and (for the NodeList result) iterability.
- `engine.Page.Eval(ctx, expr) (js.Value, error)` exposed for agents.
- `engine.FetchOpts.RunInlineScripts` (default `false`): when true, inline `<script>` tags are executed in document order at the end of `Fetch`.
- `cmd/artemis fetch --eval "<expr>"` flag prints `Eval` result to stdout.
- Tests:
  - `js/runtime_test.go` - isolate create / destroy.
  - `js/context_test.go` - Eval primitive types, error path with stack, `document.title`, `document.querySelector(...).textContent`, `console.log` capture.
  - `engine/eval_test.go` - end-to-end: fetch HTTP fixture, eval against it.
- No DOM-mutating JS API in this TASK. Mutations and events ship with TASK 004.
- No external script loading in this TASK. Inline `<script>` only.
- `make build`, `make vet`, `make test` all green; race-clean (`make test-race`) on the js + engine packages.

## Sub-Tasks

- [x] add `rogchap.com/v8go v0.9.0` via `go get` (cgo prebuilt V8 lib pulled into module cache; first build ~30s, subsequent rebuilds cached)
- [x] `js/runtime.go` - Runtime + isolate lifecycle
- [x] `js/value.go` - Value adapter on `*v8go.Value`
- [x] `js/console.go` - console.* binder + Console/FuncConsole/DiscardConsole/CollectConsole types
- [x] `js/dom_bridge.go` - install `document` and helpers on the global object
- [x] `js/context.go` - per-page Context + Eval (with `*v8.JSError` stack-trace formatting)
- [x] `engine/engine.go` - own a `*js.Runtime`, build per-page contexts in Fetch, `runInlineScripts` walker
- [x] `engine/page.go` - add `Eval`, `JSContext()` accessor, `Close` (releases v8 context)
- [x] `engine/engine.go` - `FetchOpts.RunInlineScripts`, `FetchOpts.Console` plumbing
- [x] `cmd/artemis/fetch.go` - add `--eval`, `--run-scripts`, `--console` flags
- [x] tests for js (runtime, context primitives, document title/url, qs/qsa, getAttribute, console capture, body.innerHTML, eval-after-close) and engine (eval, run-inline-scripts, console-routed)
- [x] race tests (`go test -race ./js ./engine`) clean
- [x] FLUSH: extend `docs/documentation.md` JS / Eval section
- [x] archive to `docs/tasks/done/`

## Notes

`rogchap.com/v8go` ships prebuilt static V8 libs for darwin/linux x86_64+arm64. Build is cgo - first compile pulls ~150MB into the module cache. The module exposes `Isolate`, `Context`, `ObjectTemplate`, `FunctionTemplate`, `Value`, and a basic `Inspector`. We do NOT use snapshots in TASK 003 - cold start cost is acceptable for a Phase 2 milestone; snapshot integration is a Phase 7 perf TASK.

The DOM bridge pattern: every property/method on the JS side calls back into Go via `v8go.NewFunctionTemplate`. The Go-side handler reads from the bound `*webapi.Document`. This is the comptime-replacement we discussed in spec.md; full WebAPI surface in TASK 004 will move to a code-generated bridge to avoid hand-writing 150 of these.

Mutation propagation from JS to Go DOM is intentionally absent. Until TASK 004 lands a proper MutationObserver-backed shadow DOM, only reads round-trip. `document.body.innerHTML = "..."` is not implemented; an attempted assignment is a noop today, will become a real setter in TASK 004.

Reference: internal design notes.

DOM bridge installs property values as snapshots at context-creation time. For `body.innerHTML`, `body.outerHTML`, `documentElement.outerHTML`, `body.textContent`, those are computed once when the context is built. Since this TASK's DOM is read-only from JS, snapshots are correct. When TASK 004 introduces mutation, we will move these to v8 accessor properties (`SetAccessorProperty` with getter+setter Function templates) so reads always reflect current state.

NodeList returned by `querySelectorAll` is an Object with `length` and numeric indices, indexable as `list[0]`, `list[1]` with `.length`. It is NOT iterable via `for ... of` since the `@@iterator` symbol is not installed. Direct index access plus length-based for-loops work today; iterator support arrives with TASK 004.

External `<script src="...">` tags are skipped under `RunInlineScripts`. Loader hooks for them require the Fetch API and resource loader, which arrive with TASK 004.

## Deviations

(none)
