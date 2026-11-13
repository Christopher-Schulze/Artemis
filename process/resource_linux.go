//go:build linux

package process

import (
	"fmt"
	"time"
)

// newResourceSampler returns a per-browser Linux sampler. Linux ps reports
// %cpu as the average over each process lifetime, which right after a launch
// sums far above the real load of a fresh multi-process Chromium; the sampler
// therefore measures CPU seconds from /proc and reports the load since the
// previous sample. Only the supervise goroutine calls it.
func newResourceSampler() resourceSampler {
	var load cpuLoad
	return func(processGroupID int, profileDir string) (ResourceUsage, error) {
		cpuSeconds, memoryBytes, err := sampleProcfsGroup("/proc", processGroupID)
		if err != nil {
			return ResourceUsage{}, fmt.Errorf("sample process group: %w", err)
		}
		profileBytes, err := directorySize(profileDir)
		if err != nil {
			return ResourceUsage{}, fmt.Errorf("sample profile disk: %w", err)
		}
		return ResourceUsage{CPUPercent: load.percent(cpuSeconds, time.Now()), MemoryBytes: memoryBytes, ProfileDiskBytes: profileBytes}, nil
	}
}
