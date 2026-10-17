//go:build darwin || linux

package platform

import (
	"runtime"

	"golang.org/x/sys/unix"
)

func processPeakMemory() ProbeResult {
	var usage unix.Rusage
	if err := unix.Getrusage(unix.RUSAGE_SELF, &usage); err != nil {
		return failedProbe("getrusage process lifetime peak RSS", err)
	}
	unit := uint64(1024)
	if runtime.GOOS == "darwin" {
		unit = 1
	}
	value, err := peakRSSBytes(usage.Maxrss, unit)
	if err != nil {
		return failedProbe("getrusage process lifetime peak RSS", err)
	}
	return ProbeResult{State: StateAvailable, Source: "getrusage process lifetime peak RSS", Reason: "kernel high-water for the whole current process, including earlier and concurrent work", Value: value}
}
