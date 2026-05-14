# TASK 017b-1: iframe scripts execute (leaky shared-realm)

## Why

Real per-frame V8 realm (TASK 017b-2) is 2-3 weeks of architecture work.
Step 1 unlocks the 70% case (embed widgets bootstrap themselves into
`iframe.contentDocument`) without separate Contexts.

## Done

- [x] iframe inline `<script>` tags now execute when `iframe.contentDocument` is first accessed
- [x] iframe scripts see iframe's `document` (not parent's) via globalThis.document swap with save/restore
- [x] script body wrapped in `new Function(body)()` so the parent context's variable bindings are not polluted by iframe script `var` declarations (still leaky for window globals)
- [x] errors inside iframe scripts swallowed (don't break parent flow)
- [x] tests: iframe builds its content via document.getElementById().innerHTML; multiple iframes each run independently

## Limitations (vs full 017b)

- iframe scripts share parent's `window`, `localStorage`, timers, custom globals - they leak in both directions
- `window.parent === window` still holds inside iframe scripts (no real frame tree)
- No cross-origin enforcement: iframe from different origin can read parent globals (security model trusts the embedder)
- External `<script src=...>` inside iframe NOT loaded (would need plumbing engine HTTP into registry)
- iframe scripts run synchronously when `contentDocument` first accessed, not on parse like real browsers

## Real per-frame V8 realm (deferred)

To get true isolation we need:
1. Per-iframe `*v8.Context` (separate global, separate scope)
2. Cross-context value marshaling for `iframe.contentDocument` from parent
3. Per-Context bootstrap (or shared via V8 snapshot - TASK 042)
4. Cross-Context postMessage queue
5. Origin parsing + same-origin policy enforcement
6. Frame tree (`window.parent`/`top`/`frames` traversal)
7. iframe `load`/`unload` event lifecycle

Estimated: 2-3 weeks. Marked TASK 017b-2.
