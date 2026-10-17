package platform

import (
	"context"
	"fmt"
	"time"
)

// ResourceLimits is a fresh observation, not a second budget authority.
// CPUQuota uses rounded-up logical CPUs; memory fields use bytes.
type ResourceLimits struct {
	ObservedAt     time.Time   `json:"observedAt"`
	AllowedCPUs    ProbeResult `json:"allowedCPUs"`
	CPUQuota       ProbeResult `json:"cpuQuota"`
	MemoryCeiling  ProbeResult `json:"memoryCeiling"`
	MemoryHeadroom ProbeResult `json:"memoryHeadroom"`
}

// ObserveResourceLimits reads current process constraints without changing
// affinity, cgroups, runtime settings or any file.
func ObserveResourceLimits(ctx context.Context) ResourceLimits {
	if ctx == nil {
		return unknownResourceLimits(fmt.Errorf("resource limits: context required"))
	}
	if err := ctx.Err(); err != nil {
		return unknownResourceLimits(err)
	}
	observedAt := time.Now().UTC()
	result := observeResourceLimits(ctx)
	if err := ctx.Err(); err != nil {
		return unknownResourceLimits(err)
	}
	result.ObservedAt = observedAt
	return result
}

func unknownResourceLimits(err error) ResourceLimits {
	observation := failedProbe("process resource constraints", err)
	return ResourceLimits{ObservedAt: time.Now().UTC(), AllowedCPUs: observation, CPUQuota: observation, MemoryCeiling: observation, MemoryHeadroom: observation}
}

// ConstrainResources only narrows explicit canonical ceilings. Unknown host
// observations never supply additional capacity. Linux constraint read failure
// closes transient memory admission while retaining a bounded heap fallback.
func ConstrainResources(caps PlatformCapabilities, observed ResourceLimits, configuredCPU int, configuredMemory int64) (cpu int, memory int64, transientMemory int64, err error) {
	if configuredCPU <= 0 || configuredMemory <= 0 {
		return 0, 0, 0, fmt.Errorf("positive configured CPU and memory ceilings required")
	}
	cpu, memory = configuredCPU, configuredMemory
	if caps.NumCPU > 0 && caps.Evidence.CPU.State == StateAvailable {
		cpu = min(cpu, caps.NumCPU)
	}
	for _, probe := range []ProbeResult{observed.AllowedCPUs, observed.CPUQuota} {
		if probe.State == StateAvailable && probe.Value > 0 && probe.Value < uint64(cpu) {
			cpu = int(probe.Value)
		} else if probe.State == StateUnknown {
			cpu = 1
		}
	}
	if caps.Evidence.Memory.State == StateAvailable && caps.TotalMemoryMB > 0 && caps.Evidence.Memory.Value > 0 {
		memory = int64(min(uint64(memory), caps.Evidence.Memory.Value))
	}
	if observed.MemoryCeiling.State == StateAvailable {
		memory = int64(min(uint64(memory), observed.MemoryCeiling.Value))
	}
	transientMemory = memory
	if observed.MemoryCeiling.State == StateUnknown || observed.MemoryHeadroom.State == StateUnknown || (caps.IsContainerized && observed.MemoryCeiling.State != StateAvailable) {
		transientMemory = 0
	} else if observed.MemoryHeadroom.State == StateAvailable {
		transientMemory = int64(min(uint64(transientMemory), observed.MemoryHeadroom.Value))
	}
	return cpu, memory, transientMemory, nil
}
