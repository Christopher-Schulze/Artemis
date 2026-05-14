# TASK 014 (Phase A5): True async runtime

## Why

The current fetch is synchronous on the V8 thread: parallel fetches via `Promise.all([...])` actually serialize. WebSocket-client (TASK 030) and proper setInterval cannot work without true async. This TASK adds a goroutine path for outbound HTTP plus a cross-thread Promise resolution channel that the V8 thread drains between scripts.

## Acceptance

- `js.Context` runs `fetch()` on a goroutine. The Promise returned to JS is unresolved at first; the goroutine writes the result to a per-Context channel; the V8 thread drains the channel and resolves the Promise.
- `(*Context).WaitIdle(ctx) error` blocks until in-flight fetches have settled. Drains the channel + pumps microtasks each round so resolution -> .then chains run.
- `engine.Page.WaitIdle(ctx) error` exposes the same on the engine.
- `Promise.all([fetch(a), fetch(b)])` runs the two HTTP requests concurrently.
- `engine.FetchOpts.Async` (default false for backwards compatibility) opt-in flag.
- All green; race-clean.

## Sub-Tasks

- [x] add per-Context async channel + inflight counter
- [x] reroute `installFetch` to spawn a goroutine, return Promise
- [x] add drain function that resolves Promises on the V8 thread
- [x] `Context.WaitIdle` blocks until inflight=0
- [x] `Page.WaitIdle` exposes it
- [x] tests
- [x] FLUSH
- [x] archive

## Notes

V8 isolate is single-threaded. All Promise.Resolve calls and Response object construction MUST run on the V8 thread (the goroutine that built the Context). Goroutines spawned for HTTP only touch Go state (FetchFunc and the channel); they never call into V8. The drain function runs on the V8 thread and is what crosses the cgo boundary back into v8go.

## Deviations

(none)
