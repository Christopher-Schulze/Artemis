# v8go Provenance

This directory contains a vendored, patched copy of `github.com/tommie/v8go` used by Artemis.

## Upstream

- **Package**: `github.com/tommie/v8go`
- **Version**: v0.39.0 (V8 15.5.35.25)
- **Upstream source**: https://github.com/tommie/v8go
- **Lineage**: tommie/v8go is the maintained fork of `rogchap.com/v8go` v0.9.0 (https://github.com/rogchap/v8go), with automated V8 upgrades
- **License**: BSD-style (see `LICENSE`)
- **Go module**: `module github.com/tommie/v8go`, consumed through `replace github.com/tommie/v8go => ./third_party/v8go`

## Artemis patches

The changes from upstream are documented in `ARTEMIS_PATCHES.md`: the process-wide startup snapshot with its snapshot creator, owned execution cancellation, exception-safe property writes and promise settlement, bounded binary transfer and checked strings, native Map retirement, owned callback returns, null-prototype objects, promise rejection tracking, and the prebuilt darwin binding archives.

## Binary dependencies

V8 itself is not vendored. The static V8 and Chromium libc++ libraries come from the upstream modules `github.com/tommie/v8go/deps/<os_arch>` (pinned in `go.mod` and verified by `go.sum`), built by the upstream release pipeline from the V8 revision in `deps/v8_hash`.

Artemis vendors one binary per darwin target: the C++ binding compiled from this directory's `.cc` and `.h` files.

| Platform | Path | Built by |
|---|---|---|
| macOS arm64 | `lib/darwin_arm64/libv8go_artemis.a` | `go run ./cmd/artemis-v8go-binding` in the Artemis module |
| macOS x86_64 | `lib/darwin_amd64/libv8go_artemis.a` | same |

`lib/BINDING.sha256` records the digest of the sources the archives were compiled from; `TestBindingArchiveMatchesSources` fails when a source changes without a rebuild. On Linux cgo compiles the binding from source, which needs Clang >= 21 and `CC=clang-21 CXX=clang++-21 CGO_CXXFLAGS=-nostdinc++`.

## Reproduction

Install LLVM Clang >= 21 (for example Homebrew `llvm`), then from the Artemis module root run `go run ./cmd/artemis-v8go-binding -llvm <llvm>/bin`. The tool compiles every `.cc` file with the flags of `cgo.go` plus `-nostdinc++` and the cgo export header for each target, archives the objects and rewrites `lib/BINDING.sha256`.

## License attribution

- `LICENSE` — upstream v8go BSD-style license.
- `CHANGELOG.md`, `CONTRIBUTING.md`, `README.md` — upstream attribution files preserved.
- `ARTEMIS_PATCHES.md` — Artemis modifications and build notes.
