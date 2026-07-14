//go:build windows

package benchmark

import (
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modpsapi                 = windows.NewLazySystemDLL("psapi.dll")
	procGetProcessMemoryInfo = modpsapi.NewProc("GetProcessMemoryInfo")
)

type processMemoryCounters struct {
	cb                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
}

func currentRusage() (rusage, error) {
	h := windows.CurrentProcess()
	var counters processMemoryCounters
	counters.cb = uint32(unsafe.Sizeof(counters))
	r1, _, err := procGetProcessMemoryInfo.Call(uintptr(h), uintptr(unsafe.Pointer(&counters)), uintptr(counters.cb))
	if r1 == 0 {
		return rusage{}, err
	}

	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &creation, &exit, &kernel, &user); err != nil {
		return rusage{}, err
	}

	cpuMs := float64(user.Nanoseconds()+kernel.Nanoseconds()) / 1e6
	return rusage{CPUMs: cpuMs, RSSBytes: int64(counters.WorkingSetSize)}, nil
}

func rusageDiff(start, end rusage) rusage {
	cpu := end.CPUMs - start.CPUMs
	if cpu < 0 {
		cpu = 0
	}
	rss := end.RSSBytes
	if rss < 0 {
		rss = 0
	}
	return rusage{CPUMs: cpu, RSSBytes: rss}
}

func measureFunc(f func() error) (MetricSet, error) {
	start, err := currentRusage()
	if err != nil {
		return MetricSet{}, err
	}
	t0 := time.Now()
	if err := f(); err != nil {
		return MetricSet{}, err
	}
	wall := float64(time.Since(t0).Microseconds()) / 1000.0
	end, err := currentRusage()
	if err != nil {
		return MetricSet{}, err
	}
	diff := rusageDiff(start, end)
	return MetricSet{
		WallMs:     wall,
		CPUMs:      diff.CPUMs,
		RSSBytes:   diff.RSSBytes,
		Throughput: MetricSet{WallMs: wall}.ThroughputForPages(1),
	}, nil
}
