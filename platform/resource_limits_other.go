//go:build !linux

package platform

import "context"

func observeResourceLimits(context.Context) ResourceLimits {
	observation := ProbeResult{State: StateUnavailable, Source: "process cgroup constraints", Reason: "no cgroup observer on this build; explicit ceilings remain authoritative"}
	return ResourceLimits{AllowedCPUs: observation, CPUQuota: observation, MemoryCeiling: observation, MemoryHeadroom: observation}
}
