package js

import _ "embed"

// snapshotBlob is the V8 startup snapshot produced by
// `go run ./cmd/artemis-snapshot/`. It bakes the parsed + first-run
// state of every BootstrapSource into a binary blob that
// NewIsolateFromSnapshot deserialises in microseconds. NewRuntime uses
// it automatically; if absent, the runtime falls back to the
// from-scratch isolate path.
//
// Regenerate after any bootstrap source change:
//
//	go run ./cmd/artemis-snapshot/
//
//go:embed snapshot.bin
var snapshotBlob []byte
