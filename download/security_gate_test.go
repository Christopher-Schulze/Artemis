package download

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestSecurityGateDownloadDiskExhaustionIsAtomic(t *testing.T) {
	manager := newTestManager(t, 64, 16, []string{"*/*"})
	for _, filename := range []string{"first.bin", "second.bin"} {
		if _, err := manager.Store(filename, "", []byte("12345678")); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := manager.Store("overflow.bin", "", []byte("x")); err == nil || !strings.Contains(err.Error(), "quota") {
		t.Fatalf("disk exhaustion error=%v", err)
	}
	usage, err := manager.DiskUsage()
	if err != nil {
		t.Fatal(err)
	}
	if usage != 16 {
		t.Fatalf("disk usage=%d, want 16", usage)
	}
	if _, err := os.Stat(filepath.Join(manager.Directory(), "overflow.bin")); !os.IsNotExist(err) {
		t.Fatalf("rejected download survived: %v", err)
	}
	assertNoPartialFiles(t, manager.Directory())
}

func TestSecurityGateConcurrentDownloadQuotaNeverExceedsBudget(t *testing.T) {
	manager := newTestManager(t, 64, 32, []string{"*/*"})
	const workers = 32
	var successes atomic.Int64
	errorsFound := make(chan error, workers)
	var wait sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		worker := worker
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := manager.Store(fmt.Sprintf("worker-%02d.bin", worker), "", []byte("data"))
			if err == nil {
				successes.Add(1)
				return
			}
			if !strings.Contains(err.Error(), "quota") {
				errorsFound <- err
			}
		}()
	}
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Fatalf("concurrent store: %v", err)
	}
	if got := successes.Load(); got != 8 {
		t.Fatalf("successful stores=%d, want 8", got)
	}
	usage, err := manager.DiskUsage()
	if err != nil {
		t.Fatal(err)
	}
	if usage != 32 {
		t.Fatalf("disk usage=%d, want exact quota 32", usage)
	}
	assertNoPartialFiles(t, manager.Directory())
}
