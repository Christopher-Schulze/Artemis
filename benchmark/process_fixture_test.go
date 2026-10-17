//go:build darwin || linux

package benchmark

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if len(os.Args) >= 4 && os.Args[1] == "artemis-process-fixture" {
		os.Exit(runProcessFixture(os.Args[2], os.Args[3]))
	}
	os.Exit(m.Run())
}

func runProcessFixture(role, receipt string) int {
	if role == "leaf" {
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		<-ctx.Done()
		return 0
	}
	path, err := os.Executable()
	if err != nil {
		return 2
	}
	child := exec.Command(path, "artemis-process-fixture", "leaf", receipt)
	if err := child.Start(); err != nil {
		return 3
	}
	file, err := os.OpenFile(receipt, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return 4
	}
	_, writeErr := fmt.Fprintf(file, "%d", child.Process.Pid)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		return 5
	}
	// The adversarial leader exits while its real descendant remains alive.
	if _, err := fmt.Fprintln(os.Stdout, "process-fixture-version"); err != nil {
		return 6
	}
	return 0
}

func TestCompetitorVersionReapsOwnedDescendant(t *testing.T) {
	dir := t.TempDir()
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(dir, "lightpanda")
	// A hard link preserves one physical executable; no binary copy is created.
	if err := os.Link(path, linked); err != nil {
		t.Fatal(err)
	}
	digest, err := fileSHA256(linked)
	if err != nil {
		t.Fatal(err)
	}
	receipt := filepath.Join(dir, "descendant.pid")
	runner := NewCompetitorRunner(CompetitorConfig{BinaryDir: dir, ExpectedSHA256: digest, VersionArgs: []string{"artemis-process-fixture", "owner", receipt}})
	version, err := runner.Version(t.Context())
	if err != nil || version != "process-fixture-version" {
		t.Fatalf("version boundary: %q, %v", version, err)
	}
	data, err := os.ReadFile(receipt)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err == syscall.ESRCH {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && err != syscall.ESRCH {
		t.Error(err)
	}
	t.Fatal("owned descendant remained live after version command completed")
}
