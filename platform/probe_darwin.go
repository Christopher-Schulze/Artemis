package platform

import "golang.org/x/sys/unix"

func probeMemory() ProbeResult {
	const source = "sysctl hw.memsize"
	bytes, err := unix.SysctlUint64("hw.memsize")
	if err != nil {
		return failedProbe(source, err)
	}
	if bytes == 0 {
		return ProbeResult{State: StateUnknown, Source: source, Reason: "kernel reported zero physical memory"}
	}
	return ProbeResult{State: StateAvailable, Source: source, Reason: "kernel physical memory bytes; excludes session/container budgets", Value: bytes}
}

func probeCPUAffinity() ProbeResult {
	return ProbeResult{State: StateUnavailable, Source: "Darwin affinity backend", Reason: "no CPU binding syscall backend is linked"}
}

func probeTCPFastOpen() ProbeResult {
	return ProbeResult{State: StateUnavailable, Source: "Darwin TCP backend", Reason: "TCP Fast Open activation is restricted to the Linux backend"}
}

func probeContainer() ProbeResult {
	return ProbeResult{State: StateUnknown, Source: "Darwin container observation", Reason: "Linux cgroup/marker evidence is not available"}
}
