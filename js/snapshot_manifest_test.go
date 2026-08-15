package js

import (
	"encoding/json"
	"runtime"
	"testing"

	v8 "rogchap.com/v8go"
)

func TestCurrentSnapshotManifestMatchesEmbeddedInputs(t *testing.T) {
	manifest, err := CurrentSnapshotManifest()
	if err != nil {
		t.Fatalf("CurrentSnapshotManifest: %v", err)
	}
	if manifest.SHA256 != SnapshotAssetSHA256() {
		t.Fatalf("manifest digest=%s, embedded digest=%s", manifest.SHA256, SnapshotAssetSHA256())
	}
	if manifest.Size != SnapshotAssetSize() {
		t.Fatalf("manifest size=%d, embedded size=%d", manifest.Size, SnapshotAssetSize())
	}
}

func TestDecodeSnapshotManifestRejectsUnknownAndTrailingData(t *testing.T) {
	manifest, err := EmbeddedSnapshotManifest()
	if err != nil {
		t.Fatalf("EmbeddedSnapshotManifest: %v", err)
	}
	valid, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	unknown := append([]byte(`{"unexpected":true}`), '\n')
	if _, err := DecodeSnapshotManifest(unknown); err == nil {
		t.Fatal("DecodeSnapshotManifest accepted an unknown field")
	}

	trailing := append(append([]byte(nil), valid...), []byte("\n{}")...)
	if _, err := DecodeSnapshotManifest(trailing); err == nil {
		t.Fatal("DecodeSnapshotManifest accepted trailing JSON")
	}

	malformed := append(append([]byte(nil), valid...), []byte("\n{")...)
	if _, err := DecodeSnapshotManifest(malformed); err == nil {
		t.Fatal("DecodeSnapshotManifest accepted malformed trailing data")
	}
}

func TestValidateSnapshotManifestRejectsBlobCorruption(t *testing.T) {
	manifest, err := CurrentSnapshotManifest()
	if err != nil {
		t.Fatalf("CurrentSnapshotManifest: %v", err)
	}
	input, err := BuildSnapshotInputManifest(SnapshotStubBootstrap())
	if err != nil {
		t.Fatalf("BuildSnapshotInputManifest: %v", err)
	}
	corrupt := append([]byte(nil), snapshotBlob...)
	corrupt[len(corrupt)-1] ^= 0x01
	if err := ValidateSnapshotManifest(manifest, corrupt, input, v8.Version(), runtime.Version(), runtime.GOOS, runtime.GOARCH); err == nil {
		t.Fatal("ValidateSnapshotManifest accepted a corrupted blob")
	}
}

func TestValidateSnapshotManifestRejectsSourceSetDrift(t *testing.T) {
	manifest, err := CurrentSnapshotManifest()
	if err != nil {
		t.Fatalf("CurrentSnapshotManifest: %v", err)
	}
	input, err := BuildSnapshotInputManifest(SnapshotStubBootstrap())
	if err != nil {
		t.Fatalf("BuildSnapshotInputManifest: %v", err)
	}
	input.SourceSetSHA256 = "0000000000000000000000000000000000000000000000000000000000000000"
	if err := ValidateSnapshotManifest(manifest, snapshotBlob, input, v8.Version(), runtime.Version(), runtime.GOOS, runtime.GOARCH); err == nil {
		t.Fatal("ValidateSnapshotManifest accepted source-set drift")
	}
}
