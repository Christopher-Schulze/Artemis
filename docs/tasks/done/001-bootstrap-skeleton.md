# TASK 001: Bootstrap repository skeleton

## Why

Greenfield repository. Need the Repo Structure (per CLAUDE.md): `LICENSE`, `README.md`, `docs/documentation.md`, `docs/spec.md`, `docs/tasks.md`, `docs/tasks/`, `docs/tasks/done/`, `Makefile`, `go.mod`, `cmd/artemis/`, `scripts/`. Reference source must be preserved as a research reference, not at repo root. Build must compile so future TASKs start from a green baseline.

## Acceptance

- `research/` contains the original reference source unmodified.
- `LICENSE` is AGPL-3.0 at repo root.
- `README.md` documents project, license, status, build instructions.
- `docs/documentation.md` exists with TOC and skeleton sections matching CLAUDE.md.
- `docs/spec.md` documents target architecture, module map, public Go API surface, CLI surface, steering protocol, memory model, telemetry channels, build targets.
- `docs/tasks.md` lists Active / Queue / Blocked / Done with TASK 001 active and TASKs 002-009 queued matching the spec roadmap.
- `go.mod` declares `module artemis` on Go 1.26.
- `Makefile` provides `build run test test-race vet fmt tidy clean`.
- `cmd/artemis/main.go` builds and `./artemis version` prints `0.0.1-dev`.
- `.gitignore` covers macOS, Go binaries, IDE, env files, cgo intermediates.
- `make build` succeeds, `make test` succeeds (no tests yet -> exit 0), `make vet` succeeds.

## Sub-Tasks

- [x] move reference source to `research/`
- [x] copy AGPL-3.0 `LICENSE` to repo root
- [x] write `README.md`
- [x] write `.gitignore`
- [x] write `Makefile`
- [x] write `go.mod`
- [x] write `cmd/artemis/main.go` (version, help)
- [x] write `docs/documentation.md`
- [x] write `docs/spec.md`
- [x] write `docs/tasks.md` and this detail file
- [x] verify `make build` produces working `./artemis` binary
- [x] verify `./artemis version` prints `0.0.1-dev`
- [x] verify `make vet` clean

## Notes

Stack decisions locked (see spec.md): V8 via `rogchap.com/v8go`, HTML via `golang.org/x/net/html`, HTTP via stdlib `net/http`, WebSocket via `github.com/coder/websocket`, SQLite via `modernc.org/sqlite`, OTel via `go.opentelemetry.io/otel`. No deps added in TASK 001 - they enter on first use in their respective TASKs to avoid premature `go.sum` churn.

CDP and MCP explicitly excluded. WebSocket included as both browser-side client (Phase 3, JS WebSocket API) and server-side custom JSON steering protocol (Phase 7). Telemetry includes both local OTel tracing and opt-out anonymous phone-home (no URLs, no content).

Reference source under `research/` is read-only. Any reference in code or docs uses a `research/...` path form per the research-ref rule.

The reference source ships one stray Go file under `research/` that fails `go vet` with a redundant-newline diagnostic. To keep `research/` unmodified, the Makefile filters `./...` through `go list | grep -v /research/` and feeds the result to `vet` and `test`. This is the canonical pattern; future TASKs add packages and they are picked up automatically.

## Deviations

(none)
