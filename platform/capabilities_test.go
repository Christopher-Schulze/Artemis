package platform

import (
	"encoding/json"
	"runtime"
	"testing"

	"github.com/Christopher-Schulze/Artemis/internal/jpegcodec"
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
	for _, observation := range []struct {
		flag   bool
		result ProbeResult
	}{
		{caps.HasTCPFastOpen, caps.Evidence.TCPFastOpen},
		{caps.HasCPUAffinity, caps.Evidence.CPUAffinity},
		{caps.HasMmap, caps.Evidence.Mmap},
		{caps.HasHWJPEGDecode, caps.Evidence.HardwareJPEG},
	} {
		if observation.flag != (observation.result.State == StateAvailable) || observation.result.Source == "" || observation.result.Reason == "" {
			t.Fatalf("flag has no proved support: %+v", observation)
		}
	}
	codec := jpegcodec.Probe()
	if caps.HasHWJPEGDecode != codec.Available || caps.Evidence.HardwareJPEG.Reason != codec.Reason {
		t.Fatalf("capability disagrees with actual codec admission: %+v; %+v", caps.Evidence.HardwareJPEG, codec)
	}
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		if caps.Evidence.Memory.State != StateAvailable || caps.TotalMemoryMB == 0 || caps.TotalMemoryMB != caps.Evidence.Memory.Value/(1024*1024) {
			t.Fatalf("physical memory absent or wrong units: %+v", caps.Evidence.Memory)
		}
	}
}

func TestIsContainerizedNonLinux(t *testing.T) {
	if runtime.GOOS == "linux" {
		result := probeContainer()
		if result.Source == "" || result.Reason == "" {
			t.Fatal("container detection has no evidence")
		}
		return
	}
	if isContainerized() {
		t.Error("isContainerized should return false on non-linux")
	}
}

func TestIsContainerizedReturnsBool(t *testing.T) {
	result := probeContainer()
	if result.Source == "" || result.Reason == "" || (result.State != StateAvailable && result.State != StateUnknown) {
		t.Fatalf("container observation has no explicit truth state: %+v", result)
	}
}

func TestPlatformCapabilitiesStruct(t *testing.T) {
	caps := Detect()
	data, err := json.Marshal(caps)
	if err != nil {
		t.Fatal(err)
	}
	var decoded PlatformCapabilities
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	second, err := json.Marshal(decoded)
	if err != nil || string(second) != string(data) || decoded.Evidence.ObservedAt.IsZero() {
		t.Fatalf("frozen snapshot lost evidence: %v", err)
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
