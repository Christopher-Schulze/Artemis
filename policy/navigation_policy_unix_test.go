//go:build darwin || linux

package policy

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"golang.org/x/sys/unix"
)

func TestNavigationPolicyNativeOpenRejectsSymlinkAndCannotBlockOnFIFO(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.json")
	writePolicyFile(t, path, `{"version":1}`)
	link := filepath.Join(dir, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	file, err := openNavigationPolicyFile(link)
	if file != nil {
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if !errors.Is(err, unix.ELOOP) {
		t.Fatalf("native open followed a symlink: %v", err)
	}
	fifo := filepath.Join(dir, "fifo")
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	// A raced FIFO must not wait for a writer before its descriptor is rejected.
	file, err = openNavigationPolicyFile(fifo)
	if err != nil {
		t.Fatal(err)
	}
	info, statErr := file.Stat()
	closeErr := file.Close()
	if err := errors.Join(statErr, closeErr); err != nil {
		t.Fatal(err)
	}
	if info.Mode().IsRegular() {
		t.Fatal("FIFO claimed a regular-file descriptor")
	}
	policy, err := NewNavigationPolicy(NavigationPolicyConfig{Path: fifo})
	if err != nil {
		t.Fatal(err)
	}
	policy.Stop()
	if policy.CurrentPolicy() != nil || policy.LastReloadError() == nil {
		t.Fatal("FIFO policy admitted")
	}
}

func TestNavigationPolicyOwnedLoopsDoNotLeakNativeResources(t *testing.T) {
	baselineFDs, err := policyOpenFileDescriptors()
	if err != nil {
		t.Fatal(err)
	}
	baselineGoroutines := runtime.NumGoroutine()
	for range 32 {
		policy, _ := newFilePolicy(t, `{"version":1,"default":"deny"}`)
		for range 8 {
			policy.StartHotReload()
		}
		if err := policy.Reload(); err != nil {
			t.Fatal(err)
		}
		policy.Stop()
		select {
		case <-policy.doneCh:
		default:
			t.Fatal("owned loop was not joined")
		}
	}
	waitPolicy(t, func() bool {
		runtime.GC()
		currentFDs, err := policyOpenFileDescriptors()
		if err != nil {
			t.Fatal(err)
		}
		return currentFDs <= baselineFDs && runtime.NumGoroutine() <= baselineGoroutines
	})
}

func policyOpenFileDescriptors() (int, error) {
	var limit unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_NOFILE, &limit); err != nil {
		return 0, fmt.Errorf("read file descriptor limit: %w", err)
	}
	const scanLimit = uint64(4096)
	if limit.Cur > scanLimit {
		limit.Cur = scanLimit
	}
	open := 0
	for descriptor := uint64(0); descriptor < limit.Cur; descriptor++ {
		if _, err := unix.FcntlInt(uintptr(descriptor), unix.F_GETFD, 0); err == nil {
			open++
		}
	}
	return open, nil
}
