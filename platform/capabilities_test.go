package platform

import (
	"runtime"
	"testing"
)

func TestPlatformCapabilities(t *testing.T) {
	caps := Detect()
	if caps.NumCPU <= 0 {
		t.Fatal("NumCPU must be positive")
	}
	if runtime.GOOS == "linux" && !caps.HasMmap {
		t.Fatal("linux must report mmap")
	}
	if runtime.GOOS == "linux" && !caps.HasTCPFastOpen {
		t.Fatal("linux must report TFO")
	}
}
