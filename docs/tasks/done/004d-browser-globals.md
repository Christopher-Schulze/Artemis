# TASK 004d (Phase 3d): Browser globals

## Why

The bindings that matter most for SPA compatibility, after DOM mutation + events + fetch, are the browser globals: `window`, `window.location`, `window.navigator`, `window.localStorage`, `window.sessionStorage`, and `setTimeout` / `clearTimeout`. Most SPA frameworks reference at least three of these on init. With them missing the script throws a `ReferenceError` and the page stays half-rendered. This TASK lands a working subset, scoped to read-only location, read-only navigator, in-memory storage, and a synchronous-style timer queue that fires registered callbacks at script-boundary.

## Acceptance

- `window` global exposed; `window === globalThis` in JS so `window.X` and `X` are the same. Convenient aliases: `window.document`, `window.location`, `window.navigator`, `window.localStorage`, `window.sessionStorage`.
- `window.location` (read-only): `href`, `protocol`, `host`, `hostname`, `port`, `pathname`, `search`, `hash`, `origin`. Built from the page's URL at Context creation.
- `window.navigator` (read-only): `userAgent`, `language`, `languages`, `platform`. Configurable via `js.ContextOpts.Navigator` (defaults supplied).
- `window.localStorage` and `window.sessionStorage`: Storage-API-shaped (`length`, `getItem`, `setItem`, `removeItem`, `clear`, `key`). In-memory per Context. Both are independent stores.
- `setTimeout(fn, ms)` returns an int handle; `clearTimeout(handle)` cancels. Callbacks queue; the Context fires the queue after every Eval and every inline script. Delays are not simulated; ordering follows queue order.
- `setInterval` and `clearInterval` exposed for compatibility but `setInterval` fires the callback once (documented limitation); a real interval needs a wall-clock loop and a future TASK delivers that.
- `make build`, `make vet`, `make test`, `go test -race ./js ./engine` all green.

- [x] mark active, write detail
- [x] `js/window.go` - location, navigator, memStorage, timerQueue, installWindow / installTimers
- [x] `ContextOpts.Navigator` of type `NavigatorConfig` with defaults
- [x] `Context.Eval` calls `fireTimers` after RunScript so setTimeout-scheduled callbacks run before Eval returns
- [x] tests: window === globalThis; location parts (href/protocol/host/hostname/port/pathname/search/hash/origin); navigator defaults + override (UA, language, languages, platform); localStorage roundtrip + key + clear + remove; local vs session independence; setTimeout fires; clearTimeout cancels; chained setTimeout in callback also runs
- [x] all green: build, vet, test, race
- [x] FLUSH documentation.md
- [x] archive

## Notes

`setInterval` semantics are deliberately wrong-but-close-enough. A real interval requires a goroutine + cross-thread Promise/callback resolution; that is the same plumbing as TASK 004c2 (parallel async). When 004c2 lands, intervals can become real and we revisit this TASK's docs.

`window.location` is read-only in 004d. Setting `location.href = "/foo"` or calling `location.assign(...)` should arguably navigate to a new page; in our agent-driven model navigation is something the agent decides, not a JS-side side effect. We expose getters only; setters and methods like `assign`, `replace`, `reload` arrive in 004e if needed.

Storage is per Context (per page load). Real browsers persist localStorage across page loads; an embedder that wants persistence can implement `js.Storage` interface and pass via `ContextOpts.LocalStorage` / `SessionStorage`.

Reference: internal design notes.

`Storage.length` is exposed as a snapshot integer at install time rather than as a live getter. v8go does not expose accessor properties on `Object` instances, only on `ObjectTemplate`, and our storage objects are built via NewInstance to close over Go-side pointers. The snapshot goes stale after `setItem`/`removeItem`/`clear`. Workaround: a `Storage.lengthOf()` method always returns the live length. Documented in documentation.md so SPA code that relies on `localStorage.length` is told the right name. A future TASK can move storage to template-based accessor properties when v8go grows the API or when we ship a JS-side facade similar to the DOM bootstrap.

`setInterval` is aliased to `setTimeout`: the callback fires once per scheduled boundary, not on a wall-clock interval. Real intervals require a goroutine + cross-thread Promise/callback resolution; that ships with TASK 004c2.

`window.location` is read-only. Setting `location.href = ...` and calling `assign / replace / reload` are no-ops in 004d; they belong to navigation, which is an agent-level decision, not a JS-side side effect.

## Deviations

`Storage.length` snapshot vs. live - documented above; `lengthOf()` workaround.
