package js

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
)

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

// SnapshotAssetSize returns the exact byte size of the embedded V8 startup
// snapshot for release-manifest verification.
func SnapshotAssetSize() int64 {
	return int64(len(snapshotBlob))
}

// SnapshotAssetSHA256 returns the embedded V8 startup snapshot digest.
func SnapshotAssetSHA256() string {
	sum := sha256.Sum256(snapshotBlob)
	return hex.EncodeToString(sum[:])
}
