package js

import (
	"encoding/json"
	"runtime"
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

func TestSnapshotManifestBindsTheGoReleaseLineNotThePatch(t *testing.T) {
	// The committed target is passed explicitly, so the Go-line rule is
	// proven on every build host, not only on the snapshot target.
	manifest, err := EmbeddedSnapshotManifest()
	if err != nil {
		t.Fatal(err)
	}
	targetOS, targetArch := manifest.TargetOS, manifest.TargetArch
	if manifest.GoVersion != "go1.27" || manifest.ToolchainRef != snapshotToolchainRef(v8.Version(), "go1.27.0", targetOS, targetArch) {
		t.Fatalf("manifest go_version=%q toolchain_ref=%q, want the go1.27 release line", manifest.GoVersion, manifest.ToolchainRef)
	}
	input, err := BuildSnapshotInputManifest(SnapshotStubBootstrap())
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		goVersion string
		valid     bool
	}{
		{"go1.27.0", true}, {"go1.27.3", true}, {"go1.27.4", true}, {"go1.27.19", true}, {"go1.27rc2", true},
		{"go1.26.9", false}, {"go1.28.0", false}, {"go1.2", false}, {"devel go1.28-0123456789ab", false}, {"", false},
	}
	for _, test := range tests {
		err := ValidateSnapshotManifest(manifest, snapshotBlob, input, v8.Version(), test.goVersion, targetOS, targetArch)
		if (err == nil) != test.valid {
			t.Fatalf("%q: err=%v, want valid=%t", test.goVersion, err, test.valid)
		}
	}
	if fresh := NewSnapshotManifest(input, snapshotBlob, v8.Version(), "go1.27.4", targetOS, targetArch); fresh.GoVersion != manifest.GoVersion || fresh.ToolchainRef != manifest.ToolchainRef {
		t.Fatalf("a go1.27.4 generation records %q/%q, committed %q/%q", fresh.GoVersion, fresh.ToolchainRef, manifest.GoVersion, manifest.ToolchainRef)
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
	if err := ValidateSnapshotManifest(manifest, snapshotBlob, SnapshotInputManifest{}, v8.Version(), runtime.Version(), runtime.GOOS, runtime.GOARCH); err == nil {
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
