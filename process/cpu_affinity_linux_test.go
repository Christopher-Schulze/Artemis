package process

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/platform"
)

func TestCPUAdmissionActualChromiumGuardianExecInheritance(t *testing.T) {
	allowed, proof := platform.AllowedCPUSet(t.Context())
	if proof.State != platform.StateAvailable || len(allowed) == 0 {
		t.Fatalf("actual cpuset unavailable: %+v", proof)
	}
	script := strings.Replace(browserReadyScript, `mkdir -p "$profile"`, "mkdir -p \"$profile\"\n/bin/cat /proc/self/status >\"$profile/cpu-status\"", 1)
	owner := &observedCPUAdmission{}
	browser, err := Launch(t.Context(), LaunchConfig{BinaryPath: writeBrowserScript(t, script), CPUAdmission: owner, CPUCount: 1, CPUAffinity: allowed[:1], StartupTimeout: 5 * time.Second, ShutdownTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := browser.Close(); err != nil {
			t.Error(err)
		}
	}()
	actual := browser.CPUAffinity()
	if actual.State != platform.StateAvailable || actual.Value != 1 {
		t.Fatalf("actual process-start affinity not proved: %+v", actual)
	}
	status, err := os.ReadFile(filepath.Join(browser.ProfileDir(), "cpu-status"))
	if err != nil {
		t.Fatal(err)
	}
	var cpus string
	for _, line := range strings.Split(string(status), "\n") {
		if value, ok := strings.CutPrefix(line, "Cpus_allowed_list:"); ok {
			cpus = strings.TrimSpace(value)
		}
	}
	if cpus != strconv.Itoa(allowed[0]) {
		t.Fatalf("guardian/browser child inherited wrong CPUs: %q", cpus)
	}
	if active, released := owner.counts(); active != 1 || released != 0 {
		t.Fatal("owned affinity process did not retain CPU capacity")
	}
}
