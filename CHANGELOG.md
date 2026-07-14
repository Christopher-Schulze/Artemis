# Changelog

All notable changes to Artemis are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html) once
the first public tag is cut.

## [Unreleased]

### Added
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
