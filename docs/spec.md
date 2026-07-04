# Artemis Specification

Final technical blueprint. Target state only. No timeline, no rationale narration. Source of truth for module boundaries, dependencies, and public surface.

## Scope

In-scope: HTTP fetch, HTML parse, DOM tree, CSS parse + computed style, JavaScript execution via V8, full WebAPI surface required for modern SPAs, network interception, cookies, robots.txt, IpFilter, WebBotAuth, agent extraction layer (markdown, semantic tree, structured data, links, forms, click/type actions), Go library API, CLI, custom JSON-over-WebSocket steering server, local OpenTelemetry tracing, opt-out anonymous phone-home telemetry.

Out-of-scope: rendering (layout, paint, compositor), Chromium DevTools Protocol (CDP), Model Context Protocol (MCP) server, Puppeteer / Playwright wire compatibility.

## External Dependencies

| Concern | Library | License | Linkage |
|---|---|---|---|
| JavaScript engine | `rogchap.com/v8go` (stock V8) | MIT (binding) / BSD-3 (V8) | cgo |
| HTML parser | `golang.org/x/net/html` | BSD-3 | pure Go |
| HTTP/1.1 + HTTP/2 | `net/http` (stdlib) | BSD-3 | pure Go |
| WebSocket (client + server) | `github.com/coder/websocket` | ISC | pure Go |
| CSS selectors (used by webapi.QuerySelector) | `github.com/andybalholm/cascadia` | BSD-2 | pure Go |
| SQLite | `modernc.org/sqlite` | BSD-3 | pure Go |
| IDNA | `golang.org/x/net/idna` | BSD-3 | pure Go |
| OpenTelemetry | `go.opentelemetry.io/otel` | Apache-2.0 | pure Go |
| Structured logging | `log/slog` (stdlib) | BSD-3 | pure Go |

All licenses MIT-compatible.

## Top-Level Layout

```
cmd/artemis/         CLI entry, subcommand dispatch
engine/              Browser, Session, Page, Frame, Config, App, Runner, EventManager, ScriptManager
js/                  V8 bridge: Isolate, Context, Bridge, Scheduler, Snapshot, codegen for WebAPI bindings
parser/              HTML parser facade (wraps x/net/html)
css/                 Tokenizer, Parser, StyleManager, computed style, color
webapi/              DOM + WebAPI surface (Document, Element, Node, Event, Fetch, XHR, ...)
network/             HTTP client, cache, robots, ipfilter, webbotauth, ws-client
agent/               actions, forms, links, markdown, dump, semantic, structured, interactive
serve/               JSON-over-WebSocket steering server
storage/             Storage abstraction, SQLite backend, Blackhole
telemetry/           Local OpenTelemetry tracing, opt-out phone-home
internal/arena/      sync.Pool-backed page-scoped allocator
internal/cookies/    Cookie jar
internal/mime/       MIME types
internal/log/        slog wrapper
internal/id/         ID generation
internal/notify/     In-process event bus
internal/signal/     Signal handling
internal/crash/      Panic recovery
docs/                documentation.md, spec.md, tasks.md, tasks/, tasks/done/
research/lightpanda/ Reference source (Reference: research/lightpanda/src/...)
scripts/             Bash tooling (added on demand)
```

## Module Mapping (Lightpanda Zig -> Artemis Go)

| Lightpanda (Reference: research/lightpanda/src/...) | Artemis Go target | Notes |
|---|---|---|
| `App.zig` | `engine/app.go` | top-level lifecycle |
| `Config.zig` | `engine/config.go` | runtime config struct |
| `cli.zig` | `cmd/artemis/cli.go` | flag parsing, subcommand dispatch |
| `Server.zig` | `serve/server.go` | WS steering server |
| `Sighandler.zig` | `internal/signal/handler.go` | |
| `crash_handler.zig` | `internal/crash/handler.go` | |
| `log.zig` | `internal/log/log.go` | slog facade |
| `id.zig` | `internal/id/id.go` | |
| `slab.zig` | `internal/arena/slab.go` | object pool |
| `string.zig` | `internal/strs/strs.go` | |
| `datetime.zig` | (stdlib `time`) | dropped |
| `cookies.zig` | `internal/cookies/jar.go` | |
| `Notification.zig` | `internal/notify/notify.go` | |
| `ArenaPool.zig` | `internal/arena/pool.go` | sync.Pool wrapper |
| `SemanticTree.zig` | `agent/semantic.go` | |
| `browser/Browser.zig` | `engine/browser.go` | |
| `browser/Session.zig` | `engine/session.go` | |
| `browser/Page.zig` | `engine/page.go` | |
| `browser/Frame.zig` | `engine/frame.go` | |
| `browser/Factory.zig` | `engine/factory.go` | |
| `browser/Runner.zig` | `engine/runner.go` | |
| `browser/EventManager.zig` `browser/EventManagerBase.zig` | `engine/events.go` | |
| `browser/ScriptManager.zig` `browser/ScriptManagerBase.zig` | `engine/scripts.go` | |
| `browser/StyleManager.zig` | `css/style.go` | |
| `browser/HttpClient.zig` | `network/http.go` | wraps `net/http` |
| `browser/URL.zig` | (stdlib `net/url`) | dropped |
| `browser/Mime.zig` | `internal/mime/mime.go` | |
| `browser/actions.zig` | `agent/actions.go` | |
| `browser/forms.zig` | `agent/forms.go` | |
| `browser/links.zig` | `agent/links.go` | |
| `browser/markdown.zig` | `agent/markdown.go` | |
| `browser/dump.zig` | `agent/dump.go` | |
| `browser/structured_data.zig` | `agent/structured.go` | |
| `browser/interactive.zig` | `agent/interactive.go` | |
| `browser/color.zig` | `css/color.go` | |
| `browser/reflect.zig` | `js/bridge.go` + `scripts/codegen-webapi.sh` | comptime -> codegen |
| `browser/parser/Parser.zig` `browser/parser/html5ever.zig` | `parser/html.go` | wraps `x/net/html` |
| `browser/css/Tokenizer.zig` | `css/tokenizer.go` | |
| `browser/css/Parser.zig` | `css/parser.go` | |
| `browser/js/Isolate.zig` | `js/isolate.go` | wraps `v8go.Isolate` |
| `browser/js/Context.zig` | `js/context.go` | wraps `v8go.Context` |
| `browser/js/Env.zig` | `js/env.go` | |
| `browser/js/Bridge.zig` | `js/bridge.go` | Go<->V8 method binding |
| `browser/js/Caller.zig` | `js/caller.go` | |
| `browser/js/Execution.zig` | `js/execution.go` | |
| `browser/js/Scheduler.zig` | `js/scheduler.go` | microtask + timer queue |
| `browser/js/Snapshot.zig` | `js/snapshot.go` | wraps v8go snapshot creator |
| `browser/js/Inspector.zig` | `js/inspector.go` | |
| `browser/js/HandleScope.zig` `browser/js/Local.zig` `browser/js/Value.zig` `browser/js/Object.zig` `browser/js/String.zig` `browser/js/Number.zig` `browser/js/Integer.zig` `browser/js/BigInt.zig` `browser/js/Array.zig` `browser/js/Function.zig` `browser/js/Module.zig` `browser/js/Promise.zig` `browser/js/PromiseResolver.zig` `browser/js/PromiseRejection.zig` `browser/js/RegExp.zig` `browser/js/TaggedOpaque.zig` `browser/js/TryCatch.zig` `browser/js/Identity.zig` `browser/js/Origin.zig` `browser/js/Platform.zig` `browser/js/Private.zig` | `js/values.go` | thin wrappers over v8go primitives |
| `browser/webapi/*.zig` | `webapi/*.go` | one file per WebAPI type, names lowercased |
| `network/http.zig` | `network/http.go` | |
| `network/IpFilter.zig` | `network/ipfilter.go` | |
| `network/Robots.zig` | `network/robots.go` | |
| `network/WebBotAuth.zig` | `network/webbotauth.go` | |
| `network/WsConnection.zig` | `network/ws.go` | client only |
| `network/Network.zig` | `network/network.go` | |
| `network/cache/*` | `network/cache/*.go` | |
| `network/layer/*` | `network/layer/*.go` | |
| `storage/Storage.zig` | `storage/storage.go` | |
| `storage/Blackhole.zig` | `storage/blackhole.go` | |
| `storage/sqlite/*` | `storage/sqlite/*.go` | `modernc.org/sqlite` |
| `sys/idna.zig` | (stdlib `golang.org/x/net/idna`) | dropped |
| `sys/libcrypto.zig` | (stdlib `crypto/*`) | dropped |
| `sys/libcurl.zig` | (stdlib `net/http`) | dropped |
| `telemetry/telemetry.zig` | `telemetry/tracing.go` | local OTel |
| `telemetry/lightpanda.zig` | `telemetry/phone_home.go` | opt-out, anonymous, no URLs / no content |
| `cdp/*` | excluded | — |
| `mcp/*` | excluded | — |
| `vendor/libidn2/*` | (stdlib `x/net/idna`) | dropped |

## Engine Public Surface (final target)

| Symbol | Kind | Purpose |
|---|---|---|
| `engine.Config` | struct | runtime configuration (proxy, cookies path, robots, ipfilter, ws-server addr, telemetry opt-out, timeouts) |
| `engine.New(cfg Config) (*Engine, error)` | constructor | create root engine |
| `engine.Engine.Fetch(ctx, url string, opts FetchOpts) (*Page, error)` | method | open a page |
| `engine.Engine.NewSession(opts SessionOpts) (*Session, error)` | method | create isolated session |
| `engine.Engine.Close() error` | method | release resources |
| `engine.FetchOpts` | struct | wait_until, wait_ms, wait_selector, wait_script, headers, cookies |
| `engine.Page.Markdown() string` | method | dump as markdown |
| `engine.Page.HTML() string` | method | serialized DOM |
| `engine.Page.Links() []Link` | method | extract links |
| `engine.Page.StructuredData() []StructuredItem` | method | JSON-LD, microdata, OpenGraph |
| `engine.Page.SemanticTree() *SemanticNode` | method | agent-friendly tree |
| `engine.Page.Eval(ctx, expr string) (Value, error)` | method | evaluate JS |
| `engine.Page.Click(selector string) error` | method | dispatch click |
| `engine.Page.Type(selector, text string) error` | method | type text |
| `engine.Page.Form(selector string) *Form` | method | form interaction handle |
| `engine.Page.Close() error` | method | release page resources |

## CLI Surface (final target)

| Command | Purpose |
|---|---|
| `artemis version` | print version |
| `artemis help` | usage |
| `artemis fetch <url>` | fetch + dump (`--dump html|markdown|text`, `--wait-until`, `--wait-ms`, `--wait-selector`, `--wait-script`, `--obey-robots`, `--proxy`, `--log-format`, `--log-level`) |
| `artemis run --script FILE <url>` | execute user script in page context |
| `artemis serve` | start WS steering server (`--host`, `--port`, default `127.0.0.1:9333`) |

## Steering Protocol (custom JSON-over-WebSocket, not CDP)

Endpoint default: `ws://127.0.0.1:9333`. Frames are JSON, one message per frame.

Request envelope: `id` string, `cmd` string, `params` object.
Response envelope: `id` string, `ok` bool, `value` any (on `ok=true`), `error` `{code,message}` (on `ok=false`).
Event envelope (server-pushed): `event` string, `params` object.

| `cmd` | `params` | `value` |
|---|---|---|
| `engine.config` | `{proxy,robots,timeoutMs,...}` | `{}` |
| `session.new` | `{cookies,headers}` | `{sessionId}` |
| `page.open` | `{sessionId,url,wait}` | `{pageId}` |
| `page.eval` | `{pageId,expr}` | `{value}` |
| `page.click` | `{pageId,selector}` | `{}` |
| `page.type` | `{pageId,selector,text}` | `{}` |
| `page.form.submit` | `{pageId,selector,fields}` | `{}` |
| `page.dump` | `{pageId,format:"html"|"markdown"|"text"|"semantic"|"structured"|"links"}` | `{data}` |
| `page.close` | `{pageId}` | `{}` |
| `session.close` | `{sessionId}` | `{}` |

| `event` | `params` |
|---|---|
| `console` | `{pageId,level,message,args}` |
| `navigation` | `{pageId,url}` |
| `dialog` | `{pageId,kind,message}` |
| `request` | `{pageId,id,method,url,headers}` |
| `response` | `{pageId,id,status,headers}` |
| `error` | `{pageId,message}` |

## Memory Model

Per-page `internal/arena.Arena` backed by `sync.Pool` of byte buffers and node slabs. Page lifetime owns its arena; `Page.Close` releases buffers back to the pool. DOM strings are slices into HTML-parser output buffers where safe; copies only on JS boundary crossing. V8 isolate is per session, reused across pages within session.

## Telemetry

| Channel | Default | Opt-out | Sends |
|---|---|---|---|
| Local OpenTelemetry tracing | enabled | n/a | spans to configured exporter or none |
| Phone-home | enabled | `ARTEMIS_DISABLE_TELEMETRY=true` | version, OS/arch, anonymous instance ID, page-fetch counts, error class counts |

Phone-home never transmits URLs, request bodies, response bodies, page content, cookies, headers.

## Build Targets

| Target | Output |
|---|---|
| `make build` | `./artemis` (linux, darwin; cgo enabled once V8 integrated) |
| `make test` | `go test ./...` |
| `make test-race` | `go test -race ./...` |
| `make vet` | `go vet ./...` |
| `make fmt` | `gofmt -s -w .` |
| `make tidy` | `go mod tidy` |
| `make clean` | remove binary, caches |
