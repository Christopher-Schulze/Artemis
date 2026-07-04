package platform

import (
	"os"
	"runtime"
	"strings"
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
}

// Detect builds capabilities from GOOS/GOARCH.
func Detect() PlatformCapabilities {
	osName := runtime.GOOS
	arch := runtime.GOARCH
	caps := PlatformCapabilities{
		NumCPU:          runtime.NumCPU(),
		IsContainerized: isContainerized(),
	}
	switch osName {
	case "linux":
		caps.HasTCPFastOpen = true
		caps.HasCPUAffinity = true
		caps.HasMmap = true
		if arch == "amd64" || arch == "arm64" {
			caps.HasHWJPEGDecode = true
		}
	case "darwin":
		caps.HasMmap = true
		if arch == "arm64" {
			caps.HasHWJPEGDecode = true
		}
	}
	return caps
}

// isContainerized detects whether the process is running inside a
// container by checking for the existence of /.dockerenv (Docker),
// /.dockerinit (legacy Docker), or a cgroup v1/v2 mount line that
// mentions docker, containerd, kubepods, or lxc in /proc/1/cgroup
// (Linux only). On non-Linux platforms it returns false.
func isContainerized() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	// Docker creates /.dockerenv in every container.
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	// Legacy Docker.
	if _, err := os.Stat("/.dockerinit"); err == nil {
		return true
	}
	// Check /proc/1/cgroup for container runtime markers.
	data, err := os.ReadFile("/proc/1/cgroup")
	if err == nil {
		lower := strings.ToLower(string(data))
		for _, marker := range []string{"docker", "containerd", "kubepods", "lxc", "kubernetes"} {
			if strings.Contains(lower, marker) {
				return true
			}
		}
	}
	// Check /proc/self/mountinfo for overlay filesystem (common in containers).
	data, err = os.ReadFile("/proc/self/mountinfo")
	if err == nil {
		return strings.Contains(string(data), "overlay")
	}
	return false
}
