package platform

import (
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestDarwinMemoryMatchesIndependentKernelCommand(t *testing.T) {
	command := exec.CommandContext(t.Context(), "/usr/sbin/sysctl", "-n", "hw.memsize")
	command.WaitDelay = time.Second
	output, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	bytes, err := strconv.ParseUint(strings.TrimSpace(string(output)), 10, 64)
	if err != nil || bytes == 0 {
		t.Fatalf("independent memory observation: %q %v", output, err)
	}
	caps := Detect()
	if caps.Evidence.Memory.State != StateAvailable || caps.Evidence.Memory.Value != bytes || caps.TotalMemoryMB != bytes/(1024*1024) {
		t.Fatalf("kernel memory/units disagree: evidence=%+v MiB=%d expected bytes=%d", caps.Evidence.Memory, caps.TotalMemoryMB, bytes)
	}
}
