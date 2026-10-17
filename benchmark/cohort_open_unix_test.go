//go:build darwin || linux

package benchmark

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestCohortMemberOpensWithoutFollowingSymlinksOrBlockingOnFIFO(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "actual"), []byte("sealed bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("actual", filepath.Join(directory, "alias")); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(filepath.Join(directory, "fifo"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	}()
	if file, err := openCohortMember(root, "alias"); err == nil {
		t.Error("leaf symlink was followed")
		if err := file.Close(); err != nil {
			t.Error(err)
		}
	}
	file, err := openCohortMember(root, "fifo")
	if err != nil {
		t.Fatal(err)
	}
	info, statErr := file.Stat()
	closeErr := file.Close()
	if statErr != nil || closeErr != nil || info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatalf("nonblocking FIFO open did not retain its actual identity: stat=%v close=%v", statErr, closeErr)
	}
	if file, _, err := openCohortFile(t.Context(), root, "fifo", MaxArtifactFileBytes); err == nil {
		t.Error("FIFO was admitted as artifact data")
		if err := file.Close(); err != nil {
			t.Error(err)
		}
	}
}
