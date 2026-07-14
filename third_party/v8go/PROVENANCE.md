# v8go Provenance

This directory contains a vendored, patched fork of `rogchap.com/v8go` used by Artemis.

## Upstream

- **Package**: `rogchap.com/v8go`
- **Version**: v0.9.0
- **Upstream source**: https://github.com/rogchap/v8go
- **License**: BSD-style (see `LICENSE`)
- **Go module**: `module rogchap.com/v8go`

## Artemis patches

The changes from upstream are documented in `ARTEMIS_PATCHES.md` and include:

1. V8 startup-snapshot bindings (`SnapshotCreator`, `NewIsolateWithSnapshot`, etc.) used by `js/snapshot.bin`.
2. A header ABI fix for `deps/include/v8-snapshot.h` to match the prebuilt `libv8.a` symbol.

## Binary dependencies

Prebuilt V8 static libraries are shipped under `deps/` for supported platforms:

| Platform | Path | Size (approx) |
|---|---|---|
| macOS arm64 | `deps/darwin_arm64/libv8.a` | ~278 MB |
| macOS x86_64 | `deps/darwin_x86_64/libv8.a` | ~278 MB |
| Linux arm64 | `deps/linux_arm64/libv8.a` | ~278 MB |
| Linux x86_64 | `deps/linux_x86_64/libv8.a` | ~278 MB |

These binaries are preserved from the upstream v0.9.0 release artifacts and are
redistributed under the same BSD-style license as the upstream source. The
binaries are compiled from V8 source at the upstream-locked V8 version; the
exact upstream V8 revision is recorded by the upstream `v8go` release build.

## Reproduction

To rebuild the `libv8.a` binaries from source, follow the upstream build
instructions at https://github.com/rogchap/v8go#building. The Artemis patches
in `ARTEMIS_PATCHES.md` must be applied before building.

## License attribution

- `LICENSE` — upstream v8go BSD-style license.
- `CHANGELOG.md`, `CONTRIBUTING.md`, `README.md` — upstream attribution files preserved.
- `ARTEMIS_PATCHES.md` — Artemis modifications and build hygiene notes.
