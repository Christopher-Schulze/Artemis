# TASK 009 (Phase 8): Performance benchmark suite

## Why

Benchmarks lock in current performance and surface regressions when later TASKs touch hot paths. Real Lightpanda comparison requires a wall-clock harness running their binary side-by-side, deferred; this TASK lands the Go-side benchmarks and a Makefile target.

## Acceptance

- `make bench` runs `go test -bench=. -benchmem` across `parser`, `webapi`, `agent`, `engine` (those packages with bench files) and prints results.
- Benchmark coverage: HTML parse, DOM walk, Markdown dump, Text extract, JS eval round-trip, Click event dispatch, Form submit (in-process httptest), Page Fetch end-to-end.
- All bench files compile and run; pass-criterion is "they finish without error" - thresholds are not enforced.

- [x] `parser/bench_test.go` (parse small + medium)
- [x] `agent/bench_test.go` (Markdown, Text, Links, Structured, Semantic)
- [x] `engine/bench_test.go` (Fetch, Fetch+Scripts, Eval, ClickRoundTrip)
- [x] `Makefile` `bench` target running `go test -bench=. -benchmem`
- [x] all benches finish without error
- [x] FLUSH documentation.md
- [x] archive

Sample numbers (darwin/arm64, M-series, 2026-05-08):
- ParseSmall: 2.1µs/op, 28 allocs
- ParseMedium (200 sections): 481µs/op
- Markdown 50 sections: 46µs/op
- Text: 12µs/op
- Eval `document.title`: 2.4µs/op
- ClickRoundTrip (JS dispatch + listener + DOM mutate): 8.9µs/op
- Fetch httptest end-to-end: 462µs/op

## Notes

Lightpanda comparison is intentionally deferred. Their benchmark harness lives in their `demo` repo; running it cross-process is a project of its own. Once the v1 surface is stable we can rerun their numbers locally.

## Deviations

(none yet)
