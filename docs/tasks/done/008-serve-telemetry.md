# TASK 008 (Phase 7): WS steering server + telemetry

## Why

The agent loop becomes useful to non-Go programs only if Artemis is steerable from outside the process. This TASK ships a custom JSON-over-WebSocket steering server (not CDP) and a slog-based tracing layer that surfaces engine activity for observability. The phone-home channel is a documented stub today: contract is captured, transmission is deferred.

## Acceptance

- `serve/server.go` exposes `serve.New(*engine.Engine, Opts) *Server` and `(*Server).ListenAndServe(ctx, addr) error`. Implements the JSON protocol from spec.md: `session.new/close`, `page.open/close`, `page.eval`, `page.dump`, `page.click_by_text`, plus events `console`, `error`.
- WebSocket transport via `github.com/coder/websocket`.
- `telemetry/tracing.go` provides `NewTracer(logger *slog.Logger) *Tracer` and `Tracer.Span(ctx, name, attrs...) *Span` with `End()` and `Error(err)`.
- `telemetry/phone_home.go` documents the opt-out contract: version, OS/arch, anonymous instance UUID, fetch counts. `ARTEMIS_DISABLE_TELEMETRY=true` disables the stub. No actual transmission yet; the stub logs to slog at info level.
- `cmd/artemis serve --host 127.0.0.1 --port 9333` runs the server.
- `make build`, `make vet`, `make test`, `go test -race ./serve ./telemetry ./engine ./js` all green.

- [x] `github.com/coder/websocket v1.8.14` added
- [x] `serve/protocol.go` (Request, Response, Err, Event)
- [x] `serve/server.go` (Server, ListenAndServe, command dispatch for session.new/close, page.open/close, page.eval, page.dump html|markdown|text|title|links|structured|semantic, page.click_by_text)
- [x] `serve/server_test.go` integration (real WS round-trip: session, open, eval, dump, close)
- [x] `telemetry/tracing.go` (Tracer, Span with attrs and error) + tests
- [x] `telemetry/phone_home.go` (PhoneHome with atomic counters, ARTEMIS_DISABLE_TELEMETRY env opt-out, structured-log stub) + tests
- [x] CLI `artemis serve --host --port --obey-robots --block-private-ips`
- [x] all green: build, vet, test
- [x] FLUSH documentation.md
- [x] archive

## Notes

The WS server holds at most one engine but can multiplex many sessions. Each session owns its own pages keyed by string ID. Closing a session closes all of its pages.

Real OTel integration is straightforward when wanted: replace the slog backend in `Tracer` with an OpenTelemetry exporter. The `Tracer` API is intentionally compatible.

Phone-home contract documented, transmission deferred:
- never URLs, never content, never headers
- only: artemis version, GOOS/GOARCH, anonymous UUID generated on first run and stored at `~/.artemis/instance-id`, total fetch count, total eval count, error class counts
- opt-out via `ARTEMIS_DISABLE_TELEMETRY=true`

Reference: internal design notes.

## Deviations

(none yet)
