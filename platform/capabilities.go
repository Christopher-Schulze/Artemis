package platform

import (
	"runtime"
)

// PlatformCapabilities reports runtime feature availability (spec ss28.15.11 P8.5).
type PlatformCapabilities struct {
	HasTCPFastOpen   bool
	HasCPUAffinity   bool
	HasMmap          bool
	HasHWJPEGDecode  bool
	NumCPU           int
	TotalMemoryMB    uint64
	IsContainerized  bool
}

// Detect builds capabilities from GOOS/GOARCH.
func Detect() PlatformCapabilities {
	os := runtime.GOOS
	arch := runtime.GOARCH
	caps := PlatformCapabilities{
		NumCPU:          runtime.NumCPU(),
		IsContainerized: isContainerized(),
	}
	switch os {
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

func isContainerized() bool {
	return false
}
