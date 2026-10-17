package platform

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func probeMemory() ProbeResult {
	const source = "sysinfo totalram * mem_unit"
	var info unix.Sysinfo_t
	if err := unix.Sysinfo(&info); err != nil {
		return failedProbe(source, err)
	}
	bytes, err := physicalMemoryBytes(uint64(info.Totalram), uint64(info.Unit))
	if err != nil {
		return failedProbe(source, err)
	}
	return ProbeResult{State: StateAvailable, Source: source, Reason: "kernel physical memory bytes; excludes session/container budgets", Value: bytes}
}

func probeCPUAffinity() ProbeResult {
	const source = "sched_getaffinity/sched_setaffinity current thread"
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	set, size, err := readAffinityMask(context.Background())
	if err != nil {
		return failedProbe(source, err)
	}
	if err := unix.SchedSetaffinityDynamic(0, set); err != nil {
		return failedProbe(source, err)
	}
	actual := unix.NewCPUSet(size)
	if err := unix.SchedGetaffinityDynamic(0, actual); err != nil || !slices.Equal(actual, set) {
		return failedProbe(source, fmt.Errorf("unchanged CPU affinity readback mismatch: %v", err))
	}
	return ProbeResult{State: StateAvailable, Source: source, Reason: "existing allowed CPU set successfully reapplied without changing affinity", Value: 1}
}

func probeTCPFastOpen() (result ProbeResult) {
	const source = "tcp_fastopen client bit and TCP_FASTOPEN_CONNECT"
	data, err := readProbeFile(context.Background(), "/proc/sys/net/ipv4/tcp_fastopen", 64)
	if err != nil {
		return failedProbe(source, err)
	}
	flags, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 32)
	if err != nil {
		return failedProbe(source, err)
	}
	if flags&1 == 0 {
		return ProbeResult{State: StateUnavailable, Source: source, Reason: "kernel TCP Fast Open client mode is disabled"}
	}
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, unix.IPPROTO_TCP)
	if err != nil {
		return failedProbe(source, err)
	}
	defer func() {
		if err := unix.Close(fd); err != nil {
			result = failedProbe(source, fmt.Errorf("close owned probe socket: %w", err))
		}
	}()
	if err := unix.SetsockoptInt(fd, unix.IPPROTO_TCP, unix.TCP_FASTOPEN_CONNECT, 1); err != nil {
		if errors.Is(err, unix.ENOPROTOOPT) || errors.Is(err, unix.EOPNOTSUPP) {
			return ProbeResult{State: StateUnavailable, Source: source, Reason: "kernel rejects TCP_FASTOPEN_CONNECT"}
		}
		return failedProbe(source, err)
	}
	value, err := unix.GetsockoptInt(fd, unix.IPPROTO_TCP, unix.TCP_FASTOPEN_CONNECT)
	if err != nil || value != 1 {
		return failedProbe(source, fmt.Errorf("socket option readback %d: %v", value, err))
	}
	return ProbeResult{State: StateAvailable, Source: source, Reason: "client mode enabled and owned unconnected socket option verified; no packet sent", Value: 1}
}

func probeContainer() ProbeResult {
	const source = "Linux container markers and /proc/1/cgroup"
	for _, path := range []string{"/.dockerenv", "/.dockerinit"} {
		if _, err := os.Stat(path); err == nil {
			return ProbeResult{State: StateAvailable, Source: source, Reason: "container runtime marker exists", Value: 1}
		} else if !errors.Is(err, os.ErrNotExist) {
			return failedProbe(source, err)
		}
	}
	data, err := readProbeFile(context.Background(), "/proc/1/cgroup", 64*1024)
	if err != nil {
		return failedProbe(source, err)
	}
	if containerCgroup(data) {
		return ProbeResult{State: StateAvailable, Source: source, Reason: "container runtime marker in cgroup membership", Value: 1}
	}
	return ProbeResult{State: StateUnknown, Source: source, Reason: "no container marker found; cgroup namespaces may hide host membership"}
}
