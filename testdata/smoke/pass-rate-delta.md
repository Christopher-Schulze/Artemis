# Artemis Live Web Hardening — Pass-Rate Delta (TASK-2328)

## Smoke Matrix Pass-Rate Delta

| Metric | TASK-2325 (before) | TASK-2328 (after) | Delta |
|--------|-------------------|-------------------|-------|
| Total scenarios | 10 | 10 | 0 |
| Passed | 7 (first run) → 9 (after fixes) | 9 | 0 |
| Failed | 2 (first run) → 0 (after fixes) | 0 | 0 |
| Skipped | 1 (login env-gated) | 1 (login env-gated) | 0 |
| Non-skipped pass rate | 7/9 (77.8%) → 9/9 (100%) | 9/9 (100%) | 0 |

## Failure Classes Found and Fixed

### 1. Text Drift (nav-example)
- **Failure:** example.com body text changed from "illustrative examples" to "documentation examples", breaking an exact-match assertion.
- **Fix (TASK-2325):** Updated scenario YAML to use `text_contains` with the actual current text.
- **Regression test (TASK-2328):** `serve/text_drift_regression_test.go` — `TestTextDriftTolerantAssertion` (7 subtests) + `TestTitleContainsDriftTolerant`. Verifies that `text_contains` assertions are drift-tolerant via partial substring matching. No live-network dependency.

### 2. WS Read Limit (challenge-cloudflare)
- **Failure:** WebSocket read limit (32KB default) was too small for large HTML dumps from challenge-prone sites (nowsecure.nl). The page.dump response exceeded the limit and the WS read failed.
- **Fix (TASK-2325):** Raised WS read limit to 8MB on both server (`serve/server.go`) and client (`cmd/artemis-smoke/runner.go`).
- **Regression test (TASK-2328):** `serve/ws_readlimit_regression_test.go` — `TestWSReadLimitLargePageDump` (~100KB HTML) + `TestWSReadLimitBoundary` (~500KB HTML). Verifies that page dumps exceeding the old 32KB limit succeed over the wire. Also fixed the `dial` test helper to set the 8MB read limit on client connections. No live-network dependency.

### 3. Selector Drift (prophylactic)
- **Failure class:** No current selector-drift failures in the smoke matrix (all scenarios use stable CSS selectors). However, selector drift is the most common failure class for adaptive scrapers on the real web.
- **Regression test (TASK-2328):** `scraper/finder_drift_regression_test.go` — 5 tests covering the full adaptive Finder chain (CSS → XPath → Text → Attribute → Structural heuristic). `TestFinderSelectorDriftCSSFailureTextFallback` proves that when a CSS selector breaks due to a site redesign, the text-fallback stage still locates the element. No live-network dependency.

## Deterministic Regression Coverage

All regression tests use inline HTML fixtures or httptest servers — no live-network dependency in the committed test suite. This satisfies the acceptance criterion: "no live-network dependency in the committed test suite."

| Test File | Tests | Failure Class |
|-----------|-------|---------------|
| `scraper/finder_drift_regression_test.go` | 5 | Selector drift |
| `serve/ws_readlimit_regression_test.go` | 2 | WS read limit |
| `serve/text_drift_regression_test.go` | 9 (7+2) | Text drift |
| **Total** | **16** | |
