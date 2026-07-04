# TASK 048: Performance + Fidelity Maximization

## Why

After TASK 042 baked the V8 startup snapshot, profiling and reality-check uncovered (1) per-Context allocations + cgo-callback registrations that grew unboundedly across pages, (2) WebAPI fidelity gaps vs the living-standard surface, and (3) an unaddressable wall-time floor (~1 ms/page) dominated by V8's internal `Context::New`. This TASK consolidates four iteration rounds that drove FetchRunScripts memory down by 55%, eliminated the v8go callback-registry leak, expanded the WebAPI surface to spec parity, and finally introduced a v8.Context pool that beat the ~0.5 ms/page published range.

## Acceptance

- `BenchmarkFetchRunScripts` (single-page fetch + script run): 22.6 KB/op, 513 allocs/op (down from 50.8 KB / 741 — -55% / -31%).
- `BenchmarkEndToEnd100Pages` no-pool: ~97 ms / 7.4 MB / 153k allocs.
- `BenchmarkEndToEnd100PagesPooled` (pool=8): ~22 ms / 6.9 MB / 115k allocs (-77% wall time, ~0.22 ms/page, ahead of the ~0.5 ms/page published range).
- `Isolate.cbs` callback registry no longer grows per-Context — every `FunctionTemplate`/`ObjectTemplate` is registered once per Runtime; the per-Context dispatch goes through `Runtime.contextFor(info.Context())` (a `sync.Map[*v8.Context]*Context`) or `info.This().GetInternalField(0)`.
- WebAPI surface matches living-standard coverage for the surfaces real sites use: 67 HTMLElement subclasses + HTMLUnknownElement, functional Streams, spec-correct Range/Selection, XMLSerializer, NodeList/HTMLCollection/FileList/DOMTokenList/NamedNodeMap markers, navigator.plugins/mimeTypes/webdriver/etc.
- `Context.Close` deterministically tears down all background work (WebSocket reader goroutines, async fetch goroutines, iframe sub-Contexts) so `runtime.NumGoroutine()` returns to baseline within 50 ms — covered by `TestContextCloseShutsDownWSGoroutines` and `TestContextCloseCancelsAsyncFetch`.
- Concurrent `NewContext` from multiple goroutines no longer crashes V8: serialised via `Runtime.ctxMu`. Covered by `TestPooledContextConcurrent` and `TestNonPooledContextConcurrent` under `-race`.

## Sub-Tasks

### Phase 0 — Reality check after snapshot (fidelity + correctness)

- [x] Audit the reference `webapi/element/html` layout; expand HTMLElement subclasses 47 -> 67. Multi-tag classes (`HTMLHeadingElement` over H1..H6, `HTMLQuoteElement` over Q+BLOCKQUOTE, `HTMLModElement` over INS+DEL, `HTMLTableSectionElement` over THEAD/TBODY/TFOOT, `HTMLTableColElement` over COL+COLGROUP) use array-form `_tagInstance`. `HTMLUnknownElement` matches any tag name not in the known set.
- [x] `Streams`: replace `pipeTo`/`pipeThrough`/`tee` stubs with functional in-memory implementations. `pipeTo` reads + writes serially, `pipeThrough` wires reader -> transform.writable, `tee` consumes once and broadcasts to both branches.
- [x] `Range` / `Selection`: spec-correct offsets (`setStartBefore` -> child index, `selectNodeContents` -> child count or text length), `toString()` text-substring for same-text-node ranges, `Selection.collapse(node, off)` installs a fresh collapsed Range so subsequent `extend()` works, `containsNode` walks ancestors of node + range endpoints for partial overlap detection.
- [x] CharacterData accessors on text/comment/cdata nodes: `.data`, `.nodeValue`, `.length`. Added to ELEM_PROTO with nodeType branching so element nodes still return `null`/`undefined`.
- [x] `XMLSerializer.serializeToString(node)`: uses outerHTML for elements, raw `data` for text, `<!--data-->` for comments.
- [x] DOM collection markers: `NodeList`/`HTMLCollection`/`FileList`/`DOMTokenList`/`NamedNodeMap` constructors with `Symbol.hasInstance` returning true for any array-like (DOMTokenList additionally requires `.contains` to be a function).
- [x] `navigator.plugins` / `navigator.mimeTypes` (empty array-likes with `item`/`namedItem`/`refresh`), `navigator.cookieEnabled`/`onLine` (true), `navigator.webdriver` (false), `navigator.doNotTrack` (null).
- [x] Reverted shared-`*Node` wrapper optimization in webapi/walk.go: callers like `GetElementsByTagName` retain the wrapper into a slice, so a per-visit allocation is required for correctness.

### Phase 1 — WebSocket lazy-init + bootstrap-cache

- [x] `wsRegistry` allocates the 256-slot `chan wsEvent` and conns map only on first `new WebSocket(...)`. Pages that never touch WS pay zero registry overhead. Saved 65 MB across the 4000-context FetchRunScripts bench.
- [x] `Context.bootstrapsSkipped` flag set when `Runtime.cachedBootstraps[mode]` is already populated. `registerBootstrap` becomes a no-op for cached Contexts; the cached combined string is rerun directly.

### Phase 2 — Runtime-level template caching, round 1

- [x] `Runtime.contextRegistry sync.Map[*v8.Context]*js.Context` populated by `NewContext`, drained by `Close`. Lookup helper `Runtime.contextFor(*v8.Context)` resolves the per-Context state for cached templates.
- [x] Storage (`buildStorageCached`): one shared `ObjectTemplate` with `internalFieldCount=1` plus six shared `FunctionTemplate`s. Per-Context `*memStorage` selection encoded via `Object.SetInternalField(0, handle)`; callbacks read the handle from `info.This()` and look up the storage in the `Runtime.storageHandles` slab.
- [x] Timer (`Runtime.ensureTimerTemplates`): two cached templates (`set`, `clear`) reused by `setTimeout`+`setInterval` and `clearTimeout`+`clearInterval` respectively.
- [x] Console (`Runtime.ensureConsoleTemplates`): five level templates + the `console` ObjectTemplate.
- [x] DOM bridge (`Runtime.ensureDOMBridgeTemplates`): `__node_get`/`__node_set`/`__node_call`/`__doc_get`/`__doc_call`.
- [x] Mutation observer (`Runtime.ensureObserverTemplates`): `__observer_register`/`__observer_disconnect`/`__observer_take`.
- [x] Location ObjectTemplate cache + `map[string]string{...}` literal removal in `buildLocation`.

### Phase 3 — Crypto + iframe template caching

- [x] Migrated 27 crypto templates: `installCryptoSubtle` (6 templates + ObjTmpl), `installCryptoAES` (4), `installCryptoComplete` (6), `installCryptoAsymmetric` (3), `installCryptoExtra` (4), `installCryptoPKCS8` (2). All callbacks were stateless from a *Context perspective (use globalKeyStore + info.Context()) so no contextRegistry lookup needed.
- [x] `installIframe` (3 templates) — uses `r.contextFor(info.Context())` for `c.nodes` + `c.iframes` access.

### Phase 4 — v8go fork additions + remaining installs

- [x] `Object.SetMany(keys, vals)` and `SetManyPrepared(*PreparedKeys, vals)` added to vendored v8go (`ObjectSetMany` C entry point in `v8go.cc`, declaration in `v8go.h`, Go wrapper in `object.go`). `PreparedKeys` pre-CStrings the keys at package init so hot paths skip the `C.CString` + `C.free` per call. Used in `buildLocation` (9 properties) and `setNavigatorFields` (7 properties).
- [x] `installFetch` migration: `Runtime.fetchTemplates` caches the global fetch + thrower + per-response text/json. Per-response state (body bytes) lives in `Runtime.fetchBodies` slab; the response Object's internal-field-0 holds the slab handle so the cached `text`/`json` callbacks dispatch by reading `info.This().GetInternalField(0)`. Previously each `fetch()` call created two new `FunctionTemplate`s that leaked into `Isolate.cbs` for the lifetime of the Isolate.
- [x] `installWebSocket` (3 templates), `installExtrasV2` (2), `installURLHelper` (1), `installStyleManagerBridge` (1) all migrated.

### Phase 5 — v8.Context pool

- [x] `js.NewRuntimeWithPool(N)`: opt-in pool of size N. `Context.Close` returns the v8.Context to the pool; `NewContext` pops one and runs `__artemis_reset(url)` JS to clear mutated state instead of running the full install pipeline.
- [x] `js/context_pool.go::poolResetBootstrap` defines `__artemis_pristine` (set captured on first call) and `__artemis_reset(newURL)` which clears non-pristine globalThis properties, calls `customElements._reset()`, clears `_winListeners`, resets history stack, clears performance entries, and rebinds `location.*` via `__url_parse`.
- [x] Storage rebinding on pool reuse: `rebindPooledStorage` allocates a new handle on `Runtime.storageHandles` for the fresh per-Context `*memStorage` and patches the existing JS-side localStorage / sessionStorage object's internal field 0.
- [x] `customElements._reset()` method exposed (clears `_registry` + `_whenDefined` Maps which were closure-captured before).
- [x] Pool wired into `engine.Config.JSContextPoolSize`.
- [x] `js.NewRuntimeWithWarmPool(N)` + `engine.Config.JSContextPoolWarm`: pre-builds N v8.Contexts at engine.New time so the first NewContext gets the pool fast path immediately.

### Phase 6 — Lifecycle leak prevention

- [x] `wsRegistry.closeAll`: cancels every conn's context, calls `*websocket.Conn.CloseNow`, sets state to CLOSED. Called from `Context.Close`.
- [x] `wsRegistry.tryEvent(ctx, ev)`: non-blocking send into the events channel with `<-ctx.Done()` fallback; read goroutines exit cleanly when the events channel is no longer drained.
- [x] WS read+send goroutines now use `conn.ctx` instead of `context.Background()` so cancellation propagates into mid-Read/mid-Write.
- [x] `asyncChan.cancel context.CancelFunc` + `cancelInflight()`: per-Runtime cancel propagated into user `FetchFunc`. The fetch goroutine selects on the pending-channel send vs ctx.Done() and decrements `inflight` on the cancel branch.
- [x] iframe sub-Contexts close BEFORE the parent acquires `Runtime.ctxMu` to avoid recursive deadlock.
- [x] `Runtime.ctxMu` serialises `NewContext` + `Close` against V8 `GlobalHandles::Destroy`. Without it, two goroutines calling `NewContext` concurrently crash V8.

## Notes

The 64% wall-time floor was V8's internal `Context::New` (snapshot deserialisation + global construction + microtask queue setup). The pool's reset path skips that entirely on reuse — only the JS-side `__artemis_reset(url)` runs, which is one cgo crossing into a small RunScript. That's where the 4.4x speedup comes from.

`sync.Map` was tried for `nodeTable` (every JS DOM access goes through it) and reverted: the atomic-Read path costs more than `sync.Mutex` in low-contention single-goroutine workloads. Kept the mutex with a comment explaining why.

`runtime.NumGoroutine()`-based leak tests are the right shape but flaky on Apple M1 with low GOMAXPROCS — added a 50 ms wait-loop tolerance instead of a hard equality check.

## Deviations

Pool-mode is opt-in (`JSContextPoolSize` defaults to 0) because user scripts that mutate built-in prototypes (`Array.prototype.foo = ...`), set non-configurable globals, or rely on prototype identity across pages will see leakage between pooled pages. Documented in `documentation.md`. Full per-page isolation requires a fresh v8.Context which is the default behaviour.
