package js

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const SnapshotManifestSchemaVersion = 1

// SnapshotSourceDigest is the immutable identity of one snapshot-eligible
// bootstrap source. The order in SnapshotInputManifest.BootstrapSources is
// part of the snapshot contract.
type SnapshotSourceDigest struct {
	Name   string `json:"name"`
	Size   int    `json:"size"`
	SHA256 string `json:"sha256"`
}

// SnapshotInputManifest is the canonical, deterministic input projection for
// the V8 startup snapshot generator.
type SnapshotInputManifest struct {
	SchemaVersion    int                    `json:"schema_version"`
	StubSHA256       string                 `json:"stub_sha256"`
	BootstrapSources []SnapshotSourceDigest `json:"bootstrap_sources"`
	NativeStubNames  []string               `json:"native_stub_names"`
	SourceSetSHA256  string                 `json:"source_set_sha256"`
}

var snapshotNativeStubNames = []string{
	"__cascade_style", "__fetch_abort", "__iframe_load", "__list_on_attrs",
	"__observer_disconnect", "__observer_register", "__observer_take", "__wrap",
	"__ws_close", "__ws_open", "__ws_send",
}

var snapshotNativeNamePattern = regexp.MustCompile(`"(__[A-Za-z][A-Za-z0-9_]*)"`)

// The callback-name accessor returns a defensive copy of the canonical native
// inventory used by the snapshot bootstrap.
func SnapshotNativeStubNames() []string {
	return append([]string(nil), snapshotNativeStubNames...)
}

// BuildSnapshotInputManifest derives the complete ordered snapshot input
// identity. The bootstrap prelude source is included so changes to the prelude or
// generated callback inventory cannot leave a stale blob accepted.
func BuildSnapshotInputManifest(stubSource string) (SnapshotInputManifest, error) {
	if strings.TrimSpace(stubSource) == "" {
		return SnapshotInputManifest{}, errors.New("snapshot input: empty stub source")
	}
	if err := validateNativeStubInventory(stubSource); err != nil {
		return SnapshotInputManifest{}, err
	}

	sources := BootstrapSources()
	seen := make(map[string]struct{}, len(sources))
	digests := make([]SnapshotSourceDigest, 0, len(sources))
	for index, source := range sources {
		if strings.TrimSpace(source.Name) == "" || source.Source == "" {
			return SnapshotInputManifest{}, fmt.Errorf("snapshot input: bootstrap %d is incomplete", index)
		}
		if _, ok := seen[source.Name]; ok {
			return SnapshotInputManifest{}, fmt.Errorf("snapshot input: duplicate bootstrap %q", source.Name)
		}
		seen[source.Name] = struct{}{}
		digests = append(digests, SnapshotSourceDigest{
			Name:   source.Name,
			Size:   len(source.Source),
			SHA256: digestString([]byte(source.Source)),
		})
	}

	names := SnapshotNativeStubNames()
	canonical := strings.Builder{}
	fmt.Fprintf(&canonical, "schema_version=%d\nstub_sha256=%s\n", SnapshotManifestSchemaVersion, digestString([]byte(stubSource)))
	for index, source := range digests {
		fmt.Fprintf(&canonical, "bootstrap[%d].name=%s\nbootstrap[%d].size=%d\nbootstrap[%d].sha256=%s\n", index, source.Name, index, source.Size, index, source.SHA256)
	}
	for index, name := range names {
		fmt.Fprintf(&canonical, "native_stub[%d]=%s\n", index, name)
	}

	return SnapshotInputManifest{
		SchemaVersion:    SnapshotManifestSchemaVersion,
		StubSHA256:       digestString([]byte(stubSource)),
		BootstrapSources: digests,
		NativeStubNames:  names,
		SourceSetSHA256:  digestString([]byte(canonical.String())),
	}, nil
}

func validateNativeStubInventory(source string) error {
	actualMatches := snapshotNativeNamePattern.FindAllStringSubmatch(source, -1)
	actual := make([]string, 0, len(actualMatches))
	seen := make(map[string]struct{}, len(actualMatches))
	for _, match := range actualMatches {
		name := match[1]
		if _, ok := seen[name]; ok {
			return fmt.Errorf("snapshot input: duplicate native stub %q", name)
		}
		seen[name] = struct{}{}
		actual = append(actual, name)
	}
	want := SnapshotNativeStubNames()
	sort.Strings(actual)
	sort.Strings(want)
	if len(actual) != len(want) {
		return fmt.Errorf("snapshot input: native stub inventory count=%d, want=%d", len(actual), len(want))
	}
	for index := range want {
		if actual[index] != want[index] {
			return fmt.Errorf("snapshot input: native stub inventory mismatch at %d: got %q, want %q", index, actual[index], want[index])
		}
	}
	return nil
}

func digestString(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
