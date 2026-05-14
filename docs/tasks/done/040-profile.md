# TASK 040: Profile + identify hot paths

## Why

Quantitative basis for performance work. Without a profile, optimisation is guesswork.

## Done

- [x] CPU profile via `go test -bench=. -cpuprofile`
- [x] heap profile via `-memprofile`
- [x] hot path analysis with `go tool pprof -top -cum`
- [x] documented numbers as session baseline

## Findings (darwin/arm64, M-series, 2026-05-08)

| Path | Share | Verdict |
|---|---|---|
| `runtime.cgocall` (V8 boundary) | 55% | dominant; fundamental cost of cgo + V8 |
| `v8.Context.RunScript` | 47% | every script crosses cgo |
| `js.NewContext` (Bootstrap-Scripts) | 17% | per-page cost; reduced by TASK 041 script cache |
| `engine.Engine.Fetch` | 17% | mostly V8 context build + HTTP |
| `engine.Page.Eval` | 18% | mostly RunScript |

Top remaining optimisation targets in priority order:
1. **V8 snapshot embedding** (TASK 042) - the biggest possible cold-start win, ~10x faster Context init.
2. **HTTP/2 multiplex pool** (TASK 043) - parallel subresource fetch with one connection.
3. **Zero-copy DOM strings** (TASK 044) - the parser already keeps source bytes; we copy once on every textContent read. Slice semantics could elide the copy.

SIMD (TASK 045) and assembly (TASK 046) are NOT next steps based on this profile - the cost is in cgo + JS, not in pure-Go byte loops.
