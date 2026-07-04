# TASK 047: Performance benchmark baseline (best effort)

## Why

Quantify where Artemis stands against published demo numbers for comparable renderless engines.

## What was measured

`BenchmarkEndToEnd100Pages` and `BenchmarkEndToEnd100PagesNoScripts` in `engine/bench_e2e_test.go` fetch 100 realistic HTML pages from a local `httptest.Server`, parse + (optionally) run inline scripts + dump Markdown. Each page has:
- Title + 5 meta tags + JSON-LD
- Nav, article with H1 + 10 paragraphs (each containing inline links + emphasis)
- 8-item list
- Inline `<script>` that builds extra DOM via createElement+appendChild
- Footer

## Results (darwin/arm64, M-series, 2026-05-08)

| Bench | ms/100 pages | per-page | bytes/op | allocs/op |
|---|---|---|---|---|
| EndToEnd 100 with scripts | 84.5ms | **0.85ms** | 9.5MB | 162k |
| EndToEnd 100 no scripts | 78.4ms | **0.78ms** | 8.6MB | 117k |

Per-page engine cost (parse + JS context + script execution + Markdown dump) is well under 1ms in this localhost-only loop.

## Comparison to comparable engines

A comparable engine's published demo: **100 pages in ~5000ms = ~50ms/page on AWS m5.large**, running against real internet URLs.

The two numbers are not directly comparable:
- That 50ms/page includes real network RTT to the upstream sites (~30-50ms typical on AWS).
- Our 0.85ms/page excludes network RTT (httptest is in-process).
- The competitor runs more JS (full external scripts loaded from CDN); our pages have only inline scripts.

Apples-to-apples comparison would require:
1. Running the competitor binary against the same httptest fixtures to isolate engine cost
2. Running Artemis against the same real-world URL set with realistic network

Both are non-trivial harness work. **The headline conclusion** from the numbers we have: Artemis engine cost (parse + V8 + DOM + Markdown) is in the sub-millisecond range on a modern Apple Silicon host, comfortably below the 50ms competitor budget that's dominated by network. The remaining performance work (TASK 042 V8 snapshot, TASK 043 HTTP/2 pool, TASK 044 zero-copy) targets cross-process competitiveness rather than overcoming a fundamental gap.

## Sub-Tasks

- [x] e2e fixture generator
- [x] BenchmarkEndToEnd100Pages with + without scripts
- [x] document numbers
- [x] FLUSH
- [x] archive

## Notes

A side-by-side wall-clock harness against the competitor binary across 933 real URLs (its demo dataset) is a separate sub-task tracked under TASK 047b when we have appetite to set up the demo repo and run both processes head-to-head. Today's data is enough to assert "engine cost is in the right zone".
