package process

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// procfsClockTicks is the Linux USER_HZ that scales utime/stime in
// /proc/<pid>/stat; it is 100 on every Linux architecture Artemis supports.
const procfsClockTicks = 100

// sampleProcfsGroup sums the cumulative CPU seconds and resident memory of
// every live process in one process group from a procfs root. A process that
// exits while the tree is read no longer counts.
func sampleProcfsGroup(root string, processGroupID int) (float64, int64, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return 0, 0, fmt.Errorf("read %s: %w", root, err)
	}
	pageSize := int64(os.Getpagesize())
	var cpuSeconds float64
	var memoryBytes int64
	for _, entry := range entries {
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		stat, err := os.ReadFile(filepath.Join(root, entry.Name(), "stat"))
		if err != nil {
			continue
		}
		// The command name may contain spaces and parentheses; the
		// numeric fields start after its last closing parenthesis.
		end := bytes.LastIndexByte(stat, ')')
		if end < 0 {
			return 0, 0, fmt.Errorf("parse %s/%s/stat: no command name", root, entry.Name())
		}
		fields := strings.Fields(string(stat[end+1:]))
		if len(fields) < 22 {
			return 0, 0, fmt.Errorf("parse %s/%s/stat: %d fields", root, entry.Name(), len(fields))
		}
		group, err := strconv.Atoi(fields[2])
		if err != nil || group != processGroupID {
			continue
		}
		utime, userErr := strconv.ParseUint(fields[11], 10, 64)
		stime, systemErr := strconv.ParseUint(fields[12], 10, 64)
		rssPages, rssErr := strconv.ParseInt(fields[21], 10, 64)
		if userErr != nil || systemErr != nil || rssErr != nil {
			return 0, 0, fmt.Errorf("parse %s/%s/stat: invalid cpu or rss field", root, entry.Name())
		}
		cpuSeconds += float64(utime+stime) / procfsClockTicks
		// RSS counts the Chromium binary, libraries and shared memory once
		// per process; PSS shares them out, so the group sum is real usage.
		if pss, ok := procfsPSS(filepath.Join(root, entry.Name(), "smaps_rollup")); ok {
			memoryBytes += pss
		} else {
			memoryBytes += rssPages * pageSize
		}
	}
	return cpuSeconds, memoryBytes, nil
}

// procfsPSS reads the proportional set size from a smaps_rollup file. It
// reports false when the file is missing (the process exited or the kernel
// predates smaps_rollup) or carries no Pss line.
func procfsPSS(path string) (int64, bool) {
	rollup, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(rollup), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 && fields[0] == "Pss:" && fields[2] == "kB" {
			kib, err := strconv.ParseInt(fields[1], 10, 64)
			if err != nil {
				return 0, false
			}
			return kib * 1024, true
		}
	}
	return 0, false
}

// cpuLoad turns cumulative process-group CPU seconds into the CPU percent
// used since the previous sample (100 means one fully busy core). The first
// sample only sets the baseline; a total that drops because processes exited
// reads as no load for that interval.
type cpuLoad struct {
	cpuSeconds float64
	at         time.Time
}

func (l *cpuLoad) percent(cpuSeconds float64, now time.Time) float64 {
	previous := *l
	*l = cpuLoad{cpuSeconds: cpuSeconds, at: now}
	if previous.at.IsZero() || cpuSeconds <= previous.cpuSeconds {
		return 0
	}
	elapsed := now.Sub(previous.at).Seconds()
	if elapsed <= 0 {
		return 0
	}
	return (cpuSeconds - previous.cpuSeconds) / elapsed * 100
}
