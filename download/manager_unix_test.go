//go:build !windows

package download

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDownloadManagerReportsRejectedFileCleanupFailure(t *testing.T) {
	manager := newTestManager(t, 64, 128, []string{"image/png"})
	target := filepath.Join(manager.Directory(), "blocked.txt")
	if err := os.WriteFile(target, []byte("plain text"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(manager.Directory(), 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(manager.Directory(), 0o700); err != nil {
			t.Errorf("restore directory mode: %v", err)
		}
	})
	_, err := manager.Adopt(target, "")
	if err == nil || !strings.Contains(err.Error(), "cleanup rejected target") {
		t.Fatalf("cleanup error=%v", err)
	}
	if _, statErr := os.Stat(target); statErr != nil {
		t.Fatalf("rejected file should remain when cleanup fails: %v", statErr)
	}
}
