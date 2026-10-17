package platform

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path"
	"strconv"
	"strings"
)

type kernelFileReader func(context.Context, string, int64) ([]byte, error)

func observeCgroupLimits(ctx context.Context, membership, mounts []byte, read kernelFileReader) ResourceLimits {
	result := ResourceLimits{}
	memory, err := cgroupLocationFor(membership, mounts, "memory")
	if err != nil {
		result.MemoryCeiling, result.MemoryHeadroom = failedProbe("cgroup memory hierarchy", err), failedProbe("cgroup memory hierarchy", err)
	} else {
		result.MemoryCeiling, result.MemoryHeadroom = observeCgroupMemory(ctx, memory, read)
	}
	cpu, err := cgroupLocationFor(membership, mounts, "cpu")
	if err != nil {
		result.CPUQuota = failedProbe("cgroup CPU hierarchy", err)
	} else {
		result.CPUQuota = observeCgroupCPU(ctx, cpu, read)
	}
	return result
}

func cgroupAncestors(location cgroupLocation) ([]string, error) {
	var result []string
	for current := location.leaf; ; current = path.Dir(current) {
		if len(result) >= 128 || (current != location.mount && !strings.HasPrefix(current, strings.TrimSuffix(location.mount, "/")+"/")) {
			return nil, fmt.Errorf("cgroup hierarchy exceeds observable bounds")
		}
		result = append(result, current)
		if current == location.mount {
			return result, nil
		}
	}
}

func observeCgroupMemory(ctx context.Context, location cgroupLocation, read kernelFileReader) (ProbeResult, ProbeResult) {
	const source = "visible process cgroup memory ancestors"
	directories, err := cgroupAncestors(location)
	if err != nil {
		return failedProbe(source, err), failedProbe(source, err)
	}
	limit, headroom, seen := uint64(math.MaxUint64), uint64(math.MaxUint64), false
	for _, directory := range directories {
		maxName, currentName := "memory.max", "memory.current"
		if location.version == 1 {
			maxName, currentName = "memory.limit_in_bytes", "memory.usage_in_bytes"
		}
		data, err := read(ctx, path.Join(directory, maxName), 64)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return failedProbe(source, err), failedProbe(source, err)
		}
		maximum, unlimited, err := parseMemoryCeiling(data)
		if err != nil {
			return failedProbe(source, err), failedProbe(source, err)
		}
		seen = true
		if unlimited {
			continue
		}
		current, err := read(ctx, path.Join(directory, currentName), 64)
		if err != nil {
			return failedProbe(source, err), failedProbe(source, err)
		}
		used, err := strconv.ParseUint(strings.TrimSpace(string(current)), 10, 64)
		if err != nil {
			return failedProbe(source, err), failedProbe(source, err)
		}
		remaining := uint64(0)
		if used < maximum {
			remaining = maximum - used
		}
		limit, headroom = min(limit, maximum), min(headroom, remaining)
	}
	if !seen {
		err := fmt.Errorf("memory controller files not observable")
		return failedProbe(source, err), failedProbe(source, err)
	}
	if limit == math.MaxUint64 {
		probe := ProbeResult{State: StateUnavailable, Source: source, Reason: "all visible memory ceilings are unlimited; hidden ancestors are not proven"}
		return probe, probe
	}
	return ProbeResult{State: StateAvailable, Source: source, Reason: "minimum visible ancestor ceiling; not host RAM or a replacement budget", Value: limit}, ProbeResult{State: StateAvailable, Source: source, Reason: "minimum visible ceiling minus current usage; hidden ancestors remain kernel-enforced", Value: headroom}
}

func parseMemoryCeiling(data []byte) (uint64, bool, error) {
	value := strings.TrimSpace(string(data))
	if value == "max" {
		return 0, true, nil
	}
	limit, err := strconv.ParseUint(value, 10, 64)
	return limit, false, err
}

func observeCgroupCPU(ctx context.Context, location cgroupLocation, read kernelFileReader) ProbeResult {
	const source = "visible process cgroup CPU ancestors"
	directories, err := cgroupAncestors(location)
	if err != nil {
		return failedProbe(source, err)
	}
	minimum, seen := uint64(math.MaxUint64), false
	for _, directory := range directories {
		name := "cpu.max"
		if location.version == 1 {
			name = "cpu.cfs_quota_us"
		}
		data, err := read(ctx, path.Join(directory, name), 64)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return failedProbe(source, err)
		}
		if location.version == 1 {
			period, err := read(ctx, path.Join(directory, "cpu.cfs_period_us"), 64)
			if err != nil {
				return failedProbe(source, err)
			}
			data = []byte(strings.TrimSpace(string(data)) + " " + strings.TrimSpace(string(period)))
		}
		count, unlimited, err := parseCPUCeiling(data, location.version)
		if err != nil {
			return failedProbe(source, err)
		}
		seen = true
		if !unlimited {
			minimum = min(minimum, count)
		}
	}
	if !seen {
		return failedProbe(source, fmt.Errorf("CPU controller files not observable"))
	}
	if minimum == math.MaxUint64 {
		return ProbeResult{State: StateUnavailable, Source: source, Reason: "visible CPU quotas unlimited; allowed cpuset still applies"}
	}
	return ProbeResult{State: StateAvailable, Source: source, Reason: "minimum visible quota/period rounded up for runtime parallelism", Value: minimum}
}

func parseCPUCeiling(data []byte, version int) (uint64, bool, error) {
	fields := strings.Fields(string(data))
	if len(fields) != 2 {
		return 0, false, fmt.Errorf("invalid CPU quota pair")
	}
	period, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil || period == 0 {
		return 0, false, fmt.Errorf("invalid CPU quota period")
	}
	if (version == 2 && fields[0] == "max") || (version == 1 && fields[0] == "-1") {
		return 0, true, nil
	}
	quota, err := strconv.ParseUint(fields[0], 10, 64)
	if err != nil || quota == 0 {
		return 0, false, fmt.Errorf("invalid CPU quota")
	}
	return (quota-1)/period + 1, false, nil
}
