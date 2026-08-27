//go:build darwin

package process

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRemoveOrphanClones_RemovesOnlyIdleClones verifies the sweep removes clone
// directories that no live process holds open while preserving in-use ones and
// unrelated directories.
func TestRemoveOrphanClones_RemovesOnlyIdleClones(t *testing.T) {
	base := t.TempDir()

	orphan := filepath.Join(base, "com.google.Chrome.code_sign_clone", "code_sign_clone.AAA")
	live := filepath.Join(base, "com.google.Chrome.code_sign_clone", "code_sign_clone.BBB")
	unrelated := filepath.Join(base, "com.google.Chrome.code_sign_clone", "not_a_clone")
	for _, d := range []string{orphan, live, unrelated} {
		if err := os.MkdirAll(filepath.Join(d, "Google Chrome.app.bundle"), 0o700); err != nil {
			t.Fatalf("seed dir %s: %v", d, err)
		}
	}

	inUse := map[string]bool{"code_sign_clone.BBB": true}
	removed := removeOrphanClones(base, inUse)

	if len(removed) != 1 || filepath.Base(removed[0]) != "code_sign_clone.AAA" {
		t.Fatalf("expected only code_sign_clone.AAA removed, got %v", removed)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Errorf("orphan clone should be removed, stat err=%v", err)
	}
	if _, err := os.Stat(live); err != nil {
		t.Errorf("in-use clone must be preserved: %v", err)
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Errorf("non-clone directory must be preserved: %v", err)
	}
}

// TestRemoveOrphanClones_EmptyBaseIsNoop verifies the sweep is a safe no-op when
// there are no clone directories.
func TestRemoveOrphanClones_EmptyBaseIsNoop(t *testing.T) {
	if removed := removeOrphanClones(t.TempDir(), map[string]bool{}); len(removed) != 0 {
		t.Fatalf("expected no removals in empty base, got %v", removed)
	}
}

// TestCodeSignCloneDir_DerivesSiblingOfTempDir verifies the clone directory is
// resolved as the X sibling of the process temp dir when it exists, and returns
// empty when it does not.
func TestCodeSignCloneDir_DerivesSiblingOfTempDir(t *testing.T) {
	got := codeSignCloneDir()
	if got == "" {
		return // X dir absent on this machine; empty result is the valid contract
	}
	if filepath.Base(got) != "X" {
		t.Fatalf("code-sign clone dir should be the X sibling, got %q", got)
	}
	if info, err := os.Stat(got); err != nil || !info.IsDir() {
		t.Fatalf("resolved clone dir must exist and be a directory: %v", err)
	}
}
