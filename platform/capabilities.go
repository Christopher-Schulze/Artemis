package platform

import (
	"runtime"
	"time"

	"github.com/Christopher-Schulze/Artemis/internal/jpegcodec"
)

// PlatformCapabilities reports runtime feature availability (spec ss28.15.11 P8.5).
type PlatformCapabilities struct {
	HasTCPFastOpen  bool
	HasCPUAffinity  bool
	HasMmap         bool
	HasHWJPEGDecode bool
	NumCPU          int
	TotalMemoryMB   uint64
	IsContainerized bool
	Evidence        CapabilityEvidence
}

// Detect freezes current host observations and the linked JPEG implementation.
// Physical memory is not a session budget or a container memory limit.
func Detect() PlatformCapabilities {
	evidence := CapabilityEvidence{
		ObservedAt:  time.Now().UTC(),
		TCPFastOpen: probeTCPFastOpen(), CPUAffinity: probeCPUAffinity(),
		Mmap: probeMmap(), Memory: probeMemory(), Container: probeContainer(),
		CPU:          ProbeResult{State: StateAvailable, Source: "runtime.NumCPU", Reason: "runtime-reported logical processor count", Value: uint64(runtime.NumCPU())},
		HardwareJPEG: probeJPEG(),
	}
	return PlatformCapabilities{
		HasTCPFastOpen:  evidence.TCPFastOpen.State == StateAvailable,
		HasCPUAffinity:  evidence.CPUAffinity.State == StateAvailable,
		HasMmap:         evidence.Mmap.State == StateAvailable,
		HasHWJPEGDecode: evidence.HardwareJPEG.State == StateAvailable,
		NumCPU:          int(evidence.CPU.Value), TotalMemoryMB: evidence.Memory.Value / (1024 * 1024),
		IsContainerized: evidence.Container.State == StateAvailable && evidence.Container.Value == 1,
		Evidence:        evidence,
	}
}

// ProbeTCPFastOpen runs only the TCP Fast Open probe of Detect, for callers
// such as per-client HTTP transports that need no other capability.
func ProbeTCPFastOpen() ProbeResult {
	return probeTCPFastOpen()
}

// ProbeMmap runs only the anonymous mapping probe of Detect.
func ProbeMmap() ProbeResult {
	return probeMmap()
}

func probeJPEG() ProbeResult {
	backend := jpegcodec.Probe()
	result := ProbeResult{State: StateUnavailable, Source: "linked JPEG codec admission", Reason: backend.Reason}
	if backend.Path != "" {
		result.Source = backend.Path
	}
	if backend.ObservationFailed {
		result.State = StateUnknown
	}
	if backend.Available {
		result.State, result.Value = StateAvailable, 1
	}
	return result
}

// isContainerized detects whether the process is running inside a
// container by checking for the existence of /.dockerenv (Docker),
// /.dockerinit (legacy Docker), or a cgroup v1/v2 mount line that
// mentions docker, containerd, kubepods, or lxc in /proc/1/cgroup
// (Linux only). On non-Linux platforms it returns false.
func isContainerized() bool {
	result := probeContainer()
	return result.State == StateAvailable && result.Value == 1
}
