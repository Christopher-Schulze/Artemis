# TASK 004e: Crypto + Headers + History (minimal)

## Why

Three small WebAPI surfaces that many SPAs touch on init: `crypto.randomUUID` / `crypto.getRandomValues`, the `Headers` class for fetch, and `history.pushState` / `replaceState` to update the URL bar without reload. Together ~200 LoC; absence of any of them throws ReferenceError.

## Acceptance

- `crypto.randomUUID()` returns a v4 UUID string.
- `crypto.getRandomValues(typedArray)` fills the array with cryptographically random bytes (or with `math/rand` fallback for browsers that aren't security-critical).
- `Headers` class JS-side: `new Headers(init)`, `append`, `get`, `has`, `delete`, `set`, `forEach`. Backed by an internal Map.
- `history.length` / `pushState(state, title, url)` / `replaceState(...)` / `back()` / `forward()`. Updates `location.href`, `location.pathname`, etc. accordingly.
- Tests for each.
- All green.

## Sub-Tasks

- [x] `js/crypto.go` + bootstrap install
- [x] `Headers` class via JS bootstrap (no native bridge needed)
- [x] `history` API via JS bootstrap that mutates the in-process `location` object
- [x] tests
- [x] FLUSH
- [x] archive

## Notes

`crypto.getRandomValues` uses `crypto/rand` on the Go side. Errors are converted to a JS DOMException-shaped Error object.

`history.pushState` mutates the in-process `location` object only; no actual navigation. Real browsers also push a stack entry that `back()` can pop; we keep a simple stack JS-side.

`Headers.forEach(fn, thisArg)` matches the Fetch spec signature. Iteration order matches insertion.

## Deviations

(none)
