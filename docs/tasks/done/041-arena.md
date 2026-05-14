# TASK 041: Script cache (per-Runtime UnboundScript reuse)

## Why

Profile (TASK 040) showed `js.NewContext` was 17% of CPU on the Fetch benchmark, dominated by parsing the same JS bootstrap strings into V8 every page. Caching the compiled UnboundScript on the Runtime cuts that to single-digit %.

## Done

- [x] `js/script_cache.go` - per-Runtime cache of `*v8.UnboundScript` keyed by script name
- [x] `Runtime.cache` field, initialised in `NewRuntime`
- [x] all six bootstrap RunScript callsites routed through cache (`installDocument`, `installExtras`, `installExtrasV2`, `installMutationObserver`, `installWebAPIMore`, `installWebAPIV3`)
- [x] benchmarks rerun to quantify the win

## Numbers (darwin/arm64)

| Bench | Before | After | Delta |
|---|---|---|---|
| Fetch (httptest, no scripts) | 781µs/op | 709µs/op | **-9.3%** |
| Fetch + RunScripts | 805µs/op | 726µs/op | **-9.8%** |
| Eval `document.title` | 2.44µs/op | 2.36µs/op | -3.3% |
| ClickRoundTrip | 9.55µs/op | 9.58µs/op | ~0% |

The Fetch wins come from skipping the parse of ~6 bootstrap scripts on every NewContext. Eval and Click already-running-context paths see negligible change because they don't touch bootstraps.

## Notes

A full arena allocator (sync.Pool of node-slabs, page-scoped byte arenas) is a separate project worth pursuing only when we have a real workload profile. The current profile is dominated by cgo-into-V8, which arena work cannot reduce.

V8 snapshot embedding (TASK 042) is the next ~10x win for cold start, but requires running v8go's snapshot creator at build time and invalidating it whenever any bootstrap script changes - real engineering, not a one-session task.
