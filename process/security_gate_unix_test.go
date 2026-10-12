//go:build darwin || linux

package process

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestSecurityGateOwnedBrowserMemoryAndDiskExhaustion(t *testing.T) {
	tests := []struct {
		name            string
		script          string
		budget          ResourceBudget
		resourceSampler resourceSampler
	}{
		{
			name:   "memory",
			script: browserReadyScript,
			budget: ResourceBudget{MaxMemoryBytes: 1, SampleInterval: 5 * time.Millisecond},
			resourceSampler: func(int, string) (ResourceUsage, error) {
				return ResourceUsage{MemoryBytes: 2}, nil
			},
		},
		{
			name:   "profile disk",
			script: browserWritesProfileScript,
			budget: ResourceBudget{MaxProfileDiskBytes: 1024, SampleInterval: 5 * time.Millisecond},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			browser, err := Launch(context.Background(), LaunchConfig{
				BinaryPath: writeBrowserScript(t, test.script), Headless: true,
				StartupTimeout: 5 * time.Second, ShutdownTimeout: time.Second,
				ResourceBudget: test.budget, resourceSampler: test.resourceSampler,
			})
			if err != nil {
				t.Fatal(err)
			}
			profile := browser.ProfileDir()
			select {
			case <-browser.Done():
			case <-time.After(3 * time.Second):
				t.Fatal("owned browser survived resource exhaustion")
			}
			if err := browser.Err(); !IsCode(err, ErrorResourceBudget) {
				t.Fatalf("resource exhaustion error=%v", err)
			}
			if err := browser.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(profile); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("resource breach retained disposable profile: %v", err)
			}
		})
	}
}

func TestSecurityGateOwnedBrowserHasNoGoroutineFileDescriptorOrProfileLeak(t *testing.T) {
	baselineFDs, err := processSecurityGateOpenFileDescriptors()
	if err != nil {
		t.Fatal(err)
	}
	baselineGoroutines := runtime.NumGoroutine()
	for iteration := 0; iteration < 10; iteration++ {
		browser, err := Launch(context.Background(), LaunchConfig{
			BinaryPath: writeBrowserScript(t, browserReadyScript), Headless: true,
			StartupTimeout: 5 * time.Second, ShutdownTimeout: time.Second,
		})
		if err != nil {
			t.Fatal(err)
		}
		profile := browser.ProfileDir()
		if err := browser.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(profile); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("iteration %d retained disposable profile: %v", iteration, err)
		}
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		runtime.GC()
		currentFDs, err := processSecurityGateOpenFileDescriptors()
		if err != nil {
			t.Fatal(err)
		}
		currentGoroutines := runtime.NumGoroutine()
		if currentFDs <= baselineFDs+2 && currentGoroutines <= baselineGoroutines+2 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("browser lifecycle leaked resources: fds=%d baseline=%d goroutines=%d baseline=%d", currentFDs, baselineFDs, currentGoroutines, baselineGoroutines)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func processSecurityGateOpenFileDescriptors() (int, error) {
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
