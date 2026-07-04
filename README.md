# Artemis

> Ultra-performant headless browser engine in Go. Built for AI agents. No Chrome, no Chromium, no rendering.

[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

## Status

Early development. Heavy WIP. Not usable yet.

## Why

Crawling the modern web requires running JavaScript. Running Chromium-Headless at scale is expensive: high RAM, high CPU, hard to package. Artemis is a from-scratch headless browser engine in Go, designed for agent-driven web automation. No layout engine, no compositor, no paint pipeline. Just enough browser to make modern websites render their content under JavaScript so an agent can read, click, fill, extract.

## Architecture

- **JS engine**: V8 via [`rogchap.com/v8go`](https://github.com/rogchap/v8go) (BSD, stock V8, no Chromium)
- **HTML parser**: `golang.org/x/net/html`
- **HTTP**: `net/http` + HTTP/2 (stdlib)
- **WebSocket**: client (browser-side `WebSocket` API) + custom agent-steering server (own JSON protocol, not CDP)
- **DOM + WebAPI surface**: from scratch in Go
- **CSS parser**: from scratch in Go
- **Storage**: SQLite via `modernc.org/sqlite` (pure Go)
- **Telemetry**: local OpenTelemetry tracing + opt-out anonymous phone-home (disable via `ARTEMIS_DISABLE_TELEMETRY=true`)

See [`docs/spec.md`](docs/spec.md) for the full module map and target architecture.

## Lineage and Credit

Artemis is an independent re-implementation in Go, inspired by the architecture of [Lightpanda Browser](https://github.com/lightpanda-io/browser) (Zig, AGPL-3.0). Lightpanda is used only as a read-only reference for module structure, WebAPI coverage, and design choices; no Lightpanda source is copied into or distributed with Artemis. Artemis does not include CDP or MCP servers; instead it ships a Go library API, a CLI, and a custom JSON-over-WebSocket steering protocol designed for agent embedding.

Artemis is original Go code and is released under the MIT License.

## Build

Requires Go 1.26+, a C toolchain (for V8 via cgo).

```sh
make build
```

## License

[MIT](LICENSE).
