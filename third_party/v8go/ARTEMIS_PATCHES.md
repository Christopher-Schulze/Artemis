# Artemis patches against rogchap/v8go v0.9.0

This is a vendored copy of [rogchap.com/v8go](https://github.com/rogchap/v8go)
v0.9.0 with the modifications below. Upstream code is unchanged except where
noted. Original BSD-style license preserved in `LICENSE`.

The fork is consumed via a `replace` directive in the artemis root `go.mod`:

    replace rogchap.com/v8go => ./third_party/v8go

## Added: V8 startup-snapshot bindings (TASK 042)

Upstream v8go does not expose `v8::SnapshotCreator`. Artemis bakes the
parse + first-run state of every JS bootstrap script into a startup
snapshot (`js/snapshot.bin`, embedded via `go:embed`) so each new
`Context` can deserialise the state in microseconds instead of
re-evaluating ~30 K LoC of class definitions per page.

### `v8go.h`

- New typedef `m_snapshotCreator` / `SnapshotCreatorPtr`
- New struct `SnapshotBlob { const uint8_t* data; int length; RtnError error; }`
- New extern decls:
  - `IsolatePtr NewIsolateWithSnapshot(const uint8_t* data, int len)`
  - `SnapshotCreatorPtr SnapshotCreatorNew()`
  - `RtnError SnapshotCreatorRunScript(SnapshotCreatorPtr, const char* source, const char* origin)`
  - `SnapshotBlob SnapshotCreatorCreateBlob(SnapshotCreatorPtr)`
  - `void SnapshotCreatorDelete(SnapshotCreatorPtr)`
  - `void SnapshotBlobFree(uint8_t* data)`

### `v8go.cc`

- New `m_snapshotCreator` struct (wraps `v8::SnapshotCreator` + persistent
  default `Context`)
- Implementations for the six new C functions above
- `NewIsolate()` now writes `nullptr` to isolate slot 1 to mark the
  "no startup data attached" case
- `IsolateDispose()` reads the StartupData* from slot 1 BEFORE calling
  `iso->Dispose()`, then frees the heap copy if present
- SnapshotCreator constructor is called with explicit casts
  `(static_cast<const intptr_t*>(nullptr), static_cast<StartupData*>(nullptr))`
  to disambiguate the two overloads in `v8-snapshot.h`

### `snapshot.go` (new file)

Pure-Go wrapper exposing:

- `type SnapshotCreator` with `RunScript`, `CreateBlob`, `Dispose`
- `func NewSnapshotCreator() *SnapshotCreator`
- `func NewIsolateFromSnapshot(snapshot []byte) *Isolate`

## Patched: `deps/include/v8-snapshot.h` ABI mismatch

The shipped header declares the SnapshotCreator constructors with
`const StartupData* existing_blob = nullptr`. The prebuilt `libv8.a` was
compiled against an earlier header revision where this parameter was
non-const, producing a different mangled symbol. Calls would link-fail
with `undefined symbol: v8::SnapshotCreator::SnapshotCreator(long
const*, v8::StartupData const*)`.

Fix: drop the `const` qualifier on the `existing_blob` parameter so the
compiler emits the symbol the lib actually exports. Two occurrences,
both in the SnapshotCreator constructor declarations.

## Build hygiene notes

- `deps/darwin_arm64/libv8.a` is preserved as-is (~278 MB binary blob)
- The `.fossa.yml`, `CHANGELOG.md`, `CONTRIBUTING.md`, `README.md` from
  upstream are kept for license attribution
- All `*_test.go` files are kept; running upstream's tests after the
  patches is a useful smoke test (`go test rogchap.com/v8go/...`)
