package platform

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"slices"

	"golang.org/x/sys/unix"
)

func readAffinityMask(ctx context.Context) (unix.CPUSetDynamic, int, error) {
	for size := 1024; size <= maxAffinityCPUs; size *= 2 {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		mask := unix.NewCPUSet(size)
		if err := unix.SchedGetaffinityDynamic(0, mask); err != nil {
			if errors.Is(err, unix.EINVAL) {
				continue
			}
			return nil, 0, err
		}
		if mask.Count() == 0 {
			return nil, 0, fmt.Errorf("empty effective CPU set")
		}
		return mask, size, nil
	}
	return nil, 0, fmt.Errorf("kernel CPU affinity mask exceeds bounded capacity")
}

func allowedCPUSet(ctx context.Context) ([]int, ProbeResult) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	mask, size, err := readAffinityMask(ctx)
	if err != nil {
		return nil, failedProbe("sched_getaffinity effective CPUs", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, failedProbe("sched_getaffinity effective CPUs", err)
	}
	cpus := make([]int, 0, mask.Count())
	for cpu := 0; cpu < size; cpu++ {
		if mask.IsSet(cpu) {
			cpus = append(cpus, cpu)
		}
	}
	return cpus, ProbeResult{State: StateAvailable, Source: "sched_getaffinity effective CPUs", Reason: "actual permitted CPU IDs; no placement change", Value: uint64(len(cpus))}
}

type cpuAffinityOutcome struct {
	evidence ProbeResult
	err      error
}

func runCPUAffinity(ctx context.Context, cpus []int, operation func(context.Context) error) (ProbeResult, error) {
	result := make(chan cpuAffinityOutcome, 1)
	done := make(chan struct{})
	go func() {
		unexpected := fmt.Errorf("CPU affinity worker exited without an operation result")
		outcome := cpuAffinityOutcome{evidence: failedProbe("owned CPU affinity thread", unexpected), err: unexpected}
		defer func() { result <- outcome; close(done) }()
		evidence, err := runAffinityThread(ctx, cpus, operation)
		outcome = cpuAffinityOutcome{evidence: evidence, err: err}
	}()
	outcome := <-result
	<-done
	return outcome.evidence, outcome.err
}

func runAffinityThread(ctx context.Context, cpus []int, operation func(context.Context) error) (evidence ProbeResult, returnErr error) {
	const source = "owned thread sched_getaffinity/sched_setaffinity/readback"
	runtime.LockOSThread()
	original, size, err := readAffinityMask(ctx)
	if err != nil {
		runtime.UnlockOSThread()
		return failedProbe(source, err), err
	}
	// LockOSThread creates the clean runtime template before any mutation.
	// Failed restoration quarantines this thread until its goroutine exits.
	defer func() {
		if err := restoreAffinityMask(context.WithoutCancel(ctx), original, size); err != nil {
			returnErr = errors.Join(returnErr, err)
			evidence = failedProbe(source, returnErr)
			return
		}
		runtime.UnlockOSThread()
	}()
	evidence, err = activateAffinityMask(ctx, original, size, cpus)
	if err != nil {
		return evidence, err
	}
	return evidence, invokeAffinityOperation(ctx, operation)
}

func activateAffinityMask(ctx context.Context, original unix.CPUSetDynamic, size int, cpus []int) (ProbeResult, error) {
	const source = "owned thread sched_getaffinity/sched_setaffinity/readback"
	target := unix.NewCPUSet(size)
	for _, cpu := range cpus {
		if cpu >= size || !original.IsSet(cpu) {
			err := fmt.Errorf("requested CPU is outside the current effective cpuset")
			return failedProbe(source, err), err
		}
		target.Set(cpu)
	}
	if err := ctx.Err(); err != nil {
		return failedProbe(source, err), err
	}
	if err := unix.SchedSetaffinityDynamic(0, target); err != nil {
		return failedProbe(source, err), err
	}
	actual := unix.NewCPUSet(size)
	if err := unix.SchedGetaffinityDynamic(0, actual); err != nil || !slices.Equal(actual, target) {
		err := fmt.Errorf("CPU affinity activation readback mismatch: %v", err)
		return failedProbe(source, err), err
	}
	if err := ctx.Err(); err != nil {
		return failedProbe(source, err), err
	}
	return ProbeResult{State: StateAvailable, Source: source, Reason: "exact permitted CPU mask executed; restoration readback required", Value: uint64(len(cpus))}, nil
}

func restoreAffinityMask(ctx context.Context, original unix.CPUSetDynamic, size int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := unix.SchedSetaffinityDynamic(0, original); err != nil {
		return fmt.Errorf("restore CPU affinity: %w", err)
	}
	actual := unix.NewCPUSet(size)
	if err := unix.SchedGetaffinityDynamic(0, actual); err != nil || !slices.Equal(actual, original) {
		return fmt.Errorf("CPU affinity restoration readback mismatch: %v", err)
	}
	return nil
}
