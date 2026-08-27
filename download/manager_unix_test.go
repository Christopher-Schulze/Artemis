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
	chmodTestDirectory(t, manager.Directory(), 0o500)
	t.Cleanup(func() {
		chmodTestDirectory(t, manager.Directory(), 0o700)
	})
	_, err := manager.Adopt(target, "")
	if err == nil || !strings.Contains(err.Error(), "cleanup rejected target") {
		t.Fatalf("cleanup error=%v", err)
	}
	if _, statErr := os.Stat(filepath.Clean(target)); statErr != nil {
		t.Fatalf("rejected file should remain when cleanup fails: %v", statErr)
	}
}

func chmodTestDirectory(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	path = filepath.Clean(path)
	directory, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	chmodErr := directory.Chmod(mode)
	closeErr := directory.Close()
	if chmodErr != nil || closeErr != nil {
		t.Fatalf("chmod directory: chmod=%v close=%v", chmodErr, closeErr)
	}
}
