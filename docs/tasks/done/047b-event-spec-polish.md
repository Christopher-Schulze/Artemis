# TASK 047b: Event spec + extraction polish

## Why

Pre-snapshot polish round closing the gap between Artemis' rough event/extraction surface and the WHATWG/HTML living-standard expectations real-world sites depend on. Triggered by repeated "this should work but doesn't" findings while running scripted page fixtures.

## Acceptance

- CSS values inherit from parent for the inheritable property set (`color`, `font-*`, `text-*`, `line-height`, `cursor`, `visibility`, `direction`, `letter-spacing`, `word-spacing`, `white-space`).
- `AbortController.abort()` rejects an in-flight `fetch()` promise with a DOMException-shaped `AbortError`. Pre-aborted signals reject immediately.
- Same `*webapi.Node` returns the SAME JS wrapper handle across calls, so `el1 === el2` works and `Set`/`WeakSet` deduplicate correctly.
- `FormData` passed as `fetch(url, {body})` is auto-encoded as `application/x-www-form-urlencoded`; `Content-Type` is set when not provided.
- `addEventListener(type, fn, options)` honors `{capture: true}` and the boolean `useCapture` shorthand. Listener storage keys on (type, capture).
- Event dispatch fires capture-phase listeners walking root-to-target, then `AT_TARGET` listeners (both capture+bubble registered on the target itself), then bubble-phase listeners walking target-to-root.

## Sub-Tasks

- [x] CSS cascade: `inheritedProps` set + parent-walk in `styleManager.computed`. Tests cover deep inheritance + override.
- [x] `AbortController` / `AbortSignal`: signal-aborted check on `fetch` entry; rejection with `name: 'AbortError'`. Sync tests on pre-aborted signal.
- [x] Per-Context wrapper identity cache: `__id`-keyed map in JS bootstrap, `__wrap(id)` returns existing wrapper if present.
- [x] FormData detection in `parseFetchArgs`: detect via internal `_pairs` shape, call `toString()` to encode, set Content-Type if absent.
- [x] Capture-phase listeners + listener-options: rewrite `_listeners` storage as `Map<type, {capture: Set, bubble: Set}>`; dispatchEvent walks ancestors collecting capture, then target's both, then ancestors collecting bubble.
- [x] AT_TARGET phase fires both lists on target.

## Notes

The wrapper-identity fix exposed a separate latent bug in `GetElementsByTagName`: a shared `*Node` wrapper aliased every result to the last visited node. Reverted that walk-allocation optimization in webapi/walk.go in TASK 048's polish (kept here because the regression was caused by the wrapper-cache work).

Listener-options also includes `once` and `signal` — `once` is implemented (auto-remove after first fire); `signal` (AbortSignal-driven removal) is not. Documented as a known limitation.

## Deviations

None.
