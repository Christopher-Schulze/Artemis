package js

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"

	_ "embed"
	v8 "rogchap.com/v8go"
)

const (
	SnapshotAssetPath    = "codebase/backend/artemis/js/snapshot.bin"
	SnapshotAssetMIME    = "application/octet-stream"
	SnapshotGeneratorRef = "codebase/backend/artemis/cmd/artemis-snapshot"
	SnapshotOwnerRef     = "artemis/js.Runtime"
	SnapshotV8GoVersion  = "v0.9.0"
)

// SnapshotManifest is the embedded, cryptographically bound provenance for
// the V8 startup snapshot. It is generated beside snapshot.bin and embedded
// into the same Artemis binary.
type SnapshotManifest struct {
	SchemaVersion    int                    `json:"schema_version"`
	SourcePath       string                 `json:"source_path"`
	MIME             string                 `json:"mime"`
	Size             int64                  `json:"size"`
	SHA256           string                 `json:"sha256"`
	SourceSetSHA256  string                 `json:"source_set_sha256"`
	GeneratorRef     string                 `json:"generator_ref"`
	ToolchainRef     string                 `json:"toolchain_ref"`
	V8Version        string                 `json:"v8_version"`
	V8GoVersion      string                 `json:"v8go_version"`
	GoVersion        string                 `json:"go_version"`
	TargetOS         string                 `json:"target_os"`
	TargetArch       string                 `json:"target_arch"`
	ActivationOwner  string                 `json:"activation_owner"`
	BootstrapSources []SnapshotSourceDigest `json:"bootstrap_sources"`
	NativeStubNames  []string               `json:"native_stub_names"`
}

//go:embed snapshot_manifest.json
var embeddedSnapshotManifest []byte

// NewSnapshotManifest creates the canonical provenance projection for a
// freshly generated blob.
func NewSnapshotManifest(input SnapshotInputManifest, blob []byte, v8Version, goVersion, targetOS, targetArch string) SnapshotManifest {
	return SnapshotManifest{
		SchemaVersion:    SnapshotManifestSchemaVersion,
		SourcePath:       SnapshotAssetPath,
		MIME:             SnapshotAssetMIME,
		Size:             int64(len(blob)),
		SHA256:           digestString(blob),
		SourceSetSHA256:  input.SourceSetSHA256,
		GeneratorRef:     SnapshotGeneratorRef,
		ToolchainRef:     snapshotToolchainRef(v8Version, goVersion, targetOS, targetArch),
		V8Version:        v8Version,
		V8GoVersion:      SnapshotV8GoVersion,
		GoVersion:        goVersion,
		TargetOS:         targetOS,
		TargetArch:       targetArch,
		ActivationOwner:  SnapshotOwnerRef,
		BootstrapSources: append([]SnapshotSourceDigest(nil), input.BootstrapSources...),
		NativeStubNames:  append([]string(nil), input.NativeStubNames...),
	}
}

// DecodeSnapshotManifest strictly decodes one manifest and rejects trailing
// or unknown data so a damaged or silently extended asset cannot activate.
func DecodeSnapshotManifest(data []byte) (SnapshotManifest, error) {
	var manifest SnapshotManifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return SnapshotManifest{}, fmt.Errorf("snapshot manifest: decode: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return SnapshotManifest{}, errors.New("snapshot manifest: trailing JSON")
		}
		return SnapshotManifest{}, fmt.Errorf("snapshot manifest: trailing data: %w", err)
	}
	return manifest, nil
}

// EmbeddedSnapshotManifest returns the manifest compiled into the Artemis
// binary.
func EmbeddedSnapshotManifest() (SnapshotManifest, error) {
	return DecodeSnapshotManifest(embeddedSnapshotManifest)
}

// ValidateSnapshotManifest proves that a blob and its provenance match the
// current source, target and linked V8 runtime before snapshot activation.
func ValidateSnapshotManifest(manifest SnapshotManifest, blob []byte, input SnapshotInputManifest, v8Version, goVersion, targetOS, targetArch string) error {
	if manifest.SchemaVersion != SnapshotManifestSchemaVersion {
		return fmt.Errorf("snapshot manifest: schema_version=%d, want=%d", manifest.SchemaVersion, SnapshotManifestSchemaVersion)
	}
	if manifest.SourcePath != SnapshotAssetPath || manifest.MIME != SnapshotAssetMIME {
		return fmt.Errorf("snapshot manifest: asset identity mismatch path=%q mime=%q", manifest.SourcePath, manifest.MIME)
	}
	if manifest.Size <= 0 || manifest.Size != int64(len(blob)) {
		return fmt.Errorf("snapshot manifest: size=%d, blob=%d", manifest.Size, len(blob))
	}
	if manifest.SHA256 != digestString(blob) {
		return fmt.Errorf("snapshot manifest: blob digest mismatch")
	}
	if !v8.SnapshotBlobIsValid(blob) {
		return errors.New("snapshot manifest: V8 rejected snapshot blob")
	}
	if manifest.SourceSetSHA256 != input.SourceSetSHA256 {
		return fmt.Errorf("snapshot manifest: source-set drift: manifest=%s current=%s", manifest.SourceSetSHA256, input.SourceSetSHA256)
	}
	if manifest.GeneratorRef != SnapshotGeneratorRef || manifest.ActivationOwner != SnapshotOwnerRef {
		return fmt.Errorf("snapshot manifest: generator/owner mismatch")
	}
	if manifest.V8Version != v8Version || manifest.V8GoVersion != SnapshotV8GoVersion || manifest.GoVersion != goVersion || manifest.TargetOS != targetOS || manifest.TargetArch != targetArch {
		return fmt.Errorf("snapshot manifest: runtime compatibility mismatch")
	}
	if manifest.ToolchainRef != snapshotToolchainRef(v8Version, goVersion, targetOS, targetArch) {
		return fmt.Errorf("snapshot manifest: toolchain reference mismatch")
	}
	if !sameSnapshotSources(manifest.BootstrapSources, input.BootstrapSources) || !sameStrings(manifest.NativeStubNames, input.NativeStubNames) {
		return errors.New("snapshot manifest: input inventory mismatch")
	}
	return nil
}

// CurrentSnapshotManifest validates the committed embedded blob against the
// source and runtime currently compiled into this process.
func CurrentSnapshotManifest() (SnapshotManifest, error) {
	manifest, err := EmbeddedSnapshotManifest()
	if err != nil {
		return SnapshotManifest{}, err
	}
	input, err := BuildSnapshotInputManifest(SnapshotStubBootstrap())
	if err != nil {
		return SnapshotManifest{}, err
	}
	return manifest, ValidateSnapshotManifest(manifest, snapshotBlob, input, v8.Version(), runtime.Version(), runtime.GOOS, runtime.GOARCH)
}

func snapshotToolchainRef(v8Version, goVersion, targetOS, targetArch string) string {
	return strings.Join([]string{"go=" + goVersion, "v8go=" + SnapshotV8GoVersion, "v8=" + v8Version, "target=" + targetOS + "/" + targetArch}, ";")
}

func sameSnapshotSources(left, right []SnapshotSourceDigest) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
