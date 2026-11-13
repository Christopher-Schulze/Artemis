package js

import (
	"encoding/json"
	"runtime"
	"strings"
	"testing"

	v8 "github.com/tommie/v8go"
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

func TestSnapshotManifestBindsV8AndTargetButNoGoRelease(t *testing.T) {
	// The committed target is passed explicitly, so the binding is proven on
	// every build host, not only on the snapshot target.
	manifest, err := EmbeddedSnapshotManifest()
	if err != nil {
		t.Fatal(err)
	}
	targetOS, targetArch := manifest.TargetOS, manifest.TargetArch
	if strings.Contains(";"+manifest.ToolchainRef, ";go=") || manifest.ToolchainRef != snapshotToolchainRef(v8.Version(), targetOS, targetArch) {
		t.Fatalf("toolchain_ref=%q, want V8, v8go and target only", manifest.ToolchainRef)
	}
	if _, err := DecodeSnapshotManifest([]byte(strings.Replace(string(embeddedSnapshotManifest), "{", `{"go_version":"go1.27.3",`, 1))); err == nil {
		t.Fatal("a manifest that binds a Go release decoded")
	}
	input, err := BuildSnapshotInputManifest(SnapshotStubBootstrap())
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateSnapshotManifest(manifest, snapshotBlob, input, v8.Version(), targetOS, targetArch); err != nil {
		t.Fatalf("committed snapshot rejected on its own target: %v", err)
	}
	for _, mismatch := range []struct{ v8Version, targetOS, targetArch string }{
		{v8.Version() + "-other", targetOS, targetArch},
		{v8.Version(), "plan9", targetArch},
		{v8.Version(), targetOS, "mips"},
	} {
		if err := ValidateSnapshotManifest(manifest, snapshotBlob, input, mismatch.v8Version, mismatch.targetOS, mismatch.targetArch); err == nil {
			t.Fatalf("snapshot admitted %+v", mismatch)
		}
	}
	if fresh := NewSnapshotManifest(input, snapshotBlob, v8.Version(), targetOS, targetArch); fresh.ToolchainRef != manifest.ToolchainRef {
		t.Fatalf("a fresh generation records %q, committed %q", fresh.ToolchainRef, manifest.ToolchainRef)
	}
}

func TestSnapshotManifestUsesStandaloneIdentity(t *testing.T) {
	manifest, err := EmbeddedSnapshotManifest()
	if err != nil {
		t.Fatal(err)
	}
	if manifest.SourcePath != "js/snapshot.bin" || manifest.GeneratorRef != "cmd/artemis-snapshot" {
		t.Fatalf("snapshot identities are not module relative: %q %q", manifest.SourcePath, manifest.GeneratorRef)
	}
	manifest.SourcePath = "private-root/" + manifest.SourcePath
	if err := ValidateSnapshotManifest(manifest, snapshotBlob, SnapshotInputManifest{}, v8.Version(), runtime.GOOS, runtime.GOARCH); err == nil {
		t.Fatal("snapshot admitted a noncanonical source identity")
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
		if err := ValidateSnapshotManifest(manifest, snapshotBlob, SnapshotInputManifest{}, v8.Version(), runtime.GOOS, runtime.GOARCH); err == nil {
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
	if err := ValidateSnapshotManifest(manifest, corrupt, input, v8.Version(), runtime.GOOS, runtime.GOARCH); err == nil {
		t.Fatal("ValidateSnapshotManifest accepted a corrupted blob")
	}
}

func TestValidateSnapshotManifestRejectsSourceSetDrift(t *testing.T) {
	manifest, matches := currentToolchainMatches(t)
	if !matches {
		if err := ValidateSnapshotManifest(manifest, snapshotBlob, SnapshotInputManifest{}, v8.Version(), runtime.GOOS, runtime.GOARCH); err == nil {
			t.Fatal("ValidateSnapshotManifest accepted foreign-toolchain manifest")
		}
		return
	}
	input, err := BuildSnapshotInputManifest(SnapshotStubBootstrap())
	if err != nil {
		t.Fatalf("BuildSnapshotInputManifest: %v", err)
	}
	input.SourceSetSHA256 = "0000000000000000000000000000000000000000000000000000000000000000"
	if err := ValidateSnapshotManifest(manifest, snapshotBlob, input, v8.Version(), runtime.GOOS, runtime.GOARCH); err == nil {
		t.Fatal("ValidateSnapshotManifest accepted source-set drift")
	}
}
