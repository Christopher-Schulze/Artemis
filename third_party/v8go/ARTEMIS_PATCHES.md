# Artemis patches against github.com/tommie/v8go v0.39.0

This is a vendored copy of [github.com/tommie/v8go](https://github.com/tommie/v8go)
v0.39.0 (V8 15.5.35.25), the maintained fork of `rogchap.com/v8go` v0.9.0, with
the modifications below. Upstream code is unchanged except where noted. The
original BSD-style license is preserved in `LICENSE`.

The copy is consumed through a `replace` directive in the Artemis `go.mod`
(and the `go.mod` of any module that embeds Artemis):

    replace github.com/tommie/v8go => ./third_party/v8go

Artemis additions live in `artemis.h`/`artemis.cc` and the Go files
`array_buffer.go`, `bounded_string.go`, `error.go`, `map.go`,
`promise_rejections.go`, `snapshot.go`, `binding_sources.go`,
`artemis_cgo_darwin.go` and `bindingdigest/`; behavioural patches to upstream
files are listed per item.

## Prebuilt darwin binding archives

V8 from tommie/v8go is built against Chromium's hardened libc++, so the C++
binding must be compiled with Clang >= 21, the vendored libc++ headers and
`-nostdinc++`. Go accepts neither the compiler choice nor `-nostdinc++` in
`#cgo` directives. Every `.cc` file therefore starts with `//go:build !darwin`;
on darwin `artemis_cgo_darwin.go` links `lib/<os_arch>/libv8go_artemis.a`,
built by Artemis' `cmd/artemis-v8go-binding`, so darwin builds need no special
`CC`, `CXX` or `CGO_CXXFLAGS`. `lib/BINDING.sha256` holds the source digest
(`bindingdigest.SourceDigest`); `TestBindingArchiveMatchesSources` fails when a
`.cc` or `.h` file changes without rebuilding the archives. On Linux cgo
compiles the sources and needs `CC=clang-21 CXX=clang++-21 CGO_CXXFLAGS=-nostdinc++`.

## Added: process-wide startup snapshot (TASK 042, TASK-2996)

Artemis bakes the parse and first run of every JS bootstrap script into a
startup snapshot (`js/snapshot.bin`) so each page realm deserialises in
microseconds instead of re-evaluating the bootstrap.

V8 15 shares one read-only heap across all isolates of a process, and the
snapshot creator promotes objects into read-only space, so isolates from
different snapshots cannot coexist (the second either aborts or silently loses
builtins). The snapshot is therefore process-wide:

- `SetProcessSnapshot(blob)` (Go) / `SetProcessSnapshot` (C) registers the blob
  before V8 initialises; afterwards it returns `ErrV8Initialized`, and a
  different blob is refused. `NewIsolate` passes it as `snapshot_blob` to every
  isolate (`V8::SetSnapshotDataBlob` is unreachable with V8's embedded snapshot).
- The snapshot's default context is pristine, so plain `NewContext` realms are
  unchanged; the scripted realm is additional context 0, opened by
  `NewContextFromSnapshot(iso, 0)` (C `NewContextFromSnapshot`, which sets the
  same embedder REF as `NewContext`).
- `NewSnapshotCreator`, `RunScript`, `CreateBlob`, `Dispose`: the creator uses
  `SnapshotCreator(CreateParams)`, keeps the scripted context as
  `AddContext` index 0 and refuses to run while a process snapshot is active.
- `SnapshotBlobIsValid` rejects input shorter than the fixed header before
  `StartupData::IsValid`, whose header read is a CHECK, and needs no
  initialised V8.
- `TestProcessSnapshotSharedByEveryIsolate` proves in a child process that every
  isolate starts from the snapshot, the scripted realm has the baked state, plain
  realms are pristine and no second snapshot can be mixed in.

## Added: owned execution cancellation recovery

`Isolate.CancelTerminateExecution()` (`IsolateCancelTerminateExecution`) calls
`v8::Isolate::CancelTerminateExecution`. Artemis invokes it only after the
owning operation has unwound its JavaScript frames and joined all started
cancellation callbacks. `Isolate.RetainedValueCount` exposes the internal
context's handle accounting separately from page contexts.

## Patched: exception-safe native property writes and settlement

`ObjectSet`, `ObjectSetAnyKey`, `ObjectSetIdx` (`object.cc`) and the added
`ObjectSetMany` return `RtnError` through their Go error results instead of
applying V8's fatal `Maybe.Check()` to page-controlled setters; a failed batch
stops before later writes, and an empty thrown value keeps a non-empty marker
(`ExceptionError`, `errors.cc`). Go `Object.Set`, `SetSymbol`, `SetIdx` and
`SetInternalField` release the handles they create for Go primitives;
caller-owned `Valuer` handles stay live. `MethodCall` releases its property
lookup. `Object.SetMany`/`SetManyPrepared`/`PrepareKeys` set many properties in
one cgo crossing.

`ValueRelease` (`value.cc`) takes the isolate's Locker before touching the
handle table. `PromiseResolverResolve`/`Reject` (`v8go.cc`) convert an empty
`Maybe` to false and refuse settlement while execution is terminating, so an
interrupted native settlement is never acknowledged.

## Added: bounded binary transfer and checked string conversion

`Context.NewArrayBuffer` copies bytes into V8-owned storage.
`Value.BufferByteLength`, `BufferBytes` and `SetBufferBytes` copy exact
ArrayBuffer/view regions, rejecting shared or detached storage and mismatched
lengths before writing; no Go or V8 pointer escapes its owner.
`Value.StringChecked` propagates conversion exceptions; `StringWithLimit`
rejects excess UTF-16 units or UTF-8 bytes before allocating.

`BindingSources` embeds the Go, C++ and header sources so Artemis binds them
into its conformance identity alongside the V8 version.

## Added: native Map retirement, callback returns, TypeError, null-prototype objects

`Value.DeleteMapKey` deletes from same-isolate native Map storage without page
dispatch or microtasks. `Value.ReturnFromCallback` marks an owned return handle;
the callback trampoline (`function_template.cc`) releases it after copying the
return value. `Context.NewTypeError` builds a TypeError without page
constructors. `Context.NewNullPrototypeObject` allocates an object with a null
prototype directly.

## Added: owned promise rejection tracking

`Context.EnablePromiseRejectionTracking` opts a realm into V8's promise reject
hook; creation contexts resolve through the embedder REF to registered live
owners. Each realm retains at most 4096 promise/reason pairs with sticky quota
failure. `Context.TakePromiseRejections` returns at most sixteen bounded
diagnostics without calling page getters. `Isolate.SetPromiseRejectionObserver`
notifies the host synchronously; Dispose retires it. Contexts unregister in
`ContextFree` (`ArtemisContextRetireRejections`). This hook and upstream's
`Isolate.SetPromiseRejectedCallback` share V8's single reject callback; the one
set last is active.

## Upstream files

`CHANGELOG.md`, `CONTRIBUTING.md`, `README.md`, `.clang-format`, `deps/*.py`
and the upstream `*_test.go` files are kept for attribution and as a smoke
test (`go test github.com/tommie/v8go/...`); the upstream `test/` and `tools/`
directories are not vendored.
