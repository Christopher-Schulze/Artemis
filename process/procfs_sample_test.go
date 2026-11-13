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

// TestSampleProcfsGroupCountsSharedMemoryOnce is the second Linux CI
// regression: summed RSS counted shared pages once per Chromium process and
// crossed the 2 GiB budget; the group sum uses PSS, with RSS only as fallback.
func TestSampleProcfsGroupCountsSharedMemoryOnce(t *testing.T) {
	root := t.TempDir()
	pageSize := int64(os.Getpagesize())
	writeProcStat(t, root, "300", "chrome", 300, 1, 1, 100000)
	writeProcStat(t, root, "301", "chrome", 300, 1, 1, 100000)
	writeProcStat(t, root, "302", "chrome", 300, 1, 1, 7)
	for pid, rollup := range map[string]string{
		"300": "55c4e7a1d000-7ffd61bfe000 ---p 00000000 00:00 0 [rollup]\nRss:  400000 kB\nPss:   120000 kB\nPss_Anon: 90000 kB\n",
		"301": "Rss:  400000 kB\nPss:    80000 kB\n",
		"302": "Rss:  28 kB\n", // no Pss line: RSS fallback
	} {
		if err := os.WriteFile(filepath.Join(root, pid, "smaps_rollup"), []byte(rollup), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_, memoryBytes, err := sampleProcfsGroup(root, 300)
	if err != nil {
		t.Fatal(err)
	}
	if want := (120000+80000)*int64(1024) + 7*pageSize; memoryBytes != want {
		t.Fatalf("memory bytes %d, want PSS sum plus RSS fallback %d (summed RSS would be %d)", memoryBytes, want, 200007*pageSize)
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
