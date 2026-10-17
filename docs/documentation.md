# Artemis Documentation

Single source of truth for Artemis architecture, configuration and public APIs. Code-level details live inline; release history lives in `CHANGELOG.md`.

## Table of Contents

- [Project Overview](#project-overview)
- [Release Capability Contract](#release-capability-contract)
- [License](#license)
- [Repository Layout](#repository-layout)
- [Build and Run](#build-and-run)
- [Configuration](#configuration)
- [CLI](#cli)
- [Library API](#library-api)
- [JavaScript Execution](#javascript-execution)
- [Fetch API](#fetch-api)
- [Browser Globals](#browser-globals)
- [Agent Extraction](#agent-extraction)
- [Forms and Actions](#forms-and-actions)
- [Network Stack](#network-stack)
- [Additional Web Globals](#additional-web-globals)
- [Hybrid Execution Router](#hybrid-execution-router)
- [Playwright Peer Lane](#playwright-peer-lane)
- [Browser Provider Switch](#browser-provider-switch)
- [CDP Bridge](#cdp-bridge)
- [Bridge Actions](#bridge-actions)
- [CDP Operations](#cdp-operations)
- [Tab Management](#tab-management)
- [Renderless Engine](#renderless-engine)
- [Stealth Layer](#stealth-layer)
- [Observation Layer](#observation-layer)
- [Solver / CAPTCHA Pipeline](#solver--captcha-pipeline)
- [Human-like Input](#human-like-input)
- [Security Layer](#security-layer)
- [Profile / Session Management](#profile--session-management)
- [Scraper Subsystem](#scraper-subsystem)
- [Prompts System](#prompts-system)
- [Actions / Auto-Login](#actions--auto-login)
- [Platform Capabilities](#platform-capabilities)
- [Steering Server](#steering-server)
- [Telemetry](#telemetry)
- [Reference-Bound Visual Grounding](#reference-bound-visual-grounding)
- [MCP and WebMCP](#mcp-and-webmcp)
- [Source-Grounded Extraction](#source-grounded-extraction)
- [Script Containment and V8 Maintenance](#script-containment-and-v8-maintenance)
- [Reusable Verified Workflows](#reusable-verified-workflows)
- [Ad and Tracker Blocking](#ad-and-tracker-blocking)

## Project Overview

Artemis is a browser engine written in Go for AI-agent extraction and automation. Version 0.1.0 supports HTML fetch, V8 JavaScript, DOM/WebAPI execution, agent-shaped extraction, a persistent JSON-over-WebSocket steering server, a low-level Chromium/CDP lifecycle with owned or external process semantics, the canonical deterministic hybrid router contract, and verified page/worker stealth pre-script injection when an acknowledged environment profile is supplied. Challenge resolution is experimental and always requires policy admission plus a verified postcondition. `artemis mcp` serves one session as a Model Context Protocol server on stdio (see [MCP and WebMCP](#mcp-and-webmcp)).

## Release Capability Contract

`artemis capabilities` emits the canonical typed registry. `supported` requires a production entrypoint, a lifecycle owner, and an observable behavior test. `experimental` is callable but carries no compatibility guarantee. `unavailable` means that package symbols or synthetic values may exist but must not be registered or marketed as working behavior. The root `artemis.Agent` owns a hybrid-routed renderless runtime, tracked sessions, typed fetch actions, cancellation, health, and idempotent shutdown. `router.ChromiumExecutor` composes a caller-owned CDP page into the same typed route/evidence contract. Use `engine.Engine` for lower-level renderless embedding or `serve.Server` for persistent renderless wire steering.

Versions follow semantic versioning. Before 1.0, minor releases may break experimental interfaces with release notes; supported interfaces receive at least one minor-release deprecation window. Security fixes target the latest minor release. A release claim must be present in the registry and pass its named behavior test. Performance claims require a reproducible artifact with environment, corpus and statistics.

`cmd/artemis-release` accepts a clean committed Artemis source root, an exact full Git HEAD, semantic version, `release_hardened` build profile, the exact HEAD committer epoch as source-date epoch, supported target, pinned toolchain digest, fresh output root outside the source Git worktree, and one or more explicit non-empty regular deliverables. Use the standalone repository root for public releases. Artifact source bytes are hashed before and after copying, the source worktree is revalidated before publication, and any drift aborts.

The release set is staged in a sibling directory, validated, and published by one exclusive atomic rename. `release-manifest.json` binds the exact deliverable paths, file modes, sizes and SHA-256 digests; `checksums.txt` covers every shipped non-manifest file; `sbom.cdx.json` is emitted by the pinned official CycloneDX Go encoder and validated without network access against the pinned CycloneDX 1.7 JSON, SPDX, JSF and cryptography schemas; `license-report.json` correlates every selected Go module and replacement with exact copied license texts and rejects unreferenced or duplicate evidence; `licenses/` contains those immutable texts. Unknown Git identity, source or artifact mutation, incomplete module edges, missing license evidence, malformed schema data, existing output, partial writes and publication failures leave no visible partial release set.

## License

License: [MIT](../LICENSE). Artemis is original Go code.

## Repository Layout

```
artemis/
  cmd/
    artemis/           CLI entry point
    artemis-snapshot/  V8 startup snapshot baker (run via `make snapshot`)
    artemis-smoke/     standalone smoke harness
  engine/              top-level Engine handle: Fetch, Submit, Page, Config
  js/                  V8 isolate + Context lifecycle, native bindings, JS bootstraps
    snapshot.bin       embedded V8 startup snapshot (regenerated on demand)
    snapshot_manifest.json embedded snapshot provenance and source-set identity
  webapi/              DOM (Document, Node, Walk), HTML5 element subclasses
  parser/              HTML parser shim around golang.org/x/net/html
  agent/               extraction layer: Markdown, Text, Links, StructuredData,
                       SemanticTree, Forms, ClickByText, Type
  network/             HTTP client, robots.txt, IP filter, cookie jar
  css/                 CSS parser + inline cascade engine
  bridge/              CDP Bridge: Chrome control, context hierarchy, state machine,
                       provider registry, execution router, code mode, batcher
    actions/           high-level actions: click, type, form, scroll, selector resolution
    cdpops/            low-level CDP ops: element queries, box model, navigation, pointer
    tabs/              multi-tab management: registry, executor, dialog handler, locking
  renderless/          in-process no-render JS browser path: engine, runtime, context pool,
                       webapi registry, script router, intercept handler, CSS parser,
                       capability profile
  stealth/             anti-detection: launch flags, emulation, UA, worker parity,
                       fingerprint, geo-presets, 3-level stealth (default/stealth/paranoid)
  observe/             page observation: AX tree, network ring buffer, console capture,
                       metrics, output formatters (HAR, NDJSON)
  solver/              challenge/CAPTCHA pipeline: detector, vision solver, metrics store
  input/               human-like input: Bezier mouse, keystroke timing, touch events
  security/            browser security: SSRF, IDPI, ad blocking, navigation policy,
                       secret redaction, consent management
  profile/             enterprise session/profile mgmt: encrypted credentials, cookies,
                       sessions, storage, identity, proxy profiles
  scraper/             Scrapling-inspired scraping: adaptive selectors, AI finder,
                       structured extract, concurrent engine, diff re-scrape, HAR streaming
    parsers/           parse-phase worker pool with LockOSThread pinning
  prompts/             9 AgentScope browser agent prompt templates + template executor
  actions/             auto-login: form detection, login flow, post-login verification
  platform/            measured CPU, memory, mmap, affinity, TCP and codec admission
  process/             Chromium discovery, isolated launch, supervision, diagnostics,
                       process-group cleanup and disposable profile ownership
  serve/               WS steering server (JSON over WebSocket)
  telemetry/           OpenTelemetry hooks + anonymous counters
  internal/            non-exported helpers (pool, provenance guard)
    provenance/        provenance guard: forbids derivation-attributing references
    docs/              doc-coverage guard: documentation.md drift checker
  third_party/v8go/    vendored tommie/v8go v0.39.0 (V8 15.5) with the Artemis
                       patches (process snapshot, SetMany, bounded transfer)
  third_party/utls/    absorbed uTLS v1.8.2 (Chrome ClientHello parrots)
                       — internal package, no external dependency
  docs/                this directory
    documentation.md   you are here
    release-procedures.md signing, provenance and release operations
    seal-evidence-matrix.md capability evidence and its limitations
  skills/artemis/      agent-facing operating contract (SKILL.md) — the
                       decision tree + exact invocation for agent
                       frameworks that discover repo skills
  testdata/            test fixtures (smoke scenarios, etc.)
  scripts/             tooling scripts (added on demand)
  LICENSE              MIT
  Makefile             build / test / fmt / vet / snapshot / snapshot-check / bench
  go.mod               Go module (with `replace github.com/tommie/v8go => ./third_party/v8go`)
  README.md            landing page
```

## Build and Run

Requires Go 1.27+ and a cgo toolchain. On macOS the default Xcode clang suffices: the v8go C++ binding is linked from the prebuilt `third_party/v8go/lib/<os_arch>/libv8go_artemis.a`. On Linux cgo compiles the binding and needs LLVM Clang >= 21 with `CC=clang-21 CXX=clang++-21 CGO_CXXFLAGS=-nostdinc++`, because V8 is built against Chromium's libc++. After changing a `.cc` or `.h` file in `third_party/v8go`, rebuild the darwin archives with `go run ./cmd/artemis-v8go-binding -llvm <llvm>/bin`.

```sh
make build       # produces ./artemis
make run         # go run ./cmd/artemis
make test        # all tests
make test-race   # tests with -race
make test-security-gates # SSRF, fuzz, mutation, race, leak, and exhaustion gates
make bench       # all benchmarks
make snapshot    # regenerate js/snapshot.bin and js/snapshot_manifest.json
make snapshot-check # regenerate in memory and verify committed snapshot assets
make vet         # static analysis
make fmt         # gofmt -s -w
make tidy        # go mod tidy
make clean       # remove ./artemis, bin/, dist/, and Go build/test caches
```

The V8 startup snapshot (`js/snapshot.bin`) and its provenance manifest
(`js/snapshot_manifest.json`) are checked into the repo and embedded via
`go:embed`. Regenerate both whenever you change a snapshot-eligible JS
bootstrap (anything in `js.BootstrapSources()`), then run `make snapshot-check`.
The generator binds the ordered bootstrap digests, native callback inventory,
V8/v8go/Go target tuple, generator identity and activation owner. It runs V8
in predictable, single-threaded mode and rejects wall-clock state in the
snapshot bootstrap, so identical inputs produce byte-identical output. The
native callback inventory lists only names that every realm replaces with a Go
native or its own bootstrap definition, so a snapshot realm exposes the same
globals as a from-scratch realm (for example on Linux, where the darwin-built
blob is not admitted); `TestSnapshotStubsAreReplacedInEveryRealm` rejects any
surviving no-op stub. The
runtime validates the blob and manifest before activation and falls back to a
cold isolate with an observable unavailable status on any mismatch.

## Configuration

`platform.ObserveResourceLimits(ctx)` reads current Linux affinity and visible cgroup v1/v2 CPU/memory ancestors with bounded, cancellation-aware file reads. Evidence distinguishes a finite visible ceiling, unlimited visible files and an unavailable observation; hidden ancestors are not proven. `platform.ConstrainResources` narrows explicit CPU/memory ceilings using measured CPU/physical-memory snapshots and these observations. Unknown constraint reads close transient admission; a positively detected container without an observable finite memory boundary cannot inherit host-wide transient capacity. Other builds preserve explicit bounded fallbacks. Neither function writes affinity, cgroups, files, GC settings or a separate resource ledger.

`engine.Config` controls runtime behavior. The zero value is valid; defaults are applied by `engine.New`.

| Field | Default | Purpose |
|---|---|---|
| `UserAgent` | empty → host-matched Chrome UA (`ChromeLike` identity) | sent on every outbound request; `engine.DefaultUserAgent` selects the honest-bot identity |
| `ProxyURL` | empty (direct policy-bound connections) | explicit HTTP(S) proxy; environment proxy variables are ignored. The generated Chrome-style transport currently admits HTTP upstream proxies only and rejects HTTPS upstream proxies before connection. |
| `Timeout` | `30s` | per-request timeout |
| `MaxBodyBytes` | `50 MiB` | response body cap; `network.ErrBodyTooLarge` on overflow |
| `ObeyRobots` | `false` | GET admission checks robots.txt for the actual caller agent; denied URLs return `engine.ErrRobotsDisallowed`, retrieval failures propagate |
| `AdBlock` / `DisableAdBlock` | built-in blocker / `false` | ad/tracker blocking of every subresource is on by default (spec L4206-L4210); `AdBlock` shares one `security.AdBlocker` (whitelist, EasyList versions) across engines, `DisableAdBlock` turns it off. See [Ad and Tracker Blocking](#ad-and-tracker-blocking) |
| `IgnoreRobotsForCrawls` | `false` | crawl fetches (`FetchOpts.Crawl`, CLI `fetch --crawl`) respect robots.txt by default (spec L4416); `true` is the operator opt-out. Ordinary agent fetches behave like a user's browser and follow `ObeyRobots` only |
| `PolicyConfig` | `network.PolicyConfig{}` (default-deny) | network policy; zero value blocks private/loopback IPs and limits destinations to public ports 80/443. Set `AllowPrivateNetworks: true` and `AllowedPorts` to permit fixture/loopback servers. |
| `SessionID` | empty | optional redacted correlation key propagated through HTTP, JavaScript fetch, iframe, stylesheet, and WebSocket policy decisions |
| `Diagnostics` | bounded in-memory ledger | redacted policy/resource ledger configuration; set `Path` for private JSONL persistence |
| `DownloadRoot` | `~/.artemis/tmp/browser` | root for the canonical `<session>/downloads` directory; override only for an isolated embedding or test runtime |
| `MaxDownloadDiskBytes` | `1 GiB` | aggregate committed-download quota per session |
| `MinDownloadFreeBytes` | `512 MiB` | disk headroom preserved after each accepted download |
| `SessionBudget` | `8 tabs, 256 requests, 256 MiB responses, 512 MiB transient input/image work, 1 GiB disk, 16 concurrent requests, 30m` | hard engine-session limits shared by navigation, robots, JavaScript fetch, iframe, stylesheet, intercepted responses, page leases, and downloads; `MaxMemoryBytes` combines mapping and image reservations |
| `MemoryAdmission` | standalone session owner | optional embedding application's shared transient input/image capacity authority supplements the session ceiling; reservation lasts through actual unmapping and joined image work |
| `JSContextPoolSize` | `0` (disabled) | size of the v8.Context pool. When > 0, `Page.Close` returns the underlying v8.Context to the pool and the next `Fetch(... RunScripts=true)` reuses it via JS-side `__artemis_reset(url)`. Skips ~30% of NewContext CPU cost. See [v8.Context pool](#v8context-pool) for caveats. |
| `JSContextPoolWarm` | `false` | when paired with `JSContextPoolSize > 0`, pre-builds all N v8.Contexts at engine.New time so the first Fetch hits the pool fast path immediately. |

`engine.FetchOpts` overrides per-call:

| Field | Purpose |
|---|---|
| `Headers http.Header` | bounded canonical request headers; caller values replace defaults exactly once. Ambiguous content identity and caller-owned transport framing are rejected. |
| `MaxBodyBytes int64` | per-call response body cap |
| `RunScripts bool` | run inline `<script>` tags after parse, plus any external scripts whose URLs are reachable |
| `RunInlineScripts bool` | run inline `<script>` tags only (no external script fetch) |
| `Console js.Console` | sink for `console.*`; defaults to discard |
| `Navigator js.NavigatorConfig` | overrides `navigator.userAgent`/`language`/`languages`/`platform` for this Page |
| `AsyncFetch bool` | run JS `fetch()` in asynchronous goroutines; the V8 thread drains results between scripts and during `WaitIdle` |
| `OnRequest func(*RequestInfo) (*ResponseInfo, error)` | request interception; non-nil response short-circuits the network call with a mock |

`engine.SessionBudget` is fail-closed and scoped. Requests, response bytes and lifetime are accounted per network session (`network.WithSessionID`, default `Config.SessionID`): the first breach returns a typed `*engine.BudgetError` and cancels all in-flight and subsequent work of that session only, so one client cannot invalidate the shared engine. Tabs, concurrency and transient memory are engine-wide admission limits: a breach refuses the new page, request or claim while admitted work continues. `engine.AdmissionLimits` (via `Config.AdmissionLimits`) lets the embedding application's pressure-aware authority supply live tab and concurrency ceilings consulted on every admission: a positive value tightens the static budget at once, a lower value never ends admitted work, and a value above the static budget never loosens it. `ChromiumBrowser.SetLivePageLimit` applies the same live ceiling to Chromium page admission beneath `SetMaxPages`. `Engine.ReleaseSession` forgets a closed session's account and disk usage. `Engine.SessionUsage()` is the engine-wide aggregate (disk summed across sessions), `Engine.SessionUsageFor(id)` one session's consumption. Responses keep their representation: HTML is parsed as served, other text media types (`text/*`, JSON, JavaScript, XML) become literal `<pre>` text that never gains document or script semantics, and binary media fail with `engine.ErrUnsupportedRepresentation` (the router escalates; use `Download` for files). External scripts that fail to load or return non-200 are listed in `Page.Degradations()` and router `PageOutput.Degradations` instead of being hidden; script exceptions keep failing the fetch so the router escalates. `SessionBudget.MaxDiskBytes` also tightens `MaxDownloadDiskBytes`, so renderless response persistence cannot bypass the session disk ceiling. `Engine.Diagnostics()` and `Agent.Diagnostics()` return closed-schema redacted records; a configured ledger write failure blocks the affected network/resource work.

## CLI

```
artemis <command> [flags] [args]
```

| Command | Purpose |
|---|---|
| `version` | print version |
| `help` | print top-level usage |
| `capabilities` | print versioned support states as JSON |
| `fetch <url>` | fetch URL, optionally run scripts, dump html / markdown / text / title / links / structured / semantic |
| `download <url>` | fetch into the session-owned store with path, MIME, size, quota, free-space, and atomic-write enforcement |
| `run <url>` | load a JavaScript file and execute it in the page context |
| `serve` | start the JSON-over-WebSocket steering server |
| `control` | send an authenticated typed command to an existing steering server and return its reconnect identity |
| `observe` | capture a bounded Chromium DOM/accessibility snapshot as JSON, HAR or NDJSON |
| `query` | return a task-scoped persistent Chromium projection, revision delta or unchanged result |
| `act` | execute one typed Chromium action and emit evidence as JSON |
| `session` | open, list and close sessions held by the existing steering server |
| `profile` | manage browser profiles (list, create, get, delete) |
| `doctor` | diagnose the environment and runtime readiness |
| `trace` | fetch a URL and emit a trace of lifecycle events as JSON |
| `diagnostics` | read bounded redacted policy/resource records as JSON |
| `benchmark` | run the benchmark harness and emit a scorecard |

`fetch`, `run`, `observe`, `act`, `trace` and `serve` accept `--adblock` (default true; `--adblock=false` disables ad/tracker blocking). `serve` additionally accepts `--adblock-whitelist` (default `~/.artemis/browser/adblock_whitelist.json`; empty keeps it in memory) and `--adblock-updates` (default false: download verified EasyList/EasyPrivacy versions every 24h into `~/.artemis/browser/blocklists/`).

`act`, `observe`, `trace` and `serve` accept `--sandbox=required|disabled`, `--max-cpu-percent`, `--max-memory-bytes`, `--max-profile-bytes`, and `--session-timeout`. CPU values must be finite and positive, memory/profile limits positive, and the session lifetime at least 1ms. Invalid limits fail before diagnostics or browser initialization. Sandbox disabling is never accepted through raw Chromium arguments and emits an explicit stderr warning when deliberately selected.

`fetch`, `observe`, `act` and `query` share persistent control flags: `--server` (default `ARTEMIS_SERVER`), `--token` (default `ARTEMIS_SERVE_TOKEN`), `--client-id` (default `ARTEMIS_CLIENT_ID`), `--session`, `--page` and `--snapshot-epoch` (default 0). Persistent fetch/observe/act require an existing page and no URL; query always uses the existing owner. Query forbids snapshot-epoch because revisions identify its observations. Process/policy flags belong on the server and are rejected if explicitly supplied to a persistent operation. Retain the server-issued client identity across invocations.

`fetch`, `run`, `observe`, `act` and `serve` share `--action-policy` (default `AGENT_BROWSER_ACTION_POLICY`, falling back to `AGENT_BROWSER_POLICY`) and `--confirm-actions` (default `AGENT_BROWSER_CONFIRM_ACTIONS`). These preserve the canonical navigation/action permission and confirmation checks.

Required CLI stdout documents are limited to 16 MiB including any final newline. JSON is checked after encoding and before writing; text/link output is also checked before writing. This is a serialized-output limit, not a bound on the total heap used by rendering or serialization. Oversized output, a short write or a failed descriptor returns 1 and retains owned cleanup. Exit 2 denotes rejected arguments; exit 1 denotes runtime/output/cleanup failure. Previously emitted effect evidence remains meaningful when a later cleanup fails; never blindly replay an action based only on its exit code. Repeatable header, MIME and destination-port flags accept at most 128 values and 32 KiB of raw values per family. Header admission additionally reuses canonical network validation: names at most 256 bytes, values at most 4096 bytes, at most 32 KiB of canonical header content, valid HTTP fields and no ambiguous credential/content identity. Raw headers accept either delimiter, including `X-Name: value=with-equals`. Fetch/run reject negative body limits and destination ports outside 0..65535 before engine creation; 0 retains the engine's existing default.

`observe` additionally accepts `--format {json|har|ndjson}` (default `json`). `json` emits the full `ObservationEvidence` document; `har` emits the captured network events as an HTTP Archive 1.2 file with credential-bearing headers redacted; `ndjson` emits one captured network event per line. An unknown value is rejected with exit 2 before Chromium is launched.

`observe` one-shot flags include `--binary`, `--timeout` (default `20s`), `--interactive` (default false) and `--max-nodes` (default 2000). It requires one URL. `query` accepts `--mode full|interactive|subtree|evidence` (default `interactive`), `--subtree`, `--target`, `--frame`, `--roles` (comma-separated), `--text` (literal case-insensitive filter), `--max-nodes` (default 200), `--max-bytes` (default 32768, range 512..16777216), `--since` and `--timeout` (default `20s`). Subtree mode requires a subtree reference and other modes forbid one. Selectors preserve the canonical 256-byte text, 128-byte identity and 32-role/64-byte-role bounds. Query emits validated full/delta/unchanged JSON within the requested byte budget, adding a newline only when it fits; an unrepresentable response is exit 1.

`act` requires `--request` with at most 1 MiB of typed action JSON and one URL for one-shot use. `--binary` selects the installed Chromium executable; `--timeout` defaults to `30s`. Persistent operation instead uses the existing page and its observed snapshot epoch. Both modes emit the actual typed action outcome and return 1 when the action reports failure. Owned form-runtime cleanup runs even after output failure; cleanup failure does not suppress an already completed action's evidence.

`control` accepts `--server`, `--token`, `--client-id`, `--cmd` (default `version`), `--params` (default `{}`, valid JSON at most 1 MiB) and `--timeout` (default `30s`). The registered `--session`, `--page` and `--snapshot-epoch` flags are rejected here: these values belong in typed command parameters. JSON output contains the server-issued `clientId` and actual `result`; protocol, connection and output failures return 1. Control never automatically retries a dispatched operation.

`profile` accepts `--root`, required `--owner`, `--op list|create|get|delete` (default `list`), `--name` (required for create/get/delete) and `--scope private|workspace_shared|operator_shared` (default `private`). It emits the actual profile, profile array or deletion status as JSON. Unknown operations/scopes and positional arguments fail before manager construction; runtime ownership/persistence failures return 1. The CLI checks `ProfileManager.Err()` before every operation: unreadable or corrupt metadata cannot become an empty successful list. Embedders retaining the slice-only `List` API must also check initialization through `Err()`.

`session` uses the existing authenticated Serve: `--server`, `--token` and `--client-id` use the same environment defaults as control. It accepts `--op open|list|close` (default `list`) and positive `--timeout` (default `30s`). Open accepts optional `--profile` and `--class ephemeral|persistent|attached` (default `ephemeral`); non-ephemeral classes require a profile. Named profiles require the server's `--profile-root`. Close requires `--id`; list and close require the server-issued client identity returned by open. Every operation emits `{clientId,result}` with the actual typed server result: open returns `{sessionId,ownerUserRef}`, list returns `{sessions:[{sessionId,ownerUserRef,active,tabCount}]}`, close returns `{}`. The server assigns owner identity and retains active sessions and managed persistent-profile locks after the CLI connection exits. Close settles the actual server session and releases its managed lock. Session lifetime is limited by the server/runtime; this does not promise survival across a server restart or prove that Chromium has been launched. Session `--root` and `--owner` have been removed; configure the root on Serve and retain its client identity. Registered `--session`, `--page`, `--snapshot-epoch` and operation-inapplicable flags are rejected, including explicit empty values. Invalid operations/classes, missing identity/server, non-positive timeout and positional arguments return 2 before connection. Protocol, connection, output and cleanup failures return 1; dispatched effects are never automatically replayed.

`benchmark` accepts `--skip-competitor` (default true), `--require-head-to-head`, `--download-url`, `--competitor-sha256`, `--competitor-dir`, `--iterations` (1..1000, default 5), `--output`, `--benchmark-tag`, `--cpu-profile`, `--mem-profile`, `--timeout`, `--format json|summary` (default `json`) and repeatable `--regression-budget metric:absolute_delta:relative_ratio`. It uses the canonical benchmark harness and fresh output directory, emits the actual scorecard/summary and propagates harness/output failure as exit 1. Its own validator rejects malformed or contradictory configuration before execution.

Subcommand `--help` prints the registered flags to stderr and retains exit 2, except benchmark help, which returns 0. Top-level `help` (aliases `-h`, `--help`) writes the complete command/capability help to stdout; `version` (aliases `-v`, `--version`) writes the release version; `capabilities` writes the canonical versioned capability registry as JSON. These top-level output paths return 1 on a required write failure and reject extra arguments with exit 2 before writing.

`fetch` flags: `--dump {html\|markdown\|text\|title\|links\|structured\|semantic}` (default `markdown`), `--user-agent`, `--proxy`, `--timeout`, `--max-body-bytes`, `--header k=v` (repeatable), `--run-scripts`, `--crawl` (crawler traffic: robots.txt is respected for the URL and every redirect hop, spec L4416), `--fence` (wrap text dumps, links and `--eval` output in the spoof-proof IDPI fence), `--eval <expr>`, `--console`, `--allow-private-networks`, `--allow-port`. Exit 0 on success; 1 on runtime error; 2 on argument error.

`run` requires `--script FILE <url>`. Flags: `--user-agent`, `--proxy`, `--timeout` (default `60s`), `--max-body-bytes`, `--header` (repeatable), `--run-inline-scripts` (default true), `--json` (prints `{"trust":{...},"value":<result>}`), `--fence` (fences the text result), `--console`, `--wait-idle` (default true), `--allow-private-networks`, `--allow-port`, `--action-policy` and `--confirm-actions`. The script must resolve to a regular file at most 8 MiB; empty, unreadable, non-regular or oversized input fails before engine construction and network dispatch. Reads remain bounded if the file grows. Text results retain the empty-line convention for nil/undefined values; JSON mode retains valid JSON or quotes string results. Required stdout failure returns exit 1 and still runs owned page/engine cleanup. Argument errors return 2; missing or invalid script files return 1.

`download` flags: `--filename` (basename only), `--session-id` (generated by default), `--timeout`, `--max-file-bytes`, `--max-session-bytes`, `--allow-content-type` (repeatable), `--allow-private-networks`, and `--allow-port`. It emits verified JSON metadata containing the canonical path, sniffed MIME, size, and SHA-256. Arbitrary output paths are rejected.

`sitemap` flags: `--timeout`, `--max-depth`, `--max-sitemaps`, `--max-urls`, `--allow-private-networks`, and `--allow-port`. `artemis sitemap <origin-url>` loads robots.txt `Sitemap:` directives plus `/sitemap.xml` and `/sitemap_index.xml`, follows sitemap indexes recursively (gzip supported, cycle-safe, 50 MiB per document) through the policy-bound client and emits JSON with the deduplicated same-host URLs (lastmod/changefreq/priority), fetched sitemaps, per-sitemap failures and a truncation flag (spec L4416).

`serve` flags: `--host` (default `127.0.0.1`), `--port` (default 9333; 0 permits an ephemeral port), `--ready-json` (default false), `--obey-robots`, `--block-private-ips` (default true), `--allowed-port`, `--token`, `--origin`, `--rate`, `--burst`, `--client-rate`, `--client-burst`, `--profile-root` (default empty, disabling named profiles), `--chromium` (default true) and `--binary`, plus the shared resource/action-policy flags above. Optional readiness JSON is written once after the numeric-loopback listener binds and contains its actual `address`, `processId`, `protocol` and fresh `instanceId`, with no token. Authenticated version/capabilities results repeat that instance identity; `serve.Client.ServerInstanceID()` exposes the handshake observation. Readiness-write failure aborts startup and closes the listener. Server cancellation joins its HTTP owner and propagates shutdown errors. The server owns one configured profile runtime through Agent shutdown; do not share its root with another live runtime manager. It rejects positional arguments, invalid ports/origin patterns and non-positive rate/burst limits before starting the Agent. See [Steering Server](#steering-server).

`doctor` accepts `--format json|text` (default `json`), `--timeout` (default `10s` per runtime check) and `--binary` (default installed Chrome discovery). It separately reports executable discovery (`chromium_binary`), an actual owned Chromium/CDP version handshake including shutdown (`chromium`), Agent start plus stop (`agent_start`) and capability-registry validation (`capabilities`). A discoverable executable alone does not prove readiness. This probe does not claim page navigation or isolated-context readiness. A failed check or required output write returns 1; invalid arguments return 2. No positional arguments are accepted.

Attaching to an owned Chromium treats dial, `Browser.getVersion`, event subscription and `Target.setDiscoverTargets` as one handshake. While the owned process runs, a refused or dropped DevTools connection after `DevToolsActivePort` is retried with backoff (50 ms steps, capped at 500 ms) for up to 10 s; process exit ends the attempt at once as `process.ErrorBrowserCrash` with exit status, last handshake error and bounded browser output. A handshake still failing at the deadline reports that the process is running, with its output. Attaching to an endpoint without an owned process keeps five short attempts.

`trace` requires `--url` with an HTTP(S) URL. It accepts `--format json|text` (default `json`), `--binary`, `--sandbox`, `--trace-dir`, `--screenshots` (default true), `--snapshots` (default true), `--sources` (default false), `--max-cpu-percent`, `--max-memory-bytes`, `--max-profile-bytes`, `--session-timeout`, `--allow-private-networks` and `--allow-port`. The command has a 30s work deadline. `page_loaded.status` is the observed document HTTP response matched to the actual navigation frame and loader; `page_loaded.url` is its final URL with userinfo, query and fragment removed. Redirects, child-frame responses and page-authored performance values cannot substitute that response. An HTTP 404 is reported as 404; successful trace capture may still return 0. Missing, failed or lost response evidence, archive/cleanup errors and required output failures return 1; invalid arguments return 2. The archive retains bounded network entries including frame/loader identities. `BrowserTraceRecorder.DocumentResponse(frameID, loaderID)` returns a value copy, `found=false` while awaiting an observed response, or an error for invalid identities, inactive/failed recording or unavailable truncated evidence. It uses the recorder's existing event consumer.

Trace recording admits at most 128 screenshot/snapshot/source artifacts, 8 MiB per uncompressed ZIP entry and 64 MiB of retained artifact plus encoded debug payload. Actual compressed ZIP output and decoded ZIP aggregate, including metadata, are each limited to 64 MiB; serialization can therefore reject an otherwise admitted payload when metadata no longer fits. These are payload/archive bounds, not total Go heap or transport-response bounds. Each debug section retains at most 1024 entries, each text/identity field at most 4096 bytes and aggregate debug strings at most 8 MiB; dropped collector evidence is explicitly marked truncated. Source capture traverses at most 1024 frames, attempts at most 64 unique URLs and retains a 512 KiB prefix per source with explicit truncation; base64 validation includes any discarded tail. CLI trace requires complete document readiness. Stop keeps the existing event consumer active through artifact capture, closes its subscription, drains its finite queued events/errors and joins it before archive serialization. Publication is atomic and exclusive, preserves existing archives and removes only owned partial files on failure. `ReadTraceZip` validates the actual opened regular file and rejects compressed input above 64 MiB, entries above 8 MiB, decoded aggregate above 64 MiB, more than 132 entries and duplicate names without returning partial decoded content.

### Standalone smoke evidence

`--scorecard-stdout` emits the exact already-published scorecard bytes after owned cleanup, including completed failure evidence. Diagnostics remain on stderr; a stdout read/write failure returns 1. Interrupted or incomplete runs do not emit a completed scorecard. This supports composition without duplicating the existing runner.

The public `smoke` package owns this same scenario schema, runner, scorecard, outcomes and evidence lifecycle. `smoke.Command(args)` implements the existing CLI contract; `cmd/artemis-smoke` delegates to it. Library callers may use `LoadScenarios`, `NewRunner` and the existing owned runner methods directly. Co-located tests exercise the same owner; there is no second smoke implementation.

`DefaultFixture(ctx, source)` and `DecodeFixture(ctx, manifest, source)` admit bounded YAML bytes without creating resources. `DecodeScorecard(ctx, manifest, source, data, readArtifact)` checks the complete canonical schema-2 scorecard, source/matrix/manifest bindings, ordered scenario/step identities, outcomes/counters, binary/readiness evidence and every referenced artifact's exact bytes/hash/size. The supplied reader owns filesystem or canonical-store authorization; the decoder grants neither. Large retained command values are checked through their artifacts, including identity/assertion outcomes and equality with any inline preview. `ConfigurationDigest(host, port, stepTimeout)` exposes the producer's same digest for normalized non-secret settings. Hash/schema validation does not authenticate a producer or execute stored data.

Smoke inputs carry the shared fourteen-field fixture manifest in `inputs.fixture_manifest`. The descriptor binds the exact loaded YAML bytes, content-addressed identity, complete sorted command-capability set, Serve runner, assertion gate and expected scorecard. The standalone default declares internal content and run-evidence retention. `--fixture-manifest <path>` instead admits bounded canonical manifest JSON against those actual bytes before building or starting a browser; mismatched source, runner, capabilities, gate or expected output fails without fallback. Declared privacy, retention and domain/connector references remain intact. These declarations are transport evidence, not producer authentication, a registry registration or an applied retention job. The isolated mode describes the owned browser/session lifetime; configured live sites remain external network targets. This option does not change existing smoke network authorization or assertions.

`go run ./cmd/artemis-smoke --scenarios testdata/smoke/scenarios.yaml --build-dir . --out artemis-smoke-out` builds one owned Artemis binary and drives the actual authenticated steering protocol. `--bin` instead snapshots a caller-owned regular executable without altering it. `--host` must be numeric loopback (default `127.0.0.1`); `--port=0` assigns an independent ephemeral listener. Positive `--step-timeout` defaults to `30s`; each scenario may supply its own positive timeout. The runner proves child PID, bound endpoint and fresh instance through readiness plus authenticated version before probing health or running commands. Parallel runs receive independent credentials and client identities. Unmet `requires` environment variables skip before spawning.

`--out` is an output parent. A private staging directory contains every persisted result and log; one exclusive `run-*` reservation receives the complete `evidence/` directory by atomic rename after session, socket, process, reader and temporary-binary cleanup. Completed evidence includes a schema-2 `scorecard.json`; existing results are preserved. Interrupted/failed publication removes only owned staging/reservations, while unexpected foreign contents are retained and reported. A failed final parent sync reports that complete evidence was already published; it never fabricates a clean result or removes that completed evidence.

The scorecard binds actual binary SHA-256/bytes/version/Go version, embedded VCS revision availability and dirty-build status, raw scenario-file SHA-256, normalized immutable matrix SHA-256 and normalized host/port/step-timeout SHA-256. The configuration digest does not claim to identify the ambient environment, remote-site content or compiler reproducibility. Each artifact reference includes its relative path, actual SHA-256 and byte count; these bytes are verified before publication. `page.dump` persists the full typed response as JSON. Results above 64 KiB omit inline `got` and retain `value_artifact`; no payload is silently truncated. Step/scenario elapsed values are integer milliseconds; scenario timing includes cleanup. Scenario input is limited to 1 MiB, 128 scenarios, 256 steps per scenario and 4096 total steps; each artifact is at most 8 MiB, aggregate persisted output at most 64 MiB and each drained process log at most 1 MiB. These are serialized payload limits, not total heap limits.

Outcomes are `pass`, `missing_environment`, `network_skip`, `network_failure`, `product_failure`, `harness_failure` and `interrupted`. Only a canonical `fetch_failed` response with structured `failureKind=network` may become a tolerant `page.open` skip; message text, policy/resource failures and assertion failures never establish an outage. `network_fail: strict` retains the observed network failure. Failed cleanup overrides a skip. Product/network/harness failure counters remain separate; `failed` aggregates all non-skipped failures. Successful or deliberately skipped completed runs return 0; any non-skipped/runtime/output/cleanup failure returns 1 and rejected CLI arguments return 2. SIGINT/SIGTERM cancels owned work before the CLI exits. Shared Go caches remain reusable; temporary compiler output belongs to the same disposable binary owner.

`binary.commit_known` and `binary.modified_known` distinguish observed VCS metadata from its absence. A binary built with `-buildvcs=false` does not claim a known clean build. An empty `network_fail` retains the scenario format's default tolerant mode; use `strict` to reject observed external-network outages.

### Local fixture admission

Capability conformance source identity and declared-test discovery include JS, Engine, Agent, Network, CSS, WebAPI and Renderless owners. The existing compiled catalog reports ConformanceCoverage with declared/mapped counts and every unmapped source reference in stable suite/test order. Shared references count once, and returned omission references detach from the compiled cache. These are catalog-linkage counts: platform build eligibility, passing runs, requirement coverage and release admission still need their actual proofs. Missing mappings do not grant additional support; source presence and test declarations cannot promote an API beyond its existing scoped case.

RenderlessCapabilityProfile.bindingCapabilities classifies every exact observed descriptor in source-inventory order, including nested, private, native, null/undefined and alias descriptors. Each record names its current conformance case. The existing Registry remains the only authority: declared API access paths and their canonical binding targets receive their recorded scoped evidence; object-level support does not promote children or undeclared aliases. Unmapped descriptors and conflicting evidence require escalation. The installed `PerformanceObserver` has no callback delivery and remains escalation-required. Global case-conflict denials also apply to descriptor claims. Classification uses one Registry snapshot and a linear pass over APIs plus descriptors. Returned records and CSS-derived copies own independent slices. This exhaustive descriptor accounting does not establish browser parity, passing test runs or semantic support for unmapped members.

Cookie writes use the existing Runtime-cached DOM bridge and resolve the current Context at invocation; the isolate callback registry does not retain closed pages through a per-page cookie closure. Failed native string conversion cannot reach the cookie hook. Weak-pointer controls prove actual closed Go Context collection while the Runtime stays alive in both pool modes. Pristine realm reuse releases reset/seed results, Storage lookup handles and intermediate navigator-language handles; repeated actual reuse preserves native page/internal handle counts.

The performance clock has one cached native template per Runtime and a monotonic origin per Context. Fresh, snapshot-created and pristine-reused realms publish the current Context's actual timeOrigin; bootstrap initialization cannot erase it. Clock arguments/receivers/results and installer handles retire after native use. Fresh realms receive the existing entry bootstrap before page admission, without redundant native entry stubs. Full Performance entry/observer semantics and entry-memory admission remain outside this clock proof; the existing unsupported classification is not promoted by these controls.

`crypto.getRandomValues` fills the actual bytes of all nine integer TypedArray brands, including signed/clamped and BigInt views, from Go crypto/rand. It preserves the input object, respects view offset/length, accepts empty views and measures the 65,536-byte quota in bytes. Plain arrays, floating-point arrays, DataView, buffers and proxies reject; the existing shared/detached-backing boundary remains closed. Native byte length and exact-region writes do not read page properties or dispatch setters. Invalid types and quota failures throw actual realm DOMException instances with TypeMismatchError/QuotaExceededError; missing arguments throw native TypeError. The constructor is captured before page scripts and released on close, and exception code lookup uses a private frozen map. Random callbacks and installers retire native handles and use Runtime-cached templates instead of registering per-page callbacks. RandomUUID retains its version-4/variant shape. This does not claim complete SubtleCrypto or WebCrypto parity. Reference: https://www.w3.org/TR/webcrypto/#Crypto-method-getRandomValues.

Console, native URL parsing and computed-style callbacks retire native arguments/receivers and transfer owned results/errors after return copying. Console converts all arguments before one sink publication, admits at most 4096 arguments and 1 MiB of joined UTF-8 text including separators, and rejects conversion/quota failures without publishing partial text. Sink retention remains caller-owned. The retained console joins converted strings; complete browser formatter/devtools behavior is not claimed. URL results use checked writes on null-prototype objects; inherited setters cannot intercept construction, and href/base conversion failures propagate. Computed-style identity validation avoids numeric coercion, property conversion failures propagate, and typed CSS failures remain execution-observable after JavaScript catches them. These are ownership/error repairs, not complete WHATWG URL or CSS/browser conformance.

The iframe owner admits at most 64 child documents/failures and 16 MiB of cumulative admitted HTML source per parent Context, additionally honoring the configured response-body limit. Transport/response/parse/build/script failures remain observable through the owning execution and are cached without repeating failed transport. Empty successful HTML responses create actual empty documents. Close releases child contexts, documents, failures, pending payloads and accounting. Native frame callbacks retire their argument/receiver/result handles.

Parent-to-frame JSON messages load the child before delivery, use a captured serializer, validate native payloads and bound pending ownership to 256 messages and 1 MiB total serialized UTF-8 bytes. Draining releases payload accounting and target indexes. The default target origin `/` admits only the parent's actual HTTP origin; explicit HTTP origins and `*` compare against the loaded document's final URL after redirects. Events carry the serialized sender origin with default ports omitted. Transfer lists requiring transfer reject explicitly. This is the retained synchronous JSON subset: full structured clone/transfer, browser task ordering, bidirectional WindowProxy identity, nested frame loading, navigation and complete frame event semantics remain open. Reference: https://html.spec.whatwg.org/multipage/web-messaging.html#posting-messages.

DOM bridge callbacks retire their argument/receiver handles and transfer owned return handles after native return-value copying. Cached null/undefined values stay isolate-owned. Node/document lookups, mutations, inline-style results, cookie access, DOMParser and static event-attribute enumeration have repeated real-operation handle-count controls. Native collection and attribute-tuple objects have null prototypes, check every write and release intermediate handles; inherited page setters cannot intercept their construction. Caught DOM exceptions retire their native values immediately rather than accumulating until evaluation ends. These controls cover native handles, not whole-document node-table memory or full browser DOM parity.

The Context timer owner uses a monotonic deadline heap with deterministic ID ties. It admits up to 4096 owned timers and 4096 callbacks per pump, supports timeout/interval arguments and string handlers, cancellation and nested-delay clamping, and retires callback handles on completion/cancel/close. Eval and pending-fetch WaitIdle surface synchronous callback exceptions while continuing other due callbacks; failed one-shot timers retire and repeating timers rearm after exceptions. Each pump retains at most 16 detailed timer failures and reports the count of additional failures. WaitIdle wakes for due work and owner cancellation without a persistent thread. WaitIdle does not wait for all future page timers. Complete browser event-loop/CSP semantics and wider loader-level exception reporting remain TASK-2795 work. Timer reference: https://html.spec.whatwg.org/multipage/timers-and-user-prompts.html#timers.

V8's native rejection hook records actual Promise identities and reasons per creation realm. Handler admission removes pending identities; take and physical close retire retained native roots. Each realm admits at most 4096 pending rejections, with sticky quota failure until take. Diagnostics do not invoke reason getters/toString: up to sixteen messages per realm inspect primitive reasons or Error own-data messages. Strings over 2048 UTF-16 units use fixed reporting; shorter strings copy at most 2048 UTF-8 bytes plus an omission suffix. Each execution observes its own/actually touched creation realms and retains at most sixteen sorted context reports; untouched siblings remain independent. Empty/opaque reasons remain failures, and typed UnhandledPromiseRejectionError prevents acknowledged success at Eval/WaitIdle boundaries. Foreign creation realms join the actual executing owner; logical close cannot erase a pending report before physical disposal. Loading checkpoints retain rejected-listener failure across retry, and actual HTTP script/loading rejections reject Page admission. Full browser unhandledrejection/rejectionhandled event dispatch and complete event-loop parity are not claimed.

Runtime construction, Eval, EvalString, EvalDiscard, WaitIdle and loading admission share one cancellable isolate gate. Caller deadlines and runtime/context closure interrupt native JavaScript, including timer/microtask/loading callbacks; started termination callbacks join before V8 execution is restored for later use. Nested iframe scripts/messages borrow that same execution span and also observe their child lifetime; only the outermost span clears native termination after all frames unwind. Inline script and message-listener failures cannot acknowledge parent success even when parent JavaScript catches the exception. Queued cancelled callers return without performing script effects. EvalString copies its string and EvalDiscard retires its effect-only result before releasing that owner. Eval results remain owned by their Context until Value.Release or Context/Runtime.Close; wrapper accessors serialize with close and become inert after owner shutdown. Raw returns a borrowed native pointer and cannot be used after release or close. Runtime.Close cancels its lifetime, closes registered contexts and the pool, then disposes the isolate; repeated concurrent closes join the same cleanup. Full browser WebAPI parity is not claimed; final conformance, isolation, mutation and race acceptance remain in TASK-2795. The caller still supplies operation deadlines; a background context does not acquire an invented deadline.

The existing compiled JS conformance identity includes every local Go source/test, the retained native wrapper's Go/C/header sources and Go tests, the linked V8 version, and the cookie/DOM owners' sources/tests. CSS and WebAPI receipts therefore invalidate when these owners change. This source identity neither authenticates a producer nor claims a native archive or complete binary identity.

The vendored native named/indexed/batched property writers return setter and Proxy exceptions as Go JSError values instead of aborting through V8 Maybe.Check. A failing batch stops before later writes; empty thrown values retain an error marker. Primitive conversion preserves NewValue failures and retires its own temporary handles on success/failure without releasing caller-owned Valuer inputs. Internal isolate and page-context handle counts are separately inspectable. Checked string conversion preserves thrown exceptions; Promise Resolve/Reject return false for an empty native Maybe instead of aborting. The caller must check returned errors; these bindings preserve V8 assignment semantics and do not authenticate page-controlled objects.

The context pool admits only pristine realms. Any Eval, loading/idle pump, nested frame execution or exposed V8 context permanently retires that realm from reuse; exposing the raw isolate also disables reuse. Deleting globals cannot reset lexical bindings or prove prototype isolation. Pristine and initially warmed realms remain reusable, with current Fetch/Navigator configuration and storage bindings. Async Fetch admits at most 64 unresolved requests, including completed results not yet drained; excess promises reject before spawning transport work. Each worker publishes exactly one completion, and Close joins all admitted workers before retiring pending native resolvers and destroying the realm. Requests borrow active Eval cancellation, joining actual transport cleanup before cancellation acknowledgement; successful execution detaches that transient caller and transfers unfinished work to the existing Context lifetime. Borrow records share the 4096-per-operation callback ownership bound, and retired results release response/error references. Injected FetchFunc implementations must honor cancellation and finish transport cleanup before returning. Complete response/Abort and callback capability proof remains open.

MutationObserver owns one callback and chronological record queue across all its targets. Repeated observe replaces that target's options without discarding pending records; overlapping direct/subtree registrations deliver one record per observer, merging requested old-value interest. Public callbacks use observer construction order and receive the observer as this/second argument. Private weak state rejects foreign method receivers; the existing DOM wrapper cache supplies real node identity, including Document, without another node registry. Filters accept iterable DOMStrings, preserve case-sensitive names and detached input snapshots, and support empty filters. Standard attribute/character-data defaults and invalid-option combinations are validated before admission. Attribute writes/removals, inline CSS and text/comment data changes capture oldValue only for interested observers; other records expose null. Missing-attribute removal emits no mutation. Reference: https://dom.spec.whatwg.org/#interface-mutationobserver.

Each Context admits at most 4096 permanent/transient registrations across all observers, 4 MiB of accounted retained options and 4096 pending records with a separate 4 MiB metadata/name/old-value/ID-backing bound. Each filter admits at most 1024 names and 64 KiB of UTF-8 data; checked native conversion rejects excess before allocating output buffers. Replacing existing target options does not consume another permanent slot. Admission checks complete deduplicated fanout before queuing any branch. Overflow leaves earlier records intact, throws through all DOM/inline-CSS writers and fails the owning evaluation even if page code catches it; the already applied DOM mutation is not rolled back. Construction failure preserves queue ownership; successful takeRecords, delivery before callback entry, disconnect and close release it. Exhaustion of the monotonic signed-native identity range rejects registration without retaining its callback. Callback delivery and takeRecords share the captured-record converter; null-prototype native objects and own-property JS array writes prevent inherited setters from intercepting transport. Checked allocation/writes retire intermediate handles; failed options do not retain callback arguments, and disconnect removes target/filter indexes. Delivery admits at most 4096 callbacks per pump and reports callback errors after the current batch. Eval/WaitIdle propagate errors; the Engine loader rejects failed Page admission. DOM insertions reject cycles, invalid parents/document children and foreign references before detaching; mutator validation failures throw without false records. Public child writers validate actual Node receivers/arguments, including required insertBefore references. Native child identities are finite bounded integers and unknown references cannot become append operations. Broader DOM and browser event-loop parity remain open.

Tree records capture previousSibling/nextSibling at the actual removal/insertion positions. Append/insert moves queue the original-parent removal before the destination addition, including same-parent/self-reference moves. Both events and transient admission share one capacity check, so a failed batch queues neither half. Full child replacement retains its detached roots; empty replacements emit no record. Subtree observation follows removed/moved descendants until that observer's next notification. Source registration identity is preserved through nested removals and deduplicated repeated detaches. takeRecords drains records while keeping inherited interest until notification; notification retires it before the callback, including when the record queue is empty. Reobserve retires only transients derived from that target's prior options. Disconnect/Close release every transient index and accounted option. Failed record serialization retains pending records and inherited interest for recovery. Ancestor/removal walks observe caller and Context cancellation.

WebSocket opening/read/send workers share closed admission and join before Context close; event pumping is bounded to the existing queue capacity. WebSocket property handlers and listeners reuse the Document/Window event error owner, including once, capture/removal identity and callback objects, with weak keys for collectable non-DOM targets. This cross-bootstrap owner is installed per context rather than frozen into the startup snapshot.

HTML `document.title` assignment updates the owning DOM through the existing node/mutation path. Go `Document.Title`, `Page.Title` and the JS getter share the first HTML title in document order, ignoring inline SVG titles; a missing title is created only when a head exists. The getter collapses ASCII whitespace while preserving non-ASCII spaces. Assignment stores literal child text, performs string conversion before changing the DOM, and rejects Symbols. Existing body titles, duplicate titles, MutationObserver delivery and real HTTP/script execution have behavior tests. SVG/XML document-title semantics and whole JS conformance remain unfinished lifecycle work. Reference: https://html.spec.whatwg.org/multipage/dom.html#document.title.

The Engine's eager script path completes owner-backed loading stages once: interactive/readystatechange, bubbling Document DOMContentLoaded, complete/readystatechange and Window load with the legacy Document target. Document, Window and elements share one listener registry with capture/bubble phases, once/passive options, removal identity and immediate propagation stop. Listener errors are bounded to sixteen 2048-character diagnostics per drain and surface through Eval, WaitIdle and loading admission after the other listeners run. A failed loading admission returns no Page. Cancellation prevents later loading stages. The dispatcher is captured before page scripts; pooled reuse resets the owned registry, wrapper cache and loading property handlers. Streaming parsing, module/defer/async script ordering, full resource-loading dependencies, iframe loading phases, AbortSignal listener options and complete browser event-loop/error semantics remain TASK-2795 work. Reference: https://html.spec.whatwg.org/multipage/parsing.html#the-end.

Benchmark writers that omit `Manifest.ID` derive `benchmark:<matrix-version>:<payload-sha256>:<case-id>` from the complete canonical input, including HTML and assertions. The standalone Harness uses this form, allowing changed case versions to coexist in the immutable registry. Explicit existing case IDs retain their bytes and remain readable; they cannot overwrite another retained descriptor. Decode requires a complete manifest and rejects a derived identity whose version/hash/case does not match the actual input, even after transport hashes are resealed. The private resolver binds the requested semantic case name to the decoded case while retaining its exact version reference; the Harness still requires complete equality with the requested matrix input.

The benchmark harness rechecks every selected fixture through the configured resolver after workload/competitor/profile cleanup and before finalizing or publishing the scorecard, report and sealed manifest. A revoked input, changed source/metadata or cancelled owner returns an error and leaves those outputs unpublished. Actual in-memory samples remain observations, not an acknowledged completed run. Standalone execution keeps its already admitted local inputs; the private resolver checks current canonical permissions, scanner, retention and byte bindings. `RunBenchmarkFixture` likewise checks current canonical authority after engine cleanup before acknowledging a valid sample. These checks are point-in-time admission, not an atomic lock against a subsequent external revocation.

`corpus.RunServe` admits the exact Serve manifest and source before creating its real loopback listener, then uses the existing authenticated client, session and page commands. Readiness must match the authenticated server instance/process. The owned fixture Agent uses the same finite origin/session/tab/network/time budgets as RunAgent, an origin-scoped static action policy permitting only navigate/evaluate/wait/extract/tab_close, and connection/client bursts of 32. Product policy and rate defaults are unchanged. Actual status, evaluation, text, title, HTML, Markdown and links are validated against the admitted scenario. Page/session/client/server/Agent cleanup contributes errors before passing. Private `RunServeCorpusFixture` checks current canonical source before execution and acknowledgement; all six corpus entries reject revoked cached references.

`Session.OpenPage` accepts at most one optional `PageOpenOptions{AsyncFetch: true}`; existing three-argument calls retain synchronous behavior. Serve `page.open` transports the same optional `asyncFetch` field, with absent/false omitted and unchanged. `page.wait_idle` explicitly waits for pending script fetches; page/session/server close owns cancellation. Streaming retains its existing synchronous opening contract. Both Serve corpus paths preserve the source scenario's async setting. Real concurrent-response and pending-HTTP cancellation controls establish this bounded behavior, without promising general native network idle or whole browser conformance.

`corpus.RunAgent` requires the Agent manifest, owns the existing public Agent/Session/ExecuteTask lifecycle and returns its actual page evidence. It restricts HTTP to the admitted server origin and configures one session/tab, 64 requests, four concurrent requests, 64 MiB total response bytes, one byte of disk admission and 30s cooperative work. Session close and Agent stop contribute errors before passing. Task failure retains its actual typed category; challenge denial never becomes a synthesized 403 page. Private `RunAgentCorpusFixture` checks current canonical source before execution and before acknowledging success. The seven original evaluation fixtures execute through the same retained task page and their full semantic validator; the obsolete Agent Eval quarantine is removed after that proof. The original corpus adapter carries the same expression/result and closes each per-case Agent session. Agent and Serve share the existing exact-origin action policy for their explicitly admitted fixture work; the product default policy still denies evaluation.

`FetchAction.AsyncFetch` is an optional `asyncFetch` boolean in the existing task JSON. Absent/false preserves synchronous behavior. True reaches both renderless executors through RouteRequest/ExecutionRequest and waits for actual pending fetches before transferring output; the direct dispatcher carries the same setting. It does not grant extra network access or promise general native network idle. The corpus maps its declared AsyncFetch setting into that field.

`FetchAction.EvaluateExpression` optionally transports `evaluateExpression` in task JSON. It evaluates one JavaScript expression on the actual retained renderless or managed Chromium route before page cleanup, without refetching. Explicit `evaluate` permission is required before navigation and again for the actual final page; the default policy denies it. Expressions must be nonblank, NUL-free UTF-8 within 64 KiB. JavaScript template-string coercion produces an owned UTF-8 string, bounded to 64 KiB; conversion exceptions and exceeded bounds fail the task. `PageResult.Evaluation` is an optional string pointer, preserving a requested empty string while leaving the old wire unchanged when absent. The final page snapshot is refreshed after evaluation. The existing script/task/session contexts apply, but pure-JS hard interruption remains unfinished lifecycle work. Evaluation is synchronous and does not implicitly await a returned Promise. Native evaluation reuses the existing action/evidence/policy runtime without replay. Renderless string copying is serialized under the existing isolate owner; no live V8 handle crosses this new boundary. All exits include the route's actual close result. A custom dispatcher without the retained route resource cannot silently acknowledge the requested evaluation.

`corpus.RunHybrid` requires the exact HybridRouter manifest and runs the existing static/renderless/native/stealth executors under the same owned origin, headless launch and bounded lifecycle policy. The original corpus adapter delegates its request/observation/resource-close path to this shared owner. Evaluation reads the actual transferred renderless page or the selected native page after readiness. `router.RuntimeExecutor.AsyncFetch` explicitly enables concurrent HTTP fetches and waits for their settled output before transferring the page; its zero value preserves existing behavior. The corpus opts into that mode. Known restrictions and actual router errors remain non-passing, including challenge denial; no synthetic status/body or replacement renderer is used. Private `RunHybridCorpusFixture` resolves current source before execution and rechecks authority before acknowledging success. Relative and absolute concurrent-fetch controls use the same JS URL resolution and canonical subresource admission; broader JS lifecycle/capability work remains TASK-2795.

`corpus.RunChromium` requires a Chromium manifest, uses the existing owned Chromium bridge/process/context/page and shares target/frame/loader-bound navigation/async observation with the native corpus adapters. The run forces a fresh sandboxed headless profile and its own HTTP origin policy, with bounded startup/shutdown/session time; operator process authorization/resource hooks remain available. Shared profiles, extra flags and caller network/proxy overrides are rejected. Actual observation, original assertions, context cancellation and every owned close contribute to the result. Internal status observation uses `TraceRecordConfig.DiscardArchive`: its existing event consumer is drained/joined and bounded captures released without creating unused ZIPs or output directories. Default archive recording remains unchanged. This path does not claim hybrid/Serve/Agent or whole native-network parity.

`corpus.RunArtemis` requires the BrowserRuntime manifest and executes through the existing ephemeral session/page authority with the same native observer and assertions. It shares the bounded headless launch/origin policy while preserving process authorization/resource hooks. One owned temporary directory contains the runtime manifest and profile; page/session cleanup precedes identity-checked directory removal, and every cleanup error prevents passing. The run has a cooperative 30s deadline checked again after cleanup. Session cleanup uses the existing synchronous owned-browser shutdown bounds and an independent context for the manager's subsequent metadata cleanup; it does not claim that a caller timer cancels native shutdown. Observation, page-close and session-close errors identify their actual stage. It does not accept caller profiles or attach to existing sessions. Private `RunArtemisCorpusFixture` resolves current canonical source before execution and checks current authority again before acknowledging success.

`corpus.RunRenderless` resolves the exact admitted case, requires the renderless runner identity and owns a fresh existing engine/server for that run. It shares actual fetch/idle/observation behavior with the existing renderless corpus adapter and retains every original semantic assertion. Its network policy permits only its own HTTP origin, GET/POST, with explicit response/request/concurrency/disk budgets and a cooperative 30s owner deadline. Observed status/title/text/HTML/Markdown/evaluation/links and manifest identity remain in Result; passing includes page/engine/server cleanup. Cancellation, source corruption, another runner and actual assertion failures cannot count as passing coverage. This entry does not establish native runner support, whole process/heap isolation or standards completeness.

The default server now admits one complete canonical corpus payload before opening its listener. It preserves every descriptor, assertion and supporting route in the original order, including whether each route uses a built-in dynamic handler. The payload pins the embedded fixture producer/server source and current Go runtime version; source files are used only to calculate that pin and are never serialized or served as fixture data. Decode accepts only the exact current corpus, so resealing changed routes, omitted cases, handler declarations or assertions cannot load a server. Dynamic handlers are rebound only from the matching compiled built-ins. This source pin is not a complete dependency-build signature or producer authentication.

`corpus.DefaultAssets(runner)` returns individual case/runner manifests over one shared content-addressed payload. All six runner names are closed, and each manifest retains its exact case, runner, kind requirement and failable corpus-test gate. `corpus.Validate` performs resource-free admission. The existing all-path and renderless corpus tests use the server's detached admitted inputs and bind requested inputs before execution; all-path result evidence includes its fixture manifest. Added routes, changed requested assertions and closed owners cannot keep the immutable corpus claim. These declarations do not bypass quarantine or establish passing runtime coverage, canonical storage permission or applied retention.

The development-only `internal/fixture.Server` retains one routing owner and one bounded scenario snapshot. `Register`, `RegisterAll`, `RegisterHTML`, `RegisterRaw` and `RegisterHandler` return errors; `NewServerWithDefaults` returns `(*Server, error)` and closes a rejected owner. Registration validates the complete batch before publishing routes, rejects duplicate normalized paths or IDs, and detaches expectation slices on admission and reads. Routes must be canonical local paths without query/fragment, traversal, wildcard syntax or encoded aliases. Custom Go handlers remain trusted callbacks; their internal mutable state is not cloned or sandboxed.

Admission limits are 1024 scenarios, 8 MiB per inline body, 32 KiB of ordinary scenario metadata and 64 MiB of registered inline body plus metadata. Inert replay Title/Contains assertions use the bounded snapshot-data exception described under Replay asset execution and remain fully counted in that registration budget. Metadata/expectation fields and script/count estimates are bounded and invalid kinds/status/content types or typed nil handlers fail before routing. Binary raw bodies remain byte-preserving. Requests have a shared 32 MiB body ceiling, eight active handler slots, 32 KiB HTTP headers, a 5s header deadline and 10s request-read deadline. The existing multipart upload reader streams without disk spill and keeps its 32 MiB encoded-request ceiling. Excess declared bodies return 413 before handler dispatch; capacity exhaustion returns 503. A benchmark runner retains the actual fixture/engine initialization cause and cannot reset a rejected fixture owner into a valid sample. These mechanics do not establish canonical asset registration, replay durability, quarantine coverage or standards parity.

Cross-adapter fixture assertions require the actual expected status; unknown zero status cannot satisfy coverage. Chromium bridge and owned BrowserRuntime fixture adapters subscribe through the existing bounded trace collector before explicit navigation and identify the document response by target/frame/loader. Final URL/status come from that response, not page-authored values or child frames. The collector is joined after extraction and its owned debug archive is disposable test evidence. Router/Agent errors fail the case without inventing a challenge response. The matrix tests execute the original challenge through both guarded runners and require their actual denial, absent page and zero passing page coverage; those negative-control successes do not satisfy the unchanged 403-page expectation or produce page replays. Other runners retain every original response assertion. Native admission or semantic mismatches still fail the tests; a green matrix test is not complete passing corpus or standards coverage.

The development WPT runner serves admitted files byte-for-byte, including the original testharness.js; it does not drop delayed callbacks or synthesize load events. Both its default constructor and externally governed input use one canonical version-1 JSON bundle containing the full pinned Subset and every original mirror file. The manifest binds the complete source/assertion bytes, exact revision identity, runner/mode and actual capability set. Recomputing a digest cannot admit changed assertions/capabilities/origin/revision, omitted/duplicate/aliased files or altered scripts. Raw embedded bytes are capped at 8 MiB aggregate, 1024 files and 16 MiB encoded input. Requested subset cases must retain their admitted path/name/expected outcome/capability; no omitted or changed assertion can substantiate coverage. The public wpt package exposes DefaultFixture, resource-free ValidateFixture and RunFixture through this existing runner so embedding hosts can resolve their canonical artifacts first. RunFixture requires the complete pinned result set, joins cleanup/cancellation, retains completed failing reports as bound failure evidence and returns no passing evidence for missing completion. It does not create another registry or browser implementation.

Each WPT case waits for observed harness completion under the caller's deadline and a cooperative 5s ceiling. Reports are limited to 1 MiB, 1024 uniquely named tests and 4096 bytes per name/message; missing/null/unknown status, duplicate JSON fields, unknown fields, trailing data, missing requested tests and failed harness completion return errors without passing evidence. These are report/admission bounds, not a JavaScript heap or interruption guarantee. The unchanged default subset completes through the Engine's actual loading owner. This bounded pinned selection does not prove broad standards conformance; TASK-2795 retains the wider runtime contracts.

`ValidateResults(ctx, manifest, content, results)` reuses fixture admission and the runner's closed status vocabulary to validate every pinned report in its original order, including exact per-report manifest binding and bounded UTF-8 diagnostics. Omitted, duplicated, reordered, foreign-bound or invalid reports are rejected. This validates data identity; it does not authenticate a producer or turn failed results into passing conformance. Embedding hosts retain their own permission, scanner, acknowledgement and result-retention authority; the standalone boundary creates no canonical store.

Fixture eligibility is a bounded attempt decision, not a capability or passing-coverage claim. AssessEligibility accepts only the six actual runner identities, retains every matching restriction with its declared owner/expiry, rejects malformed/duplicate ownership contracts and expires them at the exact deadline. Unknown runners fail closed. Admission inside each selected scenario subtest prevents unselected cases from masking focused proof. Active/expired restrictions produce structured zero-passing-coverage failures. All original capability restrictions are retired after real outcome proof, including Agent Eval and Hybrid non-200 status; controlled historical contracts retain owner/reason/exact-expiry denial tests without extending the original devin/2026-09-30 deadline. Empty restrictions do not establish full corpus success. Native polling remains bounded to five seconds and does not prove general network idle. Both native Basic-Auth fixtures prove the actual unauthorized 401 through the existing Fetch challenge owner.

Challenge detection preserves actual 403/429 status and short-body diagnostic signals without treating those alone as CAPTCHA. Existing title, DOM and network indicators remain active; explicit challenge-response headers, including nonempty case-insensitive X-Challenge, remain fail-closed. Real HTTP controls prove ordinary 400/401/403/418/429/500/503 status/body/title observations and challenge-header denial at 200/403/429. The six original non-200 Hybrid cases retain five actual page outcomes and the original challenge guard rejection. That guard test does not grant passing coverage to the unchanged challenge corpus expectation of a 403 page, and no synthetic page or solved-challenge claim is introduced.

The native Page network owner enables Fetch handleAuthRequests and processes authRequired through its existing four workers, 128-job queue, 15s work contexts and joined close lifecycle. Auth-event admission is limited to 64 KiB and a 256-byte request identity; realm, URL, headers and credential-bearing request data are not copied into cancellation jobs. Foreign sessions are ignored; malformed events or exhausted capacity fail closed. Unhandled challenges use Fetch.continueWithAuth with CancelAuth and no username/password fields after live-context/page/context/session checks. Chromium renders the original unauthorized document, so status/body proof stays actual. Explicit authorized headers continue through the existing policy-validated request resolver and are covered by a real subsequent authorized request. This does not implement credential-provider selection, interactive login, MFA or every authentication scheme. Protocol source: https://raw.githubusercontent.com/ChromeDevTools/devtools-protocol/master/json/browser_protocol.json (Fetch.enable, authRequired, continueWithAuth, AuthChallengeResponse).

### Local replay records

Development replay records use schema 1, a random capture identity and SHA-256 of canonical serialized content with the content_hash value empty. Files are named replay-<id>.json; a runner/scenario pair can retain distinct captures without replacement. Save validates identity/schema/status/metadata/hash before publication. It writes and syncs an exclusively created 0600 stage, publishes through a no-replace hard link, syncs the output directory and ancestor entries, checks the output path still identifies the held directory, removes only its verified stage and retains close/cleanup failures. An error returns no acknowledged path; a complete published record may remain for recovery if a later sync/cleanup fails. New directories use 0700; existing permissions are preserved and group/other-writable output directories are rejected.

Load requires an actual 0600 regular file, at most 16 MiB encoded JSON, matching filename identity and canonical bytes without unknown/duplicate fields or trailing data. Capture text is UTF-8, limited to 8 MiB in aggregate, with at most 1024 links, 128 redaction-log entries and 4096 bytes per metadata field. macOS/Linux use descriptor-relative openat with no-follow/nonblocking flags; other platforms reject secure replay-file loading explicitly. os.Root.OpenFile alone does not enforce this no-follow boundary because it resolves internal symlinks. Legacy/incomplete records are rejected without being modified or deleted.

Redact returns (string, error) without partial output on failure. Capture compiles at most 32 custom patterns once, rejects invalid/empty-matching expressions, limits each expression to 1024 bytes and the UTF-8 mask to 128 bytes, and treats custom masks literally. Exact replacement-size counting precedes expanded output allocation. The profile hash binds the versioned algorithm tag, mask and ordered actual built-in/custom pattern definitions; raw regex texts are not serialized in records. Every observation/source/expectation/metadata field is checked. Hostnames are omitted, URL userinfo/query/fragment removed, decoded URL paths and opaque targets redacted, and sensitive MIME parameters masked. Opaque fixture/runner identities and URL authorities/schemes requiring redaction are rejected. Save/load enforce the verified profile and deterministic known redaction-log identities. LoadReplay(path) selects the default profile; LoadReplay(path, cfg) requires that matching custom profile or explicit Enabled=false for raw captures. Profile mismatch or a redacted record containing residual configured-sensitive content fails without a loaded record. This proves the configured profile, not universal absence of every possible secret.

AsRegression returns (Scenario, error), validates the sealed record and gives each capture its own regression identity/URL. It serves captured HTML or text/status and drops the original HTTP handler and script/async flags. It retains observed title/text requirements and rejects plaintext titles or oversized expectations it cannot reproduce. The projected scenario carries a private inert-content guard; registration rejects handler/script/async overrides. Its HTTP response preserves captured bytes and enforces CSP sandbox with default-src 'none', inline styles only, no base URI, form submission or embedding, plus no-referrer, nosniff and no-store. Both native adapters verify that scripts, meta refresh and embedded resource requests cannot alter the snapshot or reach a live endpoint; ordinary fixtures retain actual execution. Replay-only protection is recreated after validated loading and is not a serialized caller permission. The record checksum detects corruption; it does not authenticate a runner or hash a fixture handler's executable implementation. Canonical registry admission, exact provenance/retention, resource-bundle replay and complete isolated execution ownership remain active TASK-2794 work. Policy references: https://www.w3.org/TR/CSP3/#directive-sandbox and https://html.spec.whatwg.org/multipage/browsers.html#sandboxing.

### Redacted diagnostics

The diagnostics ledger stores only canonical policy fields (`operation`, `transport`, `result` and `reason_code` from closed value sets; values outside them are recorded as `unknown`/`other`, never verbatim; `host`, `port`; `session_ref` = HMAC-SHA256 under a per-installation key in `<ledger dir>/session.key`, or a per-process key for in-memory stores, so references are stable per installation and unlinkable across installations; `Engine.DiagnosticSessionRef`/`Agent.DiagnosticSessionRef` map a session to its reference) and numeric resource fields (CPU, RSS, requests, response bytes, disk bytes, tabs, concurrency). URL paths/queries/userinfo, page content, request/response bodies, headers, cookies, credentials, selectors, and raw session IDs are structurally absent. Network decisions are emitted before socket creation; a configured ledger failure denies the network operation. Chromium emits an immediate sample after readiness and periodic CPU/RSS/profile-disk samples; a persistence failure terminates the owned process.

Persistent JSONL defaults to `~/.artemis/audit/artemis.jsonl` for `serve`, `act`, and `observe`; `ARTEMIS_DIAGNOSTICS_FILE` overrides it and also enables persistence for `fetch`, `download`, `run`, and `trace`. On macOS and Linux, the mode-`0600` ledger uses a cross-process sidecar lock and atomic compaction; other platforms refuse persistent ledgers instead of locking process-locally. Appends and compactions are fsynced, including the directory after creation or rename; an append torn by a crash (unterminated last line) is dropped and the ledger rewritten, while acknowledged records survive. The ledger directory must be a real directory (made `0700`) and the ledger, lock and key files regular files: symlinks are refused and opened with `O_NOFOLLOW`. External replacement is detected by file identity as well as size and modification time. It enforces all defaults simultaneously: 7 days, 4096 records, 8 MiB total, 16 KiB per record. `artemis diagnostics --type all|policy|resource --limit N [--file PATH]` reads newest records without exposing browser content.

## Library API

### Fixture manifest transport

`corpus.ValidateResult(ctx, manifest, content, result)` checks the complete pinned input, exact result-manifest association and every original passing semantic assertion without starting a runtime. Observations must be NUL-free UTF-8, with at most 4096 links and 1 MiB aggregate text/link bytes; observed status and duration must be valid. A run that failed before owning a page may carry elapsed time with an otherwise zero result, never invented page data or passing coverage. Failed observations retain their data without claiming successful assertions. The embedding owner supplies current permission, diagnostic redaction, canonical storage, lifetime and source/output authority; this validator does not authenticate the producer.

`Manifest.ValidateReference` checks bounded inert metadata and canonical SHA-256 syntax without claiming to have read the referenced bytes. Captured corpus replays retain a detached optional fixture manifest inside their existing sealed result. Capture checks its current corpus/case/runner association before stripping executable handlers; load retains the inert historical reference without requiring the current corpus generation. Its strings and lists enter replay byte bounds and the configured identity privacy checks; unsafe opaque provenance rejects capture rather than silently changing its identity. The optional field is omitted when absent, preserving earlier record encoding. Retaining this reference alone does not resolve or retain the original corpus payload through an artifact closure.

The public fixtureasset package contains only portable manifest data and byte validation, with no native browser dependency or registry/storage authority. Manifest exposes fourteen JSON fields: id, owner_subsystem, asset_path, asset_hash, fixture_kind, privacy_class, domain_refs, capability_refs, connector_refs, expected_artifact_refs, replay_mode, runner_ref, quality_gate_ref and retention. Kind has twelve closed values; ReplayMode has isolated/shadow/dry_run; PrivacyClass describes content as public/internal/personal/special_category rather than diagnostic readership.

Manifest.Validate(content) verifies the actual SHA-256 and bounds asset bytes to 16 MiB, references to 64 per list/256 bytes each and aggregate metadata to 32 KiB. AssetPath is an opaque reference bounded to 1024 bytes; the embedding owner resolves it with its actual access policy. Encode(manifest, content) returns canonical JSON capped at 128 KiB. Decode(data, content) rejects unknown/duplicate fields, omitted/null reference lists, trailing/noncanonical data, invalid UTF-8 and changed asset bytes. Detached preserves reference order and gives callers independent lists. Retention is an explicit lifecycle-profile reference, not an automatic deletion request. These APIs validate transport; producer authenticity, current access, retained availability and successful execution require the existing embedding authorities.

The benchmark package's EncodeFixture(scenario, manifest) returns EncodedFixture with canonical matrix-versioned JSON containing the complete Scenario and actual HTML. The caller explicitly supplies ownership, content class, gate and retention; ID must equal the scenario ID, kind must be benchmark and runner_ref must equal FixtureRunnerRef (artemis:benchmark:renderless). Hash/path are derived from actual JSON; conflicting existing declarations fail. DecodeFixture validates actual bytes and rejects unknown, duplicate, omitted, noncanonical or trailing fields while retaining every execution flag and expectation. Inputs are local renderless scenarios with at most 8 MiB HTML. NewArtemisRunnerFromFixtures accepts 1-1024 inputs under a 64 MiB aggregate encoded limit. Every RunScenario/RunScenarioBench must match the complete registered input; callers cannot replace HTML or weaken expectations after admission. The embedding owner supplies current permission/scanner/retention authority. Transport validation alone never counts as successful execution or complete isolation.

Both benchmark command entries use Harness's admitted fixture path. Harness selects the requested workload inputs before profiling, downloads or output. Standalone built-in inputs produce portable public-synthetic manifests with explicit owner, runner, semantic gate and run-evidence retention references. HarnessConfig.FixtureResolver optionally delegates current admission to an embedding authority; configured denial never falls back to standalone production. Returned inputs must preserve the exact requested scenario, including HTML, execution settings and every assertion. The harness rechecks configured authority before each workload and rejects changed bytes or manifest metadata. ScenarioEvidence.fixtureManifest records the complete detached declaration beside the existing HTML/expectation evidence, within the existing sealed scorecard cohort. No additional fixture directory or source copy is created. These records bind input identity; they do not authenticate the producer or prove an unexecuted capability.

### Replay asset execution

PrepareResultEvidence(ctx, content, manifest, policy, result, diagnostic) checks exact capture/hash identity and the complete original passing projection assertions, then returns ResultEvidence sanitized under that input's verified profile. It starts no runtime and reuses the record's compiled redactor for every newly observed field and diagnostic. Both original and sanitized passing observations must satisfy the original assertions; redaction cannot rescue a false result. Failed runs retain observed data or an otherwise zero absent-page result with elapsed time. Observation text/title/URL are bounded to 8 MiB plus 64 KiB aggregate metadata, diagnostics to 64 KiB, with valid UTF-8 and nonnegative duration. The caller must supply the same Policy on every admission; custom regex definitions are not part of result transport, and stored data never grants an unredacted opt-out. Producer authenticity and canonical result storage remain embedding responsibilities.

Inert replay projections retain full captured Title/Contains assertions under an 8 MiB aggregate assertion bound. Every assertion byte counts toward the existing 64 MiB server registration quota. Ordinary fixtures retain their 16 KiB field/32 KiB metadata limits, and other projection metadata keeps those limits. Captured serialized source always uses ordinary bounds; the private in-memory projection flag cannot create an unloadable capture. Inert handler/script/async/idle/script-count/evaluation overrides fail before registration. Projection errors preserve their concrete validation cause.

The replay package uses the existing capture record, redaction compiler, fixture server and renderless engine. CaptureSnapshot(observation, declaration, policy) returns a manifest and canonical record bytes. The caller supplies actual observations plus explicit ownership, privacy class, quality gate and retention; generated identity/hash/path must be empty on input. The declaration must name kind replay, mode isolated and runner_ref replay.RunnerRef (artemis:replay:renderless). This captures an observed snapshot, not original executable source or handler state. BindAsset attaches declarations to existing strict record bytes; ValidateAsset additionally requires their complete actual digest/path binding. Policy's zero value requires the default redacted profile; custom Mask/Patterns must match capture exactly and AllowUnredacted is an explicit opt-out.

Run(context, content, manifest, policy) verifies the record and executes its existing inert projection. A fresh engine allows only GET over HTTP to the exact owned loopback hostname/port, one request/tab/concurrent operation, 8 MiB response bytes and a ten-second owner deadline. A random access token protects the server's index and snapshot before registration; it stays in the owned request header. Page, engine and server close before success. HTML uses actual document title/text; text/plain uses the actual MIME header and unchanged response bytes. Result binds actual status/title/text and elapsed integer milliseconds to capture ID and asset hash. Invalid semantics, profile, input, cancellation or cleanup fail rather than contribute passing evidence. No disk replay copy is required. The embedding owner must revalidate current permission/scanner/scope/retention and retained bytes before Run. Producer authenticity, resource-bundle reproduction, general Engine MIME parity, process/profile isolation and canonical product-wide budget ownership are separate contracts; isolated here declares the inert snapshot mode.

### Engine usage

```go
import (
    "context"
    "time"
    "github.com/Christopher-Schulze/Artemis/engine"
)

eng, err := engine.New(engine.Config{
    Timeout:           10 * time.Second,
    JSContextPoolSize: 8,
    JSContextPoolWarm: true,
})
defer eng.Close()

page, err := eng.Fetch(context.Background(), "https://example.test/", engine.FetchOpts{
    RunScripts: true,
})
defer page.Close()

title := page.Title()
md    := page.Markdown()
text  := page.Text()
html  := page.HTML()

v, err := page.Eval(context.Background(), `document.querySelector('h1').textContent`)
if err != nil {
    return err
}
defer v.Release()
println(v.String())
```

`engine.Engine.Download(ctx, url, filename)` fetches with `network.TargetDownload` and commits only to its session-owned directory. `engine.Page.SaveDownload(filename)` applies the identical boundary to an already fetched response. `engine.Page` otherwise exposes `URL`, `StatusCode`, `Headers`, `Document`, `RawBody`, `HTML`, `Text`, `Markdown`, `Title`, `Eval`, `WaitIdle`, `Click`, and `Close`. The DOM lives in package `webapi` (`*webapi.Document`, `*webapi.Node`) with `QuerySelector` / `QuerySelectorAll` (cascadia-backed), `Tag`, `Attr`, `Children`, `Text`, and tree walking via `webapi.Walk`. `agent.HTML`, `agent.Text`, `agent.Markdown`, `agent.Title` are direct converters from a `*webapi.Document`. Form/action helpers (`agent.Forms`, `agent.FindForm`, `agent.ClickByText`, `agent.Type`, `engine.Engine.Submit`) cover form-driven workflows.

## JavaScript Execution

Stream source/sink initialization owns and observes returned asynchronous results before pull/write/close/abort work begins. A rejected start/pull reaches waiting consumers with the exact reason; handled consumer failures do not become unrelated unhandled rejections. Readable pulls serialize actual outstanding work and serve additional waiting reads after settlement. Transform start runs once and propagates failure to both existing sides. Writes and closes retain the first error for subsequent operations; sink writes receive the same error controller as sink start. Public WritableStream.close and writer.close share the actual awaited sink-close owner. Close admission rejects later writes and repeated close; public close rejects a locked stream. Successful write/close results normalize to undefined. This does not add a general writable queue or establish full Streams conformance.

The in-memory stream subset stores error state separately from its reason, preserving undefined/null/false/zero/NaN/empty-string/zero-BigInt failures. Reads recheck errors after pulling and discard queued chunks on error. Readable cancel closes pending reads before awaiting the actual source cleanup and propagates thrown/rejected/thenable failures. Writable abort delegates writer/stream entry points to the same sink cleanup, shares its pending Promise, propagates the actual sink failure and retains the original stream error for later writes. Settled terminal owners do not invoke cleanup again; successful cleanup resolves with undefined. Existing read/pipe/transform/tee effects remain tested. Full queuing, backpressure, byte streams, lock/brand validation and general Streams conformance are not established. Reference: https://streams.spec.whatwg.org/#readable-stream-cancel and https://streams.spec.whatwg.org/#writable-stream-abort.

Static HTML event attributes compile for every current document, including actual pristine realm reuse after the prior listener registry is cleared. Syntax failures enter the existing event-error owner instead of silently omitting handlers. Script-enabled Eval/loading admission reports the failure; static Fetch without script execution preserves the HTML and does not require valid JavaScript. A loading failure stays sticky across retry. Per-document attributes remain excluded from the startup snapshot and are compiled against the current native node owner.

Compatibility callback failures use the existing Document/Window event-error owner. Idle callbacks, BroadcastChannel, MessagePort property/listener/direct dispatch, synthetic IntersectionObserver/ResizeObserver, custom-element connected callbacks, FileReader load callbacks, XHR state/load/error callbacks, geolocation denial callbacks and synthetic canvas toBlob callbacks retain bounded errors until the current Eval/WaitIdle/loading drain. Other listeners and elements continue delivery after a failure. Empty thrown strings still produce a nonempty diagnostic, and the next execution can recover after drain. XHR controls cover synchronous/asynchronous Fetch delivery and preserve later state/load callbacks after earlier failures; geolocation remains denied. This error-observability contract does not grant browser-equivalent semantics to synthetic APIs or expand the existing supported subsets.

Each fetched `*engine.Page` owns a V8 execution context (via the vendored `github.com/tommie/v8go`, stock V8). The context is created at parse time and released by `page.Close()`. The engine's V8 isolate is shared across pages and released by `engine.Close()`.

The isolate is initialised from the embedded `js/snapshot.bin`. Snapshot-eligible bootstraps provide DOM, Headers, AbortController, MutationObserver, URL/URLSearchParams, TextEncoder and other pure definitions. Cross-bootstrap closures, including WebSocket, fetched Response and per-document event attributes, initialize per context. The exact source/native inventory and blob manifest govern admission. Regenerate changed eligible inputs with `go run ./cmd/artemis-snapshot/` and reproduce them with `go run ./cmd/artemis-snapshot/ -check`.

The vendored v8go lives at `third_party/v8go` (replace directive in `go.mod`); V8 libraries come from the upstream `github.com/tommie/v8go/deps/<os_arch>` modules. V8 15 shares one read-only heap across a process's isolates, so the admitted snapshot is the process snapshot of every isolate: `js.NewRuntime` calls `v8go.SetProcessSnapshot` before V8 starts, the snapshot's default context stays pristine, and page realms open the bootstrap realm with `v8go.NewContextFromSnapshot(iso, 0)`. If V8 already started without it (an isolate was created first), the runtime reports `SnapshotUnavailable` with the reason and bootstraps each realm from source.

```go
page, _ := eng.Fetch(ctx, "https://example.test/", engine.FetchOpts{
    RunInlineScripts: true, // execute inline <script> tags after parse
    Console:          js.FuncConsole(func(level, msg string) { /* ... */ }),
})
defer page.Close()

v, err := page.Eval(ctx, `document.querySelector('h1').textContent`)
if err != nil {
    return err
}
defer v.Release()
println(v.String())
```

`engine.FetchOpts.RunScripts` runs every inline `<script>` tag in document order at the end of `Fetch`, plus any external `<script src="...">` whose URL is reachable through the engine's network client. `RunInlineScripts` is the inline-only variant (skips network for external scripts). `engine.FetchOpts.Console` (`js.Console` interface) captures `console.log/warn/error/info/debug`; `js.DiscardConsole`, `js.FuncConsole`, and `js.CollectConsole` are provided.

DOM bindings exposed on the JS global `document` and on element objects (live, handle-backed - JS-side mutations propagate to the Go DOM, so `page.HTML()` / `page.Markdown()` after script execution reflect the mutated tree):

`document`:

| Member | Kind |
|---|---|
| `title` | DOM-backed getter/setter; shared HTML-title owner |
| `URL` | string getter |
| `body`, `head`, `documentElement` | element getters |
| `querySelector(sel)`, `querySelectorAll(sel)` | cascadia CSS3 |
| `getElementById(id)` | tree walk |
| `getElementsByTagName(tag)`, `getElementsByClassName(cls)` | array of elements |
| `createElement(tag)`, `createTextNode(text)` | constructor methods |

Element prototype:

| Member | Kind |
|---|---|
| `tagName`, `nodeType`, `nodeName` | getter |
| `textContent` | getter + setter |
| `innerHTML` | getter + setter (re-parses) |
| `outerHTML` | getter |
| `parentNode`, `parentElement` | getter |
| `firstChild`, `lastChild`, `nextSibling`, `previousSibling` | getter |
| `childNodes` (all), `children` (elements only) | array getter |
| `setAttribute(k,v)`, `getAttribute(k)`, `removeAttribute(k)`, `hasAttribute(k)` | mutation |
| `appendChild`, `removeChild`, `insertBefore`, `cloneNode(deep)` | mutation |
| `querySelector`, `querySelectorAll` | cascadia |

`console.log/warn/error/info/debug` route to the configured `js.Console`. Standard whatwg `nodeType` values are exposed (Element=1, Text=3, Comment=8, Document=9, DocumentType=10).

**Caveat for multi-step Eval**: V8 `RunScript` evaluates each call as a top-level script in the same global. Successive evals that re-declare the same `const`/`let` identifier collide. Wrap multi-step JS in an IIFE `(() => { ... })()` or a block `{ ... }`.

Events:

| Member | Notes |
|---|---|
| `new Event(type, {bubbles, cancelable})` | constructor |
| `event.{type, target, currentTarget, bubbles, cancelable, defaultPrevented}` | properties |
| `event.preventDefault()`, `event.stopPropagation()` | methods |
| `element.addEventListener(type, fn)`, `element.removeEventListener(type, fn)` | EventTarget |
| `element.dispatchEvent(event)` returns `!event.defaultPrevented` | EventTarget |
| `element.click()` | shortcut: dispatches `Event('click', {bubbles:true})` |
| `document.addEventListener / removeEventListener / dispatchEvent` | distinct Document target sharing the canonical event registry |

Listener storage is JS-side, preserving reference equality for removal. Element, Document and Window targets share capture/bubble propagation, at-target capture and bubble registrations, once, passive and immediate propagation stop. Listener failures enter the bounded operation error owner while later eligible listeners continue. AbortSignal-driven removal remains open.

## Fetch API

`fetch(url, opts)` is exposed on the JS global. It performs the HTTP request through the engine's network client and returns a Promise carrying a Response. By default the request is synchronous on the V8 thread; set `engine.FetchOpts.AsyncFetch = true` for asynchronous fetch goroutines.

Both paths resolve URL strings on the owning V8 thread before transport. Relative, root-relative, parent, query and empty references use the parsed document URL, including its actual redirect destination, and the first `base[href]` in tree order. An invalid first base or a data/javascript base falls back to the document URL; later bases do not replace it. Absolute input remains unchanged. Malformed references or an unusable relative base reject the Promise. Resolved targets still pass through the existing subresource policy, session, response accounting and cancellation owners. The resolver uses the existing Go URI parser; full WHATWG URL parsing, history/document synchronization and inherited about:blank/srcdoc base semantics remain in TASK-2795's wider capability work. The document-base selection follows the [HTML document base URL contract](https://html.spec.whatwg.org/multipage/urls-and-fetching.html#document-base-url).

| Member | Notes |
|---|---|
| `fetch(url)` | GET, returns Promise<Response> |
| `fetch(url, {method, headers, body, signal})` | checked method/string conversion; headers record, iterable pairs or `Headers`; string, ArrayBuffer/view or compatible urlencoded `FormData` body; private-brand signal admission |
| `response.status`, `response.ok` (200-299), `response.statusText`, `response.url` | immutable properties |
| `response.headers` | immutable, case-insensitive `Headers`; cookie-setting fields are not exposed |
| `response.text()`, `response.json()` | Promise; actual UTF-8/BOM decoding and JSON parsing, single consumption |
| `response.arrayBuffer()`, `response.bytes()` | Promise; exact copied ArrayBuffer/Uint8Array bytes |
| `response.bodyUsed`, `response.clone()` | stateful body use; independent clones share immutable retained bytes |
| transport error | rejects the Promise |
| 4xx / 5xx | resolves normally with `ok=false` (per Fetch spec) |
| pre-aborted `AbortSignal` | rejects before transport with the exact stored reason, including null/false/zero/empty strings |
| running `AbortSignal`, `AsyncFetch=true` | abort algorithms cancel the actual request before event listeners; rejection retains exact reason and waits for transport return |
| `body` is `FormData` | auto-encoded as `application/x-www-form-urlencoded`, `Content-Type` set when absent |

V8 `kAuto` microtask policy drains promise continuations at the end of every top-level script. Within a single `Eval` body the continuations have not yet run; use a two-eval pattern (register the chain, then read the captured global) for inspection. Inline `<script>` tags work fine because each script is a separate task and microtasks drain between them.

The Fetch callback runs synchronously on the V8 thread by default and blocks it for the request duration. Set `engine.FetchOpts.AsyncFetch = true` for concurrent fetch goroutines; the V8 thread drains pending results at script boundaries and inside `Page.WaitIdle`.

AbortController/AbortSignal keep their authority in private WeakMaps; public state is read-only and forged receivers reject. A synthetic abort event does not cancel work. `abort()` is idempotent and retains the first exact reason; omitted/undefined reasons create a typed AbortError. Abort listeners, callback objects, once/removal and onabort reuse the canonical event registry/error owner. Native cancellation runs before listeners, so stopImmediatePropagation or listener failure cannot prevent request cancellation. Timeout validates Web IDL numeric bounds, schedules real Context-owned delayed work and produces TimeoutError; long durations use finite timer chunks. Timer/event dependencies bind once during trusted per-context bootstrap. The existing synchronous compatibility mode blocks page JavaScript, so another page callback cannot abort its in-progress synchronous transport; caller cancellation remains active. This mode does not claim browser event-loop parity. Signal semantics follow the [DOM abort contract](https://dom.spec.whatwg.org/#aborting-ongoing-activities).

Fetch signal watches extend the existing async owner, with context-scoped monotonic IDs and bounded admission. Unread non-null response bodies and their clones retain the same private signal after transport completes. Abort immediately drops their retained payload bytes; subsequent reads reject with the exact reason without marking an errored body used. Repeated reads/clones retain only the bounded reason owner until close. Successful consumption retires its signal reference; failed response decoration rolls back the invisible body. Context close joins transports and releases pending watches before destroying the realm. Algorithm-slot cleanup deletes an actual native Map key under a no-microtasks scope, bypassing page methods and avoiding JavaScript reentry after cancellation watchers have stopped.

Request URL/method/header/body conversion failures reject before transport. Method admission rejects invalid tokens and CONNECT/TRACE/TRACK, normalizes the six standard methods and preserves extension-method casing. GET/HEAD bodies reject even when empty. Exact binary regions preserve NUL, non-UTF-8 bytes and view offsets; shared/detached buffers reject. The existing policy's request-byte limit applies before transport. Script-supplied forbidden request headers are removed at this boundary, preserving transport/credential/origin ownership. Standalone Headers remains mutable; fetched response headers and clone headers are immutable. Header validation, case folding, live sorted iteration and separate Set-Cookie entries use the existing private Header state. The binding follows the [Fetch header and Body contracts](https://fetch.spec.whatwg.org/).

Fetched response metadata, header count/bytes and retained body bytes/handles are bounded. Non-null bodies become used before conversion, including failed JSON parsing; null-body statuses remain reusable empty bodies. Consumed payloads release their Go bytes when their final clone consumes or closes. Close releases remaining payloads and native owners. Body failures use native page-realm TypeError objects without invoking mutable page constructors; arbitrary abort reasons remain unchanged. Per-operation callback return handles retire after native return under the same isolate gate. Actual Chromium and renderless controls agree on nonempty/empty-200 body abort, independent clones and used-body rejection. HTTP 204 is an explicit compatibility difference: this runtime retains the Fetch null-body contract; measured Chrome 155 exposes an empty stream and different abort/use behavior, matching the [documented standard/implementation discrepancy](https://github.com/whatwg/fetch/issues/1779). Request/Response constructors, body streams, Blob bodies, full multipart FormData, AbortSignal.any and complete Fetch/event-loop capability proof remain open TASK-2795 work; these bounded controls do not claim complete Fetch equivalence.

CLI entry: `artemis fetch --eval "<expr>" <url>` prints the result; `--run-scripts` enables inline-script execution; `--console` forwards JS console.* to stderr.

## Browser Globals

| Global | Notes |
|---|---|
| `window` | identical to `globalThis` |
| `window.document` | inherited from JS Execution section |
| `window.location` | `href`, `protocol`, `host`, `hostname`, `port`, `pathname`, `search`, `hash`, `origin`. Built from the page's URL at Context creation. The `history` API mutates these in-process; setters / `assign` / `replace` / `reload` are no-op. |
| `window.navigator` | `userAgent`, `language`, `languages` (array-like with `length`), `platform`, `onLine`, `cookieEnabled`, `webdriver` (false), `doNotTrack` (null), `plugins` / `mimeTypes` (Chrome PDF plugins + types), `userAgentData` (NavigatorUAData reduced-UA shape, brands/platform derived lazily from the configured UA so snapshot-frozen contexts stay coherent), `clipboard`, `geolocation` (rejects with permission-denied), `permissions.query` (`prompt` for notifications/geolocation/camera/microphone, `denied` for persistent-storage), `serviceWorker` (guarded container), `hardwareConcurrency` (8), `deviceMemory` (8), `maxTouchPoints` (0), `pdfViewerEnabled` (true), `vendor` (`Google Inc.`). Configurable per Page via `engine.FetchOpts.Navigator`. Defaults present a host-matched Chrome identity coherent with the wire `User-Agent`. |
| `window.localStorage`, `window.sessionStorage` | `localStorage` is retained by nonempty session identity and canonical HTTP origin; anonymous contexts retain page-local state. `sessionStorage` belongs to one top-level page and origin, including same-origin frames. `getItem`, `setItem`, `removeItem`, `clear`, `key(i)` work. `length` is a snapshot at install; `lengthOf()` returns live length. Writes enforce a 5 MiB UTF-16 byte quota and 16,384 keys per store, with aggregate runtime limits of 64 MiB and 65,536 keys. Quota failure throws `QuotaExceededError` without partial mutation. |
| `setTimeout(fn, ms)`, `clearTimeout(id)` | cooperative monotonic due-time scheduling, arguments/string handlers, shared cancellation namespace and bounded admission/pumping; see Local fixture admission. |
| `setInterval`, `clearInterval` | rearm after callback completion, including a reported callback exception; cancellation or Context close retires the timer. Future deadlines require an Eval or pending-fetch WaitIdle pump. |

## Agent Extraction

Pure-Go extractors that turn a `*webapi.Document` into agent-shaped output. Work on the static DOM, identical to the post-script DOM after `RunInlineScripts`.

| Method | Returns | Notes |
|---|---|---|
| `page.Links()` | `[]agent.Link` | every `<a href>` with absolute URL; skips empty / `javascript:` / `mailto:` / `tel:` / `data:` / fragment-only |
| `page.LinksAll()` | `[]agent.Link` | unfiltered |
| `page.StructuredData()` | `agent.StructuredData` | `JSONLD` (object or array), `OpenGraph` (`og:*`), `Twitter` (`twitter:*`), `Meta` (any `name`/`property`/`http-equiv`), `Title` |
| `page.SemanticTree()` | `*agent.SemanticNode` | hierarchical view; headings nest; paragraphs/lists/quotes/code/images/links inline; nav/footer/aside/script/style/template skipped |
| `agent.SemanticString(node)` | `string` | indented Markdown-ish render of the tree |

The static extraction layer uses `parser.ParseHTML` (the HTML parser owner around `golang.org/x/net/html`). `agent.SemanticTree` returns the hierarchical semantic tree; `agent.Markdown`, `agent.Text`, `agent.Links`, `agent.StructuredData`, `agent.Forms` are the other direct converters from a `*webapi.Document`. The JS computed-style bridge separately uses `css.Cascade`.

CSS admission uses the shared bounded `tdewolff/parse/v2` lexer. `css.ParseDeclarations(source)` returns ordered `Declaration{Property,Value,Important,Offset}` records and typed diagnostics; ordinary names use ASCII lowercase and custom names preserve their case. `DeclarationBlock.Err()` rejects partial parsing. Custom values retain author comments and spelling, including valid empty values. `css.ParseInline(source) (map[string]string,error)` uses the same parser, rejects malformed input without a partial map and retains priority suffixes for `css.Serialize`; it is a declaration projection, not computed-style proof. `css.ParseStylesheet` retains `Stylesheet.Diagnostics` and `Err()` for malformed/unsupported constructs, and preserves complete ordered declarations beside property/priority projections. Selector lists are unforgiving: an unsupported member rejects the whole rule. Comment removal preserves selector token boundaries; non-browser selector extensions, stateful pseudos and pseudo-elements report unsupported semantics. Admission limits are 8 MiB per source, 65,536 tokens, 128 nested delimiters, 8,192 rules, 64 selectors per list, 16 KiB of selector text, 64 KiB per token/declaration value and 64 diagnostics with explicit saturation failure. These parsing contracts do not establish complete cascade/computed-value support or runtime capability/routing proof.

Document CSS loading preserves interleaved style/link document order and exact HTML rel-token matching. Stylesheet URLs share owning-document/first-base resolution with JavaScript fetch, including child documents. Context construction rejects missing loaders, external load failures, malformed/unsupported sheets, conditional media, alternate selection and foreign stylesheet semantics with typed redacted diagnostics; inactive disabled links and non-CSS types do not load. Limits are 128 sheets and 8 MiB aggregate document CSS. Diagnostic.Cause/Unwrap preserves cancellation/policy/budget identity without exposing transport text through Error. Typed native inline-style failures survive JavaScript catch blocks, iframe script/message boundaries, evaluation, timer/microtask pumping and document loading; the engine transfers no partial page. Automatic CSS retry is denied after script execution, unsafe/body-bearing navigation or a budget breach. Failed child CSS admission remains inaccessible and is cached for the frame lifetime. These contracts do not establish general replay authority or dynamic stylesheet refresh.

`css.StyleDeclaration` shares the canonical parser, specified-value grammar and shorthand expansion. `ParseStyleDeclaration` models mutable cssText parsing; `ParseStyleAttribute` preserves immutable source order for fewer than two declarations or two distinct expanded properties. Native controls distinguish attribute parsing from cssText assignment in both standards and quirks documents. Value and priority reads remain separate; ordered set/remove operations validate and serialize a candidate before mutation. The inline JS bridge provides a stable live wrapper with cssText, length, item/index/iteration, getPropertyValue, getPropertyPriority, setProperty and removeProperty. Retained state is capped at 1,024 elements and 8 MiB source per Context; attribute writes invalidate it and pool reset drops wrapper identity. Computed-style property methods remain live and mutation raises NoModificationAllowedError. This is bounded CSSOM behavior, not complete document-mode or property-enumeration parity.

Computed evidence is bound to its admitted document and stylesheet inputs. Changes to style/link/base nodes, attributes, contents or identity reject stale computed evidence; dynamic stylesheet lifecycle requires browser execution. Detached and foreign-document nodes cannot use the current Context's stylesheets. The input fingerprint is bounded to 4,096 relevant nodes and 8 MiB serialized input. Overlapping all/font/logical-box shorthands and time-dependent declarations reject affected computation instead of silently disappearing from the cascade.

Selector admission retains exact HTML data/ARIA and the admitted string-attribute comparisons, plus explicit ASCII-insensitive `i` matching and attribute-existence selectors. Other value comparisons require browser HTML case defaults. Case-fold-equivalent ID/class spellings that could depend on document mode reject under a bounded document scan. Native `:empty` semantics count whitespace text as content; the internal compiled predicates retain one pseudo-class of specificity. Author `:has()` rejects because the underlying matcher does not establish relative-anchor or bounded nested traversal semantics. Compound/combinator/structural matching and specificity are compared against actual Chromium; this is an explicit supported subset.

Canonical corpus cases css-002/css-003/css-004 exercise computed inheritance/variables/shorthands, live ordered CSSOM/read-only behavior, and quoted-token/priority/empty-selector semantics through the existing corpus runners. Their shared input identity also includes compiled CSS implementation/tests and the declared CSS-related JS binding/bootstrap/test inputs plus linked V8 version. Those immutable source hashes are computed once per binary; source bytes never enter portable corpus payloads. Changing a pinned implementation or test invalidates old corpus identities. This pin is not native Chromium identity or complete binary/release authentication. Native shorthand removal returns an empty string; longhand removal returns its previous value.

`css.Cascade(sheets,node,inline) (*ComputedStyle,error)` owns bounded declaration selection and computed custom-property inheritance. `ComputedStyle.Cascaded(property) (Declaration,bool)` exposes the winning authored declaration; `Declaration.Shorthand` identifies a pending longhand's source shorthand. `ComputedStyle.Value(property) (string,error)` computes admitted values and retains typed unsupported/layout/environment failures. Origin and important precedence, tuple specificity, declaration order and origin/inline rollback share the same owner. Values retain token identity through substitution and serialization, including bounded inserted separators. Computation includes legacy named/hex/RGB sRGB, actual parent/root font bases, explicit opacity/visibility and admitted margin/padding/background shorthands with independent longhand priority. Unsupported layout quantization, percentages, complex background layers and unowned default box/background values fail closed. Font computation retains float32 bases separately from CSSOM serialization, including the native 10,000px maximum and sub-epsilon zero boundary. The JS callback consumes this owner instead of treating specified strings as computed values. This bounded API is not a claim of complete CSSOM, UA stylesheet or dynamic-sheet support.

CLI: `artemis fetch --dump {links|structured|semantic}` prints TSV / JSON / Markdown-ish.

## Forms and Actions

| Symbol | Purpose |
|---|---|
| `agent.Forms(doc) []*Form` | every `<form>` on the page |
| `agent.FindForm(doc, selector) *Form` | first form matching selector or its nearest ancestor form |
| `(*Form).Fields() []FormField` | scan inputs/textareas/selects under the form (Name, Type, Value, Checked, Options) |
| `(*Form).Set(name, value)` | write the field value (value attribute or selected option or textarea text) |
| `(*Form).Toggle(name, checked bool)` | check/uncheck a checkbox or radio |
| `(*Form).Submit() (FormSubmission, error)` | resolve action against doc URL, encode URL-encoded body, return URL/Method/ContentType/Body |
| `engine.Engine.Submit(ctx, sub, opts) (*Page, error)` | perform the submission, return the next page |
| `agent.ClickByText(doc, text) (*Node, bool)` | first matching button / anchor / input[submit] (case-insensitive) |
| `engine.Page.Click(ctx, node) error` | dispatch click in JS so listeners fire |
| `agent.Type(doc, selector, text) error` | set value attribute / textarea text content |

URL-encoded form submissions are supported; multipart/file-upload bodies are not. `agent.Type` writes the value attribute directly; React-style controlled inputs that listen on the `input` event need an explicit `page.Eval(...)` to dispatch a synthetic event after the value mutation.

## Network Stack

`HTTPClient.WithResponse(ctx, request, kind, consume)` lends response bytes only during `consume`. Known-length bodies above 1 MiB and at most 64 MiB use anonymous mappings when the measured host and capacity owner admit them. Reservation includes page rounding; cancellation does not unmap behind an active consumer. Cleanup joins consumer and release errors, unmaps before freeing capacity, clears the borrowed body and creates no disk files. Unknown-length, small or unsupported responses use bounded Go storage. Ordinary `Do`/`DoTarget` retain caller-owned bytes. Engine pages keep their existing owned raw-body copy, and request concurrency is released before parsing or nested script fetches.

When an owner is supplied, every scoped Go response buffer is also reserved before allocation. Known lengths allocate once; unknown/chunked bodies grow from at most 32 KiB, admitting the replacement while the previous buffer remains owned, then settling the previous capacity after copying. The final buffer stays reserved through the consumer. Compressed layers reserve codec workspace before construction and their actual output capacity before growth; decoder close precedes workspace settlement, and layer outputs remain owned until joined response cleanup. Workspace estimates are conservative capacity bounds, not measured RSS: 1 MiB for gzip/deflate, 64 MiB for the pinned normal-window Brotli decoder, and four times the admitted Zstandard window plus 2 MiB with one decoder worker. Denial, cancellation, corruption, expansion and failed settlement cannot expose a successful borrowed response. HEAD and other body-free replies do not allocate or decode an advertised entity body.

`Engine.InputMemoryAdmission()` exposes the combined session/shared owner for embedding screenshot runtimes. `JPEGPipeline.WithScreenshotInput` decodes large base64 input directly into admitted anonymous memory while its callback runs. Derived images own their output; raw input is borrowed only until return. Every ordinary and model screenshot uses this input owner and records actual `Outcome.InputMemory` evidence. Unchanged mapped output receives a separately admitted owned copy; its overlapping reservation remains held until the input mapping retires. Ordinary bytes, format and full dimensions remain intact. Typed-nil owners fail before consumption; cleanup failure cannot publish successful output or free a still-mapped reservation.

`JPEGPipeline.WithImageWorkingMemory` reserves a conservative header-derived working envelope before allocating JPEG/PNG pixels. It covers padded progressive coefficient/pixel planes, linked native workspace allowance, Lanczos pass buffers/weights/scanlines and bounded model encoding. `Outcome.ImageMemory` identifies admitted reservation bytes, explicitly not measured RSS or actual peak usage. The same session/shared owner holds both input and image work; failed admission or settlement publishes no successful pixels. Ordinary screenshots retain their format, dimensions and owned bytes. `engine.NewMemoryAdmission` provides the existing validated session budget with an explicit cleanup callback without constructing V8; `artemis act` uses it, and the owned Agent reuses its live engine's authority. Anonymous input allocations remain individually limited to 64 MiB; the combined transient reservation defaults to 512 MiB and can be lowered. Model JPEG encoding rejects output above 24 MiB.

`AgentConfig.SessionBudget` and `AgentConfig.MemoryAdmission` forward the same ceilings and optional shared authority into the owned runtime. No embedding application is required. Low-level image consumers without an owner retain a 512 MiB per-operation working ceiling; higher bounds require an explicit capacity owner. A very wide PNG is rejected from its valid header before allocating filter rows or pixels. Byte reservations describe transient ownership, not retained artifact memory or whole-process RSS.

`JPEGPipeline.PrepareModelImage(ctx, raw, format, target, owner)` is the shared JPEG/PNG model preparation path used by screenshot actions and embedding consumers. It admits working memory before complete decoding, keeps the frozen native admission, fits through Lanczos to the configured bounds and encodes resized or converted output as bounded JPEG or PNG. JPEG uses the configured quality; PNG remains lossless. Format conversion also reserves encoder/output capacity when no resize is required. `ModelImageResult` reports actual format, dimensions, decode path, acceleration and reservation evidence. Already-fitting input retains its bytes when source and target formats match; output can borrow that input until its owner retires it. Cancellation, failed decode/encoding or failed reservation settlement returns no successful image. Ordinary screenshot output is unchanged.

`JPEGPipeline.CPU` accepts `ImageCPUConfig{Admission, RequireAdmission, Workers, Affinity}`. `ImageCPUAdmission.ReserveCPU(ctx, count)` supplies a reservation context and release handle; required, typed-nil or incomplete owners fail closed. The operation holds that reservation through decoding, encoding and every Lanczos worker join. Caller cancellation remains effective even when an injected owner returns an unrelated context. Workers are limited to 1..4 (zero selects at most four from GOMAXPROCS), partition scanlines without an image-sized queue and bind each actual Linux worker thread to the supplied allowed CPU set. Unsupported platforms execute normally without claiming affinity. The pinned imaging Lanczos kernel, fit geometry, alpha accumulation and rounding are retained in the owned resampling path because the library's internal parallel helper cannot accept worker admission or cancellation. `ResizeForModelContext(ctx, img)` exposes this same cancellable path; the existing `ResizeForModel(img)` retains its nil-on-error contract. `ModelImageResult.CPUAffinity` reports actual operation placement, independently of capacity admission. No shared owner means bounded local execution, not a shared or exclusive CPU grant. Cleanup failure suppresses successful pixels; cancellation never releases capacity behind a live worker.

The direct resize APIs require a working envelope at most 512 MiB before allocating filter weights or scanlines. Pixel count alone does not bound very wide images' Lanczos workspace. `PrepareModelImage` instead uses its existing full pre-decode memory reservation, including an explicitly admitted larger capacity when supplied. Default worker memory reserves all four permitted workers independently of mutable GOMAXPROCS, while explicit worker counts use their actual narrower envelope.

`RuntimeConfig.ImageMetrics` and `JPEGPipeline.ImageMetrics` accept an optional `func(bridge.ImageMetricEvent)` observer. Capture operations start before action validation/policy, count capture calls only at the actual CDP invocation boundary, and report success, failure, denial, cancellation or invalid requests after joined cleanup. Model preparation reuses a containing capture window, so its decoder is not counted twice; independent preparation opens its own window. Decoder counts record actual completed Go, VideoToolbox or libjpeg-turbo executions even if later encoding, cancellation or settlement suppresses the image result. Typed admission errors retain their original cause. Counters are bounded at 1,024 per operation and a finished window emits once. Events carry no image data, URLs, actor IDs or raw errors. No observer means no process-memory measurement or sampler.

`platform.ProcessPeakMemory(ctx)` observes the current process's kernel lifetime RSS high-water via getrusage on Darwin/Linux and PeakWorkingSetSize on Windows. It includes prior and concurrent process work, excludes other processes such as Chromium children, and does not claim an image-attributed peak, Go heap, GPU allocation or reservation. Unknown/unsupported readings have no fabricated numeric measurement. The event samples this sensor after cleanup using an observation-only context, including when image work was canceled. It starts no background sampler or budget authority.

`RuntimeConfig.ImageCPU(ctx)` can supply a fresh `ImageCPUConfig` for each screenshot. Ordinary JPEG/PNG verification and model preparation use that same owned CPU boundary; decode, native cleanup and working-memory settlement complete before CPU release. `WithImageWorkingMemory` passes the reservation-bound context into its `func(context.Context, platform.ProbeResult) error` consumer, so pressure preemption reaches actual pixel work. Capacity denial is a policy failure, cancellation/deadline remains cancellation/timeout, and failed settlement publishes no bytes. Omitted configuration retains bounded standalone execution.

Screenshot actions accept an optional page-owned `ref` to capture an element's current border bounds. `Collector.ScreenshotRect(ctx, node)` verifies the backend identity and fresh box model, reuses owned frame translations, clips separate renderer viewport overflow and converts to root document coordinates for CDP capture. Hidden, detached, expired, ambiguous or semantically replaced references fail before capture; nonfinite, empty, oversized or unverified transformed-frame geometry never falls back to a whole-page image. Main-document offscreen elements can be captured without scrolling. JPEG/PNG verification, model preparation, CPU/memory ownership and operation metrics remain identical to ordinary screenshots. References are observation data, not authorization. The region protocol follows [CDP captureScreenshot](https://chromedevtools.github.io/devtools-protocol/tot/Page/#method-captureScreenshot); no extra browser SDK is required.

`WithScreenshotInput` reserves the decoded buffer, including the base64 decoder's guard byte, before allocation whenever a memory owner is supplied. This also applies to small inputs and unsupported/unknown mmap builds using ordinary Go storage. The reservation remains held through the synchronous consumer and joined cancellation/cleanup. Unknown mmap observations remain unknown for eligible large inputs. Admission failures preserve their cause through `ImageAdmissionError`; screenshot actions classify them as policy denials, with no successful bytes or invented decoder work.

HTTP responses decode gzip, zlib/raw deflate, Brotli and Zstandard through the same policy-bound client. Encoded bytes and every decoded layer have the effective response-body limit; at most four encoding layers are accepted. Corrupt, unsupported or over-limit responses fail instead of becoming page text. The existing response budget counts the larger encoded/decoded body, and successfully decoded responses no longer expose stale Content-Encoding/Content-Length. HTTPS authorities use an explicit port, default 443, including IPv6, before policy-bound dialing and ALPN negotiation.

Direct HTTP transports may activate Linux `TCP_FASTOPEN_CONNECT` after measured host admission, on the actual policy-admitted socket. Eligibility is confined to body-free GET/HEAD requests; configured proxies, other methods, unsupported hosts and `HTTPClientConfig.DisableTCPFastOpen=true` retain ordinary connect behavior. Generic `Policy.DialContext` remains suitable for read-first protocols. Every socket still uses fresh DNS/IP/domain/port validation before activation. The decision sink receives bounded `tcp_fast_open` state/source/reason evidence only after the socket option operation; failed audit aborts before connect. Unsupported/denied options select normal connect, and an unverifiable fallback that cannot disable the option aborts the owned dial. An available option receipt proves successful option write/readback, not peer acceptance, SYN payload use or saved RTTs.

| Setting / hook | Effect |
|---|---|
| `engine.Config.ObeyRobots` | configured GET admission checks actual agent, escaped path/query and positive crawl delay before page dispatch; denial/failure propagates |
| `engine.Config.PolicyConfig` (zero value / `AllowPrivateNetworks: false`) | rejects loopback, RFC1918, link-local, multicast, CGNAT hosts and limits ports to 80/443 |
| `PolicyConfig.MaxRequestBodyBytes` | rejects known oversize bodies and unknown-length/chunked bodies before transport; callers must provide a bounded materialized body |
| `PolicyConfig.AllowedDownloadTypes` / `MaxDownloadBytes` | validates the sniffed MIME (`type/subtype`, `type/*`, or `*/*`) and per-file byte cap independently from request-body content types |
| `network.HTTPClient.DoTarget(..., TargetDownload)` | preserves the download policy identity and applies `MaxDownloadBytes` before buffering the response |
| `engine.Config.ProxyURL` | spec L4148: with a proxy, HTTP requests, redirects, robots fetches and JavaScript WebSockets use `Policy.ValidateProxiedRequest`: target hostnames are never resolved locally (the proxy resolves them); methods, bodies, schemes, ports, domains, blocked names and literal addresses stay fail-closed |
| `engine.Config.DNSCachePath` | spec L4512: every engine resolves through a bounded `DNSPrefetchCache` (L1 memory, 5 min TTL, 50K entries); a path adds the persistent SQLite L2 (WAL, 5 s busy timeout, pruned to 50K). Policy admission and dialing share the same cached answer. Embedding hosts choose the path, for example `<home>/browser/dns_cache.db` |
| `engine.Config.WebBotAuth` / `AgentConfig.WebBotAuth` / `HTTPClientConfig.WebBotAuth` | operator-gated Web Bot Auth: every renderless request and redirect hop carries an RFC 9421 Ed25519 signature (label `sig1`) over `@authority` and the quoted `Signature-Agent` with `created`, `expires`, `nonce`, `alg`, `keyid` (JWK thumbprint) and `tag="web-bot-auth"`, produced by the vetted `github.com/remitly-oss/httpsig-go`. `network.WebBotAuthVerifier` checks signature, components, freshness, tag and rejects replayed nonces with bounded memory; `PublishPublicKey` emits the key directory JWK set |
| JavaScript `WebSocket` | initial handshakes use `TargetWebSocket`, redirects use `TargetRedirect`, and every socket dial uses the same `network.Policy.DialContext` as Engine HTTP; nil policy fails closed |
| `engine.FetchOpts.OnRequest(req) (resp, err)` | called before the network call; non-nil resp short-circuits with a mock |
| `document.cookie` (JS) | URL-selected non-HttpOnly cookies; script writes cannot create HttpOnly state or replace/delete an existing HttpOnly cookie identity |

`network.ParseRobots` is the canonical interpreter, also used by the scraper compatibility API. Actual consecutive/repeated groups merge; empty named groups retain their identity. Agent selection is case-insensitive and deterministic. Normalized percent/UTF-8 octets, wildcard `*`, terminal `$`, query paths, longest literal specificity and Allow ties share one evaluator. Crawl-delay and absolute HTTP(S) Sitemap metadata do not split core groups. Returned rules and cached public fields are detached. `network.IsPrivateOrLocal` and `network.CheckHostPublic` are the IP filter primitives.

`HTTPClient.FetchRobots` uses the existing validated HTTP transport, cookie owner, redirects, response accounting and decompression limits. Only 404/410 admit an empty policy; other non-200 statuses, nonempty non-text/plain responses, invalid charsets, transport/policy errors and parser limits cannot become cached permission. Successful policies expire at 24 hours and are partitioned by normalized scheme/host/port, session and requested agent identity. Each client admits at most 4096 entries, 8 MiB logical policy payload, 128 in-flight origins and 257 session owners. Concurrent callers share one retrieval; caller cancellation ends its wait, while session release/client close cancels outstanding retrievals and prevents publication by retired owners.

Parser admission bounds are 1 MiB raw body, 8192-byte rule/target, 32 consecutive agents, 1024 distinct groups, 32768 stored rule assignments, 1024 wildcard programs, 128 sitemaps and 4 MiB combined logical rule/metadata payload. These payload budgets do not represent total process memory. `HTTPClient.CheckRobots` and configured engine GET fetches use the actual wire agent and escaped path/query; positive crawl delay serializes cancellable origin admission without nesting request leases. This is admission pacing, not a guarantee of exact wire start/completion intervals. Crawl fetches are robots-governed by default; ordinary agent fetches stay opt-in. A governed fetch marks its request context, so every HTTP redirect hop is admitted by `HTTPClient.CheckRobots` with the same agent before it is followed; robots.txt retrieval itself never recurses into that check. Native Chromium navigation and descendant subresource requests do not share this admission contract. Robots guidance does not replace network or action authorization.

Network configuration is detached at construction. Supplied blank/invalid domains, unsupported schemes/method tokens/media types, negative limits and overflow-prone body limits fail admission rather than becoming unrestricted defaults. Domains use normalized IDNA labels; `*.example.com` permits subdomains only. Zero limits retain documented defaults. Lists admit at most 256 domains, 64 schemes/methods/content types/download types and 65535 ports. Target URLs are bounded to 8192 bytes. DNS receives the caller cancellation plus the configured dial timeout, accepts at most 64 addresses and rejects the entire set if any destination is invalid or forbidden. Expired, absent or uninitialized owners cannot cause a new lookup or dispatch.

`network.ValidatedHeaders` creates a detached canonical snapshot: at most 128 fields, 128 values per field, 256 bytes per name, 4096 bytes per value and 32768 total name/value bytes. Duplicate case spellings, invalid field bytes and multiple Content-Type/Content-Length/Content-Encoding/Authorization values fail closed. HTTP methods are normalized before dispatch. A nonempty request body requires a valid admitted Content-Type and known bounded length; caller Content-Length must agree with the actual request metadata. Host, Transfer-Encoding and proxy credential/framing headers remain transport-owned.

`Page.SetFetchRequestHandler` exposes only network-admitted native requests. Its resolver retains the immutable original target, method, headers, body extent, request ID and CDP session. Application continuation overrides receive final policy validation before `Fetch.continueRequest`; `PostData` is strict base64 whose decoded byte count is checked. Nil `Headers` inherits the original fields, while a non-nil empty slice sends an actual empty header override. A resolver reserves one terminal attempt before any external decision or CDP command and returns `bridge.ErrFetchAlreadyResolved` for repeated attempts on a live owner. Expected denial sends `Fetch.failRequest`, returns `network.ErrPolicyDenied` and keeps the browser healthy. Closed/detached owners cannot resolve late work. Retiring a page stops its request routing before `Target.closeTarget` and fails every request it still holds paused (queued, or whose callback the retirement cancelled) with `Fetch.failRequest` `Aborted`, because Chromium continues requests that are still paused when their Fetch session detaches. If the close then fails while the target survives, the error says that routing is retired and the page's further requests stay paused until a repeated close succeeds. Local fulfillment validates status, headers, raw response bytes and compressed expansion through `Policy.ValidateResponse`; it cannot bypass the response cap. These controls do not establish complete proxy-routed DNS, JavaScript-WebSocket proxy support or ordinary Chromium response accounting.

Each native page owns one cancellable frame/network monitor, four request workers and a 128-job queue. Request and attached-target deadlines inherit that lifetime. Root target retirement and page/context/browser close cancel active work and stop queued admission; close joins the monitor/workers outside routing locks before releasing session state. Closed is terminal: late native events cannot revive or downgrade it. Public and internal page/context close callers share one completion result. Creation transfers its page slot once to the registered owner, including later startup failure. This does not establish complete per-child-session cancellation, tab executor/receipt supervision or ordinary native response accounting.

`Page.SetFetchRequestHandler` is a trusted embedding boundary: handlers may run concurrently, must honor the supplied context and return promptly. Register a deferred resolver and return `handled=true` for asynchronous work; later resolution still validates the current owner. A callback must not synchronously close/join its own executing page/context/browser. Close waits for cooperative active callbacks; it cannot forcibly terminate arbitrary Go callbacks or claim they finished before they actually returned.

`CDPTransport.Done` signals terminal transport admission; `Close` additionally waits for its actual reader to return. The native policy proxy blocks new handlers/tunnel registration at shutdown, cancels admitted request contexts, closes active tunnel sockets and joins every admitted HTTP/hijacked handler before returning. These cleanup guarantees retain the existing proxy/policy owner and do not establish complete proxy DNS or protocol parity.

`ChromiumBrowser.Stopped()` distinguishes verified owned cleanup from `Healthy()==false` during shutdown. Browser/process monitors are joined on successful cleanup; native process/profile and proxy cleanup failures prevent stopped confirmation and retain their errors. A timed-out graceful close can finish through the existing owned-process termination path, exposing a fixed shutdown warning via `ProcessWarnings` and structured logging. It never turns a failed cleanup into success or terminates an externally attached process. Closing a default context closes its created targets individually; an external browser's unrelated targets remain live. Local provider/initializer state uses real runtime health and retains unresolved cleanup ownership instead of replacing it.

### Generated Chrome-style identity (default)

When `engine.Config.UserAgent` is left empty, the engine presents a
host-matched generated desktop-Chrome identity: the `User-Agent`, the
`Sec-CH-UA*`/`Sec-Fetch-*`/`Accept`/`Accept-Language` header set
(`network.HTTPClientConfig.ChromeLike`, generated by
`network.HeaderGenerator`), and the in-page `navigator.*` surface all
derive from the generator. Caller overrides can change those fields. Setting a
custom engine `UserAgent` disables the generated Chrome header defaults.

HTTPS on this path runs through the absorbed uTLS fork
(`third_party/utls`) with its `HelloChrome_Auto` preset, currently bound to
`HelloChrome_133`. Per-host ALPN probing routes traffic to HTTP/2 or HTTP/1.1
and TLS session tickets are cached. The static Chrome145/JA3/H2/H3 fingerprint
helpers describe candidate metadata and do not prove emitted browser-version,
JA3/JA4 or ordered HTTP/2 parity; this renderless transport does not implement
HTTP/3. TLS certificate verification stays on; `TLSInsecureSkipVerify`
exists only for test fixtures. `engine.DefaultUserAgent` remains the
explicit honest-bot identity when callers want attribution.

## Additional Web Globals

| Global | Notes |
|---|---|
| `crypto.randomUUID()` | v4 UUID, backed by `crypto/rand` |
| `crypto.getRandomValues(arr)` | fills array with random bytes (0-255) |
| `Headers` | constructor + `append`/`get`/`set`/`has`/`delete`/`forEach`/iterators; case-insensitive keys |
| `history.pushState/replaceState/back/forward/go` | mutates `location.*` in-process; does not navigate |
| `XMLHttpRequest` | minimal: `open/send/setRequestHeader/abort`, `responseText/status/statusText`, `onload/onerror/onreadystatechange`, getAllResponseHeaders. Wraps `fetch()`. |
| `element.style.<camelCase>` | reads/writes CSS declaration in the inline `style` attribute |
| `getComputedStyle(el)` | live read-only property methods backed by canonical cascade/computation; unsupported properties, UA/environment/layout defaults, selector semantics, stylesheet mutations and unowned nodes retain typed failure instead of fabricated values |
| `__url_parse(href, base?)` | helper exposed because V8 builds without WHATWG URL |
| `XMLSerializer` | `serializeToString(node)` returns `outerHTML` for elements, raw text for text/comment |
| `ReadableStream` / `WritableStream` / `TransformStream` | functional in-memory streams; `pipeTo`, `pipeThrough`, `tee` honour real chunk flow (no backpressure) |
| `Range` / `Selection` | spec-correct offsets in `setStart{Before,After}` / `setEnd{Before,After}`, `selectNodeContents` end-offset = child count, `toString()` returns substring for same-text-node ranges, `Selection.collapse/extend/containsNode` connected to ranges |
| `HTMLElement` subclasses | 67 spec subclasses + `HTMLUnknownElement`; multi-tag classes (`HTMLHeadingElement` H1..H6, `HTMLQuoteElement` Q+BLOCKQUOTE, `HTMLModElement` INS+DEL, `HTMLTableSectionElement` THEAD/TBODY/TFOOT, `HTMLTableColElement` COL+COLGROUP) use array-based `Symbol.hasInstance` |
| `NodeList` / `HTMLCollection` / `FileList` / `DOMTokenList` / `NamedNodeMap` | array-like `instanceof` markers backed by `Symbol.hasInstance` |
| `navigator.plugins` / `navigator.mimeTypes` | Chrome's five built-in PDF viewer plugins + two MIME types, iterable with `item`/`namedItem`/`refresh` |
| `navigator.cookieEnabled` / `navigator.onLine` / `navigator.webdriver` | static defaults (true/true/false) |

## Hybrid Execution Router

Support state: **supported**. `router.HybridRouter` deterministically selects `static_fetch`, `renderless_js`, `chromium_cdp`, or `stealth`, validates caller policy, transfers isolated state, escalates only upward, bounds attempts, opens per-mode circuits after repeated failures, and emits route evidence containing URL hashes and state counts but no cookie/storage values. `router.RenderlessExecutor` adapts the real V8 engine; `router.ChromiumExecutor` adapts a caller-owned `*bridge.Page` and proves a committed DOM through CDP.

```go
hybrid, err := router.New(router.Config{Executors: map[router.Mode]router.Executor{
    router.ModeRenderlessJS: router.RenderlessExecutor{Engine: eng},
    router.ModeChromiumCDP: router.ChromiumExecutor{Page: page},
}})
result, err := hybrid.Execute(ctx, router.RouteRequest{
    URL: "https://example.test", Signals: router.Signals{IsHTML: true},
})
```

| Symbol | Kind | Purpose |
|---|---|---|
| `router.HybridRouter` | struct | policy-validated hybrid execution owner |
| `router.RenderlessExecutor` | struct | real V8/renderless engine adapter |
| `router.ChromiumExecutor` | struct | real CDP page adapter with DOM postcondition |
| `router.BrowserState` | struct | isolated cookie/storage/profile state transfer |
| `router.RouteEvidence` | struct | redacted route, fallback, cost, and lineage evidence |
| `router.RouteFailure` | struct | typed fallback reason and retry admission |

The legacy compatibility symbols `bridge.ExecutionRouter`, `bridge.NewExecutionRouter`, `bridge.RouteStatic`, and `bridge.RouteRendered` remain available for older callers; new code must use `router.HybridRouter` so policy, state lineage, and evidence cannot be bypassed.

## Playwright Peer Lane

Support state: **routing and provisioning supported; execution unavailable in standalone Artemis**. Playwright is a peer lane beside the direct engines, never a hidden fallback. `router.ResolveTool(intent, requires, lane)` resolves an `AgentBrowserIntent` before the hybrid router: explicit `playwright` selects the admitted lane or fails typed, explicit `artemis` selects the direct engines or fails typed, and `auto` prefers the direct engines when they satisfy every requirement and uses the lane only when it satisfies all of them. Requirements the direct engines cannot serve are `browser:firefox`, `browser:webkit`, `capability:video`, `capability:har`, `capability:trace` and `capability:browser_script`. A failure is a `router.ToolUnavailableError` (unmet requirements, lane availability, diagnostics, remediation) carried as `TaskErrorCapabilityUnavailable`; an existing Artemis session never changes engine.

`playwright.Admit` verifies a signed artifact directory: `artemis-playwright-manifest.json` with its Ed25519 signature `artemis-playwright-manifest.sig` (`key_id`, `signature`) against trusted signer keys, the agent session ABI, required `core`, `runtime`, `sbom`, `licenses` and `provenance` roles, every listed file's size and SHA-256 through an `os.Root` (no symlink escape), and the absence of unlisted files. `playwright.Cache` installs admitted artifacts content-addressed under `artifacts/<digest>`, re-verifies the copy, checks free space (artifact size plus `--min-free` headroom) and only then moves `last-known-good.json`; a rejected candidate or a corrupted cache never replaces or deletes the serving artifact, and an offline restart re-verifies the same bytes. `playwright.Project` yields the BrowserToolCapabilityProjection (`ready`, `degraded` without the agent-source compiler so only `browser_script` is removed, or `unavailable` with diagnostics and remediation) with the specified limits. Executing an admitted artifact requires an embedding host's sandboxed runtime; standalone Artemis therefore reports the lane `unavailable` with that remediation. Configure the lane with `AgentConfig.Playwright` (`CacheDir`, `TrustedKeys`, `MinFreeBytes`, `PolicyRef`); `Agent.PlaywrightLane`, `Agent.InstallPlaywrightArtifact` and `Agent.ResolveBrowserTool` expose it.

| Symbol | Kind | Purpose |
|---|---|---|
| `artemis.PlaywrightLaneConfig` | struct | lane cache directory, trusted signer keys, free-space headroom and policy reference |
| `router.AgentBrowserIntent` | type | `auto`, `artemis` or `playwright` engine intent |
| `router.ParseIntent` | func | maps an optional request field to an intent (empty means `auto`) |
| `router.ToolRequirement` | type | `browser:<name>` or `capability:<name>` requirement |
| `router.ToolEngine` | type | `artemis` or `playwright` decision engine |
| `router.ToolDecision` | struct | auditable intent, engine, requirements, artifact digest and reason |
| `router.ToolUnavailableError` | struct | typed unavailability with unmet requirements, diagnostics and remediation |
| `serve.EngineIntent` | struct | `engine` and `requires` fields of `page.open` and `chromium.open` |
| `playwright.Manifest` | struct | signed provisioning contract: versions, admission mode, session ABI, browsers, capabilities, files |
| `playwright.File` | struct | listed artifact file with role, SHA-256 and size |
| `playwright.Role` | type | `core`, `runtime`, `browser`, `compiler`, `declarations`, `sbom`, `licenses`, `provenance`, `support` |
| `playwright.AdmissionMode` | type | `installer_bundle`, `signed_update_bundle` or `policy_approved_on_demand` |
| `playwright.BrowserRevision` | struct | declared browser build |
| `playwright.Capability` | type | lane operation family such as `navigation`, `video`, `har` or `browser_script` |
| `playwright.KnownCapability` | func | reports a declared lane capability |
| `playwright.Signature` | struct | detached `key_id` and base64 Ed25519 `signature` envelope |
| `playwright.TrustedKeys` | type | trusted signer keys by key ID |
| `playwright.KeyID` | func | stable signer key ID (first 8 bytes of the SHA-256 of the public key, hex) |
| `playwright.Admission` | struct | verified artifact with digest, signer and compiler presence |
| `playwright.OpenCache` | func | opens the content-addressed artifact cache |
| `playwright.LastKnownGood` | struct | durable pointer to the serving artifact |
| `playwright.Status` | struct | verified cache state with diagnostics and remediation |
| `playwright.Projection` | struct | BrowserToolCapabilityProjection of the lane |
| `playwright.Availability` | type | `ready`, `degraded` or `unavailable` |
| `playwright.Limits` | struct | session, command, compile, result and evidence limits |
| `playwright.DefaultLimits` | func | the specified default limits |

`artemis playwright --cache DIR --trusted-key FILE [--op status|install] [--artifact DIR] [--min-free BYTES]` reports the lane projection or installs a signed artifact; `--trusted-key` names a file with the base64 or raw 32-byte Ed25519 signer public key.

## Browser Provider Switch

Support state: **local Chromium supported**. `bridge.LocalChromeProvider` owns a validated `bridge.ChromiumBrowser` for local launch or explicit external CDP attachment. `BrowserSession.Runtime` is the same owner used for contexts, pages and actions. `Healthy()` reports its actual lifecycle/transport state; `Runtime.Probe(ctx)` performs a bounded live CDP check without opening another browser. Launch configuration is not proof of health.

`bridge.CamofoxProvider` preserves the selection name but has no admitted canonical action adapter. Its launch returns an error wrapping `errors.ErrUnsupported` and its health remains false, including when `CAMOFOX_URL` is configured. An HTTP URL never becomes a synthetic CDP session. Applications embedding Artemis should use the typed runtime directly and keep their existing policy, session and process owners.

```go
registry := bridge.NewProviderRegistry()
registry.Register("chrome", &bridge.LocalChromeProvider{})
provider, config, err := registry.SelectFromConfig()
```

| Symbol | Kind | Purpose |
|---|---|---|
| `bridge.BrowserProvider` | interface | `Name()`, `Launch(ctx, config)`, `Close()`, `Healthy()` |
| `bridge.ProviderRegistry` | struct | provider registry with `Register`, `Get`, `Default`, `SelectFromConfig` |
| `bridge.LocalChromeProvider` | struct | local headless Chromium provider |
| `bridge.CamofoxProvider` | struct | selection-compatible unavailable adapter with typed launch denial |
| `bridge.ProviderConfig` | struct | provider launch configuration |
| `bridge.BrowserSession` | struct | active browser session handle |
| `bridge.SelectBrowserProvider(candidates, preferred)` | func | select provider from candidates |

## CDP Bridge

Support state: **kernel supported**. `process.Browser`, `bridge.CDPTransport`, `bridge.ChromiumBrowser`, `bridge.BrowserContext` and `bridge.Page` own real discovery, launch/attach, request/event transport, context/target lifecycle and cleanup. Use `bridge/actions.Runtime` for supported high-level actions; legacy helper types alone do not establish supported behavior.

`ChromiumBrowser.NewContext` bounds `Target.createBrowserContext` to 15 s independent of the caller context. An unanswered admission within that bound, while the caller is still live, means the browser UI thread is wedged (observed on macOS: the main thread blocked acquiring a RunningBoard App Nap assertion under host load). The browser then becomes terminal with `CDPErrorUnresponsive` (`browser_unresponsive`): the transport closes, pending correlations release, an owned process is terminated and every later call fails fast. Recovery is an explicit relaunch; a caller-side cancellation or deadline only fails that call and leaves the browser healthy.

`BrowserContext.NewPage` opens every page in its own window (`Target.createTarget` with `newWindow`). A page that is a background tab of a shared window produces no frames, so pointer and key input dispatched to it waits for an acknowledgement that never arrives; with one window per page, input reaches every page of a context without activating or reordering tabs (`bridge.TestChromiumInputReachesEveryPageOfAContext`).

| Symbol | Kind | Purpose |
|---|---|---|
| `bridge.BridgeState` | type | bridge state machine states |
| `bridge.BridgeStateMachine` | struct | state machine with `IsActiveState`, `IsTerminalState` |
| `bridge.ContextKind` | type | context hierarchy kinds (Alloc, Browser, Tab) |
| `bridge.CDPContextUnit` / `bridge.CDPContextTree` | struct | context hierarchy units |
| `bridge.Batcher` | struct | CDP command batcher for pipelining |
| `bridge.CDPPipeline` | struct | CDP command pipeline |
| `bridge.CDPTask` / `bridge.CDPTaskResult` | struct | batched task + result |
| `bridge.MandatoryBatchType` | type | mandatory batch types (boxModel+style, navigate+wait+snapshot, etc.) |
| `bridge.EventFilter` | struct | CDP domain enable/disable manager |
| `bridge.CodeModeExecutor` | struct | Code Mode: LLM-generated JS async arrow functions |
| `bridge.BridgeInitializer` | struct | bridge initialization with `BridgeInitConfig` |
| `bridge.ChromeDiscovery` | struct | system Chromium auto-detection |
| `bridge.TabRegistry` | struct | tab registry (bridge-level) |
| `bridge.NetworkRequestBuffer` | struct | network ring buffer (max 50 entries/tab) |
| `bridge.RefRegistry` / `bridge.RefHandle` | struct | ARIA ref-ID registry for snapshots |
| `bridge.CircuitBreaker` | struct | per-domain circuit breaker (5 failures → 60s pause) |
| `bridge.IdleWatchdog` | struct | idle tab watchdog with `IdleWatchdogConfig` |
| `bridge.InactivityMonitor` | struct | inactivity monitor with `InactivityConfig` |
| `bridge.ConfigHasher` / `bridge.ConfigHash` | struct | config-hash for session isolation |
| `bridge.PolicyHook` / `bridge.PolicyEngine` | interface | navigation policy hooks |
| `bridge.PrivacyRoutingHook` | struct | privacy routing hook |
| `bridge.AuditHook` | struct | OCSF audit hook |
| `bridge.RecoveryAction` | interface | recovery action interface |
| `bridge.PageLoadStrategy` | struct | page load strategy (auto-wait) |
| `bridge.ScreenshotFormat` | type | screenshot format (PNG, JPEG) |
| `bridge.JPEGPipeline` | struct | JPEG screenshot pipeline |
| `bridge.CDPConnectMode` | type | CDP connection mode |

`bridge.JPEGPipeline.DecodeScreenshot` admits at most 32 MiB of encoded base64 and 33,554,432 decoded pixels, validates the JPEG frame dimensions before allocating decoded pixels, and returns a decode label only after complete success. `DecodeJPEG` consumes caller-owned raw JPEG bytes without copying the input. `DecodeJPEGResult(ctx, raw)` additionally returns operation-local acceleration evidence and checks cancellation before admission and after joined decoding. The constructor freezes native admission privately; mutating public capability fields cannot select or relabel a decoder. Native failure selects Go `image/jpeg` with `degraded` evidence; rejected/unlinked admission uses `unavailable` evidence and failed observations remain `unknown`, all with the actual `sw_go_image` fallback path. Cancellation and failed decoding publish no image or label. `DecodePath()` names the guaranteed software fallback; it is not a receipt for the last operation. `ResizeForModel` uses `imaging` Lanczos resampling, preserves aspect ratio, and returns the original image when it already fits; invalid model dimensions, empty images and excessive pixel counts return nil. The default model bounds are 1024 by 1024.

The real CDP screenshot producer and pipeline share `DecodeScreenshotData`: bounded scalar/table-based cristalhq/base64 decoding with CR/LF compatibility through the Go standard decoder. The convenience call owns one output allocation. `DecodeScreenshotDataInto(dst, encoded)` writes into caller-owned non-overlapping output without allocating an input/output buffer on a successful call; allocate at least `base64.StdEncoding.DecodedLen(len(encoded))+1` bytes for the fast decoder's guard byte. Immutable encoded input is borrowed only during the synchronous call. This is not a SIMD or allocation-free image-decode claim. A shared structural JPEG pass skips length-delimited metadata, handles entropy stuffing/restart markers, requires a completed image stream and enforces at most 100 scans before either decoder, so native rejection cannot bypass the scan-work budget through software fallback.

On macOS, CGo builds link the system VideoToolbox frameworks. Admission requires a JPEG-specific hardware session and a positive `UsingHardwareAcceleratedVideoDecoder` property, rather than only `VTIsHardwareDecodeSupported`. Every native decode requires the same proof, validates output geometry/pixel format, borrows input only within its synchronous call, and closes all owned sessions and CoreFoundation references. Unsupported JPEG sessions or properties select Go fallback. The `hw_videotoolbox` label requires actual successful hardware decoding, not an Apple Silicon CPU identity.

Linux native CPU decoding is optional: install libjpeg-turbo 3.1 or newer with its development headers and `pkg-config`, then build with `CGO_ENABLED=1 go build -tags artemis_libjpeg ./cmd/artemis`. The same explicit tag is available on macOS with an existing compatible libjpeg-turbo installation. This backend enforces strict decode warnings, 100 progressive scans, 128 MiB intermediate memory and the shared pixel budget; every decoder is destroyed before return. Its compatibility label `hw_libjpeg_turbo` names the successfully executed native CPU library, and does not assert GPU hardware or that a SIMD instruction ran. Untagged Linux and non-CGo builds require no native library and use Go decoding. Automatic inference dispatch remains a separate integration contract.

## Bridge Actions

The `bridge/actions` package implements typed Chromium actions with stable-reference resolution, actionability checks, postcondition evidence, bounded retries, cancellation, and frame-aware execution. Downloads are event-driven through CDP, write only to the injected or canonical session-owned store, are canceled on policy/quota breach, are MIME-sniffed and hashed before success, and restore Chromium to download-deny mode after every action. A per-browser-context download lease (`bridge.Page.AcquireDownloadLease`) serializes download-behavior ownership so concurrent pages cannot redirect or deny each other's transfers; `Browser.downloadWillBegin` is accepted only for a frame of the acting page (`Page.OwnsFrame`: main, same-process or attached frame), progress counts only for that GUID, and a completed 0-byte file fails with `postcondition` and is never adopted. The legacy `downloadDir` request field is rejected rather than treated as a caller-controlled filesystem path.

Screenshot requests default to ordinary full-dimension PNG. Explicit JPEG requests use quality 80 unless `quality` selects 1..100. Format/quality contradictions and unsupported formats are rejected before CDP effects. Both image formats must completely decode within the shared encoded/pixel budgets; a valid header with a damaged body cannot succeed. An explicit `{"kind":"screenshot","forModel":true}` request selects JPEG and prepares a 1024x1024 aspect-preserving model input, optionally with an explicit JPEG `quality`. `forModel` is rejected for other actions or PNG. Ordinary screenshots preserve their original returned bytes/dimensions; already-bounded model images avoid re-encoding. Outcomes carry `decodePath`, JPEG `decodeAcceleration` state/source/reason, actual returned `width`/`height`, and `modelPrepared` only for successfully prepared model inputs. One action runtime retains one lazily captured immutable boot snapshot. The existing policy, before/after evidence and cancellation rules still apply; this option does not initiate inference or extra captures.

Each admitted action receives a random 128-bit `actionId`. `evidence.effect` distinguishes `not_attempted`, `read_only`, `may_have_occurred`, and `dispatched`; dispatch is a local execution observation, not a business completion receipt. `attempts` counts actual action executions and `captureAttempts` counts before-action observation attempts. Caller `idempotent=true` only opts into a bounded retry request: runtime-owned replay admission permits screenshot, document-readiness wait, and tab listing; every other kind executes at most once, including arbitrary evaluate/assert expressions and PDF lifecycle events. After a dispatched action, failure to capture its required after-snapshot returns `success=false` with typed `evidence_unavailable` or cancellation/timeout while retaining the original payload and effect evidence. Never replay an ambiguous or unverified effect automatically. Closing the root target cannot produce a live after-snapshot and is reported as unverified rather than inventing one. Successful navigation requires exact parsed destination agreement, including origin, path, query and fragment; a redirect or prefix lookalike does not satisfy it. HTTP(S) comparison normalizes root slashes, default ports and literal dot segments; unsupported canonicalization forms fail conservatively.

| Symbol | Kind | Purpose |
|---|---|---|
| `actions.NewClickAction(ref)` | func | create click action for ARIA ref |
| `actions.ClickWithMovement(ctx, runtime, ref, start, target)` | func | click on the runtime after moving along a generated Bezier path |
| `actions.HumanProfile` | struct | `Request.Human`: real CDP human-like input. Click: Bezier approach with frame timing, pre-click delay, ±5 px target offset, 30-119 ms hold, jittered release (explicit `MovePath`/`PreClickDelay` override). Type: per-key rawKeyDown/char/keyUp with character-aware or `KeyDelay`±`KeyVariance` rhythm and `TypoRate` wrong-key/Backspace corrections. Scroll: eased `ScrollSteps` wheel events over `ScrollDuration`. Non-zero `Seed` reproduces a sequence; bounds are validated; cancellation always delivers pending keyUp/mouseReleased. `ClickAction`, `TypeAction` and `ScrollAction` map their fields into it; `Request.Button` selects left/right/middle |
| `actions.NewTypeAction(ref, text)` | func | create type action |
| `actions.NewFormFill(ref, value)` | func | form fill action |
| `actions.NewFormSelect(ref, value)` | func | form select action |
| `actions.NewFormCheck(ref)` | func | form check action |
| `actions.NewFormSubmit(ref)` | func | form submit action |
| `actions.FormBatch(ctx, actions)` | func | batch form actions |
| `actions.NewScrollAction(direction, amount)` | func | scroll action |
| `actions.ResolveSelector(selector)` | func | resolve ARIA ref / CSS / XPath |
| `actions.ResolveAndValidate(ctx, selector)` | func | resolve + validate selector |
| `actions.GenerateClickPath(start, end)` | func | Bezier click path |
| `actions.ComputeScrollSteps(total, steps)` | func | scroll step distribution |
| `actions.EaseInOut(t)` | func | easeInOut interpolation |

## CDP Operations

Support state: **kernel and typed actions supported**. `process.Browser`, `bridge.CDPTransport`, `bridge.ChromiumBrowser`, `bridge.BrowserContext`, and `bridge.Page` own binary discovery, isolated launch, browser identity, bounded request correlation, target events, navigation, crash handling and cleanup. `bridge/observe.Collector` supplies real DOM/AX references; `bridge/actions.Runtime` performs admitted actions with postconditions. The helper shapes below are lower-level primitives, not a substitute for that action owner.

Owned Chromium launches are resource-contained by default: the process group has hard CPU, RSS, profile-disk, and lifetime ceilings; owner-context cancellation and budget breaches terminate the full group; unexpected leader exit reaps helpers; disposable profiles and configured-profile endpoint/lease state are cleaned after normal close or crash. The optional typed resource sink receives an immediate post-readiness sample and every periodic sample before budget enforcement. On macOS and Linux a small POSIX guardian disables core dumps, watches the original Artemis owner PID, reaps the group, and removes owned profile state even if the owner process exits without running Go cleanup. Chromium's OS sandbox remains required unless `process.SandboxDisabled` is explicitly selected; direct `--no-sandbox` and `--disable-setuid-sandbox` arguments are rejected.

Owned CPU placement uses LaunchConfig.CPUAdmission, RequireCPUAdmission, CPUCount and CPUAffinity. The injected CPUAdmission reserves before profile creation and supplies a reservation-bound context plus a release handle; missing, typed-nil or incomplete ownership fails closed. CPUCount bounds the requested affinity set, with one unit when an injected owner supplies no explicit count. The Linux process-start callback applies and verifies the allowed CPU mask before fork/exec, so the POSIX guardian and browser inherit it. Browser.CPUAffinity reports that actual start path; ordinary/unsupported paths carry no activation claim. Reservation cancellation/preemption and the caller's independent cancellation terminate the owned process; returning an unrelated reservation context cannot disable the caller's stop boundary. CPU release follows a bounded complete process-group state observation through /bin/ps; live, stopped-but-resumable, malformed, truncated or unknown observations retain capacity. Zombie/dead members cannot produce CPU work. Group-signal errors permit release only when an independent fresh observation proves no live CPU producers. Startup/registration failure joins the process owner before release. Cleanup errors remain visible through Close and Err, and an unproved group retains its profile/recovery state. Unconfigured standalone launches retain the existing local resource budgets.

| Symbol | Kind | Purpose |
|---|---|---|
| `cdpops.GetBoxModel(ref)` | func | element box model |
| `cdpops.GetElementCenter(box)` | func | element center coordinates |
| `cdpops.IsElementVisible(box)` | func | visibility check |
| `cdpops.IsElementClickable(info)` | func | clickability check |
| `cdpops.FilterVisible(elements)` | func | filter visible elements |
| `cdpops.FindByRef(elements, ref)` | func | find element by ARIA ref |
| `cdpops.GenerateBezierPath(start, end, steps)` | func | Bezier curve path |
| `cdpops.AddJitter(p, maxJitter)` | func | add jitter to point |
| `cdpops.CSSToDevicePixels(p, scale)` | func | CSS → device pixel transform |
| `cdpops.DeviceToCSSPixels(p, scale)` | func | device → CSS pixel transform |
| `cdpops.NewNavigator()` | func | page navigation manager; whole navigation operations serialize (cancellable while queued) so results, `State` and `CurrentURL` stay bound to their own call |
| `cdpops.LifecycleWatcher` / `cdpops.LifecycleEvent` | interface / struct | `Page.lifecycleEvent` stream for the caller's target; `networkidle` / `networkalmostidle` wait for Chrome's `networkIdle` / `networkAlmostIdle` event of the navigation's own frame and loader (current main-frame document for `WaitForLoad`), never for `readyState`; callers without it get `ErrLifecycleEventsRequired` before any effect. `load` / `domcontentloaded` keep `readyState` `complete` / `interactive` |
| `cdpops.NewPointerDispatcher()` | func | pointer event dispatcher |
| `cdpops.Point` / `cdpops.Rect` / `cdpops.Quad` / `cdpops.BoxModel` | struct | geometry types |
| `cdpops.ElementInfo` | struct | element metadata |
| `cdpops.MouseEvent` / `cdpops.MouseButton` | type | mouse event types |

## Tab Management

The `bridge/tabs` package implements multi-tab management: tab registry + lifecycle, concurrent tab execution, alert/confirm/prompt dialog handling, and per-tab locking.

| Symbol | Kind | Purpose |
|---|---|---|
| `tabs.NewTabRegistry()` | func | create tab registry |
| `tabs.NewTabExecutor(registry, maxConcurrent)` | func | concurrent tab executor |
| `tabs.NewTabLock()` | func | per-tab lock |
| `tabs.NewDialogHandler()` | func | dialog handler |
| `tabs.NewAlertDialog(message, url)` | func | alert dialog |
| `tabs.NewConfirmDialog(message, url)` | func | confirm dialog |
| `tabs.NewPromptDialog(message, url, defaultPrompt)` | func | prompt dialog |
| `tabs.Tab` | struct | tab handle |
| `tabs.TabState` | type | tab state (Open, Closed, Loading) |
| `tabs.DialogAction` / `tabs.DialogType` | type | dialog action/type enums |
| `tabs.RecyclePolicy` | struct | idle and memory-pressure recycling of pool tabs |
| `tabs.TargetRecycler` | interface | target source that clears origin data and blanks a target |

Pool tab recycling (spec L4482, L4522): the tabs a `TabRegistry` created itself (for example through the action runtime's `tab_open`) are pool tabs; session pages opened elsewhere in the same browser context and the active tab are never recycled. `TabRegistry.RecycleTick(ctx, tabs.RecyclePolicy)` recycles every pool tab idle for `IdleAfter` (default 5 min; last activity, or creation time before any activity) and, when `HeapAlloc` exceeds `HeapFraction` (default 70%) of the GOMEMLIMIT (`debug.SetMemoryLimit(-1)`; no limit means no pressure), the least recently active remaining pool tab. Recycling goes through the source's `TargetRecycler`: the action runtime's source clears the origin's cookies, local/session storage, IndexedDB, Cache Storage and service workers with `Storage.clearDataForOrigin` and navigates the target to `about:blank` without closing it, so DNS, TLS and HTTP/2 state stay warm and the tab stays reusable. `TabRegistry.RunRecycler` ticks every `Interval` (default 30 s) and records failures in `LastRecycleError`; `RecycledTotal` counts recycles. `actions.NewRuntimeWithConfig` runs this supervisor for its registry (`RuntimeConfig.TabRecycle`, `DisableTabRecycle`) and `Runtime.Close`, also run by the page's cleanup, stops and joins it, so it never outlives its page or browser.

## Renderless Engine

The `renderless` package is the in-process no-render JS browser path. It provides a standalone engine that runs V8 with DOM/WebAPI globals, inline + external script execution, fetch/XHR bridge, cookie jar, OnRequest mock/intercept, robots/private-IP guard, CSS parse/cascade/computed style, and a generated `RenderlessCapabilityProfile`. It never owns layout/paint/CDP/profile/server semantics and escalates to `bridge/` via the execution router.

| Symbol | Kind | Purpose |
|---|---|---|
| `renderless.NewEngine(cfg)` | func | create renderless engine |
| `renderless.Engine` | struct | renderless engine handle |
| `renderless.EngineConfig` | struct | engine configuration |
| `renderless.Page` | struct | renderless page |
| `renderless.NewPage(url, status, body)` | func | create page from raw HTML |
| `renderless.ContextPool` | struct | V8 context pool |
| `renderless.NewContextPool(maxSize)` | func | create context pool |
| `renderless.IsolatePool` | struct | bounded isolate slot accounting (IDs only, holds no isolates) |
| `renderless.NewIsolatePool(maxSize)` | func | create isolate pool |
| `renderless.RuntimeContext` | struct | runtime context |
| `renderless.ScriptRouter` | struct | script execution router |
| `renderless.NewScriptRouter()` | func | create script router |
| `renderless.InterceptHandler` | struct | request intercept handler |
| `renderless.NewInterceptHandler()` | func | create intercept handler |
| `renderless.NewMockResponse(status, body)` | func | mock response for intercept |
| `renderless.CSSParser` / `renderless.CSSRule` | struct | CSS parser + rule |
| `renderless.ComputedStyle` | struct | computed style engine |
| `renderless.RenderlessCapabilityProfile` | struct | generated capability profile |
| `renderless.GenerateCapabilityProfile(cfg, registry)` | func | generate capability profile |
| `renderless.WebAPIRegistry` / `renderless.WebAPIGlobal` | struct | WebAPI registry + global |
| `renderless.InterceptAction` | type | intercept action (continue, mock, fail) |
| `renderless.ScriptType` | type | script type (inline, external) |

`NewWebAPIRegistry` observes a fresh ordinary JS Context once per process and retains an immutable descriptor graph. Observation is bounded, joins Context/Runtime ownership, never executes descriptor getters and writes no artifacts. Native V8 global names come from a separate pristine isolate; newly installed public globals join the existing registry automatically. `InstalledBindings()` returns detached copies of all observed descriptors, including private callbacks, shared-object aliases and prototype edges. Canonical API aliases resolve actual descriptors; `Register` cannot invent uninstalled bindings. `BindingPath` records the observed target and `Implemented` records presence, not semantic proof. Profiles retain `bindingSourceHash`, `bindingInventoryHash` and `bindingDescriptorCount`; the source hash pins JS/native wrapper sources, cookie/DOM authority, tests and linked V8/Go versions, not a complete executable or authenticated producer. Observation failure denies engine capabilities through `bindingInventoryError`. A shared scoped conformance matrix supplies names/categories and resolves exact executable test declarations from compiled JS, Engine, Agent, Network, CSS, WebAPI and Renderless sources. `classificationSourceHash` binds those sources/tests, the JS/native/cookie/DOM identities, Go version and platform. `conformance` records case/category/scope plus test suite/file/line/content hash. Missing/ambiguous/invalid declarations, missing cases and conflicting case ownership fail closed through `conformanceError`; caller-supplied case IDs cannot promote APIs. Same-case declared aliases retain their common classification. Test references do not attest that a run passed or authenticate a producer; execution/release gates and complete runtime acceptance remain TASK-2795 work.

`Register` never infers real support from presence: omitted/invalid categories and absent bindings classify as `unsupported_escalate`. Profile generation emits one deterministic classification per case-insensitive binding identity; case collisions and contradictory category lists require escalation. Fetch/XHR support flags derive from that same classification. Registry declarations cannot promote reserved unsupported CSS APIs; current conformance observations remain their admission path.

`renderless.CSSParser.Parse(source) ([]CSSRule,error)` uses the canonical bounded CSS parser; `CSSRule` embeds its actual ordered `css.Rule`. `NewComputedStyle(rules).Compute(node,inline,property) (string,error)` requires a real `*html.Node` and delegates selection, inheritance, variables and computation to `css.Cascade`. Malformed/unsupported input and layout queries preserve canonical typed diagnostics.

`GenerateCapabilityProfile` does not invent CSS conformance: `getComputedStyle`, `CSSStyleSheet` and `CSSStyleDeclaration` require escalation until current observations establish the admitted subset. Only `getComputedStyle` exists as a global; absent constructors stay unsupported. `corpus.GenerateCSSCapabilityProfile(ctx,base,results)` requires all six current source-pinned renderless/Chromium observations for `css-002`, `css-003` and `css-004`, validates every semantic assertion and derives parser/selectors/cascade/computed-style/inline-style support. `CSSConformance` carries source, CSS and binding hashes plus each case/runner/fixture/result digest. The host owns current canonical receipt admission and may attach its evidence references; this projection authenticates neither producer nor release. Typed runtime rejection still overrides profile support for unsupported selectors, values, document dependencies and layout.

CSS failures during idle pumping retain post-script phase evidence. Completed script effects, unsafe navigation methods, request bodies and CSS/resource budget failures cannot be replayed through automatic browser escalation. Pre-script safe navigation may escalate through the existing router and policy; CSS support metadata grants no action or replay authority.

## Stealth Layer

Support state: **supported for measured, policy-acknowledged page and worker injection**. `StealthDefault` keeps local/LAN browsing unmodified. `StealthStealth` and `StealthParanoid` require a valid legal acknowledgement and public-domain policy; the immutable `EnvironmentProfile` validates UA/client-hint, platform, locale, timezone, viewport, and optional measured WebGL/network values before generating scripts.

Activation is environment-driven and fail-closed — `ARTEMIS_STEALTH=stealth|paranoid` together with `ARTEMIS_STEALTH_PURPOSE` and `ARTEMIS_STEALTH_LEGAL_BASIS` enables the measured-profile path in both the runtime and the CLI (`observe`, `act`, `trace`); without the acknowledgement the request fails instead of silently downgrading. The Chromium path applies identity via CDP-native `Emulation` overrides (User-Agent + Client Hints metadata, locale, timezone, Accept-Language) before the first navigation — no `HeadlessChrome` token, no GPU-forcing `--disable-gpu` (SwiftShader fingerprint), real `ANGLE`/Metal WebGL strings, `Notification.permission=default`, and guarded hardware/navigator interfaces (`mediaDevices`, `serviceWorker`, `clipboard`, `credentials`, `locks`, `storage`, `wakeLock`, `hid`, `usb`, `serial`, `bluetooth`, `presentation`, `keyboard`, `virtualKeyboard`, `launchQueue`, `managed`). Paranoid additionally sends a plausible search referrer on the first navigation only. `bridge.BrowserContext.NewPageWithScripts` installs the page pre-script before navigation, and the CDP target router injects the worker script while child targets are paused. The real behavior contract is `bridge.TestChromiumTargetScriptsRunBeforePageAndWorkerCode`; this is not a universal anti-detection or challenge-solving guarantee.

The patch and launch-flag counts below describe the existing stealth catalog. The production `BrowserRuntime` path activates only measured profile overrides that passed validation; it does not market the catalog counts as proof of universal anti-detection.

| Symbol | Kind | Purpose |
|---|---|---|
| `stealth.StealthDefault` | `StealthLevel` | local/LAN: zero stealth |
| `stealth.StealthStealth` | `StealthLevel` | public sites: 27 patches + 60 flags |
| `stealth.StealthParanoid` | `StealthLevel` | hardened sites: 29 patches + 60 flags |
| `stealth.DetermineStealthLevel(targetURL, policy, lookup)` | func | determine level from URL + policy |
| `stealth.SuggestEscalation(current, signals)` | func | suggest escalation from bot signals |
| `stealth.Profile` | struct | stealth profile (viewport, UA, vendor, platform, languages, timezone, etc.) |
| `stealth.Script(p)` | func | generate stealth script for profile |
| `stealth.BundledScript()` | func | bundled 27-patch script (~25KB) |
| `stealth.ParanoidScript()` | func | paranoid-mode +2 on-demand patches |
| `stealth.Quick()` | func | quick stealth script |
| `stealth.ContextHash(opts)` | func | session-isolation hash |
| `stealth.BuiltinGeoPresets()` | func | 8 built-in geo presets |
| `stealth.ValidatePreset(p)` | func | validate geo preset |
| `stealth.ValidateManifest(dir)` | func | validate stealth manifest |
| `stealth.LaunchFlagCount()` / `stealth.LaunchFlagCountFor(level)` | func | launch flag counts |
| `stealth.PatchCount()` / `stealth.PatchCountFor(level)` | func | patch counts |
| `stealth.ReferrerForDomain(rawURL, mem)` | func | referrer from domain memory |
| `stealth.IsSwiftShader(renderer)` | func | detect SwiftShader |
| `stealth.IsChromiumFingerprint(config)` | func | validate H2 fingerprint |
| `stealth.ValidateH2Settings(frame)` | func | validate H2 settings frame |
| `stealth.DeriveEffectiveType(rtt, downlink)` | func | derive network effective type |
| `stealth.ParseChromeVersion(ua)` | func | parse Chrome version from UA |
| `stealth.EnvironmentProfile` | struct | immutable versioned session environment and fingerprint contract |
| `stealth.NewEnvironmentProfile(...)` | func | validate measured facts and derive stable profile identity |
| `stealth.NewDocumentScript(profile)` | func | render a JSON-escaped page pre-script |
| `stealth.NewWorkerScript(profile)` | func | render a worker-safe pre-script with the same identity |
| `stealth.EnvironmentProfile.ValidateConsistency(probe)` | method | fail closed on observed fingerprint drift |
| `stealth.STEALTH_ARGS` | var | stealth launch arguments |
| `stealth.BasePatchCount` / `stealth.ParanoidPatchCount` | const | patch count constants |

## Observation Layer

`bridge/observe.Collector` captures initialized DOM/AX evidence from the main renderer and every attached out-of-process iframe. Frame/method failures are explicit warnings, and failed frame documents contribute no inferred actionable targets. A node is interactable only after a successful native hit test. Observation and pointer dispatch share viewport-relative content quads, verified iframe content-box offsets and hit checks through every renderer ancestor. The frame hierarchy includes the actual trees of attached renderer sessions; the main renderer's tree alone omits remote children. Covered, detached, unknown-hit and budget-omitted targets fail closed. Rotated or scaled iframe geometry remains explicitly unverified and cannot admit pointer actions. Open shadow roots, nested local frames and scrolled viewports retain their owning renderer route.

The `observe` package implements page observation: accessibility tree extraction + diff, network ring buffer + subscriber pattern, console log capture (ring buffer 1000), performance metrics, and output formatters (HAR, NDJSON).

Stable-reference retention is configured on `bridge/observe.Config`. `MaxReferences` defaults to 4096, `MaxReferenceBytes` to 4194304 bytes of canonical encoded redacted node payload, and `RetainedEpochs` to 2 (current and previous navigation epoch). Zero selects defaults; negative retention budgets are rejected. Least recently observed references are evicted with their identity mappings, and reference IDs never recycle. Current nodes omitted by count or payload limits have no usable ref and report `reference_retention` truncation. An evicted ref resolves as `stale`, including one evicted during the resolver's fresh observation. Retained refs use the existing confidence threshold and ambiguity checks to resolve semantic matches within their original frame. Payload accounting bounds stored reference content and does not claim a bound on transient CDP buffers or total process memory.

`bridge/observe.DiffSnapshots` compares semantic, structural, attribute, layout and actionability state, including covered/unknown hit transitions. Ref replacement remains an add/remove identity change. Output order is deterministic by frame, document order, backend identity and ref. Unchanged nodes take a direct comparison path without allocating diff maps, including structural nodes without refs.

### Task-scoped queries

`Session.QueryChromiumPage(ctx, pageID, bridgeobserve.QueryRequest)` uses the existing owned page, live collector and reference namespace. `chromium.query` exposes the same request through the authenticated steering server. The CLI requires an existing server, reconnect identity, session and page; it never opens or navigates a page:

```sh
artemis query --session SESSION_ID --page PAGE_ID --roles button,textbox --max-nodes 100 --max-bytes 8192
artemis query --session SESSION_ID --page PAGE_ID --roles button,textbox --max-nodes 100 --max-bytes 8192 --since REVISION
artemis query --session SESSION_ID --page PAGE_ID --target REF --max-bytes 8192
artemis query --session SESSION_ID --page PAGE_ID --mode subtree --subtree REF --max-bytes 8192
```

Use the actual identities from the persistent steering flow and the actual revision/ref from its output. `query` rejects positional URLs and `--snapshot-epoch`, including an explicit zero. Reference actions still require the returned epoch through `act --snapshot-epoch`.

| Option | Contract |
|---|---|
| `mode` / `--mode` | `interactive` by default; also `full`, `subtree`, `evidence`. Evidence selects interactive nodes with native actionability evidence. |
| `subtreeRef` / `--subtree` | required only for subtree mode; remains within the root's observed frame and navigation epoch |
| `targetRef` / `--target` | page-bound reference to diagnose; an exact observed target is prioritized ahead of node, hit and byte budgets. Without role/text filters, interactive/evidence mode selects that target and semantic candidates; full/subtree retain their normal scope. |
| `frameId` / `--frame` | restrict to a known observed frame; conflicting root/target frame or subtree is rejected |
| `roles` / `--roles` | up to 32 case-insensitive AX roles, each at most 64 UTF-8 bytes; CLI uses a comma-separated list |
| `text` / `--text` | case-insensitive literal name/value filter, at most 256 UTF-8 bytes |
| `maxNodes` / `--max-nodes` | defaults to 200, bounded by collector `MaxNodes`; relevance is selected before emission limits |
| `maxBytes` / `--max-bytes` | defaults to 32768; 512 through collector `MaxSnapshotBytes`; bounds the complete compact query-result JSON, including page prefixes and metadata |
| `baseRevision` / `--since` | exact prior revision with identical normalized options on the same owning collector |
| `--timeout` | positive query deadline, default 20 seconds; cancellation includes waiting for the query lease |

The `artemis.query.v1` result carries `kind`, opaque `revision`, `baseRevision`, `epoch`, `capturedAt`, relevant `nodeCount`, warnings, truncation/loss metadata and optional `target`. `full` includes `snapshot`; `delta` includes material `added`, `changed`, `removed` nodes; `unchanged` includes neither and retains the revision. Only a complete matching baseline may produce incremental output. `resetReason` is `baseline_unavailable`, `options_changed`, `navigation`, `incomplete_observation` or `delta_budget`. Navigation resets with its new epoch; loss of an applicable baseline or truncated source evidence sets `lost`. Apply deltas only to their exact base revision; a full result replaces that projection.

Each collector retains only its latest bounded query baseline, separate from the existing bounded reference retention. Changing options or querying another projection invalidates older revisions. A query is serialized by a cancellable lease. Output omission is explicit; an oversized delta returns a full reset rather than a clipped delta. A budget that cannot hold required metadata or an explicitly observed target returns an error. These are byte budgets, not tokenizer-specific counts or bounds on transient CDP buffers/process memory. CLI output is compact JSON with a trailing newline only when it fits the requested bound; the raw steering response additionally has its normal protocol envelope.

Target diagnosis reports `exact`, `re_resolved`, `stale`, `detached`, `ambiguous` or `unavailable` plus observed actionability. Up to three deterministically ordered candidates and closed remedy names guide recovery. Covered targets suggest inspecting the covering element; incomplete evidence cannot claim detachment. Replacements require semantic verification and an explicit current reference. Remedies never execute actions or authorize dismissing an overlay. Native hit proof uses unfiltered frame-local ancestry; fresh pointer dispatch verifies the target's actual subtree independently of the projected node list.

| Symbol | Kind | Purpose |
|---|---|---|
| `observe.NewAnnotator(viewportWidth, viewportHeight)` | func | create annotator |
| `observe.AXNode` / `observe.AXTreeNode` | struct | accessibility tree node |
| `observe.DiffAXTrees(before, after)` | func | diff AX trees (edit distance) |
| `observe.MyersDiff(before, after)` | func | Myers diff algorithm |
| `observe.SnapshotInteractive(tree)` | func | snapshot interactive elements |
| `observe.SnapshotByRole(tree, role)` | func | snapshot by ARIA role |
| `observe.DedupRoleSnapshot(nodes)` | func | deduplicate role snapshot |
| `observe.FormatHAR(entries)` | func | format entries as a complete HAR document |
| `observe.FormatNDJSON(events)` | func | format NDJSON output |
| `observe.FormatConsoleNDJSON(entries)` | func | format console NDJSON |
| `observe.FormatOutput(format, events)` | func | format captured events as HAR or NDJSON |
| `observe.HARDocument` | struct | top-level HAR file object; the log nests under `log` |
| `observe.HARPostData` | struct | request body section of a HAR entry |
| `observe.HARExporter.FromNetworkEvents(events)` | method | map captured events to HAR entries with headers, timings, post data and credential redaction |
| `observe.NetworkEvent.Headers` / `.ResponseHeaders` | field | request and response headers kept as separate sets |
| `observe.TruncateContent(content, maxLen)` | func | truncate content |
| `observe.ContentRoles` / `observe.InteractiveRoles` / `observe.StructuralRoles` | var | role classification maps |
| `observe.DefaultConsoleBufferSize` | const | 1000 entries |
| `observe.DefaultViewportWidth` / `observe.DefaultViewportHeight` | const | 1280×720 |

### Recording primitives

`observe.WorkflowCaptureRecorder.RecordContext(ctx, action)` stores an immutable copy of supplied action evidence. `Recording` and `Stop` return deep copies. Stop finalizes once, cancels its active pixel callback and rejects further append. Waiting for the record lease is context-cancellable. A browser owner supplies `WorkflowCaptureConfig.CaptureScreenshot`; callbacks must honor the provided context. With low selector confidence and no pixel provider, the action reports `screenshot_needed` and no screenshot is counted. Empty metadata, invalid images and a valid header without complete pixels cannot create screenshot evidence.

Defaults are 128 actions and 8 MiB charged recording content, including 4096 reserved metadata bytes and conservative persisted-image path allowance. Tool JSON fields are capped at 32768 bytes, selectors at 16 with values up to 1024 bytes, and each supplied AX projection at 2000 nodes. Pixel sources are a bounded regular file or embedded base64, exclusively one; PNG/JPEG must fully decode within 4 MiB, 8192 per dimension and 8 million pixels. `ScreenshotDir` persists owner-provided pixels to a new mode-0600 file. `SaveToFile` writes compact JSON to a new mode-0600 destination and preserves existing files; parent directories are created with mode 0700. These are recording primitives. Native browser before/action/after wiring, parameterized replay, authority and skill activation have separate contracts; an imported recording or decoded image does not prove that they executed.

## Solver / CAPTCHA Pipeline

The `solver` package implements a 2-stage challenge/CAPTCHA pipeline: vision solve (screenshot → LLM vision → instruction → execute) → user fallback. `ChallengeDetector` records deterministic DOM, title, network, response, and visual signals. `ChallengeResolver` is the policy boundary: it admits only allowlisted domains, challenge types and strategies, bounds attempts/duration, and reports `solved` only after a caller-provided postcondition is true. Unsupported and disallowed outcomes remain typed failures.

| Symbol | Kind | Purpose |
|---|---|---|
| `solver.NewChallengeDetector()` | func | create challenge detector |
| `solver.ChallengeDetector` | struct | challenge detector |
| `solver.ChallengeType` | type | challenge type enum |
| `solver.TypeNone` | `ChallengeType` | no challenge |
| `solver.TypeCloudflare` | `ChallengeType` | Cloudflare challenge |
| `solver.TypeRecaptcha` | `ChallengeType` | reCAPTCHA |
| `solver.TypeHCaptcha` | `ChallengeType` | hCaptcha |
| `solver.TypeGeneric` | `ChallengeType` | generic challenge |
| `solver.InferenceHub` | interface | LLM inference hub interface |
| `solver.NewInferenceHubHook(hub)` | func | create inference hub hook |
| `solver.InferenceHubRequest` / `solver.InferenceHubResponse` | struct | inference request/response |
| `solver.PipelineResult` | struct | pipeline result |
| `solver.PipelineStage` | type | pipeline stage (vision, fallback) |
| `solver.PipelineStats` | struct | pipeline statistics |
| `solver.MetricsStore` | struct | SQLite metrics store |
| `solver.OpenMetricsStore(path)` | func | open metrics store |
| `solver.FormatChallengePrompt(challengeType, context)` | func | format challenge prompt |
| `solver.PageSignals` | struct | page signals for detection |
| `solver.ChallengeInfo` | struct | detected challenge info |
| `solver.ChallengeResolver` | struct | policy-gated strategy execution with verified outcome |
| `solver.ChallengeOutcome` | struct | secret-free typed challenge result |
| `solver.DefaultVisionModel` | const | `qwen3.6-vision` |

## Human-like Input

The `input` package implements human-like input: Bezier curve mouse movement with jitter, keystroke timing with Gaussian distribution, and touch events for mobile emulation.

| Symbol | Kind | Purpose |
|---|---|---|
| `input.GenerateMousePath(start, end, cfg, rng)` | func | Bezier mouse path |
| `input.MoveMouse(start, end)` | func | simple mouse move |
| `input.BezierCurve(p0, p1, p2, t)` | func | Bezier curve point |
| `input.BezierPath(start, end, steps, rng)` | func | Bezier path with jitter |
| `input.ClickGaussianOffset(sigma, rng)` | func | Gaussian click offset |
| `input.TypingDelays(text, cfg, rng)` | func | keystroke delays |
| `input.TypingRhythm(text, cfg, rng)` | func | keystroke rhythm + keys |
| `input.GaussianJitter(baseMs, sigmaPct, rng)` | func | Gaussian jitter |
| `input.EaseInOutScroll(totalDistance, steps)` | func | easeInOut scroll steps |
| `input.Scroll(distance, steps, hasIO, hasSL)` | func | scroll with infinite-scroll detection |
| `input.DetectInfiniteScroll(hasIO, hasSL)` | func | detect infinite scroll |
| `input.NewMouseClick(point, button)` | func | mouse click event |
| `input.DefaultMouseMoveConfig()` | func | default mouse move config |
| `input.MousePath` / `input.MousePoint` / `input.MouseClick` | struct | mouse types |
| `input.TypingConfig` | struct | typing configuration |
| `input.ScrollResult` / `input.ScrollStrategy` | type | scroll result/strategy |
| `input.TouchEvent` / `input.TouchEventType` | type | touch event types |

## Security Layer

A file version replaced during a read is retried without replacing policy/error state or counting a new configuration failure. Initial unstable input still denies and reports its failure; a stable invalid document retains the last valid decision and remains observable.

`Agent.Health().ActionPolicy` and server health/capability responses report readiness, file/host configuration, reload and failure counts, and bounded `lastReloadError`. Errors expose no file contents or paths. A rejected reload retains a ready last-valid authority while exposing its failure; shutdown marks it unavailable.

`policy.NavigationPolicy` owns the only reusable decision/schema implementation; `bridge` preserves its existing compatibility names, and `security.NavigationPolicy` translates its legacy configuration into that same owner. `policy.NewNavigationPolicy` loads the bounded file policy; `NewStaticNavigationPolicy` validates already admitted JSON bytes without acquiring file IO or a poller. The embedding owner retains ingress and snapshot replacement. Inspect `LastReloadError`, call `StartHotReload` when owned, and call `Stop` on shutdown. Repeated/concurrent starts share one poller; Stop permanently rejects reload/start, joins active work, and subsequent evaluation denies. The canonical `network.Policy` still owns actual egress.

Managed `Agent`/`Session` commands and one-shot `fetch`, `run`, `observe` and `act` bind `BrowserPolicy` before execution. Without a file or host engine, the profile allows `navigate`, `wait`, `screenshot`, `tab_list`, `tab_close`, `observe`, `query` and `extract`; all other categories deny. Navigation can execute site scripts, including renderless inline scripts when requested, and does not promise zero effects. Caller evaluation, `run --script` and JavaScript assertions require `evaluate`; assertions also require `assert`. Session shutdown always releases owned resources. Direct `engine.Page`, raw CDP, profile and other low-level packages are trusted embedding APIs: callers must provide their authorization boundary. An injected `ChromiumActions` executor must supply `ChromiumPolicyContext`; missing native context denies rather than using a caller's URL claim.

`AgentConfig.ActionPolicyPath` selects the file; `PolicyEngine` adds a trusted host constraint and `ConfirmActions` adds confirmation categories. File and host decisions combine as constraints: denial wins, then confirmation, then permission. A configured missing/invalid file fails startup without fallback. The Agent owns one five-second reload poller and joins it on shutdown; one-shot CLI commands own it for their command lifetime. CLI `--action-policy` defaults to `AGENT_BROWSER_ACTION_POLICY`, then `AGENT_BROWSER_POLICY`; `--confirm-actions` defaults to `AGENT_BROWSER_CONFIRM_ACTIONS`. Persistent clients configure policy on `serve`; local overrides are rejected. Publish a complete replacement file atomically. Confirmation is a typed, non-dispatching decision, not an approval service or an automatically resumable action.

Built-in `Session.Assert`/`page.assert` modes `selector_exists`, `title_contains`, `text_contains`, `url_contains` and `status_eq` read typed document state under `query` authority, including the actual page URL and session ownership. They work with the existing reader default and reject an `expr` rather than interpreting caller code. `eval_truthy` requires a nonempty expression and both `assert` and `evaluate` authority; deny and confirmation produce no evaluation effect. Unknown modes and invalid input fail before execution. Session close cancels and joins admitted assertions before disposing their owned page resources.

Chromium interaction checks derive the root and referenced/explicit frame URLs through their owning native renderers. All relevant realms must satisfy one immutable policy snapshot. Selected tabs use the refreshed owned target URL. History authorization binds the native selected entry's URL immediately before dispatching its ID, rather than authorizing only the current document. `cdpops.Navigator.GoBack`/`GoForward` accept at most one optional target-authority callback; trusted low-level callers retain their ownership. Native security origins are used only for inherited blank/srcdoc/blob documents; opaque, foreign or unavailable frames deny. File-policy `assert` and `frame_evaluate` also require the `evaluate` category because their expressions can mutate the page. Action receipts retain `policyDecision` as `allow`, `deny` or `challenge`, alongside conservative effect/attempt evidence. URL rules never replace the separate egress boundary or fine-grained credential/effect authorization.

The JSON schema supports optional `version: 1`, `default: allow|deny|challenge`, URL `rules: [{action, pattern}]`, action-category `allow`, `deny`, `confirm` lists and `domain_allowlist`. Missing defaults deny; unavailable/invalid initial content denies and remains observable. Categories normalize to bounded lowercase identifiers; precedence is deny, confirmation, allow, default. Domain constraints admit exact hosts/subdomains with label boundaries. Whole request authorization evaluates categories and URL/domain constraints from one immutable snapshot. `EvaluateAction` and `EvaluateDomain` provide component information only. URL-only documents cannot grant non-navigation actions. Explicit empty versus omitted lists/rules preserve their meaning across `CurrentPolicy` and JSON round trips.

Documents are limited to 64 KiB, 256 URL rules and 256 entries per category/domain list; patterns to 2,048 bytes and category identifiers to 64 bytes. Unknown/duplicate/case-alias fields, nulls, invalid decisions and trailing JSON are rejected. Requests require bounded HTTP(S) URLs without credentials. Regex patterns are anchored to the entire URL; dots/query markers alone are literal. Literal patterns require the same scheme/hostname and a path prefix; an explicit pattern port must match, otherwise egress policy decides the port. `ParseConfirmActions` validates the reference comma-separated environment format. `PolicyHook` denies missing/disabled engines, malformed decisions, unsupported rewrites and context expiry. External evaluation runs outside the hook lock; caller headers are detached and a concurrent disable overrides an allow response. Browser effect/secret grants remain the embedding owner's responsibility.

Reload checks content hashes rather than timestamp ordering. Each distinct valid candidate publishes once; malformed changes keep the last valid policy and each identical failing candidate counts once. `CurrentPolicy` returns detached rule data. Publish updates using a new complete file and atomic rename: partial in-place writes may correctly register a rejected intermediate candidate. Leaf symlinks and nonregular files are rejected; macOS/Linux also use nonblocking no-follow open to prevent a raced FIFO/symlink from owning shutdown. Other platforms retain descriptor identity checks; kernel/filesystem IO has no cancellation API. `WriteDefaultPolicyFile` exclusively creates a deny-default `0600` file, preserving an existing destination.

The `security` package implements browser security: SSRF prevention (private IP blocking), indirect prompt injection (IDPI) defense, ad/tracker blocking (ABP filter engine with embedded base list and verified EasyList/EasyPrivacy updates), navigation policy (domain allowlist), secret redaction (3-point pipeline), and consent management.

| Symbol | Kind | Purpose |
|---|---|---|
| `security.IsPrivateIP(ip)` | func | private IP check (RFC1918, loopback, link-local) |
| `security.NewAdBlocker()` / `NewAdBlockerWithConfig(whitelist, rollout)` | func | enabled blocker with the embedded base list (in-memory or persistent whitelist); implements `network.RequestFilter` |
| `security.AdBlocker` | struct | `BlockRequest`, `Check`, `AddPattern`, `AddRules` (operator ABP filters, unsupported lines rejected), `Install`/`Advance`/`RecordBreakage` (rollout), `AcceptBreakage`, `Whitelist`, `Enable`/`Disable` |
| `security.ParseAdBlockRules(r)` | func | compiled ABP network-filter set with host/token index and line statistics |
| `security.NewEasyListUpdater(cfg, blocker)` | func | pinned, mirror-verified, size-checked list updates with manifest proof, cache restore and `Run` loop |
| `security.AdBlockFinding` / `ClassifyAdBlock` / `AdBlockProbeScript` | type/func | Layer 2 adblocker-wall decision |
| `security.AdBlockResult` / `security.AdBlockCategory` | type | ad block result/category (`ad`, `tracker`, `social`, `consent`) |
| `security.NewRedactor()` | func | create secret redactor |
| `security.Redactor` | struct | secret redaction pipeline |
| `security.CheckResult` | struct | redaction check result |
| `security.ContainsInvisibleChars(s)` | func | detect invisible characters (IDPI) |
| `security.NewIDPIChecker(cfg)` / `CheckSource(origin, source, content)` | func | IDPI taint+fence: every fence is `<untrusted_web_content id="<random 128-bit>" origin="..." source="..." trust="untrusted">` ... `</untrusted_web_content id="...">`; content cannot close or forge it (unknown boundary id, embedded ASCII fence tags neutralized). Default block/allow lists are empty (operator configuration) |
| `security.IDPIChecker.Label(pageURL, source, content)` / `Fence` / `WithTrustMember` | func | content trust label `{origin, source, trust:"untrusted", threat, taintRefs, policyDowngraded}` (spec L4202). Every page-derived standalone JSON output carries it as top-level `trust` (`fetch --dump structured`, `run --json`, `observe` json and NDJSON records, `_trust` in HAR, `query`, `act`, `control`, `sitemap`, `trace`, `download`); Serve responses and `stream.result` events carry it in the envelope `trust` field (protocol `2.1.0` and later) for `page.open/eval/dump/assert`, `chromium.open/observe/query/act` and `stream`. The label is never removed; an allowlisted origin only sets `policyDowngraded`. Policy-blocked content fails (`content_blocked` on Serve, exit 1 on the CLI) |
| `profile.BrowserLoginExecutor.FillAndSubmit` | method | unlocks and fills a vault secret only when the top-level page host, every form `action`/`formaction` target and a secure scheme (http only on loopback) match the credential domain, checked before unlocking and again immediately before the fill; mismatches fail with `ErrCredentialTargetMismatch` |
| `actions.KindType` / `KindFill` | request | each keystroke verifies the requested element still owns focus before keyDown and before its character; a focus move stops typing with `ErrInputRecipientChanged` so no character reaches another recipient |
| `security.DetectLoginFlow(text)` | func | detect login flow |
| `security.NewNavigationPolicy()` | func | create navigation policy |
| `security.NavigationPolicy` | struct | domain allowlist policy |
| `security.ConsentPage` / `security.ConsentProfile` | struct | consent management types |
| `security.ConsentMode` / `security.ConsentAction` | type | consent mode/action enums |
| `security.AutoConfirm(ctx, page, profile)` | func | auto-confirm consent dialog |
| `security.ResolveConsentMode(raw)` | func | resolve consent mode |
| `security.DefaultConsentAction()` | func | default consent action |

## Profile / Session Management

Support state: **supported for isolated persistent Chromium sessions and owner-checked credential/MFA execution**. The real browser persistence and restart fixture is `profile.TestBrowserRuntimePersistentCookieAndStorageAcrossRestart`; authenticated flows require an injected encrypted `CredentialStore`, a `BrowserLoginExecutor`, an allowlisted purpose/domain policy, and a verified postcondition.

Renderless `Agent.CreateSession` also isolates HTTP cookies and origin storage. Redirects, document cookies, JavaScript fetch/XHR, stylesheet loads and frame loads use the same session partition while retaining shared transport and policy. `HTTPClient.CookieJarForSession(sessionID)` returns the owner jar; the configured default or empty identity uses the single-session default. Admission caps additional cookie partitions at 256, retained storage origins at 64 per session/page and 1,024 per runtime; it rejects overflow without evicting live state. `Agent.CloseSession` stops owned operations/pages before `Engine.ReleaseSession(sessionID) error` clears that owner's retained state. Direct engine callers must stop the owner's work before releasing it.

`network/cookiejar.Jar` is the single HTTP/document cookie owner, adapted from Go 1.27.2's RFC 6265 implementation with its BSD license retained. The bundled public suffix list is the default. HTTP `Cookies`/`SetCookies` and non-HTTP `DocumentCookies(*url.URL) []*http.Cookie` / `SetDocumentCookies(*url.URL, []*http.Cookie)` select/mutate the same store under the same lock, preserving domain/path/expiry/order and legitimate visible-cookie writes. Document reads exclude HttpOnly; document writes cannot create it, replace/delete an existing live HttpOnly identity, or overlay applicable Secure cookies from an insecure origin. Same-name cookies at different paths retain their separate identities. `ContextOpts.CookieJar` must implement both document methods; opaque standard-library jars and typed nil owners fail construction with `js.ErrDocumentCookieAuthority`, retire the failed context and preserve later runtime use. Trusted explicit GetCookie/SetCookie callbacks retain their embedding contract. Source identity includes the cookie owner, tests, license and Go toolchain; this is not a complete browser-cookie or binary-authenticity claim. Non-HTTP boundary reference: https://httpwg.org/specs/rfc6265.html#section-5.3.

Loader contracts carry their real request context: `js.StylesheetLoader(ctx, url) -> ([]byte, error)` and `js.IFrameLoader(ctx, url) -> (*js.IFrameResponse, error)`, where the response includes final URL and body. `ContextOpts.RequestContext` is borrowed during construction, and frame/JS operations inherit the active evaluation/lifetime context. Frame document access compares the final canonical origin and returns null across origins, including redirected frames. Contexts receive the owning `ContextOpts.CookieJar` rather than the parent document's cookie closures.

| Symbol | Kind | Purpose |
|---|---|---|
| `profile.BrowserProfile` | struct | browser profile |
| `profile.ProfileManager` | struct | multi-profile manager |
| `profile.CredentialStore` | struct | encrypted credential store (AES-256-GCM) |
| `profile.SessionManager` | struct | session health + auto-relogin |
| `profile.CookieJar` | struct | cookie persistence + expiry |
| `profile.StorageManager` | struct | localStorage/sessionStorage/IndexedDB |
| `profile.IdentityManager` | struct | per-profile fingerprint identity |
| `profile.GenerateUUID5(namespace, name)` | func | deterministic UUID5 generation |
| `profile.ProxyProfileConfig` | struct | session-level proxy config |
| `profile.ResolvedProxyConfig` | struct | resolved proxy with geo-mode |
| `profile.GeoMode` | type | geo-mode enum (explicit_wins, proxy_locked) |
| `profile.KeychainProvider` | struct | in-memory keychain (test) |
| `profile.SecretProvider` | interface | pluggable keychain abstraction |
| `profile.CredentialLease` | struct | short-lived decrypted credential with wipe-on-close |
| `profile.Authenticator` | struct | policy-bounded credential, MFA and postcondition orchestration |
| `profile.BrowserLoginExecutor` | struct | real CDP form/MFA executor with secret-free evidence |
| `profile.BrowserRuntime.Authenticate` | method | owner-checked authenticated operation on an owned page |

## Scraper Subsystem

The `scraper` package implements the Scrapling-inspired scraping engine: adaptive selectors (SQLite, survives redesigns), AI element finding via Inference Hub, structured data extraction (JSON-LD, OpenGraph, Twitter Cards, Microdata), concurrent scraping engine (static + renderless + browser pools), differential re-scrape with conditional GET + region hashing, HAR streaming, pagination/infinite-scroll detection, anti-scraping recovery (backoff + escalation), and server impact detection.

`AIFinder.Find` supplies actual redacted DOM context (up to 512 elements and 32768 encoded JSON bytes), excluding input values, textarea contents and scripts. Attribute/direct-text fields are bounded to 256 UTF-8 bytes; node, byte or field clipping sets `truncated`. It accepts only a unique CSS match in the current document, even when custom validation is absent or permissive. It uses at most three proposal attempts, validates every cache hit and invalidates stale entries in memory and SQLite. Cache context binds the actual HTTP origin, static URL pattern and structural intent; callers must keep dynamic input/credential values out of those selectors and intent descriptions. The callback's embedding host owns inference privacy/provider admission. This static DOM check does not establish native browser hit/actionability.

Critical selector consumers use `AdaptiveSelectorCache.LookupContext` to distinguish a miss from cancellation or storage failure; compatibility `Get`/`GetContext` retains its miss-on-error shape. Persistent writes publish into L1 only after SQLite succeeds. Lookup/store/invalidation order is serialized so a stale read cannot overwrite a newer write or restore a removed entry. Failed persistence cannot create a successful memory-only result.

`AIFinderStage2.FindStage2` returns an explicitly unverified model proposal and never populates or consumes the verified cache. `FindStage2Verified` additionally requires a current `SelectorVerifier` for uniqueness and required browser state, verifies both cached and proposed results, and reports `verified`. Model confidence alone cannot admit or cache a healed target. Configuration is captured once per operation, attempts stay capped at three, text evidence is capped at 32768 bytes, and a missing privacy router requires local inference. A provider response that contradicts local-only admission is rejected. Pixel coordinates remain proposals until a real browser verifier grounds them. These APIs are selector primitives; they do not claim a complete recorded/replayed workflow.

| Symbol | Kind | Purpose |
|---|---|---|
| `scraper.NewFinder()` | func | create adaptive element finder |
| `scraper.Finder` | struct | adaptive element locator (CSS → XPath → Text → Attribute → Structural) |
| `scraper.Result` | struct | find result with strategy + confidence |
| `scraper.ExtractedPage` | struct | extracted page result |
| `scraper.StructuredRecord` | struct | structured data record |
| `scraper.ExtractJSONLD(doc)` | func | extract JSON-LD |
| `scraper.ExtractOpenGraph(doc)` | func | extract Open Graph |
| `scraper.AdaptiveSelectorCache` | struct | SQLite adaptive selector cache |
| `scraper.DomainRateLimiter` | struct | per-domain rate limiter (token bucket) |
| `scraper.ConcurrentEngine` | struct | concurrent scraping engine |
| `scraper.HARStreamer` | struct | HAR streaming writer |
| `scraper.DiffEngine` | struct | differential re-scrape engine |
| `scraper.PaginationDetector` | struct | pagination + infinite scroll detector |
| `scraper.RecoveryChain` | struct | anti-scraping recovery chain |
| `scraper.ImpactDetector` | struct | server impact detector |
| `scraper.BloomFilter` | struct | URL dedup bloom filter |

## Prompts System

The `prompts` package provides the 9 AgentScope browser agent prompt templates as Go string constants, plus a `TemplateExecutor` that integrates template selection with browser skill execution.

| Symbol | Kind | Purpose |
|---|---|---|
| `prompts.SystemPrompt()` | func | base behavior prompt |
| `prompts.DecomposePrompt()` | func | task decomposition prompt |
| `prompts.DecomposeReflectionPrompt()` | func | decomposition reflection |
| `prompts.ObservePrompt()` | func | chunked observation prompt |
| `prompts.PureReasoningPrompt()` | func | pure reasoning prompt |
| `prompts.FormFillingPrompt()` | func | form filling prompt |
| `prompts.FileDownloadPrompt()` | func | file download prompt |
| `prompts.SummarizePrompt()` | func | task summarization prompt |
| `prompts.GetPrompt(pt)` | func | get prompt by type |
| `prompts.AllTemplates()` | func | list all templates |
| `prompts.RenderTemplate(template, variables)` | func | render template with variables |
| `prompts.EstimateTokens(text)` | func | estimate token count |
| `prompts.ShouldUsePureReasoning(situation, tokens)` | func | pure reasoning decision |
| `prompts.PromptType` | type | prompt type enum |
| `prompts.PromptTemplate` | struct | prompt template descriptor |

## Actions / Auto-Login

The `actions` package implements auto-login: form detection, login flow identification, and post-login verification.

| Symbol | Kind | Purpose |
|---|---|---|
| `actions.DetectLoginForm(ctx, page)` | func | detect login form |
| `actions.LoginDetection` | struct | login detection result |
| `actions.LoginForm` / `actions.LoginField` | struct | login form + field |
| `actions.FormIntent` | struct | form intent for batch fill |
| `actions.FormField` | struct | form field spec |
| `actions.VerifyPostLogin(page)` | func | verify post-login state |
| `actions.LoginPage` / `actions.PostLoginPage` | struct | login page interfaces |

## Platform Capabilities

`platform.Detect` returns a frozen value-owned observation snapshot, with source and reason for each result. `available` requires a successful runtime observation; `unavailable` identifies a rejected or absent backend; `unknown` identifies failed observation, insufficient evidence or permissions. `degraded` is reserved for an actual operation that falls back. Boolean compatibility fields derive from those observations, rather than OS/architecture guesses. `network.NewHTTPClient` calls only `ProbeTCPFastOpen` and `ProbeMmap`, still fresh per client, so engine construction does not pay for the affinity, memory, container and codec probes.

Darwin physical memory comes from `hw.memsize`; Linux uses checked `sysinfo.totalram * mem_unit`. `TotalMemoryMB` is physical host capacity, not a container or session budget. The mmap probe maps, writes, reads and releases one owned anonymous page. Linux affinity verifies the current allowed CPU set and reapplies that same set on one locked thread, without changing its placement. Linux TCP Fast Open requires the kernel client bit and successful socket-option write/readback on an owned unconnected socket; no packet is sent. Container markers and cgroup evidence are read with explicit work bounds; absent markers remain unknown because namespaces can hide membership. The legacy `HasHWJPEGDecode` field reports admitted linked native JPEG capability; VideoToolbox requires hardware-session/property proof, while tagged libjpeg-turbo is explicitly native CPU decoding. Rejected/unlinked native admission is unavailable. Host-support observations do not prove that a browser operation activated those optimizations.

| Symbol | Kind | Purpose |
|---|---|---|
| `platform.Detect()` | func | detect platform capabilities |
| `platform.ProbeTCPFastOpen()` | func | run only the TCP Fast Open probe (per-client HTTP dialer) |
| `platform.ProbeMmap()` | func | run only the anonymous mmap probe (per-client mapped response input) |
| `platform.PlatformCapabilities` | struct | measured host support and explicit linked JPEG backend availability |
| `platform.CapabilityState` | type | available, unavailable, degraded or unknown |
| `platform.ProbeResult` | struct | state, observation source, reason and numeric value |
| `platform.CapabilityEvidence` | struct | frozen observation time and per-feature evidence |
| `platform.AllowedCPUSet(ctx)` | func | current effective Linux CPU identities with typed evidence; bounded dynamic masks up to 65,536 CPU IDs |
| `platform.CompileCPUPlacement(allowed, granted)` | func | value-owned, disjoint browser/worker sets narrowed to actual permitted IDs and an externally granted CPU count |
| `platform.RunCPUAffinity(ctx, cpus, operation)` | func | synchronous joined callback with exact Linux placement/readback and verified restoration; ordinary execution on unsupported builds |

CPU placement is opt-in at the callback boundary. An eight-CPU grant reproduces the four/four browser-worker split; sparse cpusets and smaller grants narrow the sets without inventing CPU identities. The embedding application's existing resource authority supplies the grant and owns its reservation throughout the work. A child started inside the callback inherits its mask through fork/exec, so process placement belongs at startup. The Linux callback runs on an owned locked thread; cancellation waits for real callback completion, and a thread whose restoration cannot be proved exits without returning to the runtime thread pool. These primitives do not install a process-wide scheduler or a second capacity ledger. Productive Chromium/worker composition requires the corresponding reservation owner at each call site.

### Performance Notes

`NewContext` cold path was overhauled in TASKs 042 + 048. Per-Context allocations:

- **WebSocket registry and transport**: lazy-initialized. The 256-slot event channel, conns map, and policy-bound HTTP transport allocate only on first `new WebSocket(...)`. Pages without WS pay zero registry or transport overhead. (-65MB across a 4000-context benchmark before the transport hardening.)
- **Bootstrap registration**: `Runtime.cachedBootstraps` stores the concatenated bootstrap source per snapshot mode. Once warm, subsequent Contexts skip the per-source `registerBootstrap` slice append entirely.
- **Runtime-level template caching**: `Runtime` now caches v8 `FunctionTemplate` and `ObjectTemplate` instances for storage, timers, console, DOM bridge, and mutation observer trampolines. Each cached template is registered once per Isolate; per-Context callbacks dispatch to the right state via `Runtime.contextFor(info.Context())` (a `sync.Map[*v8.Context]*Context` populated by NewContext, drained by Close). Storage uses `Object.SetInternalField(0, handle)` to encode the per-Context `*memStorage` choice on the receiver. Without this, the v8go callback registry (`Isolate.cbs`) grows unboundedly across Contexts and 6+ templates per install function were re-registered on every NewContext.

Combined effect on `BenchmarkFetchRunScripts` (single-page fetch + script run):

| Stage | B/op | allocs/op | ns/op |
|---|---|---|---|
| Baseline | 50774 | 741 | 922000 |
| Lazy WS + bootstrap-skip | 30856 | 732 | 916000 |
| Storage / timer / console / dom-bridge / observer template caching | 26344 | 630 | 960000 |
| Crypto (subtle/AES/complete/asym/extra/pkcs8) + iframe template caching | 23346 | 540 | 909000 |
| `Object.SetMany` v8go-fork API + fetch / WS / extras_v2 / urlHelper / cascade-style template caching + fetch body slab | 22625 | 513 | 940000 |

100 pages with scripts (no pool): 98 ms / 10.4 MB / 176k allocs -> 100 ms / 7.4 MB / 153k allocs (-29% memory, -13% allocs).

Dump buffers (`HTML`, `Text`, `Markdown`, semantic raw text and outbound CDP command encoding) come from one owner, `internal/pool`. Its buffer keeps the backing array across `Reset`, so a pooled buffer saves the growth allocations of the next dump; output leaves only through `String`, which copies, so no result aliases pooled memory. Returning a buffer zeroes every byte it held (page content and CDP commands can carry customer data) and buffers above 4 MiB are dropped instead of retained. Markdown converters keep their buffer under the same bound and wipe rule. `BenchmarkDumpPayloadClasses` (`agent`, 5/50/2000 sections, median of 6 runs, Apple M-series) against the former builder pool whose `Reset` discarded the backing array: HTML -15% to -23% ns/op with 1 allocation per dump, Text -5% to -8%, Markdown -20% with allocations halved; bytes per operation drop 48-82%.

### v8.Context pool

`engine.Config.JSContextPoolSize > 0` (or `js.NewRuntimeWithPool(N)`) enables a pool of pristine v8.Context objects. Close returns only realms never exposed to page execution or native integration; executed realms are destroyed. Reused pristine realms run `__artemis_reset(url)` from `js/context_pool.go`, then rebind the current native options and owners.

Reset clears the pristine bootstrap's global, custom-element, event, history and performance state and rebinds location and actual storage partitions. Deleting properties cannot reset lexical bindings or prove prototype isolation; any page/native exposure therefore permanently prevents reuse. Pooling remains disabled by default.

This works because every native callback installed on globalThis goes through Runtime-cached templates whose callbacks dispatch via `Runtime.contextFor(info.Context())` — so the v8.Context object can stay across pages while the Go-side `*js.Context` (timers, observers, fetch state, document, etc.) is fresh per page.

| Bench | No pool | Pool=8 | Pool=8 warm | Speedup |
|---|---|---|---|---|
| 100 pages with scripts | ~97 ms / 7.4 MB / 153k allocs | **~18.4 ms / 6.5 MB / 100k allocs** | **~18.9 ms / 6.5 MB / 100k allocs** | **5.3x** |

These historical microbenchmark observations predate pristine-only admission and do not measure the current executed-page path. Release comparisons require a reproducible artifact with its environment and corpus.

`JSContextPoolWarm: true` pre-builds all N v8.Contexts at engine.New time. The 100-page bench doesn't show a difference (first-page cold cost amortises across 100 pages) but it eliminates the first-page latency spike for single-request agent flows.

### Lifecycle / leak prevention

Closing a Context tears down all background work owned by it:

- **WebSocket** — the Context admits at most 64 connections and 128 joined transport/send/close workers. Frames are bounded to 64 KiB and the smaller applicable policy request/response byte cap. The existing 256-message queue applies backpressure; one additional terminal event per admitted connection survives saturation/cancellation and drains after earlier messages in deterministic connection order. Writes preserve JS call order; graceful close waits for admitted sends in an owned worker and never holds the isolate for the peer handshake. Context close cancels sockets, joins every worker and releases queued/terminal payloads before realm destruction. The lazily created policy-bound handshake transport closes idle connections with its owner.
- **Async fetch** — the owning Context cancels transport work and joins all admitted workers. Admission bounds unresolved work to the 64-slot completion queue, so each worker publishes exactly once without blocking on V8. Close retires pending native resolvers and decrements ownership before destroying the realm. Injected FetchFunc implementations must honor cancellation and return after cleanup.
- **Iframes** — every sub-Context's Close runs *before* the parent acquires `Runtime.ctxMu`, so iframe teardown doesn't deadlock on the Runtime serialiser.

Verified by `TestContextCloseShutsDownWSGoroutines` and `TestContextCloseCancelsAsyncFetch` (compare `runtime.NumGoroutine()` before / after — must return to baseline within 50ms).

WebSocket ArrayBuffer/view sends preserve the exact binary region, including offsets, NUL and non-UTF-8 bytes; `binaryType='arraybuffer'` receives an actual native ArrayBuffer. Native payload/conversion failures surface through the existing execution owner. Callback receivers/arguments, primitive IDs and temporary payload handles retire under that owner. Subprotocol strings/iterables normalize before transport, with at most 32 unique HTTP tokens of 256 bytes each; the actual peer must select an offered case-sensitive token when any were requested. Missing/case-changed negotiation fails closed rather than emitting open. Remote close carries its actual code, reason and clean/abnormal status. Close validates code, Web IDL clamping and the 123-byte UTF-8 reason bound before changing state. Binary Blob, bufferedAmount, readonly state, URL normalization and complete browser/WebSocket capability proof remain TASK-2795 work. Reference: https://websockets.spec.whatwg.org/.

### Concurrency

Renderless WebCrypto is deliberately limited to `crypto.getRandomValues`, `crypto.randomUUID` and `crypto.subtle.digest` (SHA-1 via collision-detecting sha1cd, SHA-256/384/512). Key import/generation/export, encrypt/decrypt, sign/verify, deriveBits/deriveKey and wrap/unwrap are absent from the binding inventory and classified `unsupported_escalate`; pages that need them escalate to Chromium, whose maintained WebCrypto replaces any Go emulation. No key material is ever stored in a renderless Context and no key-generation subprocess exists.

Digest byte input is bounded to 16 MiB before copying/allocation. ArrayBuffer and views use the exact native byte region, including DataView offsets and non-UTF-8 bytes; shared/detached buffers reject. The array-shaped compatibility path requires finite integral lengths and exact byte values, observes every getter error, retires temporary handles and checks current operation/lifetime cancellation during its bounded loop. Results publish copied native ArrayBuffers through a checked resolver that retires native handles even when publication is interrupted. TextDecoder consumes buffers and exact view byte regions.

`Runtime.ctxMu` is the single cancellable isolate gate for construction, execution, loading, idle pumping and cleanup. Concurrent callers serialize through this owner; queued cancellation prevents later side effects. Active owner cancellation interrupts V8, joins termination callbacks and restores execution only after native frames unwind. Runtime disposal follows joined context/worker retirement.

Pool isolation rejects reuse for every executed or exposed realm, covering non-configurable globals, lexical declarations and prototype mutation without attempting to reconstruct pristine state after hostile code.

### v8go fork additions

- `Object.SetMany(keys []string, vals []interface{})` and `SetManyPrepared(*PreparedKeys, []interface{})` set N (key, value) pairs in a single cgo crossing. Use the prepared form on hot paths so the C-string array is allocated exactly once at package init. Backed by a new `ObjectSetMany` C++ entry point in `object.cc`.
- `Value.ReturnFromCallback()` transfers an owned callback-result handle to the native return trampoline, which releases it after copying the actual V8 return value. The caller must not reuse or release the transferred handle. Borrowed/cached handles must never be transferred. Timer identities use this owner, so repeated timer creation/cancellation does not accumulate internal-isolate handles.
- `Context.NewTypeError(message)` creates an actual TypeError in the owning realm without dispatching through page JavaScript.

Storage callbacks retire their argument, receiver, internal-field and owned result handles after each invocation. The shared primitive-return owner also serves timers. Repeated actual read/write/remove/clear/key/length operations retain neither page-context nor standalone internal-isolate handles; receivers without the Storage internal layout cannot enter native field access.

## Steering Server

`artemis serve --host 127.0.0.1 --port 9333` runs a JSON-over-WebSocket server that drives the canonical `artemis.Agent` from outside the Go process. NOT Chrome DevTools Protocol; custom shape designed for agent embedding.

```
ws://127.0.0.1:9333/
```

### Connection and lifecycle

Every WebSocket handshake requires `Authorization: Bearer <token>`. `--token` or `ARTEMIS_SERVE_TOKEN` supplies the token; when neither is set the CLI generates 256 random bits and prints the token once to stderr without placing it in structured logs. Credential-shaped URL query parameters are rejected. The server API fails closed when no token is configured, compares credentials in constant time, rejects non-loopback binds and DNS-rebind Host headers, and only accepts the compiled loopback/Artemis browser-origin allowlist unless `--origin` supplies an explicit replacement. Universal origin patterns are rejected; there is no origin-verification bypass.

Serve policy decisions and renderless resource snapshots are written to the bounded redacted diagnostics ledger. The authenticated browser protocol never returns raw ledger content; operators use `artemis diagnostics` or the in-process `Agent.Diagnostics()` API.

The first authenticated connection receives `X-Artemis-Client-ID`, an opaque server-signed capability. A reconnect sends the same header to retain access to its sessions and resumable streams. Session owner identity is server-assigned; caller-provided owner fields cannot claim another client. Every session/page/action/list/close/stream/cancel path checks that identity. Token rotation revokes active connections and their sessions.

Rate limits are always active: 30 messages/second with burst 10 per connection plus 120 requests/minute with burst 10 per normalized remote address. Address normalization strips ephemeral ports and unmaps IPv4-mapped IPv6 so reconnecting does not reset the client bucket. Ten consecutive WebSocket violations close the connection with policy-violation status; handshake violations return HTTP 429 with `Retry-After`. Idle client buckets expire after ten minutes.

Requests are envelopes `{id, version?, cmd, params}`. Responses are `{id, version, seq, ok, value?, error?}`. `error` is `{code, message}`.

The server enables lazy Chromium control by default; `--chromium=false` selects renderless-only operation. Chrome is launched only for an admitted Chromium operation, so renderless startup remains usable when Chrome is absent. `--binary` selects an installed executable. Each Agent session owns an isolated CDP browser context. Each Chromium page owns its live observation collector, action runtime and cancellable serialization lease. Shutdown cancels operations, closes pages/contexts and then closes the owned browser process. A transport crash requires explicit restart; no action is automatically replayed.

Library control uses `AgentConfig.Chromium`, `Session.OpenChromiumPage`, `Session.ObserveChromiumPage`, `Session.ActChromiumPage`, `Session.DumpChromiumPage` and `Session.CloseChromiumPage`; their session lifetime owns target cleanup. `serve.Dial(ctx, endpoint, token, clientID)` connects the typed `serve.Client` to a numeric loopback ws/wss endpoint. `Client.ClientID()` returns the reconnect capability and `Client.Call` dispatches typed commands with connection-unique IDs. Calls are serialized with cancellable admission. `Client.Close` closes that connection; `Server.Close` cancels work, revokes its sessions and closes all owned WebSockets. An Agent can outlive its closed server.

`artemis control --server ws://127.0.0.1:9333/ --cmd session.new` returns `{clientId, result:{sessionId,ownerUserRef}}`. Retain the client capability as `ARTEMIS_CLIENT_ID` or `--client-id`, and retain the returned session/page IDs. `chromium.open` creates one page; later `observe`, `act` and `fetch` use `--server`, `--session` and `--page` without a URL, so they never navigate to a newly created document. Local process/policy flags are rejected in persistent mode; configure them on the server. Commands never retry after dispatch when a response is lost.

### Commands

| Command | Params | Returns |
|---|---|---|
| `version` | `{}` | `{protocol, server, capabilities, health}` |
| `session.new` | `{profileId?, class?}` | `{sessionId, ownerUserRef}` |
| `session.list` | `{}` | `{sessions}` for the authenticated client only |
| `session.close` | `{sessionId}` | `{}` |
| `page.open` | `{sessionId, url, runScripts?, asyncFetch?}` | `{pageId, url, status, title, adblock?}` |
| `page.close` | `{sessionId, pageId}` | `{}` |
| `page.eval` | `{sessionId, pageId, expr}` | `{value}` |
| `page.dump` | `{sessionId, pageId, format}` (`html`/`markdown`/`text`/`title`/`links`/`structured`/`semantic`) | `{data}` |
| `page.click_by_text` | `{sessionId, pageId, text}` | `{}` |
| `page.type` | `{sessionId, pageId, selector, text}` | `{}` |
| `page.wait_idle` | `{sessionId, pageId}` | `{}` |
| `page.assert` | `{sessionId, pageId, mode, selector?, substring?, expr?, status?, want?}` | `{pass, got}` |
| `chromium.open` | `{sessionId, url}` | `{pageId, evidence, adblock?}` |
| `chromium.observe` | `{sessionId, pageId, mode?, subtreeRef?}` | bounded live observation evidence |
| `chromium.query` | `{sessionId, pageId, request:{options, baseRevision?}}` | `artemis.query.v1` task-scoped full, unchanged or delta result; unknown fields rejected |
| `chromium.act` | `{sessionId, pageId, snapshotEpoch?, request, leaseToken?}` | `{outcome}` including failed action evidence; `leaseToken` authorizes writes to a leased page |
| `page.extract` | `serve.PageExtractParams` `{sessionId, pageId, schema? , pagination?, json?}` | `scraper.GroundedExtraction` with trust label; exactly one of `schema` or `json` |
| `chromium.visual` | `serve.ChromiumVisualParams` `{sessionId, pageId, options:{annotate?, maxAnnotations?, cropRef?}}` | `artemis.VisualObservation` with trust label |
| `chromium.visual_act` | `serve.ChromiumVisualActParams` `{sessionId, pageId, request:{observationId, kind, x, y, button?, allowPointerFallback?}, leaseToken?}` | `artemis.VisualActionResult` with trust label |
| `chromium.visual_ground` | `serve.ChromiumVisualGroundParams` `{sessionId, pageId, observationId, instruction}` | `artemis.VisualPoint`; `capability_unavailable` without a host grounding model |
| `page.lease` | `serve.PageLeaseParams` `{sessionId, pageId, op: acquire\|release, holder?, ttlMs?, token?}` | `artemis.WriterLease` |
| `page.handoff` | `serve.PageHandoffParams` `{sessionId, pageId, op: begin\|open\|end, ttlMs?, token?}` | `serve.PageHandoffResult` (`ticket` on begin, `inspectorUrl` on open) |
| `chromium.close` | `{sessionId, pageId}` | `{}` |
| `stream` | `{sessionId, url, runScripts?}` or `{streamId, resumeFrom?}` | `{streamId, resumed?, complete?}` |
| `cancel` | `{requestId}` | `{cancelled}` |
| `token.rotate` | `{}` | `{token}` and connection revocation |
| `heartbeat` | `{}` | `{now}` |
| `workflow.record` | `{sessionId, pageId, op, name?, origins?}` | `{recording?, definition?}` with trust label |
| `workflow.run` | `{sessionId, pageId, definition, inputs?, shadow?, runId?}` | `artemis.WorkflowRunResult` with trust label |
| `adblock.whitelist` | `{op: list\|add\|remove, domain?}` | `{domains}`; the user decision for an adblocker wall and the operator maintenance path |
| `capabilities` | `{}` | `{protocol, server, capabilities, health}` |

### Protocol contract

- `page.open` and `chromium.open` accept an optional engine intent: `engine` (`auto`|`artemis`|`playwright`) and `requires` (`browser:<name>` or `capability:<name>`). The request is resolved by the Playwright lane tool router before any page opens; a request the selected engine cannot serve fails `capability_unavailable` with remediation and never opens the page through another engine. `capabilities` additionally returns `playwrightLane`, the lane projection (see [Playwright Peer Lane](#playwright-peer-lane)).
- Protocol version `2.5.0` is returned by `version` and attached to responses/events. Persistent refs include the owning page ID, so a ref cannot be rebound to another page whose internal counter happens to match. Reference actions also require the current `snapshotEpoch`; stale epochs fail before dispatch. Chromium actions require `pageId`; the unscoped injected executor is never a wire target.
- `page.dump` reads either a renderless page or its explicit owned Chromium page. Chromium Markdown/links/structured/semantic extraction reuses the canonical Go converters against the live DOM. HTTP status is observed from the browser, never fabricated as 200; missing status evidence fails explicitly.
- The optional Agent Chromium executors use the production renderless capability authority and full network policy. First escalation transfers the same owner's cookies and retained origin storage before page scripts run. CDP identity prevents implicit return to a lower engine. Custom HTTP methods, bodies and headers that cannot be safely replayed by CDP fail explicitly. Stealth routes require acknowledged configuration.
- `ownerUserRef` is server-assigned from `X-Artemis-Client-ID`; the compatibility request field is accepted only when it already matches that authenticated identity.
- `stream` retains ordered progress and terminal events under the owning client capability for reconnect/resume.
- `token.rotate` returns a new bearer token, invalidates the old token, closes authenticated connections, cancels in-flight work, and closes their sessions.
- `heartbeat` keeps idle connections alive and detects stalled peers.
- CLI flags `--chromium`, `--binary`, `--sandbox`, `--max-cpu-percent`, `--max-memory-bytes`, `--max-profile-bytes`, `--session-timeout`, `--obey-robots`, `--block-private-ips`, `--allowed-port`, `--action-policy`, `--confirm-actions`, `--token`, `--origin`, `--rate`, `--burst`, `--client-rate`, and `--client-burst` control runtime selection and access control. Chromium CPU/RSS/profile/lifetime budgets govern the shared owned process; renderless robots configuration is independent of Chromium network policy.

### Error taxonomy

Response errors use stable codes:

- `bad_request` / `bad_params` — malformed envelope or command parameters.
- `no_session` / `no_page` — stale or unknown ID.
- `ownership_denied` — cross-client session, stream, owner, or cancellation attempt.
- `rate_exceeded` — connection or normalized-client budget exhausted.
- `cancelled` — explicit or lifecycle cancellation.
- `execution_failed` — runtime failure.
- `capability_unavailable` — optional runtime not configured.
- `stale_reference` — session or page already closed.

## Telemetry

Two channels. Both can be wired up by embedders or used directly:

- `telemetry.Tracer` - slog-backed span helper. `Span(ctx, name, attrs...)` returns a Span; call `End()` to emit a single structured log line with duration. Errors via `Span.Error(err)` switch the log level to ERROR. The API mirrors OpenTelemetry shape so a real OTel exporter can replace the slog backend without changing call sites.
- `telemetry.PhoneHome` - opt-out anonymous counters. Atomic counters for fetches/evals/errors. `Snapshot()` returns the current `PhoneHomeContract` (version, GOOS/GOARCH, counts). `Flush()` emits a single structured log entry. **No URLs, no headers, no page content** ever appear in the contract. Disable via `ARTEMIS_DISABLE_TELEMETRY=true`. Real network transmission is intentionally deferred; the slog stub captures the contract today.

## Reference-Bound Visual Grounding

Spec L4178-L4195, L4264. `Session.CaptureVisual(ctx, pageID, artemis.VisualCaptureOptions)` captures the visible viewport (or, with `cropRef`, the visible box of one element) under the page's execution lease and returns an `artemis.VisualObservation`: PNG pixels, the document epoch, the main-frame `observe.ViewportState` (`bridge/observe`: frame, size, scroll, zoom; checked before and after the capture so pixels and geometry share one identity), the crop in viewport CSS pixels, the image-to-CSS `pixelRatio` and up to `maxAnnotations` (default 50, at most 200) `artemis.VisualAnnotation` entries for visible, uncovered interactive references, each with its page-bound ref, role, name, frame and box relative to the image. Boxes come from the frame-clipped border box of the referenced node. With `annotate` the box (2 px, red) and the ref label are drawn into a copy of the pixels in Go (`golang.org/x/image/font/basicfont`); the page DOM is never modified. The newest observation is the page's only current one.

`Session.ActVisual(ctx, pageID, artemis.VisualActionRequest)` clicks or hovers at a point of the current observation (CSS pixels relative to the image). Before any dispatch it refuses with `stale_reference` when the document epoch changed (navigation), the viewport scrolled, resized or zoomed, or the pixels around the point (24 CSS px radius, snapped to whole image pixels, at most 2% changed cells) differ from the observed ones, which also catches scrolled frames, canvas redraws and restyling. The point is hit-tested (`observe.PointHit`, descending into owned out-of-process frames); every annotated box containing the point must contain the hit node, otherwise another element covers it and the action is refused as ambiguous. A hit on an interactive node executes through the canonical reference action (`artemis.VisualPathReference`), with its actionability and postcondition checks. A point without an interactive DOM/AX target (canvas, inaccessible controls) needs `allowPointerFallback`; the explicit fallback (`artemis.VisualPathPointer`) dispatches the click or hover at the bound point through the action runtime and its policy hook as `actions.PointerTarget`, with the hit frame as policy realm. `ActChromiumPage` refuses pointer targets, so unbound coordinates never reach the page. Any dispatch consumes the observation. The `artemis.VisualActionResult` reports the path, ref, viewport point, the action outcome, `semanticChanged` (before/after AX evidence hashes, the authoritative change signal) and an `observe.ImageChange` from `observe.CompareImages`: a fixed grid of at most 128 x 128 sampled cells with a per-channel noise tolerance, the changed-cell ratio and the changed pixel bounds.

`Session.GroundVisual(ctx, pageID, observationID, instruction)` passes the observed image and annotations as an `artemis.VisualGroundingRequest` to the host's `AgentConfig.VisualGrounder` (`artemis.VisualGrounder`) and returns an `artemis.VisualPoint` bound to that observation. Without a configured model it fails `capability_unavailable`; a point outside the image or a confidence outside 0..1 is refused. Artemis performs no model inference itself.

## MCP and WebMCP

Spec L4036-L4038 (dual-mode docking point), L4196-L4210 (content trust). `mcpserve.New(agent, version)` returns an `mcpserve.Server` built on the official Model Context Protocol Go SDK (`github.com/modelcontextprotocol/go-sdk` v1.7.0, protocol versions 2024-11-05 through 2026-07-28 negotiated by the SDK). One MCP connection owns one Artemis session, created on the first tool call and closed when the connection ends; every tool calls the same `Session` operation as the steering server, so policy, leases, epochs and typed errors are those of the browser owner. Tools: `artemis_open` (`url`, `chromium`, optional engine intent `engine` and `requires`, resolved exactly like the steering server), `artemis_browser_capabilities` (capability registry plus Playwright lane projection), `artemis_dump` (`pageId`, `format`), `artemis_observe` (`pageId`, `mode`), `artemis_act` (`pageId`, `snapshotEpoch`, `request` decoded strictly into `actions.Request`, `leaseToken`), `artemis_query` (`request` decoded strictly into the task-scoped query request), `artemis_visual` (labeled observation JSON plus the PNG as image content) and `artemis_visual_act` (see [Reference-Bound Visual Grounding](#reference-bound-visual-grounding)), `artemis_extract` (see [Source-Grounded Extraction](#source-grounded-extraction)), `artemis_close`, `artemis_webmcp_tools` and `artemis_webmcp_call`. Successful results are `{trust, value}` JSON (text content and structured content) with the page's `security.ContentTrust` label; failures are tool results with `isError` and text starting with the stable task error code (`stale_reference: ...`). Each open Chromium page is listed as the resource `artemis://pages/{pageId}/observation` (also a resource template) whose read returns a fresh labeled observation. A cancelled MCP request cancels the tool's context and with it the browser operation. Messages longer than `mcpserve.MaxMessageBytes` (4 MiB) end the connection.

`mcp` serves this on stdin/stdout; logs go to stderr. `mcp` flags: `--chromium` (default true), `--webmcp` (launch Chromium with `--enable-experimental-web-platform-features` so pages can register WebMCP tools; requires `--chromium`), `--binary`, `--sandbox`, `--block-private-ips`, `--allowed-port`, `--obey-robots`, `--action-policy` and `--confirm-actions` with the same meaning as for `serve`.

WebMCP uses Chrome's experimental `WebMCP` CDP domain, verified against the `/json/protocol` schema of Chrome 155 (`WebMCP.enable`, `disable`, `invokeTool`, `cancelInvocation`; events `toolsAdded`, `toolsRemoved`, `toolInvoked`, `toolResponded`). `Session.WebMCPTools(ctx, pageID)` enables the domain in the page session and every attached frame session, collects the `toolsAdded` burst and returns an `artemis.WebMCPCatalog`: `artemis.WebMCPTool` entries with name (1-128 of `[A-Za-z0-9_.-]`), description (untrusted website text, at most 4 KiB), input schema (at most 64 KiB, an object schema compiled with `santhosh-tekuri/jsonschema` with every external `$ref` refused), `artemis.WebMCPAnnotations`, frame, frame origin, backend node and policy action, plus rejected tools with reasons and a revision hash over URL, tools and rejections. A browser without the domain fails `capability_unavailable`; a browser without page support (no experimental flag) reports an empty catalog. In Chrome 155 only declarative `<form toolname tooldescription>` tools of the top-level frame are reported; the browser itself drops tools without a description, duplicate names and malformed names, and Artemis rejects the same cases independently. Annotations are hints only: `consequential` escalates the policy action from `webmcp_invoke` to `webmcp_consequential`, `readOnly` never relaxes anything. `Session.InvokeWebMCPTool(ctx, pageID, artemis.WebMCPInvocation)` requires the page's current revision, rediscovers the catalog and refuses drift as `stale_reference`, requires the tool in the named frame, validates the JSON object input (at most 64 KiB) against the compiled schema, authorizes the policy action for the tool origin through the agent browser policy and the writer lease, invokes the tool in its frame's session and waits for `toolResponded` up to `timeout` (default 30 s, at most 5 min). Expiry or cancellation sends `WebMCP.cancelInvocation`. The `artemis.WebMCPResult` carries status, output (at most 1 MiB, untrusted) and error text with a `webmcp` trust label for the tool origin.

## Source-Grounded Extraction

Spec L4029 (`SourceSnapshot -> ExtractedPage`, Structured-Extract Declarative Schema), L4375-L4448. `scraper.ExtractGroundedHTML(document, schema, source)` runs the existing validated declarative `StructuredSchema` without inference and returns a `scraper.GroundedExtraction`: the value, one `scraper.FieldEvidence` per schema field (path such as `$.price` or `$[2].name`, `scraper.FieldStatus`, value, CSS selector, `tag:nth-of-type` DOM path of the source node, bounded 512-byte excerpt, match count, conflict candidates, reason, source index), `scraper.FieldCounts` and the `scraper.ExtractionSource` list (URL, kind `dom` or `json_response`, capture time, SHA-256 content hash, byte size, truncation flag and the `security.ContentTrust` label of the page). Statuses stay distinct and never become values: `found`; `missing` (selector matched nothing); `conflict` (several distinct values, or an object selector matching several containers; up to 8 candidates are listed); `truncated` (absent from a truncated source, where absence is no evidence); `invalid` (coercion failed); `withheld` (credential-like JSON key). Agreeing duplicates are one `found` value with their match count; `join` concatenates explicitly. An extraction is `complete` only when every required field is found and no field is in conflict, invalid, truncated or withheld.

`scraper.ExtractGroundedJSON(body, scraper.JSONExtractSchema, source)` extracts `scraper.JSONExtractField` entries (RFC 6901 pointer, kind `string|number|boolean|url|any`, required) from the document or from each element of the `items` array. A pointer through a key matching password, secret, token, auth, cookie, session, api key, credential, bearer, csrf or xsrf is `withheld`: neither its value nor an excerpt is returned. Numeric strings coerce to numbers, URLs resolve against the source URL; a truncated body marks every field `truncated`. `scraper.MergeGroundedPages` concatenates page results, re-indexes field sources and is complete only when the caller proves the walk exhausted the pages.

Session pages: `Session.ExtractGrounded(ctx, pageID, schema)` reads the renderless or Chromium page document through the existing dump owner (at most 8 MiB, beyond that truncated) and labels it with the page trust. `Session.ExtractGroundedPages(ctx, pageID, schema, artemis.ExtractPagination{NextSelector, MaxPages})` follows the `href` of the next link (default 10, at most 50 pages; Chromium navigates the same page under the action policy, renderless opens each further page and closes it) and stops with `no_next_page` or `cycle_closed` (complete when all pages are) or `max_pages` or `next_page_leaves_origin` (never complete). `Session.ExtractResponseJSON(ctx, pageID, artemis.JSONResponseRequest{URL, Schema})` resolves the URL against a Chromium page, refuses other origins with `policy_denied`, authorizes the `extract` action through the agent browser policy and fetches inside a fresh isolated world of the page with `credentials: same-origin` and `redirect: error`; only status, content type and the body (at most 4 MiB, beyond that truncated) return to Go, so cookies and tokens stay in the browser. Non-JSON content types fail `invalid_input`; renderless pages fail `capability_unavailable`. Serve exposes this as `page.extract` and MCP as `artemis_extract`.

## Script Containment and V8 Maintenance

Spec L3974, L4533, L4196-L4210. Direct mode runs page JavaScript in the embedded V8 of the Artemis process. An embedder that holds host secrets sets `AgentConfig.RequireScriptContainment` (or `engine.Config.RequireScriptContainment` when it builds the engine itself): `engine.Engine.Fetch` then refuses `RunScripts`, `RunInlineScripts` and `AsyncFetch` with `engine.ErrScriptContainmentRequired` before any network request, which tasks report as `capability_unavailable`; script-free fetches are unaffected. In the Agent the hybrid router registers no `renderless_js` executor and runs `static_fetch` script-free, so script-requiring routes fall back deterministically to `chromium_cdp`: the owned Chromium child under its OS sandbox (`--sandbox required`), the agent policy, resource budget and reaping of `process`. Without Chromium such routes fail `capability_unavailable`; `Session.OpenPage` with scripts fails the same way. A page that spins forever is bounded by the task timeout in Chromium and the session keeps working afterwards.

Routing of auth/profile requirements (a session that already owns a Chromium context, a login wall, WebAuthn/MFA) selects `chromium_cdp`; `stealth` is reserved for bot, WAF and CAPTCHA signals and stays the upward fallback.

The vendored engine is tommie/v8go v0.39.0 with V8 15.5.35.25 (`third_party/v8go/deps/include/v8-version.h`, upstream hash `third_party/v8go/deps/v8_hash`, patches in `third_party/v8go/ARTEMIS_PATCHES.md`). tommie/v8go is the maintained fork of rogchap/v8go (unmaintained since 2023-04-10) with daily automated V8 upgrades. Upgrading means re-vendoring a newer tag, re-applying the patch list, rebuilding the darwin binding archives and regenerating `js/snapshot.bin`; `TestBindingArchiveMatchesSources` and the snapshot manifest fail closed on any mismatch.

## Reusable Verified Workflows

Spec L4551-L4563 (Workflow Capture Recorder and replay). `Session.StartWorkflowRecording(pageID, operator, name, observe.WorkflowCaptureConfig)` attaches the existing `observe.WorkflowCaptureRecorder` to a session Chromium page; every `ActChromiumPage` action then appends an owned record: redacted tool arguments (typed and filled values become `[redacted]`, password fields are flagged sensitive), the result class with only the postcondition type and verdict, before/after AX snapshots without field values (at most 200 nodes), stable selectors (`role:name` with confidence 1 when unique, 0.4 when ambiguous, plus `[id="..."]`) and, below the confidence threshold, real PNG fallback pixels from `Page.captureScreenshot`. `Session.StopWorkflowRecording` finalizes it. Recording failures are appended to the action outcome error, never hidden.

`artemis.WorkflowFromRecording(recording, origins)` turns successful recorded actions into an `artemis.WorkflowDefinition` (version 1): `artemis.WorkflowStep` entries with an `artemis.WorkflowTarget` (accessible role and name), every recorded input as an `artemis.WorkflowParam` slot. `artemis.ValidateWorkflow(definition, inputs)` enforces the schema, at most 64 steps, origins, declared params, `artemis.WorkflowInput` values for ordinary slots and `artemis.CredentialRef` for sensitive slots (no literal secrets, no literal password-field values) and activation rules: destructive steps (click, key, select, check, uncheck) need a `compensation` note; an `artemis.WorkflowOutcome` (`url_contains`, `text_present`, `element_present`) declares the required postcondition.

`Session.RunWorkflow(ctx, pageID, definition, inputs, artemis.WorkflowRunOptions)` validates, then authorizes every step kind against every origin through the agent browser policy before any effect (a `confirm` or `deny` decision stops the run), and returns an `artemis.WorkflowRunResult` with one `artemis.WorkflowStepResult` per step (`planned`, `deferred`, `verified`, `failed`, `not_attempted`; ref, effect, failure, error and the `security.ContentTrust` label of the page the step ran on). Shadow mode resolves targets on the current page without dispatching and defers steps after the first navigation or destructive step. Execution observes the page before every step, requires the page origin to stay inside the definition origins, resolves exactly one matching node (zero or several matches are selector drift and fail closed), dispatches through the canonical action runtime once and verifies the declared outcome; the first failure stops the run with all earlier receipts. Sensitive inputs are leased from `WorkflowRunOptions.Credentials` (`profile.CredentialStore`) only after `profile.VerifyCredentialTarget` (the login rule: top-level page on the credential host, secure scheme or loopback http, every form submission target same-origin) accepts the field; values never appear in results or recordings.

Serve exposes `workflow.record` (`serve.WorkflowRecordParams`: `op` start|stop, optional `origins` to return a definition in `serve.WorkflowRecordResult`) and `workflow.run` (`serve.WorkflowRunParams`); sensitive inputs are refused over the wire because they need a host credential store. A `runId` (1-64 of `[A-Za-z0-9_-]`) journals the run under `serve.Opts.WorkflowJournalDir`, in a per-client directory, so a client can only resume its own runs; without a configured directory journaled runs fail `capability_unavailable`.

### Durable resume and handoff

Spec L4559-L4561, L4580-L4604. A non-shadow `RunWorkflow` holds the page's exclusive writer lease (`Session.AcquireWriterLease`, holder `workflow:<name>`) for its duration and releases it at the end. With `WorkflowRunOptions.Journal` every step appends a fsynced intent before dispatch and a receipt (`verified`, `failed`, `uncertain` plus the URL the page reached) after it to an append-only JSONL journal whose first record pins the SHA-256 of the definition. A dispatched destructive step that fails verification is journaled `uncertain`. Rerunning with an existing journal resumes: verified steps are kept and never re-executed; a step journaled `failed` had no verified effect and is retried; an interrupted step (intent without receipt) is checked against its declared outcome on the current page and journaled `verified` when it holds, otherwise a non-destructive step is re-executed and a destructive one becomes `uncertain`; an `uncertain` step (status `artemis.WorkflowStepUncertain`) stops the run without dispatch until an operator resolves it. Before continuing, the page returns to the last verified URL when it is inside the workflow origins. A torn final line is dropped and rewritten; any other malformed line, a step outside the definition or a different definition hash refuses resume. Dispatched outcomes are polled for up to 5 s so navigations and asynchronous updates can settle.

`Session.AcquireWriterLease(pageID, holder, ttl)` returns an `artemis.WriterLease` (ttl up to 30 minutes); a live lease of another holder is refused (`ownership_denied` on Serve), an expired lease no longer blocks. While a lease is live, `ActChromiumPage` requires its token through `artemis.WithWriterLease(ctx, token)`; observation, query and dump remain open to readers. `Session.ReleaseWriterLease` accepts only the holding token. `Session.BeginHandoff(pageID, ttl)` hands the page to a human: the human receives the writer lease and a one-time open token (an `artemis.HandoffTicket`); `Session.OpenHandoff(openToken)` consumes the token within one minute and returns the local DevTools inspector URL of exactly that page target. `Session.EndHandoff(pageID, leaseToken)` returns the page to automation; the next reference action fails `stale_reference` until the page is observed again, so the agent never acts on refs that predate the human's changes.

## Ad and Tracker Blocking

Spec L4206-L4210. One `security.AdBlocker` implements `network.RequestFilter`; `network.Policy` consults it for subresource, subframe, worker and WebSocket requests and for redirects of non-document resources, before DNS resolution. Top-level navigation is never filtered. Blocked requests fail with `network.ErrAdBlocked`: Chromium resolves them as `BlockedByClient`, renderless stylesheets contribute no rules, frames stay empty, scripts are skipped and `fetch()` rejects. Each request carries an `network.AdBlockScope` (site host, CDP resource type, bypass flag, per-page counter) set by `bridge.Page` on Fetch interception and by the renderless loaders.

Layer 1 matches against the embedded base list (`security/adblock_base.txt`: PinchTab ad, tracker, social and consent domains plus the network builtin list, all `$third-party`), operator rules (`AddRules`/`AddPattern`) and the active list version. The ABP engine supports `||`, `|`, `*`, `^`, `@@`, regex filters and `$third-party`/`$first-party`, `$domain`/`$from`, resource types, `$match-case` and `$important`; element hiding and response-rewriting filters (`$csp`, `$redirect`, `$removeparam`, ...) are counted as unsupported and skipped. PinchTab's generic path patterns (`*pixel.gif*`, `*/tr?*`, `*/collect?*`) are left to EasyList's precise variants to avoid first-party false positives. Third party compares registrable domains (public suffix list).

`security.EasyListUpdater` downloads EasyList and EasyPrivacy over HTTPS pinned to the issuing root CAs (Google Trust Services R1-R4, GlobalSign Root CA, ISRG Root X1/X2/YR; redirects must stay HTTPS), requires the independent mirror (easylist-downloads.adblockplus.org) to yield the same SHA-256 over the filter lines, refuses lists larger than twice the previous version or 16 MiB, stores at most three versions with a `manifest.json` (rule statistics, bytes, gzip bytes and checksums per feed as proof per shipped version), restores the newest valid cached version at start (tampered files are rejected), retries failed updates hourly and keeps the cached version. A new version is a rollout candidate for 50% of sessions (stable session hash) for one hour and is promoted only without more than five breakages; more breakages roll the candidate (or a recently promoted version) back and the version is never reinstalled.

Layer 2 runs when blocking dropped at least one request of a page. `security.AdBlockProbeScript` reads page text, CSS classes/ids and a `position:fixed`, `z-index>=9999` layer (computed style in Chromium, inline style in renderless). Case A (notice text plus notice class or fixed layer) yields `AdBlockFinding{decision: user_decision_required}`; the user decides through `adblock.whitelist`/`Agent.AdBlockWhitelist` and reloads. Case B (more than five blocked requests with console errors/uncaught exceptions or less than 200 characters of main text) verifies the page without blocking (renderless: a repeated GET with `FetchOpts.AdBlockBypass`; Chromium: a sibling target with `SetAdBlockBypass`); a load with restored or at least doubled main text whitelists the site, records the breakage and replaces (renderless) or reloads and re-observes (Chromium) the page, otherwise blocking is kept. Findings are exposed as `engine.Page.AdBlockFinding`, `Session.AdBlockFinding` and the `adblock` field of `page.open`/`chromium.open`. The whitelist (`security.AdBlockerDetection`) is persistent, atomically written, subdomain-aware, never expires and rejects corrupt files at open.

Layer 3 is the default-on effect, measured by `TestAdBlockLayer3ReducesTrafficAndSnapshotNodes` on a page with six ad slots in real Chromium: ad requests 18 -> 0, snapshot nodes 87 -> 15, network events 20 -> 8. Page-speed claims beyond these measured counts are not made.

## Performance Optimizations

Hot-path profiling and package benchmarks identified these allocation and contention improvements:

### security/adblock_rules.go — indexed ABP matching

`||host^` filters are indexed by host and walked by request host suffix; other filters are indexed by their longest token that is a complete URL token, so a request checks only candidate rules. Measured on the 16 Oct 2026 EasyList + EasyPrivacy (139,197 lines, 109,312 blocking, 1,391 exception, 24,719 cosmetic, 3,775 unsupported filters): 47 ms compile, 33 unindexed filters, 2.5-19 µs per request match on an M-series Mac.

### network/easylist.go — IsAdTrackerDomain label-walk

Replaced linear scan over 48 patterns (with per-call `strings.ToLower` on each pattern) with a pre-built `map[string]struct{}` pattern set + label-walk (check domain, drop leftmost label, repeat). The miss case (production hot path — most domains are NOT ad/tracker) went from 1809ns to 57ns (31x faster). `FilterAdTrackerDomains` on a 100-domain batch went from 94969ns to 5861ns (16x faster).

### js/context_pool.go + js/helpers.go — buffer pre-allocation

`jsonStringLiteral` and `jsStringLit` now pre-allocate their output buffers to `len(s)+2` (the minimum output: 2 quotes + input unchanged), eliminating reallocation growth. `jsonStringLiteral`: 282ns→232ns (-18%), 3→2 allocs. `jsStringLit`: 228ns→167ns (-27%), 4→1 allocs.

### serve/streaming.go — Broadcast fast paths

The `Broadcast` method now fast-paths the common cases: 0 subscribers (skip slice alloc + timestamp, still count broadcast) and 1 subscriber (direct call, no goroutine, no WaitGroup). 0-subscriber: 114ns→43ns (-62%). 1-subscriber: 714ns→118ns (-83%). 2+ subscribers keep the goroutine fan-out.

### webapi/node.go + mutation.go — Attr/Tag fast paths

`Tag()` now checks `DataAtom != 0` (known tag) and returns `Data` directly (already lowercase from atom string), skipping `strings.ToLower` for the common case. `Attr()` fast-paths exact key match (`a.Key == name`) on the first loop (parser lowercases keys), with `EqualFold` fallback on a second loop for user-injected attributes. `GetElementById` and `GetElementsByClassName` inline the attribute lookup to avoid the method call overhead. `GetElementById`: 7003ns→6279ns (-10%). `GetElementsByClassName`: 15255ns→14296ns (-6%).

### Benchmark suite

The benchmark harness remains development tooling. A supported release comparison requires the reproducible artifact, environment, corpus, statistics and regression thresholds.

`go run ./cmd/benchmark` and `artemis benchmark` share one validated option parser. Both default to JSON stdout and expose `--format=json|summary`, `--iterations=1..1000`, `--timeout` (default `5m`, maximum `24h`), `--output`, `--benchmark-tag`, `--skip-competitor`, `--require-head-to-head`, `--competitor-dir`, `--download-url`, `--competitor-sha256`, `--cpu-profile`, `--mem-profile`, and repeatable `--regression-budget=metric:absolute_delta:relative_ratio`. Head-to-head requires `--skip-competitor=false`. Downloads require a SHA256 digest and HTTPS; explicit loopback HTTP is permitted for local transfer tests. A budget requires a competitor baseline and a finite positive absolute or relative bound.

Use a fresh output directory, for example `go run ./cmd/benchmark --output benchmark/results/my-run --iterations 3`. Existing scorecards and profiles are rejected before execution. Profile paths must be distinct, remain inside the selected output directory, and cannot traverse symlinks within it. Local renderless cold/warm workloads are executable; Chromium, hybrid and network workloads are rejected explicitly rather than mislabeled. Default workloads use the configured iteration count. Wall/CPU measurements include fetch, script execution and extraction; RSS is the process lifetime peak, with Darwin byte and Linux KiB conversion, rather than a per-page allocation measurement.

Scorecard and scenario matrix version `2.0.0` retain every measured sample, explicit unmeasured warm priming, complete workload dimensions, iteration counts, command controls and semantic evidence. Both engines use the same DOM-backed title, link and paragraph assertions plus canonical text/Markdown digests. The `fetch-extract-close-v1` sample policy includes page creation, navigation, extraction and page-context cleanup; engine/process bootstrap is excluded for both engines. Cold samples reset the Artemis engine and restart the competitor process; warm workloads prime once per engine. A failed sample invalidates its workload even if the median succeeds. Head-to-head claims require at least five samples per engine/workload, pinned competitor provenance, matching extraction evidence and complete pairing. Unsupported competitor CPU, allocation and RSS measurements cannot serve as zero baselines. Relative budgets against a nonzero change from a zero baseline fail without an absolute bound; throughput regression uses the opposite direction to latency regression. Failed budgets fail the command.

Competitor acquisition verifies both cached and downloaded executables, bounds transfers to 256 MiB by default, limits redirects and deadlines, and publishes through an atomic link that cannot overwrite existing bytes. The adapter uses its documented `serve --host 127.0.0.1 --port PORT` CDP route and the existing bounded CDP transport. Reports recompute comparison eligibility from raw samples; each artifact is published exclusively and atomically. Profiled runs retain their observations but cannot substantiate comparative performance claims. The unchanged `scr-003` fixture now observes its script-defined `Loaded` title through the shared DOM-title and Engine loading-event owners. This establishes that fixture's actual outcome, not full browser conformance or a comparative performance win.

The manifest records the executed Artemis SHA256, VCS revision and source-tree state (explicitly unavailable when the build lacks VCS metadata), fixture hashes and expectations, Go/platform/CPU-count environment, GOMAXPROCS, context-pool size, uncontrolled OS caches, priming policy and all command controls. Download URLs retain a query-free origin plus their SHA256 fingerprint. Claims are `invalid`, `observed` or `benchmarked`, scoped to the recorded workloads, with proof references and a seven-day expiry; stale, profiled or incomplete evidence cannot produce comparative wins. Actual warm priming and every measured sample must match their declared identities and semantic expectations.

`manifest.json` is published last after owned competitor cleanup and profile flush. It hashes every scorecard, report and selected profile using confined relative paths. `benchmark.ReadRawArtifacts` requires this complete cohort and rejects altered, missing, duplicated or escaping entries. Competitor version/server subprocesses reuse the process package's owned process-group controls, bound diagnostic output and join their waits; unexpected death remains an error, while successful owner-context cancellation is joined without a false interrupt failure. Binary identity is checked before and after execution. Live interoperability and cleanup proof is reproducible with `ARTEMIS_BENCHMARK_BINARY=/absolute/path/competitor ARTEMIS_BENCHMARK_SHA256=PIN GOTOOLCHAIN=go1.27.2 go test -tags artemis_live_competitor ./benchmark -run '^TestPinnedCompetitorLive'`; the opt-in suite fails when its explicitly required binary/digest is absent and creates no executable copy.

`benchmark.ReadArtifactBundle(ctx, outputDir)` captures the exact manifest and every sealed member in receipt order, including opaque CPU/heap profile bytes. It shares one reader with `ReadRawArtifacts`, which continues capturing only JSON/Markdown while checking the complete cohort. Admission requires the canonical version-1.0.0 manifest, the three standard outputs and at most two additional members, with a 64 KiB manifest, 16 MiB per file and 64 MiB aggregate ceiling checked before member capture. Unknown/duplicate fields, mismatched bytes, changing file/root identities and symlinked roots fail without a partial returned bundle. On macOS/Linux, descriptor-relative leaf opens reject symlinks and cannot block on a substituted FIFO; other platforms explicitly reject secure file admission. Cancellation is cooperative during regular-file I/O. The seal and opaque profile bytes establish integrity only; this port does not authenticate the producer, execute/decode profiles or itself apply canonical retention. Profile generation is checked at sealing, not bounded during profiler production.

`benchmark.ValidateArtifactBundle(bundle)` admits a complete inert bundle restored by an embedding authority without creating filesystem copies. It reuses the same strict manifest decoder and bounds, requires exact receipt equality, member count/order/digests and complete actual content hashes. It grants no execution or producer authentication. Stored scorecard consumers must independently recheck current source/output permissions and the meaning of any performance or completion claim.
