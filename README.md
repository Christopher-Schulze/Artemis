# Artemis

> Hybrid headless browser engine in Go, built for AI agents. A fast renderless V8 path for maximum coverage, with a Chromium/CDP fallback for 100% compatibility.

[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

## Status

Working hybrid engine under active production-hardening. Builds and tests green (34 packages, race-clean, 72 benchmarks). The engine, scraper, stealth, solver, observation, input, security, profile, serve, and telemetry subsystems are implemented in Go. Dual-mode: embedded library (in-process) and standalone server (`artemis serve`, JSON-over-WebSocket). Renderless V8 fast path handles ~0.184 ms/page (2.7x faster than the published competitor number); Chromium/CDP fallback for full browser semantics.

## Why

Crawling the modern web requires running JavaScript, and running Chromium-Headless at scale is expensive: high RAM, high CPU, hard to package. Artemis handles as much of the web as possible on a from-scratch renderless V8 path (no layout, no compositor, no paint) that is fast and cheap, and escalates to a real Chromium browser via CDP only when a page genuinely needs full browser semantics. One package, one binary, automatic routing.

## Architecture

Artemis is a hybrid controlled by a deterministic execution router:

- **Renderless fast path** (`renderless/`, `engine/`, `js/`, `webapi/`, `css/`, `parser/`): V8 via `rogchap.com/v8go` (stock V8, no Chromium) with an isolate snapshot + context pool, a from-scratch DOM/WebAPI surface, CSS parse/cascade/computed style, `fetch`/XHR, cookies, and agent extraction (markdown, semantic tree, structured data, links, forms, actions).
- **Chromium/CDP fallback** (`bridge/`, `bridge/cdpops/`, `bridge/actions/`, `bridge/tabs/`): controls a real Chromium child process via CDP (`chromedp` + `cdproto`) for layout, screenshots, canvas/media, WebAuthn, CAPTCHA and hardened sites.
- **Provider** (`bridge/provider.go`): routes `static_fetch -> renderless_js -> chromium_cdp -> stealth -> scrape`, failing closed upward, never silently.
- **Supporting subsystems**: `stealth/` (3-level anti-detection, fingerprint patches, HTTP/2 parity), `solver/` (vision-based challenge solving), `observe/` (AX-tree snapshots + diff, network + console buffers), `input/` (Bezier human-like input), `security/` (SSRF, indirect-prompt-injection defense, ad blocking), `profile/` (encrypted multi-profile, sessions, geo-presets, session-proxy), `scraper/` (adaptive selectors + AI element finding), `serve/` (JSON-over-WebSocket steering server), `telemetry/` (local OpenTelemetry, opt-out phone-home via `ARTEMIS_DISABLE_TELEMETRY=true`).

See [`docs/documentation.md`](docs/documentation.md) for the full architecture and module reference.

## Usage

Artemis ships dual-mode:

- **Embedded library**: import the Go packages and drive the engine in-process.
- **Standalone server**: `artemis serve` exposes a JSON-over-WebSocket steering protocol so external agents can drive it over the wire.

## Build

Requires Go 1.26+ and a C toolchain (for V8 via cgo).

```sh
make build
```

## License

[MIT](LICENSE). Artemis is original Go code.
