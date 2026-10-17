//go:build darwin || linux

package process

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/platform"
)

type observedCPUAdmission struct {
	mu         sync.Mutex
	active     int
	releases   int
	requested  int
	denied     error
	releaseErr error
	ctx        context.Context
}

func (a *observedCPUAdmission) ReserveCPU(ctx context.Context, count int) (context.Context, func() error, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.denied != nil {
		return nil, nil, a.denied
	}
	a.active += count
	a.requested = count
	if a.ctx != nil {
		ctx = a.ctx
	}
	return ctx, func() error {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.active -= count
		a.releases++
		return a.releaseErr
	}, nil
}

func (a *observedCPUAdmission) counts() (int, int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.active, a.releases
}

func TestCPUAdmissionHeldThroughActualProcessExit(t *testing.T) {
	for _, test := range []struct {
		name           string
		cancel         bool
		crash          bool
		leaseCancel    bool
		foreignContext bool
	}{
		{"close", false, false, false, false},
		{"owner_cancellation", true, false, false, false},
		{"independent_owner_context", true, false, false, true},
		{"reservation_preemption", true, false, true, false},
		{"crash", false, true, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			owner := &observedCPUAdmission{}
			if test.foreignContext {
				owner.ctx = t.Context()
			}
			if test.leaseCancel {
				leaseCtx, leaseCancel := context.WithCancel(t.Context())
				defer leaseCancel()
				owner.ctx = leaseCtx
				cancel = leaseCancel
			}
			script := browserReadyScript
			if test.crash {
				script = browserCrashAfterReadyScript
			}
			browser, err := Launch(ctx, LaunchConfig{BinaryPath: writeBrowserScript(t, script), CPUAdmission: owner, CPUCount: 2, StartupTimeout: 5 * time.Second, ShutdownTimeout: time.Second})
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := browser.Close(); err != nil {
					t.Error(err)
				}
			}()
			if active, released := owner.counts(); active != 2 || released != 0 {
				t.Fatalf("live process reservation: active=%d released=%d", active, released)
			}
			if browser.CPUAffinity().State != platform.StateUnavailable {
				t.Fatal("ordinary process launch forged affinity")
			}
			if test.cancel {
				cancel()
			} else if !test.crash {
				if err := browser.Close(); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case <-browser.Done():
			case <-time.After(5 * time.Second):
				t.Fatal("owned process cleanup did not finish")
			}
			stopped, err := processGroupCPUStopped(t.Context(), browser.cmd.Process.Pid)
			if err != nil || !stopped {
				t.Fatalf("CPU producer survived cleanup: %v", err)
			}
			if active, released := owner.counts(); active != 0 || released != 1 {
				t.Fatalf("joined reservation: active=%d released=%d", active, released)
			}
		})
	}
}

func TestCPUAdmissionRejectsBeforeProfileOrProcessAllocation(t *testing.T) {
	want := errors.New("CPU capacity denied")
	profile := filepath.Join(t.TempDir(), "unallocated")
	owner := &observedCPUAdmission{denied: want}
	browser, err := Launch(t.Context(), LaunchConfig{BinaryPath: writeBrowserScript(t, browserReadyScript), UserDataDir: profile, CPUAdmission: owner})
	if browser != nil || !errors.Is(err, want) {
		t.Fatalf("admission rejection lost: %+v %v", browser, err)
	}
	if _, err := os.Stat(profile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("denied admission allocated a profile: %v", err)
	}
	var nilOwner *observedCPUAdmission
	for _, config := range []LaunchConfig{
		{RequireCPUAdmission: true}, {CPUAdmission: nilOwner}, {CPUCount: 1}, {CPUAffinity: []int{0}},
	} {
		config.BinaryPath = writeBrowserScript(t, browserReadyScript)
		if browser, err := Launch(t.Context(), config); err == nil || browser != nil {
			t.Fatalf("unowned CPU launch accepted: %v", err)
		}
	}
}

func TestCPUAdmissionRollbackAndReleaseFailure(t *testing.T) {
	for _, test := range []struct {
		name           string
		registration   bool
		releaseFailure bool
	}{
		{"profile_failure", false, false},
		{"registration_failure", true, false},
		{"release_failure", true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			want := errors.New("CPU release failed")
			owner := &observedCPUAdmission{}
			if test.releaseFailure {
				owner.releaseErr = want
			}
			config := LaunchConfig{BinaryPath: writeBrowserScript(t, browserReadyScript), CPUAdmission: owner, StartupTimeout: 5 * time.Second, ShutdownTimeout: time.Second}
			if test.registration {
				config.ChildProcessRegistrar = rejectingChildRegistrar{}
			} else {
				config.UserDataDir = config.BinaryPath
			}
			browser, err := Launch(t.Context(), config)
			if err == nil || browser != nil {
				t.Fatalf("failed launch reported success: %v", err)
			}
			if test.releaseFailure && !errors.Is(err, want) {
				t.Fatalf("cleanup error lost: %v", err)
			}
			if active, released := owner.counts(); active != 0 || released != 1 {
				t.Fatalf("failed launch retained/duplicated CPU: active=%d released=%d", active, released)
			}
		})
	}
}

func TestCPUExitObservationRejectsUnknownAndKeepsLiveGroups(t *testing.T) {
	for _, test := range []struct {
		name    string
		output  string
		stopped bool
		failed  bool
	}{
		{"running", "10 10 R\n", false, false},
		{"sleeping_helper", "10 10 Z\n11 10 S\n", false, false},
		{"stopped_can_resume", "10 10 T\n", false, false},
		{"zombies_only", "10 10 Z\n11 10 ZN\n", true, false},
		{"absent_group", "12 12 S\n", true, false},
		{"empty", "", false, true},
		{"malformed", "10 R\n", false, true},
		{"unknown_identity", "unknown 10 S\n", false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			stopped, err := parseProcessGroupCPUStopped(test.output, 10)
			if stopped != test.stopped || (err != nil) != test.failed {
				t.Fatalf("wrong group exit decision: stopped=%v err=%v", stopped, err)
			}
		})
	}
	for _, test := range []struct {
		name      string
		parts     []string
		truncated bool
	}{
		{"exact_bound", []string{"1234"}, false},
		{"single_overflow", []string{"12345"}, true},
		{"multiple_overflow", []string{"12", "345"}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			output := newCappedOutput(4)
			for _, part := range test.parts {
				if _, err := output.Write([]byte(part)); err != nil {
					t.Fatal(err)
				}
			}
			if output.Truncated() != test.truncated || len(output.String()) > 4 {
				t.Fatal("bounded output concealed snapshot truncation")
			}
		})
	}
}

func TestCPUAdmissionUnknownExitRetainsReservation(t *testing.T) {
	command := exec.CommandContext(t.Context(), "/bin/sleep", "30")
	ConfigureProcessGroup(command)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := SignalProcessGroup(command.Process, syscall.SIGKILL); err != nil {
			t.Error(err)
		}
	}()
	released := false
	owner := &Browser{cmd: command, shutdown: 0, releaseCPU: func() error { released = true; return nil }}
	if err := owner.settleCPU(); err == nil || released {
		t.Fatalf("unknown exit released CPU capacity: %v", err)
	}
	if err := SignalProcessGroup(command.Process, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	var exitError *exec.ExitError
	if err := command.Wait(); err == nil || !errors.As(err, &exitError) {
		t.Fatalf("owned child termination was not observed: %v", err)
	}
	owner.shutdown = time.Second
	if err := owner.settleCPU(); err != nil || !released {
		t.Fatalf("proved exit retained capacity: %v", err)
	}
}
