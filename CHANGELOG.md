# Changelog

All notable changes to Artemis are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html) once
the first public tag is cut.

## [Unreleased]

### Added
- Chrome-coherent renderless identity: default outbound `User-Agent`,
  `Sec-CH-UA*`/`Sec-Fetch-*`/`Accept*` headers (`network.HeaderGenerator`),
  and in-page `navigator.*` values all derive from one host-matched Chrome
  identity. `engine.DefaultUserAgent` remains the explicit honest-bot option.
- Absorbed uTLS (`third_party/utls`, v1.8.2): HTTPS on the renderless path
  uses a real Chrome ClientHello (`HelloChrome_Auto`) for JA3/JA4 parity,
  with per-host ALPN probing (h2/h1.1 routing), TLS session resumption, and
  HTTP CONNECT proxy tunneling. Certificate verification stays on by default.
- Stealth activation via `ARTEMIS_STEALTH=stealth|paranoid` with
  `ARTEMIS_STEALTH_PURPOSE`/`ARTEMIS_STEALTH_LEGAL_BASIS` acknowledgement —
  fail-closed without it. CDP-native identity overrides (User-Agent +
  Client Hints, locale, timezone, Accept-Language) applied before first
  navigation.
- Renderless browser-surface parity: `navigator.userAgentData` derived from
  the wire UA, five built-in PDF plugins + MIME types, `screen`,
  `window.chrome`, `Notification`, `speechSynthesis`, `performance.memory`,
  guarded hardware interfaces (`mediaDevices`, `serviceWorker`, `wakeLock`,
  `hid`, `usb`, `serial`, `bluetooth`, `clipboard`, `credentials`, `locks`,
  `storage`, `presentation`, `keyboard`, `virtualKeyboard`, `launchQueue`,
  `managed`).
- Human-cadence input on the Chromium path: per-key events with jitter,
  click hold durations, eased multi-point drags.
- `network` robots cache TTL/bounds and bounded domain rate limiters.

### Changed
- Chrome version identity defaults to 152; `HeadlessChrome/` token is
  normalized; `--disable-gpu` no longer forced (real ANGLE/Metal WebGL
  renderer instead of SwiftShader).
- `Notification.permission` reports `default`; `permissions.query` returns
  `prompt` for notifications/geolocation/camera/microphone (headless
  defaults reported `denied`).
- URL resolution fast paths in `agent.Markdown`/`agent.Links` and a
  per-isolate base-URL memo in `new URL()` — removes the dominant
  `url.Parse`+`ResolveReference` allocation hot spot.

### Fixed
- Use-after-pool aliasing in `agent` text/markdown extraction, lock-held
  sleeps in the scraper rate limiter, O(n²) snapshot bounding in `observe`,
  nondeterministic diff ordering, permanently-ticking fetcher timers, and
  renderer-crash visibility via `Inspector.targetCrashed`.

### Added (previous)
- V8 fork provenance document (`third_party/v8go/PROVENANCE.md`) recording
  upstream version, patches, binary dependencies, and reproduction steps.
- `SECURITY.md` with supported versions, vulnerability reporting, threat
  model summary, and hardening commitments.
- `CONTRIBUTING.md` with development workflow, local gates, code style, and
  V8 fork contribution rules.
- Cross-platform CI matrix (Linux + macOS Apple Silicon) with build, vet,
  race, bench-smoke, and Chromium fixture integration jobs.
- Reproducible performance and competitor benchmark harness
  (`benchmark/`, `cmd/benchmark`) with scorecards, regression budgets, and
  pprof profiling.

### Changed
- `js/context_pool.go` pre-warms V8 contexts for warm workloads, reducing
  average per-page wall time by ~37% in the measured benchmark.

### Removed
- Committed build artifacts (`*.test`, the `artemis` binary) are now
  gitignored and no longer tracked in the tree.
