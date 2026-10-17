package platform

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"time"
)

// CapabilityState separates proved support from failed observation and fallback.
type CapabilityState string

const (
	StateAvailable   CapabilityState = "available"
	StateUnavailable CapabilityState = "unavailable"
	StateDegraded    CapabilityState = "degraded"
	StateUnknown     CapabilityState = "unknown"
)

// ProbeResult carries a numeric observation, its authority and failure reason.
// Memory uses bytes, CPU uses logical processors, and boolean probes use 0/1.
type ProbeResult struct {
	State  CapabilityState `json:"state"`
	Source string          `json:"source"`
	Reason string          `json:"reason"`
	Value  uint64          `json:"value"`
}

// CapabilityEvidence is a value-owned snapshot; reads do not repeat OS probes.
type CapabilityEvidence struct {
	ObservedAt   time.Time   `json:"observedAt"`
	TCPFastOpen  ProbeResult `json:"tcpFastOpen"`
	CPUAffinity  ProbeResult `json:"cpuAffinity"`
	Mmap         ProbeResult `json:"mmap"`
	HardwareJPEG ProbeResult `json:"hardwareJPEG"`
	CPU          ProbeResult `json:"cpu"`
	Memory       ProbeResult `json:"memory"`
	Container    ProbeResult `json:"container"`
}

func failedProbe(source string, err error) ProbeResult {
	return ProbeResult{State: StateUnknown, Source: source, Reason: err.Error()}
}

func readProbeFile(ctx context.Context, path string, limit int64) (data []byte, returnErr error) {
	if ctx == nil || limit <= 0 || limit > 64*1024 {
		return nil, fmt.Errorf("probe context and limit within 1..65536 required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("probe requires a regular kernel file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { returnErr = errors.Join(returnErr, file.Close()) }()
	opened, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return nil, fmt.Errorf("probe file changed while opening")
	}
	data, err = io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("probe input exceeds %d bytes", limit)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return data, nil
}

func physicalMemoryBytes(ram, unit uint64) (uint64, error) {
	if ram == 0 || unit == 0 || ram > math.MaxUint64/unit {
		return 0, fmt.Errorf("invalid or overflowing kernel memory units")
	}
	return ram * unit, nil
}

func containerCgroup(data []byte) bool {
	lower := strings.ToLower(string(data))
	for _, marker := range []string{"/docker/", "docker-", "kubepods", "/containerd/", "/lxc/"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
