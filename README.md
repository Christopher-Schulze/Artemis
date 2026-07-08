<p align="center">
  <img src="assets/logo.png" alt="Artemis" width="440">
</p>

<h1 align="center">Artemis</h1>

> **The headless browser that let's your Agent step out of the frame and hunt the web** A from-scratch renderless V8 engine in Go that runs the real web at a fraction of Chromium's cost, and escalates to a full Chromium/CDP browser only when a page truly needs it. One package, one binary, automatic routing.

[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Go 1.26+](https://img.shields.io/badge/Go-1.26+-00ADD8.svg)](https://go.dev)
[![Tests: race-clean](https://img.shields.io/badge/tests-race--clean-brightgreen.svg)](#quality)

Artemis gives an autonomous agent everything it needs to *perceive and act on* the web: fetch and run JavaScript, read a page as clean Markdown or a semantic tree, extract structured data, fill forms, click, scroll, log in, defeat bot-detection, solve challenges, and stream it all to a driving agent over a wire protocol.

---

## Why Artemis

Crawling the modern web means running JavaScript, and running Chromium-Headless at scale is expensive: gigabytes of RAM, heavy CPU, painful to package and operate. Most pages don't need a full browser; they need a correct DOM, working `fetch`/XHR, cookies, and JS execution.

Artemis handles as much of the web as possible on a **renderless V8 fast path**: stock V8 with a from-scratch DOM/WebAPI surface but **no layout, no compositor, no paint**, which is fast and cheap. It escalates to a **real Chromium browser via CDP** only when a page genuinely needs full browser semantics (layout, screenshots, canvas/media, WebAuthn, hardened anti-bot). A deterministic execution router picks the cheapest path that will work and fails closed upward, never silently.

The result: near-browser fidelity at a fraction of the cost, in a single self-contained Go binary.

## Highlights

- **Hybrid engine, automatic routing:** `static_fetch → renderless_js → chromium_cdp → stealth → scrape`, cheapest-viable-path first, fail-closed.
- **~0.184 ms/page** on the renderless fast path (**2.7x faster** than the ~0.5 ms/page published baseline), backed by a warm V8 isolate-snapshot and context pool.
- **Agent-native extraction:** clean Markdown, semantic tree, structured data (JSON-LD/microdata), links, forms, and actionable elements, ready to feed an LLM.
- **Dual-mode:** embed the Go packages in-process, or run `artemis serve` and drive it from any language over JSON-over-WebSocket.
- **Serious stealth:** three-level anti-detection, fingerprint patches, HTTP/2 parity, human-like Bezier input, CDP-evasion.
- **Secure by default:** SSRF guards, indirect-prompt-injection defense, ad/tracker blocking, encrypted multi-profile isolation.
- **Benchmarked and race-clean:** 34 packages, 73 benchmarks, `-race` green.

## Architecture

Artemis is a hybrid controlled by a deterministic execution router:

- **Renderless fast path** (`renderless/`, `engine/`, `js/`, `webapi/`, `css/`, `parser/`): V8 via `rogchap.com/v8go` (stock V8, no Chromium) with an isolate snapshot and context pool, a from-scratch DOM/WebAPI surface, CSS parse/cascade/computed style, `fetch`/XHR, cookies, and agent extraction.
- **Chromium/CDP fallback** (`bridge/`, `bridge/cdpops/`, `bridge/actions/`, `bridge/tabs/`): drives a real Chromium child process via CDP (`chromedp` + `cdproto`) for layout, screenshots, canvas/media, WebAuthn, CAPTCHA and hardened sites.
- **Execution router** (`bridge/provider.go`): routes across the path ladder, failing closed upward, never silently.

### Subsystem map

| Subsystem | Package(s) | What it does |
|-----------|------------|--------------|
| Renderless engine | `renderless/`, `engine/`, `js/`, `webapi/` | V8 execution, from-scratch DOM/WebAPI, `fetch`/XHR, cookies |
| Styling | `css/`, `parser/` | HTML parse, CSS parse/cascade/computed style |
| Chromium bridge | `bridge/`, `bridge/cdpops/`, `bridge/tabs/` | Real browser via CDP for full semantics |
| Router | `bridge/provider.go` | Cheapest-viable-path routing, fail-closed |
| Stealth | `stealth/`, `network/` | Three-level anti-detection, fingerprint patches, HTTP/2 parity |
| Solver | `solver/` | Vision-based challenge / CAPTCHA solving |
| Observation | `observe/` | AX-tree snapshots + diff, network + console buffers |
| Input | `input/` | Human-like Bezier mouse/keyboard input |
| Actions | `actions/`, `bridge/actions/` | High-level page actions (login, forms, navigation) |
| Security | `security/` | SSRF defense, indirect-prompt-injection defense, ad/tracker blocking |
| Profiles | `profile/` | Encrypted multi-profile, sessions, geo-presets, session-proxy |
| Scraper | `scraper/` | Adaptive selectors + AI element finding |
| Serve | `serve/` | JSON-over-WebSocket steering server |
| Telemetry | `telemetry/` | Local OpenTelemetry, opt-out (`ARTEMIS_DISABLE_TELEMETRY=true`) |

See [`docs/documentation.md`](docs/documentation.md) for the full architecture and module reference.

## Benchmarks

Measured on the renderless fast path with a warm context pool:

| Metric | Result |
|--------|--------|
| Page fetch + JS + extract | **~0.184 ms/page** |
| vs. published baseline (~0.5 ms/page) | **2.7x faster** |
| Packages | 34 |
| Benchmarks | 73 |
| Concurrency | `-race` clean |

Reproduce with `make bench`. The head-to-head competitor harness (`benchmark/`) downloads the competitor binary at benchmark time and runs it as an external black box.

## Usage

Artemis ships dual-mode.

**Embedded library** drives the engine in-process:

```go
import "github.com/Christopher-Schulze/Artemis/engine"

eng, err := engine.New(engine.Config{})
// handle err

page, err := eng.Fetch(ctx, "https://example.com", engine.FetchOpts{})
// handle err

markdown := page.Markdown() // clean, LLM-ready page text
```

**Standalone server** exposes the JSON-over-WebSocket steering protocol so an agent in any language can drive it over the wire:

```sh
artemis serve
```

## Build

Requires **Go 1.26+** and a C toolchain (for V8 via cgo).

```sh
make build      # build the artemis binary
make test       # run the test suite
make test-race  # run with the race detector
make bench      # run the benchmark suite
```

## Quality

- 34 packages, 73 benchmarks, race-detector clean.
- Deny-by-default security: SSRF, prompt-injection, and ad/tracker defenses are on by default.

## License

Artemis is released under the **MIT License**. You are free to use, copy, modify, merge, publish, distribute, sublicense, and sell copies, in both open-source and commercial projects, provided the copyright notice and this permission notice are included in all copies or substantial portions of the software.

Copyright © 2026 Christopher Schulze. See [LICENSE](LICENSE) for the full text.
