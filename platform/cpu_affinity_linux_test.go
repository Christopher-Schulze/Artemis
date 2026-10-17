package platform

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestCPUAffinityActualThreadAndExecInheritance(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	allowed, observed := AllowedCPUSet(t.Context())
	if observed.State != StateAvailable || len(allowed) == 0 {
		t.Fatalf("actual cpuset observation failed: %+v", observed)
	}
	var child string
	evidence, err := RunCPUAffinity(t.Context(), allowed[:1], func(ctx context.Context) error {
		actual, proof := AllowedCPUSet(ctx)
		if proof.State != StateAvailable || !slices.Equal(actual, allowed[:1]) {
			return errors.New("callback did not execute on the requested actual CPU")
		}
		output, err := exec.CommandContext(ctx, "/bin/cat", "/proc/self/status").Output()
		child = string(output)
		return err
	})
	if err != nil || evidence.State != StateAvailable || evidence.Value != 1 {
		t.Fatalf("actual affinity failed: %+v %v", evidence, err)
	}
	var inherited string
	for _, line := range strings.Split(child, "\n") {
		if value, ok := strings.CutPrefix(line, "Cpus_allowed_list:"); ok {
			inherited = strings.TrimSpace(value)
		}
	}
	if inherited != strconv.Itoa(allowed[0]) {
		t.Fatalf("fork/exec did not inherit exact affinity: %q", inherited)
	}
	after, proof := AllowedCPUSet(t.Context())
	if proof.State != StateAvailable || !slices.Equal(after, allowed) {
		t.Fatal("worker placement contaminated the caller's thread")
	}
}

func TestCPUAffinityCancellationJoinsLiveCallback(t *testing.T) {
	allowed, proof := AllowedCPUSet(t.Context())
	if proof.State != StateAvailable || len(allowed) == 0 {
		t.Fatalf("actual cpuset unavailable: %+v", proof)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started, release, completed := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		_, err := RunCPUAffinity(ctx, allowed[:1], func(context.Context) error { close(started); <-release; return nil })
		completed <- err
	}()
	select {
	case <-started:
	case err := <-completed:
		t.Fatalf("worker failed before callback: %v", err)
	}
	cancel()
	select {
	case err := <-completed:
		t.Fatalf("live callback released early: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if err := <-completed; !errors.Is(err, context.Canceled) {
		t.Fatalf("callback settlement lost cancellation: %v", err)
	}
}

func TestCPUAffinityRejectsCPUOutsideActualAllowedSet(t *testing.T) {
	mask, size, err := readAffinityMask(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	outside := size
	for cpu := 0; cpu < size; cpu++ {
		if !mask.IsSet(cpu) {
			outside = cpu
			break
		}
	}
	called := false
	_, err = RunCPUAffinity(t.Context(), []int{outside}, func(context.Context) error { called = true; return nil })
	if err == nil || called {
		t.Fatal("CPU outside the actual permitted set executed work")
	}
}

func TestCPUAffinityCallbackFailureRestoresExactWorkerMask(t *testing.T) {
	allowed, observed := AllowedCPUSet(t.Context())
	if observed.State != StateAvailable || len(allowed) == 0 {
		t.Fatalf("cpuset observation failed: %+v", observed)
	}
	want := errors.New("real callback failure")
	var tid int
	evidence, err := RunCPUAffinity(t.Context(), allowed[:1], func(context.Context) error { tid = unix.Gettid(); return want })
	if !errors.Is(err, want) || evidence.State != StateAvailable {
		t.Fatalf("callback failure changed placement evidence: %+v %v", evidence, err)
	}
	actual := unix.NewCPUSet(maxAffinityCPUs)
	if err := unix.SchedGetaffinityDynamic(tid, actual); err != nil {
		if errors.Is(err, unix.ESRCH) {
			return
		}
		t.Fatalf("restored worker mask unavailable: %v", err)
	}
	if actual.Count() != len(allowed) {
		t.Fatalf("worker retained narrowed mask: count=%d want=%d", actual.Count(), len(allowed))
	}
	for _, cpu := range allowed {
		if !actual.IsSet(cpu) {
			t.Fatal("worker restoration lost an allowed CPU")
		}
	}
}

func TestCPUAffinityWorkerExitCannotStrandJoinedCaller(t *testing.T) {
	allowed, observed := AllowedCPUSet(t.Context())
	if observed.State != StateAvailable || len(allowed) == 0 {
		t.Fatalf("cpuset observation failed: %+v", observed)
	}
	evidence, err := RunCPUAffinity(t.Context(), allowed[:1], func(context.Context) error {
		runtime.Goexit()
		return nil
	})
	if err == nil || evidence.State == StateAvailable {
		t.Fatalf("worker exit reported success: %+v %v", evidence, err)
	}
}
