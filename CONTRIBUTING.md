# Contributing to Artemis

Thank you for your interest in contributing to Artemis. This document covers
the practical workflow, code style, testing, and review expectations.

## Quick Start

```bash
git clone <repo-url>
cd artemis
go build ./...
go test ./...
```

A C toolchain is required because Artemis ships a vendored V8 fork under
`third_party/v8go/` (see `third_party/v8go/PROVENANCE.md`).

## Development Workflow

1. Open an issue describing the change before starting significant work.
2. Fork the repo and create a feature branch from `main`.
3. Make surgical edits to existing subsystems. Do not create parallel or
   shadow systems.
4. Add or update tests for every changed behavior in the same change.
5. Run the local gates (below) before pushing.
6. Open a pull request with a clear description linked to the issue.

## Local Gates

```bash
go build ./...
go vet ./...
go test -race -count=1 ./...
go test -bench=. -benchmem -run='^$' -benchtime=1x ./...
```

For touched packages, run `go test -race -count=1 <package>` first.

## Code Style

- Follow effective Go and the existing local conventions.
- No stubs, placeholders, TODO bodies, or always-green tests.
- Extend existing modules; do not create v2 files or parallel systems.
- Comments only where they add information not obvious from the code.
- No emojis in code or docs.

## Tests

- Co-located `*_test.go` in the same package as the code under test.
- Tests must verify real behavior and be capable of failing.
- Cover happy path, denial/error, edge, restart/lifecycle, and degraded
  paths where relevant.
- Race tests (`-race`) for any concurrency, state, or persistence code.

## V8 / v8go Fork

The vendored V8 fork under `third_party/v8go/` carries Artemis-specific
patches documented in `third_party/v8go/ARTEMIS_PATCHES.md`. Changes to the
fork must:

1. Update `ARTEMIS_PATCHES.md` with the new patch description.
2. Update `third_party/v8go/PROVENANCE.md` if the upstream version or binary
   provenance changes.
3. Keep the BSD-style upstream license attribution intact.

## Security Issues

Do NOT open a public issue for security vulnerabilities. See `SECURITY.md`
for the private reporting process.

## License

By contributing, you agree that your contributions are licensed under the
project MIT license (see `LICENSE`).
