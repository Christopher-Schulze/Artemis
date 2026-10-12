package main

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSnapshotPathTargetsRuntimeBlob(t *testing.T) {
	if snapshotPath != "js/snapshot.bin" {
		t.Fatalf("snapshotPath = %q, want js/snapshot.bin", snapshotPath)
	}
}

func TestSnapshotManifestPathTargetsRuntimeProvenance(t *testing.T) {
	if snapshotManifestPath != "js/snapshot_manifest.json" {
		t.Fatalf("snapshotManifestPath = %q, want js/snapshot_manifest.json", snapshotManifestPath)
	}
}

func TestGenerateSnapshotIsByteDeterministic(t *testing.T) {
	// V8 only guarantees snapshot determinism across fresh processes — a
	// second CreateBlob in one process inherits allocator/GC state. The
	// production generator is a one-shot CLI, so verify via subprocess.
	first := generateSnapshotSubprocess(t)
	second := generateSnapshotSubprocess(t)
	if !bytes.Equal(first, second) {
		t.Fatal("repeated snapshot generation produced different bytes")
	}
}

// generateSnapshotSubprocess re-executes this test binary in helper mode to
// produce one snapshot per fresh process (matching the one-shot CLI path).
func generateSnapshotSubprocess(t *testing.T) []byte {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=TestSnapshotHelperProcess")
	cmd.Env = append(os.Environ(), "ARTEMIS_SNAPSHOT_HELPER=1")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("snapshot helper: %v", err)
	}
	blob, err := hex.DecodeString(strings.TrimSpace(string(out)))
	if err != nil {
		t.Fatalf("decode helper output: %v", err)
	}
	return blob
}

// TestSnapshotHelperProcess runs only as a re-executed child of
// TestGenerateSnapshotIsByteDeterministic.
func TestSnapshotHelperProcess(t *testing.T) {
	if os.Getenv("ARTEMIS_SNAPSHOT_HELPER") != "1" {
		return
	}
	blob, err := generateSnapshot()
	if err != nil {
		os.Exit(2)
	}
	fmt.Fprintln(os.Stdout, hex.EncodeToString(blob))
	os.Exit(0)
}

func TestValidateModuleRoot(t *testing.T) {
	valid := t.TempDir()
	if err := os.WriteFile(filepath.Join(valid, "go.mod"), []byte("module "+artemisModulePath+"\n"), 0o600); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := validateModuleRoot(valid); err != nil {
		t.Fatalf("validateModuleRoot(valid): %v", err)
	}
	wrong := t.TempDir()
	if err := os.WriteFile(filepath.Join(wrong, "go.mod"), []byte("module example.invalid/wrong\n"), 0o600); err != nil {
		t.Fatalf("write wrong go.mod: %v", err)
	}
	if err := validateModuleRoot(wrong); err == nil {
		t.Fatal("validateModuleRoot(wrong) returned nil")
	}
}

func TestWriteFileAtomicReplacesCompleteSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.bin")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatalf("write old snapshot: %v", err)
	}
	if err := writeFileAtomic(path, []byte("new-complete-snapshot"), 0o640); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if string(data) != "new-complete-snapshot" {
		t.Fatalf("snapshot = %q", data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat snapshot: %v", err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %o, want 640", info.Mode().Perm())
	}
}

func TestStubsBootstrapDefinesRequiredGlobals(t *testing.T) {
	for _, token := range []string{
		"globalThis.window = globalThis",
		"globalThis.document = Object.create(null)",
		"globalThis.navigator = Object.create(null)",
		"globalThis.performance = Object.create(null)",
		"const NATIVE_NAMES = [",
		"__wrap",
		"__fetch",
		"__document",
		"__crypto_random",
	} {
		if !strings.Contains(stubsBootstrap, token) {
			t.Fatalf("stubsBootstrap missing %q", token)
		}
	}
}
