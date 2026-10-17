//go:build windows

package platform

import (
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

type processMemoryCounters struct {
	Size                       uint32
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

func processPeakMemory() ProbeResult {
	var counters processMemoryCounters
	counters.Size = uint32(unsafe.Sizeof(counters))
	procedure := windows.NewLazySystemDLL("psapi.dll").NewProc("GetProcessMemoryInfo")
	if err := procedure.Find(); err != nil {
		return failedProbe("GetProcessMemoryInfo process lifetime peak RSS", err)
	}
	result, _, callErr := procedure.Call(uintptr(windows.CurrentProcess()), uintptr(unsafe.Pointer(&counters)), uintptr(counters.Size))
	runtime.KeepAlive(&counters)
	if result == 0 {
		return failedProbe("GetProcessMemoryInfo process lifetime peak RSS", fmt.Errorf("memory observation failed: %w", callErr))
	}
	if counters.PeakWorkingSetSize == 0 {
		return failedProbe("GetProcessMemoryInfo process lifetime peak RSS", fmt.Errorf("kernel returned no peak working set"))
	}
	return ProbeResult{State: StateAvailable, Source: "GetProcessMemoryInfo process lifetime peak RSS", Reason: "kernel peak working set for the whole current process, including earlier and concurrent work", Value: uint64(counters.PeakWorkingSetSize)}
}
