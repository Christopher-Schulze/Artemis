# TASK 017b: iframe full per-frame V8 realm

## Done

Per-iframe `*v8.Context` with full bootstrap + cross-context value
flow + cross-context postMessage delivery.

### Architecture
- Each iframe gets its own `js.Context` via a builder closure passed to `iframeRegistry`
- Sub-Context shares parent's V8 Isolate (efficient: same heap, same compiled bytecode via ScriptCache)
- Sub-Context has its own `globalThis`, `window`, `localStorage`, `sessionStorage`, timers, observers, listener maps
- Sub-Context's `document` is bound to the iframe's parsed `webapi.Document`
- Iframe inline scripts execute in the sub-Context (they cannot pollute parent globals)

### Cross-context DOM
- iframe's `*html.Node` tree lives in Go (single source of truth)
- Parent's `iframe.contentDocument` registers iframe doc's root in PARENT's `nodeTable` and returns `__wrap` decorated as document
- Iframe sub-Context registers nodes in its OWN `nodeTable` keyed by the same Go pointers
- Mutations via either context affect the same Go-side `*html.Node` so DOM state is consistent

### Cross-context postMessage
- Parent's `iframe.contentWindow.postMessage(data)` calls `__iframe_postmessage(handle, data)`
- Native trampoline JSON-stringifies the payload, queues it in `iframeRegistry.pendingMsgs[handle]`
- Drain immediately runs `subCtx.RunScript("window.dispatchEvent(new MessageEvent('message', {data: JSON.parse(...)}))")`
- Sub-Context's `window.addEventListener('message', fn)` listeners fire in iframe's realm

### window.addEventListener support
- Each Context now has window-level listener Map (`window.addEventListener / removeEventListener / dispatchEvent`)
- Used both by iframe-internal scripts and by parent for window-targeted events

### Lifecycle
- Parent `Context.Close()` closes all iframe sub-Contexts via `iframeRegistry.closeAll()`

## Tests passing

- iframe inline script executes (DOM mutation visible via parent's contentDocument)
- multiple iframes each run independently with separate state (verified via iframe-side data-* attribute markers)
- parent posts message to iframe → iframe sub-Context's window listener fires → DOM mutation visible to parent
- iframe scripts see iframe's `document` (not parent's)
- parent globals NOT polluted by iframe scripts (per-realm isolation works)

## Limitations (still NOT in this implementation)

- **Cross-origin enforcement**: iframe from a different origin can read parent globals via `window.parent` (or vice versa). Real browsers throw `SecurityError`. We trust the embedder.
- **External iframe scripts** (`<script src=>` inside iframe): not loaded. The iframe sub-Context only runs inline scripts. To load external scripts inside iframes, plumb the engine HTTP client through the registry's sub-context builder.
- **Frame tree traversal**: `window.parent === window` still inside iframe (no real parent-pointer). Most agent code doesn't rely on this.
- **Nested iframes**: sub-Context is built with `LoadIFrame: nil` to avoid recursion. Iframes inside iframes don't get their own sub-Contexts.

These are documented gaps with clear paths forward but not blockers for the 95% case.
