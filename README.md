<p align="center">
  <img src="assets/logo.png" alt="Artemis" width="440">
</p>

<h1 align="center">Artemis</h1>

> **A renderless web engine built for AI agents.** Artemis fetches HTML, executes JavaScript in V8, maintains a DOM, and extracts agent-ready content without a layout or paint engine.

[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Go 1.27+](https://img.shields.io/badge/Go-1.27+-00ADD8.svg)](https://go.dev)
[![Tests: race-clean](https://img.shields.io/badge/tests-race--clean-brightgreen.svg)](#quality)

Artemis 0.1.0 supports renderless fetch, JavaScript execution, extraction, an owned high-level Agent lifecycle for typed fetch actions, persistent renderless steering over JSON WebSocket, an owned or externally attached Chromium/CDP lifecycle, high-level Chromium actions, deterministic hybrid routing, browser screenshots, persistent profiles, and measured-profile stealth injection (env-gated, legal acknowledgement required). Challenge resolution is experimental.

---

## Why Artemis

Crawling the modern web means running JavaScript, and running Chromium-Headless at scale is expensive: gigabytes of RAM, heavy CPU, painful to package and operate. Most pages don't need a full browser; they need a correct DOM, working `fetch`/XHR, cookies, and JS execution.

Artemis handles compatible pages on a **renderless V8 path**: stock V8 with a from-scratch DOM/WebAPI surface but **no layout, no compositor, and no paint**. Callers must use another browser when a task requires pixel geometry, real screenshots, canvas/media, WebAuthn, or browser-native interaction.

The result is a self-contained Go binary for pages that fit the documented renderless capability contract.

## Highlights

- **Renderless execution:** HTTP fetch, HTML parsing, V8 JavaScript, DOM mutation, cookies, and request interception.
- **Agent-native extraction:** clean Markdown, semantic tree, structured data (JSON-LD/microdata), links, forms, and actionable elements, ready to feed an LLM.
- **Dual-mode:** embed the Go packages in-process, or run `artemis serve` and drive it from any language over JSON-over-WebSocket.
- **Owned Agent lifecycle:** typed fetch actions run through tracked sessions with cancellation, stable errors, health snapshots, and idempotent shutdown.
- **Explicit capability truth:** `artemis capabilities` reports supported and unavailable release surfaces from the canonical capability registry.
- **Security controls:** optional robots and private-IP guards are available on renderless fetches; enable them for untrusted URLs.

## Architecture

Artemis ships two supported execution kernels:

- **Renderless fast path** (`renderless/`, `engine/`, `js/`, `webapi/`, `css/`, `parser/`): V8 via the vendored fork at `third_party/v8go` (stock V8, no Chromium) with an isolate snapshot and context pool, a from-scratch DOM/WebAPI surface, CSS parse/cascade/computed style, `fetch`/XHR, cookies, and agent extraction.
- **Chromium/CDP kernel** (`process/`, `bridge/`): platform-aware binary discovery, isolated zero-port launch, bounded request/event transport, validated browser identity, context/target/session ownership, crash/detach state, and distinct owned/external shutdown semantics.

The `stealth/`, `profile/`, solver, and high-level Chromium action packages contain future-facing implementation pieces. They do not yet form supported profile, anti-detection, challenge-solving, screenshot, or hybrid-routing capabilities.

### Subsystem map

| Subsystem | Package(s) | What it does |
|-----------|------------|--------------|
| Renderless engine | `renderless/`, `engine/`, `js/`, `webapi/` | V8 execution, from-scratch DOM/WebAPI, `fetch`/XHR, cookies |
| Styling | `css/`, `parser/` | HTML parse, CSS parse/cascade/computed style |
| Chromium kernel | `process/`, `bridge/` | Owned/external lifecycle, CDP transport, contexts, targets, sessions, crash and shutdown handling |
| Chromium actions | `bridge/cdpops/`, `bridge/actions/`, `bridge/tabs/` | Human-cadence input (key events with jitter, click holds, eased drags) and postcondition-verified actions |
| Router | `router/`, `bridge/provider.go` | Deterministic static→renderless→CDP escalation with evidence |
| Stealth | `stealth/`, `network/` | Measured-profile injection, CDP identity overrides, Chrome-coherent renderless identity incl. uTLS; env-gated (`ARTEMIS_STEALTH`) |
| Solver | `solver/` | Vision-based challenge / CAPTCHA solving |
| Observation | `observe/` | AX-tree snapshots + diff, network + console buffers |
| Input | `input/` | Human-like Bezier mouse/keyboard input |
| Actions | `actions/`, `bridge/actions/` | High-level page actions (login, forms, navigation) |
| Security | `security/` | SSRF defense, indirect-prompt-injection defense, ad/tracker blocking |
| Profiles | `profile/` | Persistent browser profiles with isolated state |
| Scraper | `scraper/` | Adaptive selectors + AI element finding |
| Serve | `serve/` | JSON-over-WebSocket steering server |
| Telemetry | `telemetry/` | Local OpenTelemetry, opt-out (`ARTEMIS_DISABLE_TELEMETRY=true`) |

See [`docs/documentation.md`](docs/documentation.md) for the full architecture and module reference.

## Capability contract

Run `artemis capabilities` for the versioned machine-readable contract. A capability is `supported` only when the registry names its production entrypoint, lifecycle owner, and observable behavior test. Symbols, synthetic output, and fixed success strings do not qualify. Reproducible performance claims will be published only with the TASK-2360 benchmark artifact.

## For agents

The repo ships an agent-facing operating contract at
[`skills/artemis/SKILL.md`](skills/artemis/SKILL.md) — a decision table
(fetch vs. observe/act vs. serve), exact flags, the typed-action JSON kinds,
and the WebSocket command set. Agents that discover repo skills pick it up
automatically; everything else can run `artemis capabilities` for the
machine-readable contract or just `artemis fetch <url>`.

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

Requires **Go 1.27+** and a C toolchain (for V8 via cgo).

```sh
make build      # build the artemis binary
make test       # run the test suite
make test-race  # run with the race detector
make bench      # run the benchmark suite
```

## Quality

- The supported renderless, steering, Agent lifecycle, and Chromium/CDP kernel paths have behavior tests and race-detector coverage.
- Challenge solving (`solver/`) remains experimental; `chromium.h2_fingerprint` is unavailable in the capability registry. Stealth requires explicit acknowledgement via `ARTEMIS_STEALTH` + `ARTEMIS_STEALTH_PURPOSE` + `ARTEMIS_STEALTH_LEGAL_BASIS`.

## License

Artemis is released under the **MIT License**. You are free to use, copy, modify, merge, publish, distribute, sublicense, and sell copies, in both open-source and commercial projects, provided the copyright notice and this permission notice are included in all copies or substantial portions of the software.

Copyright © 2026 Christopher Schulze. See [LICENSE](LICENSE) for the full text.
