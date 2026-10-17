//go:build linux

package platform

import (
	"bytes"
	"context"
	"fmt"
	"runtime"
)

func observeResourceLimits(ctx context.Context) ResourceLimits {
	membership, err := readProbeFile(ctx, "/proc/self/cgroup", 64<<10)
	if err != nil {
		return unknownResourceLimits(err)
	}
	mounts, err := readProbeFile(ctx, "/proc/self/mountinfo", 64<<10)
	if err != nil {
		return unknownResourceLimits(err)
	}
	result := observeCgroupLimits(ctx, membership, mounts, readProbeFile)
	after, err := readProbeFile(ctx, "/proc/self/cgroup", 64<<10)
	if err != nil || !bytes.Equal(membership, after) {
		return unknownResourceLimits(fmt.Errorf("cgroup membership changed or became unavailable: %v", err))
	}
	result.AllowedCPUs = observeAllowedCPUs(ctx)
	return result
}

func observeAllowedCPUs(ctx context.Context) ProbeResult {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	set, _, err := readAffinityMask(ctx)
	if err != nil {
		return failedProbe("sched_getaffinity allowed CPUs", err)
	}
	if set.Count() == 0 {
		return failedProbe("sched_getaffinity allowed CPUs", fmt.Errorf("empty allowed set"))
	}
	return ProbeResult{State: StateAvailable, Source: "sched_getaffinity current thread", Reason: "actual allowed CPU count; no placement change", Value: uint64(set.Count())}
}
