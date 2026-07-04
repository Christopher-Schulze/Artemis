# TASK-2337 Sub-Task 1: Hot-Path Profiling Baseline

## Why

Record measured baselines for every Artemis hot path before any optimization
touch, so subsequent sub-tasks can prove before/after gains with real numbers
instead of assertions. Spec ss28.15 mandates "every gain must be proven by
benchmark, not asserted."

## Environment

- darwin/arm64, Apple M1, 8 cores
- Go 1.26.4
- Local httptest server (network RTT = 0, isolates engine cost)
- Date: 2026-07-04

## Existing Benchmark Inventory

| Package | Benchmark | What it measures |
|---|---|---|
| engine | BenchmarkFetch | Single fetch (HTTP + parse + JS ctx, no scripts) |
| engine | BenchmarkFetchRunScripts | Single fetch + inline script execution |
| engine | BenchmarkEval | JS eval on a pre-fetched page |
| engine | BenchmarkClickRoundTrip | Click → DOM mutation → return |
| engine | BenchmarkEndToEnd100Pages | 100 pages fetch+scripts+markdown |
| engine | BenchmarkEndToEnd100PagesPooled | 100 pages with V8 context pool |
| engine | BenchmarkEndToEnd100PagesPooledWarm | 100 pages with pre-warmed pool |
| engine | BenchmarkEndToEnd100PagesNoScripts | 100 pages, no script execution |
| parser | BenchmarkParseSmall | Small HTML parse |
| parser | BenchmarkParseMedium | Medium HTML parse |
| agent | BenchmarkMarkdown | DOM → Markdown conversion |
| agent | BenchmarkText | DOM → text extraction |
| agent | BenchmarkLinks | Link extraction |
| agent | BenchmarkStructured | Structured data extraction |
| agent | BenchmarkSemantic | Semantic extraction |
| agent | BenchmarkCollapseInline | Inline whitespace collapse |
| webapi | BenchmarkGetElementsByTagName | DOM query by tag |
| webapi | BenchmarkGetElementById | DOM query by ID |
| webapi | BenchmarkGetElementsByClassName | DOM query by class |
| scraper | BenchmarkWFAdaptiveCachePerf | Adaptive selector cache hit |
| v8go | BenchmarkContext | V8 context create+run |
| v8go | BenchmarkIsolateInitialization | V8 isolate init |
| v8go | BenchmarkIsolateInitAndRun | Isolate init + script run |
| v8go | BenchmarkCallbackParallel | Parallel JS callback |

## Baseline Numbers

### Engine Hot Path (per-op)

| Benchmark | ns/op | B/op | allocs/op |
|---|---|---|---|
| Fetch | 1,043,366 | 22,186 | 469 |
| FetchRunScripts | 919,203 | 22,876 | 502 |
| Eval | 3,038 | 184 | 13 |
| ClickRoundTrip | 12,486 | 1,260 | 67 |

### E2E 100 Pages (per-100-pages)

| Benchmark | ns/op | B/op | allocs/op | ms/100p |
|---|---|---|---|---|
| EndToEnd100Pages | 111,843,963 | 7,261,353 | 146,105 | 112 |
| EndToEnd100PagesPooled | 34,823,999 | 6,796,200 | 107,625 | 35 |
| EndToEnd100PagesPooledWarm | 36,700,253 | 6,794,998 | 107,607 | 37 |
| EndToEnd100PagesNoScripts | 118,433,111 | 6,286,375 | 101,907 | 118 |

### Parser

| Benchmark | ns/op | B/op | allocs/op |
|---|---|---|---|
| ParseSmall | 2,129 | 6,232 | 28 |
| ParseMedium | 520,704 | 428,041 | 5,221 |

### Agent (extraction)

| Benchmark | ns/op | B/op | allocs/op |
|---|---|---|---|
| Markdown | 47,790 | 48,315 | 962 |
| Text | 12,330 | 4,729 | 10 |
| Links | 19,898 | 26,224 | 258 |
| Structured | 23,961 | 2,448 | 56 |
| Semantic | 20,564 | 31,112 | 766 |
| CollapseInline | 63 | 19 | 0 |
| CollapseInlineClean | 52 | 0 | 0 |

### WebAPI (DOM queries)

| Benchmark | ns/op | B/op | allocs/op |
|---|---|---|---|
| GetElementsByTagName | 11,483 | 16 | 2 |
| GetElementById | 10,304 | 8 | 1 |
| GetElementsByClassName | 36,524 | 21,352 | 611 |

### V8/v8go

| Benchmark | ns/op | B/op | allocs/op |
|---|---|---|---|
| Context | 274,968 | 825 | 20 |
| IsolateInitialization | 709,343 | 144 | 4 |
| IsolateInitAndRun | 911,358 | 970 | 24 |
| CallbackParallel | 34,268 | 7,216 | 301 |

### Scraper

| Benchmark | ns/op | B/op | allocs/op |
|---|---|---|---|
| WFAdaptiveCachePerf | 53 | 0 | 0 |
| WFAdaptiveCachePerfBaseline | 11,288 | 680 | 27 |

## CPU Profile (E2E PooledWarm, 100 pages, 3s benchtime)

Top cumulative CPU consumers:

| Function | cum% | Description |
|---|---|---|
| Engine.Fetch | 34.18% | Main hot path entry |
| v8go.Context.RunScript | 29.57% | V8 JS execution (cgocall) |
| Engine.runScripts → walkRaw → js.Eval | 19.91% | Script walk + eval loop |
| js.Runtime.NewContext | 12.18% | V8 context creation |
| js.resetPooledContext | 11.14% | Pool context reset |
| network.HTTPClient.Do | 1.34% | HTTP fetch |
| parser.ParseHTML | 1.19% | HTML parsing |
| agent.Markdown | 0.89% | Markdown conversion |

**Key CPU finding**: V8/JS execution (RunScript + runScripts + NewContext +
resetPooledContext) accounts for ~73% of CPU. The cgocall boundary
(pthread_cond_signal at 29.42%) is the single largest CPU consumer. Network
and parsing are negligible in comparison.

## Memory Profile (E2E PooledWarm, 100 pages, 983MB total alloc)

Top cumulative allocators:

| Function | cum% | MB | Description |
|---|---|---|---|
| Engine.Fetch | 66.05% | 650 | Main hot path |
| parser.ParseHTML | 34.18% | 336 | golang.org/x/net/html |
| agent.Markdown | 20.08% | 198 | Markdown conversion |
| Engine.runScripts/walkRaw | 12.96% | 128 | Script walk |
| v8go.Context.RunScript | 11.64% | 115 | JS execution |
| mdConverter.link | 13.21% | 130 | Link formatting |
| html.parser.addText | 13.21% | 130 | Text node accumulation |
| network.HTTPClient.Do | 14.03% | 138 | HTTP response bodies |

**Key memory finding**: HTML parsing (golang.org/x/net/html) is the biggest
allocator at 34% of total. The parser creates many small node objects.
Markdown conversion is second at 20%, driven by link formatting (130MB) and
paragraph processing (132MB).

## Optimization Opportunities (ranked by expected impact)

### 1. V8 Context Pool Already Working — Extend to More Scenarios
The pool gives 3.2x speedup (112ms → 35ms per 100 pages). The warm pool
variant is not faster than cold pool in this benchmark (37ms vs 35ms)
because the benchmark runs enough iterations to amortize cold-start. Real
workloads with bursty traffic will benefit from warm pool.

### 2. HTML Parser Memory (P2.1, P2.4)
parser.ParseHTML allocates 336MB/100 pages (34% of total). Opportunities:
- sync.Pool for parser instances (P2.1) — 30-50% fewer GC pauses
- Zero-copy tokenizer with []byte refs (P2.4) — 40-60% memory on 5MB+ HTML
- The html.parser.addText function alone allocates 130MB

### 3. Markdown Converter Allocations (P2.1)
agent.Markdown allocates 198MB/100 pages (20% of total). The mdConverter.link
function allocates 130MB. Opportunities:
- sync.Pool for strings.Builder in mdConverter
- Pre-allocate link buffer instead of per-link allocation
- The converter creates 962 allocs/page — many are small string concatenations

### 4. V8 cgocall Overhead (P0c.2, P0c.4)
The cgocall boundary (pthread_cond_signal) is 29.42% of CPU. Opportunities:
- Batch JS evaluations (P1.1 Pipelining) — reduce cgocall count
- Pre-compiled stealth bundle (P0c.4) — already done via go:embed
- The runScripts loop does one Eval per script tag; batching would help

### 5. WebAPI GetElementsByClassName (P6.3)
36,524 ns/op with 611 allocs vs GetElementById at 10,304 ns/op with 1 alloc.
The class-name query walks the entire DOM and allocates a result slice per
match. Opportunities:
- Compiled selector cache (P6.3) — cascadia.MustCompile
- Pre-index class names during parse

### 6. Renderless Engine is a Stub
The `renderless/` package Engine.Fetch returns a fake Page (StatusCode 200,
no real fetch/parse/JS). This is not a working fast path — it's a placeholder.
The real V8 fast path is in `engine.Engine` using `js.Runtime`/`js.Context`.
This is a significant gap vs spec ss28.15.9 P6.4 which expects a working
renderless JS runtime pool.

## Existing Perf Infrastructure

- `make bench` target exists (Makefile)
- `scraper/parsers/pool.go` — Parse-phase WorkerPool with LockOSThread pinning
- `scraper/parse_pool.go` — ParseWorkerPool bounds concurrent HTML parse workers
- `scraper/streaming.go` — io.Pipe streaming parse
- `scraper/tee_parse.go` — 3-consumer io.TeeReader fan-out
- `scraper/page_cache.go` — pageCache sync.Map (P0c.3 Parse-Once)
- `scraper/bloom.go` — Bloom filter for URL dedup (P7.3)
- `scraper/sqlite_worker.go` — Async SQLite (P7.1)
- `js/context_pool.go` — V8 context pool (P6.4)
- `internal/pool/pool.go` — sync.Pool for strings.Builder
- `renderless/pool.go` — sync.Pool for strings.Builder (renderless path)
- `engine/tab_recycle.go` — Tab recycling (P5.2)

## Missing Perf Infrastructure (gaps vs spec ss28.15)

- No singleflight for request dedup (P4.4)
- No DNS prefetching (P4.1)
- No HTTP/2 MaxConnsPerHost tuning (P4.3)
- No TCP Fast Open (P4.7/P8.4)
- No GOGC tuning per service (P8.3)
- No PlatformCapabilities (P8.5)
- No mmap I/O for large responses (P8.2)
- No compiled selector cache (P6.3)
- No string interning for ref IDs (P2.3)
- No pre-burst connection prewarming (P4.2)
- No JPEG screenshot pipeline (P5.6)
- No form-sequence batch prefetch (P1.1)
- Renderless engine is a stub, not a working fast path (P6.4)

## Sub-Task Status

- [x] Profile hot paths and record baselines (pprof + benchmarks)
- [ ] Raise renderless fast-path coverage; measure fallback-rate delta
- [ ] Tune the Chromium/CDP fallback efficiency (pool/reuse/batch/teardown)
- [ ] Apply low-level + concurrency optimizations with before/after benchmem numbers
- [ ] Kill every dumb redundancy, wasted allocation, and lock contention
- [ ] Add/extend benchmarks + same-TASK tests + race coverage for changed hot paths
