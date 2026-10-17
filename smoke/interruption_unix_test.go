//go:build darwin || linux

package smoke

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRunAllActualChildInterruptionCannotPublishOrRetainOwners(t *testing.T) {
	parent := t.TempDir()
	runner := prepareEvidenceRunner(t, buildRealSmokeArtemis(t), parent)
	binaryDirectory := runner.binaryDir
	matrix, _ := loadSmokeLifecycle(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	var runErr error
	go func() { _, runErr = runner.RunAll(ctx, matrix); close(done) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			t.Error("interrupted RunAll did not join")
		}
	})
	var child *os.Process
	deadline := time.Now().Add(10 * time.Second)
	for child == nil && time.Now().Before(deadline) {
		command := exec.CommandContext(t.Context(), "ps", "-axo", "pid=,ppid=,comm=")
		output, err := command.Output()
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(output), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 3 || fields[1] != strconv.Itoa(os.Getpid()) || filepath.Base(fields[2]) != "artemis" {
				continue
			}
			pid, err := strconv.Atoi(fields[0])
			if err != nil {
				t.Fatal(err)
			}
			child, err = os.FindProcess(pid)
			if err != nil {
				t.Fatal(err)
			}
		}
		if child == nil {
			time.Sleep(5 * time.Millisecond)
		}
	}
	if child == nil {
		t.Fatal("actual child was never observed")
	}
	defer func() {
		if err := child.Release(); err != nil {
			t.Error(err)
		}
	}()
	if err := child.Signal(syscall.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	// Suspend only the positively identified child, then interrupt the actual
	// run and resume it so pending termination and all owned waits can settle.
	cancel()
	if err := child.Signal(syscall.SIGCONT); err != nil && !errors.Is(err, os.ErrProcessDone) && !errors.Is(err, syscall.ESRCH) {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("actual interrupted child/run did not join")
	}
	if !errors.Is(runErr, context.Canceled) {
		t.Fatalf("interruption lost: %v", runErr)
	}
	if err := runner.Close(); err != nil {
		t.Fatal(err)
	}
	if err := child.Signal(syscall.Signal(0)); err == nil {
		t.Fatal("actual interrupted child remains alive")
	}
	if _, err := os.Stat(binaryDirectory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("interruption retained temporary binary: %v", err)
	}
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) != 0 {
		t.Fatalf("interruption published/retained partial output: %v %+v", err, entries)
	}
}
