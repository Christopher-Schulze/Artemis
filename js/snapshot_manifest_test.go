package js

import (
	"encoding/json"
	"runtime"
	"testing"

	v8 "rogchap.com/v8go"
)

// currentToolchainMatches reports whether the committed snapshot blob was
// generated under this build's toolchain (OS/arch/Go/v8). On any other
// toolchain activation must be correctly rejected by design.
func currentToolchainMatches(t *testing.T) (SnapshotManifest, bool) {
	t.Helper()
	manifest, err := EmbeddedSnapshotManifest()
	if err != nil {
		t.Fatalf("EmbeddedSnapshotManifest: %v", err)
	}
	return manifest, manifest.ToolchainRef == CurrentToolchainRef()
}

func TestCurrentSnapshotManifestMatchesEmbeddedInputs(t *testing.T) {
	_, matches := currentToolchainMatches(t)
	if !matches {
		// Foreign toolchain: the manifest must NOT validate against the
		// current build — assert the rejection is the compat error.
		if _, err := CurrentSnapshotManifest(); err == nil {
			t.Fatal("CurrentSnapshotManifest accepted a foreign-toolchain manifest")
		}
		return
	}
	current, err := CurrentSnapshotManifest()
	if err != nil {
		t.Fatalf("CurrentSnapshotManifest: %v", err)
	}
	if current.SHA256 != SnapshotAssetSHA256() {
		t.Fatalf("manifest digest=%s, embedded digest=%s", current.SHA256, SnapshotAssetSHA256())
	}
	if current.Size != SnapshotAssetSize() {
		t.Fatalf("manifest size=%d, embedded size=%d", current.Size, SnapshotAssetSize())
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
	manifest, matches := currentToolchainMatches(t)
	if !matches {
		// On a foreign toolchain the corruption test degenerates to the
		// toolchain gate; assert that rejection instead.
		if err := ValidateSnapshotManifest(manifest, snapshotBlob, SnapshotInputManifest{}, v8.Version(), runtime.Version(), runtime.GOOS, runtime.GOARCH); err == nil {
			t.Fatal("ValidateSnapshotManifest accepted foreign-toolchain manifest")
		}
		return
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
	manifest, matches := currentToolchainMatches(t)
	if !matches {
		if err := ValidateSnapshotManifest(manifest, snapshotBlob, SnapshotInputManifest{}, v8.Version(), runtime.Version(), runtime.GOOS, runtime.GOARCH); err == nil {
			t.Fatal("ValidateSnapshotManifest accepted foreign-toolchain manifest")
		}
		return
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
