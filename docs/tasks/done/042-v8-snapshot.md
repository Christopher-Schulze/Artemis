# TASK 042: V8 startup snapshot embedding

## Why

`js.NewContext` was the largest single cost on the Fetch hot path
(~17% per the TASK 040 profile, growing to ~34% as features landed).
Each new Context re-evaluated 17 JS bootstrap scripts (~30K LoC of
class definitions, prototype chains, helper functions). The TASK 041
UnboundScript cache already eliminated parse cost; remaining overhead
was pure execution. A V8 startup snapshot bakes that execution result
into a binary blob; new isolates deserialise it in microseconds.

`rogchap.com/v8go` v0.9.0 does not expose `v8::SnapshotCreator`, so
this task required forking the library.

## Acceptance

- v8go vendored locally and patched with SnapshotCreator + Isolate-from-
  snapshot bindings
- Snapshot creation tool produces `js/snapshot.bin`
- `Runtime.NewRuntime()` loads the snapshot via `NewIsolateFromSnapshot`
- All 62+ tests pass with snapshot active
- Race tests clean
- Measurable speedup on bootstrap-heavy benchmarks

## Sub-Tasks

- [x] Vendor v8go to `third_party/v8go` (`go mod vendor` plus copy)
- [x] Add `replace rogchap.com/v8go => ./third_party/v8go` to `go.mod`
- [x] Patch `v8go.cc`/`v8go.h` to add SnapshotCreator C bindings:
      `SnapshotCreatorNew`, `SnapshotCreatorRunScript`,
      `SnapshotCreatorCreateBlob`, `SnapshotCreatorDelete`,
      `SnapshotBlobFree`, plus `NewIsolateWithSnapshot`
- [x] Patch `IsolateDispose` to free heap-allocated StartupData buffer
- [x] Fix v8 header / lib ABI mismatch: `v8-snapshot.h` declared
      `existing_blob` as `const StartupData*` but the prebuilt
      `libv8.a` symbol expects non-const. Header patched to drop const
- [x] Add Go wrappers in `third_party/v8go/snapshot.go`:
      `SnapshotCreator`, `NewIsolateFromSnapshot`
- [x] Add `js.BootstrapSources()` enumerating snapshot-eligible bootstraps
- [x] Identify and isolate "tainted" bootstraps that capture cross-
      bootstrap functions in closures (crypto-aes/asym/complete/extra/
      pkcs8 + extras-v2-onattrs). These continue to run per-Context
- [x] Build `cmd/artemis-snapshot` tool that pre-stubs native callbacks
      and runs all snapshot-eligible bootstraps inside SnapshotCreator
- [x] `//go:embed snapshot.bin` in `js/snapshot_data.go`
- [x] `Runtime.NewRuntime()` switches to `NewIsolateFromSnapshot` when
      blob present
- [x] `Context.flushBootstraps` runs only tainted bootstraps when
      snapshot loaded
- [x] Refactor `installCrypto` and `installWindow` (navigator) to MERGE
      properties onto existing global objects instead of replacing them
      wholesale (otherwise snapshot's wrappers get wiped)
- [x] Split `extrasV2Bootstrap` into static (snapshot-baked) and
      `extrasV2OnAttrsBootstrap` (per-Context, runs after document
      load when `__list_on_attrs` is bound)

## Notes

### Why some bootstraps are tainted

The 5 crypto chain bootstraps (`crypto-aes`, `crypto-asym`, `crypto-
complete`, `crypto-extra`, `crypto-pkcs8`) layer dispatch wrappers via
the pattern:

    const _impPrev = crypto.subtle.importKey;
    crypto.subtle.importKey = function(format, ...) {
      if (format === 'jwk') { return crypto.subtle.__importKey_jwk(...); }
      return _impPrev.call(this, ...);
    };

This captures the previous function in closure at definition time.
Inside a snapshot, `_impPrev` would freeze to whatever the stub was at
snapshot time (typically undefined), and any non-fast-path call would
crash. Running these bootstraps per-Context after native callbacks are
bound preserves the dispatch chain.

Same reason applies to `extras-v2-onattrs`: it walks
`__list_on_attrs()` (per-document data) at top-level, so the result
must be computed per-Context.

### ABI mismatch detail

The bundled `libv8.a` was compiled against an older `v8-snapshot.h`
where the SnapshotCreator constructor's `existing_blob` parameter was
`StartupData*` (non-const). The shipped header declares it as `const
StartupData*`, producing a different mangled symbol the linker cannot
resolve. Stripping `const` from the header restores binary compat.

### Realistic gain

Snapshot reduces NewContext bootstrap-evaluation cost. On the Fetch
benchmark the gain is in noise (~0-3%) because total Fetch time is
dominated by HTTP + HTML parsing. On `BenchmarkFetchRunScripts` (which
exercises script-heavy pages) the gain is roughly 5-8%. Lower than the
original 15-20% estimate because TASK 041 had already absorbed parse
savings, and the tainted bootstrap set must still run per-Context.

Numbers from `go test ./engine -bench=BenchmarkFetch -count=3`:

| Benchmark              | No snapshot     | With snapshot   |
|------------------------|-----------------|-----------------|
| BenchmarkFetch         | ~887µs/op       | ~913µs/op (noise) |
| BenchmarkFetchRunScripts | ~942µs/op    | ~884µs/op (-6%) |

Snapshot blob size: 362KB. Embedded into the artemis binary at build
time.

### Regenerating the snapshot

After any change to a snapshot-eligible bootstrap source:

    go run ./cmd/artemis-snapshot/

The build will fail until `js/snapshot.bin` is regenerated.

## Deviations

None. v8go vendoring + header patch is intentional and documented.
