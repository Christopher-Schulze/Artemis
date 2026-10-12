package js

import "testing"

func TestBuildSnapshotInputManifestBindsOrderedSourcesAndNativeInventory(t *testing.T) {
	manifest, err := BuildSnapshotInputManifest(SnapshotStubBootstrap())
	if err != nil {
		t.Fatalf("BuildSnapshotInputManifest: %v", err)
	}
	if manifest.SchemaVersion != SnapshotManifestSchemaVersion {
		t.Fatalf("schema_version=%d, want=%d", manifest.SchemaVersion, SnapshotManifestSchemaVersion)
	}
	sources := BootstrapSources()
	if len(manifest.BootstrapSources) != len(sources) {
		t.Fatalf("source count=%d, want=%d", len(manifest.BootstrapSources), len(sources))
	}
	for index, source := range sources {
		if manifest.BootstrapSources[index].Name != source.Name {
			t.Fatalf("source[%d]=%q, want=%q", index, manifest.BootstrapSources[index].Name, source.Name)
		}
	}
	if len(manifest.NativeStubNames) != len(SnapshotNativeStubNames()) {
		t.Fatalf("native stub count=%d, want=%d", len(manifest.NativeStubNames), len(SnapshotNativeStubNames()))
	}
	if manifest.SourceSetSHA256 == "" {
		t.Fatal("source_set_sha256 is empty")
	}
}

func TestBuildSnapshotInputManifestRejectsNativeInventoryDrift(t *testing.T) {
	if _, err := BuildSnapshotInputManifest(SnapshotStubBootstrap() + "\nconst __drift = \"__not_a_registered_stub\";\n"); err == nil {
		t.Fatal("BuildSnapshotInputManifest accepted native inventory drift")
	}
}
