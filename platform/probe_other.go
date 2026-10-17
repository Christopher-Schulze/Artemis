//go:build !darwin && !linux

package platform

func probeMemory() ProbeResult {
	return ProbeResult{State: StateUnknown, Source: "physical memory backend", Reason: "this build has no physical memory observer"}
}

func probeCPUAffinity() ProbeResult {
	return ProbeResult{State: StateUnavailable, Source: "CPU affinity backend", Reason: "this build has no CPU binding syscall backend"}
}

func probeTCPFastOpen() ProbeResult {
	return ProbeResult{State: StateUnavailable, Source: "TCP Fast Open backend", Reason: "this build has no TCP Fast Open activation backend"}
}

func probeMmap() ProbeResult {
	return ProbeResult{State: StateUnavailable, Source: "mmap backend", Reason: "this build uses ordinary reads"}
}

func probeContainer() ProbeResult {
	return ProbeResult{State: StateUnknown, Source: "container observation", Reason: "this build has no container evidence observer"}
}
