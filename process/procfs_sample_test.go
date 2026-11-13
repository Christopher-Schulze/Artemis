package process

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeProcStat(t *testing.T, root, pid, name string, group, utime, stime, rssPages int) {
	t.Helper()
	dir := filepath.Join(root, pid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// pid (comm) state ppid pgrp session tty tpgid flags minflt cminflt majflt cmajflt utime stime cutime cstime priority nice threads itrealvalue starttime vsize rss
	stat := fmt.Sprintf("%s (%s) S 1 %d %d 0 -1 4194560 10 0 0 0 %d %d 0 0 20 0 8 0 100 1000000 %d\n", pid, name, group, group, utime, stime, rssPages)
	if err := os.WriteFile(filepath.Join(dir, "stat"), []byte(stat), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSampleProcfsGroupSumsOnlyTheGroup(t *testing.T) {
	root := t.TempDir()
	writeProcStat(t, root, "100", "chrome", 100, 250, 50, 10)
	writeProcStat(t, root, "101", "chrome (renderer) x", 100, 30, 20, 5)
	writeProcStat(t, root, "200", "other", 200, 9000, 9000, 999)
	if err := os.MkdirAll(filepath.Join(root, "self"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "102"), 0o755); err != nil { // exited: no stat
		t.Fatal(err)
	}
	cpuSeconds, memoryBytes, err := sampleProcfsGroup(root, 100)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(cpuSeconds-3.5) > 1e-9 || memoryBytes != 15*int64(os.Getpagesize()) {
		t.Fatalf("cpu seconds %v memory %d, want 3.5 and 15 pages", cpuSeconds, memoryBytes)
	}
	if _, _, err := sampleProcfsGroup(filepath.Join(root, "missing"), 100); err == nil {
		t.Fatal("a missing procfs root was accepted")
	}
	writeProcStat(t, root, "103", "broken", 100, 1, 1, 1)
	if err := os.WriteFile(filepath.Join(root, "103", "stat"), []byte("103 (broken) S 1 100\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := sampleProcfsGroup(root, 100); err == nil {
		t.Fatal("a truncated stat line was accepted")
	}
}

// TestCPULoadReportsTheLoadSinceTheLastSample is the Linux CI regression: a
// fresh Chromium with a large lifetime CPU total must not read as a load
// spike; only CPU used between samples counts.
func TestCPULoadReportsTheLoadSinceTheLastSample(t *testing.T) {
	start := time.Unix(1000, 0)
	var load cpuLoad
	tests := []struct {
		cpuSeconds float64
		at         time.Duration
		want       float64
	}{
		{cpuSeconds: 40, at: 0, want: 0},                 // baseline, however large the startup total
		{cpuSeconds: 41.5, at: time.Second, want: 150},   // 1.5 CPU seconds in one second
		{cpuSeconds: 41.5, at: 2 * time.Second, want: 0}, // idle
		{cpuSeconds: 30, at: 3 * time.Second, want: 0},   // processes exited
		{cpuSeconds: 32, at: 3500 * time.Millisecond, want: 400},
	}
	for index, test := range tests {
		if got := load.percent(test.cpuSeconds, start.Add(test.at)); math.Abs(got-test.want) > 1e-9 {
			t.Fatalf("sample %d: %v percent, want %v", index, got, test.want)
		}
	}
}
