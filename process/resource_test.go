package process

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
)

func TestResourceBudgetEnforcesEveryMeasuredDimension(t *testing.T) {
	budget := ResourceBudget{MaxCPUPercent: 100, MaxMemoryBytes: 200, MaxProfileDiskBytes: 300}
	for _, test := range []struct {
		name  string
		usage ResourceUsage
	}{
		{name: "cpu", usage: ResourceUsage{CPUPercent: 100.01}},
		{name: "memory", usage: ResourceUsage{MemoryBytes: 201}},
		{name: "profile", usage: ResourceUsage{ProfileDiskBytes: 301}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := budget.exceeded(test.usage); err == nil {
				t.Fatalf("usage %+v accepted", test.usage)
			}
		})
	}
	if err := budget.exceeded(ResourceUsage{CPUPercent: 100, MemoryBytes: 200, ProfileDiskBytes: 300}); err != nil {
		t.Fatalf("exact limits rejected: %v", err)
	}
}

// A browser's profile changes all the time. A file that disappears between being listed and
// being measured must not fail a sample, because a failed sample ends the browser.
func TestDirectorySizeToleratesFilesThatComeAndGo(t *testing.T) {
	root := t.TempDir()
	kept := filepath.Join(root, "kept")
	if err := os.WriteFile(kept, []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	var churn sync.WaitGroup
	for worker := range 4 {
		churn.Go(func() {
			directory := filepath.Join(root, "cache"+strconv.Itoa(worker))
			file := filepath.Join(directory, "journal")
			for {
				select {
				case <-stop:
					return
				default:
				}
				if err := os.MkdirAll(directory, 0o700); err != nil {
					t.Error(err)
					return
				}
				if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
					t.Error(err)
					return
				}
				if err := os.RemoveAll(directory); err != nil {
					t.Error(err)
					return
				}
			}
		})
	}
	var sampleErr error
	for range 3000 {
		size, err := directorySize(root)
		if err != nil {
			sampleErr = err
			break
		}
		if size < 5 {
			sampleErr = fmt.Errorf("the file that stayed was not counted: %d bytes", size)
			break
		}
	}
	close(stop)
	churn.Wait()
	if sampleErr != nil {
		t.Fatal(sampleErr)
	}
	if _, err := directorySize(filepath.Join(root, "absent")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a profile that is gone altogether: %v", err)
	}
}
