package platform

import (
	"runtime"
	"testing"
)

func TestDetectReturnsValidCapabilities(t *testing.T) {
	caps := Detect()
	if caps.NumCPU <= 0 {
		t.Errorf("NumCPU = %d, want > 0", caps.NumCPU)
	}
	if caps.NumCPU != runtime.NumCPU() {
		t.Errorf("NumCPU = %d, want %d", caps.NumCPU, runtime.NumCPU())
	}
}

func TestDetectOSCapabilities(t *testing.T) {
	caps := Detect()
	switch runtime.GOOS {
	case "linux":
		if !caps.HasTCPFastOpen {
			t.Error("HasTCPFastOpen should be true on linux")
		}
		if !caps.HasCPUAffinity {
			t.Error("HasCPUAffinity should be true on linux")
		}
		if !caps.HasMmap {
			t.Error("HasMmap should be true on linux")
		}
		if runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64" {
			if !caps.HasHWJPEGDecode {
				t.Error("HasHWJPEGDecode should be true on linux amd64/arm64")
			}
		}
	case "darwin":
		if !caps.HasMmap {
			t.Error("HasMmap should be true on darwin")
		}
		if runtime.GOARCH == "arm64" && !caps.HasHWJPEGDecode {
			t.Error("HasHWJPEGDecode should be true on darwin/arm64")
		}
	default:
		// Other OSes: just verify Detect doesn't panic.
	}
}

func TestIsContainerizedNonLinux(t *testing.T) {
	if runtime.GOOS == "linux" {
		// On linux, just verify it doesn't panic.
		_ = isContainerized()
		return
	}
	if isContainerized() {
		t.Error("isContainerized should return false on non-linux")
	}
}

func TestIsContainerizedReturnsBool(t *testing.T) {
	// Just verify it doesn't panic and returns a bool.
	_ = isContainerized()
}

func TestPlatformCapabilitiesStruct(t *testing.T) {
	caps := PlatformCapabilities{
		HasTCPFastOpen:  true,
		HasCPUAffinity:  true,
		HasMmap:         true,
		HasHWJPEGDecode: false,
		NumCPU:          4,
		TotalMemoryMB:   8192,
		IsContainerized: true,
	}
	if !caps.HasTCPFastOpen {
		t.Error("HasTCPFastOpen should be true")
	}
	if !caps.IsContainerized {
		t.Error("IsContainerized should be true")
	}
	if caps.NumCPU != 4 {
		t.Errorf("NumCPU = %d, want 4", caps.NumCPU)
	}
	if caps.TotalMemoryMB != 8192 {
		t.Errorf("TotalMemoryMB = %d, want 8192", caps.TotalMemoryMB)
	}
}

func BenchmarkDetect(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = Detect()
	}
}

func BenchmarkIsContainerized(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = isContainerized()
	}
}
