<p align="center">
  <img src="assets/logo.png" alt="Artemis" width="440">
</p>

<h1 align="center">Artemis</h1>

> **A renderless web engine built for AI agents.** Artemis fetches HTML, executes JavaScript in V8, maintains a DOM, and extracts agent-ready content without a layout or paint engine.

[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Go 1.27.1](https://img.shields.io/badge/Go-1.27.1-00ADD8.svg)](https://go.dev)
[![Tests: behavior and race coverage](https://img.shields.io/badge/tests-behavior%20%2B%20race%20coverage-blue.svg)](#quality)

Artemis 0.1.0 supports renderless fetch, JavaScript execution, extraction, an owned high-level Agent lifecycle for typed fetch actions, persistent renderless steering over JSON WebSocket, an owned or externally attached Chromium/CDP lifecycle, high-level Chromium actions, deterministic hybrid routing, browser screenshots, persistent profiles, and measured-profile stealth injection (env-gated, legal acknowledgement required). Challenge resolution is experimental.

---

## Why Artemis

Crawling the modern web means running JavaScript, and running Chromium-Headless at scale is expensive: gigabytes of RAM, heavy CPU, painful to package and operate. Most pages don't need a full browser; they need a correct DOM, working `fetch`/XHR, cookies, and JS execution.

Artemis handles compatible pages on a **renderless V8 path**: stock V8 with a from-scratch DOM/WebAPI surface but **no layout, no compositor, and no paint**. Use its Chromium/CDP path for real browser geometry, screenshots and native interaction; a separately installed Chrome or Chromium is required for that path.

The result is a self-contained Go binary for pages that fit the documented renderless capability contract.

## Highlights

- **Renderless execution:** HTTP fetch, HTML parsing, V8 JavaScript, DOM mutation, cookies, and request interception.
- **Agent-native extraction:** clean Markdown, semantic tree, structured data (JSON-LD/microdata), links, forms, and actionable elements, ready to feed an LLM.
- **Dual-mode:** embed the Go packages in-process, or run `artemis serve` and drive it from any language over JSON-over-WebSocket.
- **Owned Agent lifecycle:** typed fetch actions run through tracked sessions with cancellation, stable errors, health snapshots, and idempotent shutdown.
- **Explicit capability truth:** `artemis capabilities` reports supported and unavailable release surfaces from the canonical capability registry.
- **Security controls:** private/loopback destinations are denied by default. Explicit network policy can admit trusted local fixtures; robots.txt enforcement is configurable.
- **Action authority:** managed sessions and CLI commands permit navigation and observation by default; interactions and caller JavaScript require an explicit action policy. File reloads retain the last valid decision.
- **Final request checks:** intercepted Chromium URL, method, header and body overrides are checked before dispatch; each paused request admits one terminal resolution. Local fulfillment also obeys response and decompression limits.

## Architecture

Artemis ships two supported execution kernels:

- **Renderless fast path** (`renderless/`, `engine/`, `js/`, `webapi/`, `css/`, `parser/`): V8 via the vendored fork at `third_party/v8go` (stock V8, no Chromium) with an isolate snapshot and context pool, a from-scratch DOM/WebAPI surface, CSS parse/cascade/computed style, `fetch`/XHR, cookies, and agent extraction.
- **Chromium/CDP kernel** (`process/`, `bridge/`): platform-aware binary discovery, isolated zero-port launch, bounded request/event transport, validated browser identity, context/target/session ownership, crash/detach state, and distinct owned/external shutdown semantics.

Chromium actions, screenshots, persistent profiles and hybrid routing have supported entries in the capability registry. Stealth requires explicit policy acknowledgement; challenge resolution remains experimental. These are separate from the renderless kernel's capabilities.

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

Run `artemis capabilities` for the versioned machine-readable contract. A capability is `supported` only when the registry names its production entrypoint, lifecycle owner, and observable behavior test. Symbols, synthetic output, and fixed success strings do not qualify. Performance comparisons require a reproducible benchmark artifact with its environment and corpus.

## For agents

The repo ships an agent-facing operating contract at
[`skills/artemis/SKILL.md`](skills/artemis/SKILL.md) — a decision table
(fetch vs. observe/act vs. serve), exact flags, the typed-action JSON kinds,
and the WebSocket command set. Agents that discover repo skills pick it up
automatically; everything else can run `artemis capabilities` for the
machine-readable contract or just `artemis fetch <url>`.

## Usage

### Install and first run

Build the standalone repository with the verified Go 1.27.1 toolchain and a C toolchain:

```sh
git clone https://github.com/Christopher-Schulze/Artemis.git
cd Artemis
export GOTOOLCHAIN=go1.27.1
make build
./artemis version
./artemis capabilities
./artemis fetch https://example.com
```

No other application or private repository is required. Renderless fetch does not
require Chrome. `observe` and `act` require Chrome/Chromium and accept an
explicit `--binary` path; `doctor` reports environment readiness.

### Embedded library

**Embedded library** drives the engine in-process:

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/Christopher-Schulze/Artemis/engine"
)

func main() {
    if err := fetch(); err != nil {
        log.Fatal(err)
    }
}

func fetch() (returnErr error) {
    eng, err := engine.New(engine.Config{})
    if err != nil {
        return err
    }
    defer func() {
        if err := eng.Close(); err != nil && returnErr == nil {
            returnErr = err
        }
    }()
    page, err := eng.Fetch(context.Background(), "https://example.com", engine.FetchOpts{})
    if err != nil {
        return err
    }
    defer func() {
        if err := page.Close(); err != nil && returnErr == nil {
            returnErr = err
        }
    }()
    _, err = fmt.Println(page.Markdown())
    return err
}
```

When embedding from a separate Go module, add both the Artemis dependency and
`replace github.com/tommie/v8go => /absolute/path/to/Artemis/third_party/v8go` to
your module's `go.mod`. Go does not inherit a dependency's replace directives;
Artemis requires its vendored snapshot-enabled fork. A source checkout and
the C toolchain below are required for library embedding.

### Persistent steering server

**Standalone server** exposes the JSON-over-WebSocket steering protocol so an agent in any language can drive it over the wire:

```sh
artemis serve
```

The default endpoint is `ws://127.0.0.1:9333/`. Supply
`Authorization: Bearer <token>` during the WebSocket handshake. The command
generates a token and prints it once to stderr unless `ARTEMIS_SERVE_TOKEN`
or `--token` supplies one. Numeric loopback binds are mandatory. The first
connection returns `X-Artemis-Client-ID`; retain it for reconnecting to owned
sessions. See [the protocol, commands and examples](docs/documentation.md#steering-server).

For interaction, supply `--action-policy policy.json` when starting the server.
For example, `{"default":"deny","allow":["navigate","observe","query","extract","click"],"domain_allowlist":["example.com"]}`
admits those categories only on that host and its subdomains. One-shot `fetch`,
`run`, `observe` and `act` accept the same flag. `--eval`, caller script execution and
JavaScript assertions additionally require `evaluate`. Confirmation returns a
non-dispatching result; configure authorization in the owner before retrying.
See [the policy contract](docs/documentation.md#security-layer) for defaults,
environment compatibility, reload ownership and the trusted low-level boundary.

For multi-step Chromium work, keep the returned identities and use the persistent
CLI mode. Set `ARTEMIS_SERVER=ws://127.0.0.1:9333/` and
`ARTEMIS_SERVE_TOKEN` to the server token, then create a session:

```sh
artemis control --cmd session.new
```

Copy its `clientId` into `ARTEMIS_CLIENT_ID`. Open a page with the returned
session ID, then reuse that exact page across commands:

```sh
artemis control --cmd chromium.open --params '{"sessionId":"SESSION_ID","url":"https://example.com"}'
artemis observe --session SESSION_ID --page PAGE_ID
artemis query --session SESSION_ID --page PAGE_ID --roles button,textbox --max-bytes 8192
artemis act --session SESSION_ID --page PAGE_ID --request '{"kind":"screenshot"}'
artemis fetch --session SESSION_ID --page PAGE_ID --dump markdown
artemis control --cmd session.close --params '{"sessionId":"SESSION_ID"}'
```

`SESSION_ID` and `PAGE_ID` denote the actual returned IDs. Reference actions also
require `--snapshot-epoch` from the observation; refs from another page or an old
document cannot silently target a fresh page. One-shot commands with a URL launch
their own browser and cannot share refs across invocations. The server lazily
starts Chrome with isolated session contexts; `--chromium=false` disables that
path. Missing Chrome leaves renderless operation available. Protocol 2.0 requires
explicit page identity for Chromium actions.

For bounded agent context, use `query` on that retained page. It returns compact
JSON with a revision, epoch and `full`, `unchanged` or `delta` kind. Repeat the
same options with `--since REVISION` using the actual returned revision.
`--text`, `--frame` and `--mode subtree --subtree REF` narrow the observation;
`--target REF` adds observed blocker evidence and bounded remedies. Check
`lost`, `truncated` and `resetReason` before applying incremental output. A
missing baseline, navigation or changed projection returns a full reset. See
[the query contract](docs/documentation.md#task-scoped-queries) for bounds and examples.

## Build

Requires the verified **Go 1.27.1** toolchain and a C toolchain (for V8 via cgo).
Set `GOTOOLCHAIN=go1.27.1` as in the first-run instructions.

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
