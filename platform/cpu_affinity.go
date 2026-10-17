package platform

import (
	"context"
	"errors"
	"fmt"
	"slices"
)

const maxAffinityCPUs = 65536

// CPUPlacement separates only browser work, never the embedding process's
// unrelated threads. Granted is supplied by the existing resource authority.
type CPUPlacement struct {
	Browser []int `json:"browser"`
	Worker  []int `json:"worker"`
}

// CompileCPUPlacement uses actual permitted IDs and never manufactures cores.
// Eight granted CPUs reproduce the 4/4 layout; smaller grants narrow both sets.
func CompileCPUPlacement(allowed []int, granted int) (CPUPlacement, error) {
	if granted < 2 || len(allowed) < 2 || len(allowed) > maxAffinityCPUs {
		return CPUPlacement{}, fmt.Errorf("CPU placement requires at least two permitted and granted CPUs")
	}
	isolated, err := validateAffinityCPUs(allowed)
	if err != nil {
		return CPUPlacement{}, err
	}
	count := min(granted, len(isolated), 8)
	middle := count / 2
	return CPUPlacement{Browser: slices.Clone(isolated[:middle]), Worker: slices.Clone(isolated[middle:count])}, nil
}

func validateAffinityCPUs(cpus []int) ([]int, error) {
	if len(cpus) == 0 || len(cpus) > maxAffinityCPUs {
		return nil, fmt.Errorf("bounded nonempty CPU set required")
	}
	isolated := slices.Clone(cpus)
	slices.Sort(isolated)
	for index, cpu := range isolated {
		if cpu < 0 || cpu >= maxAffinityCPUs || (index > 0 && isolated[index-1] == cpu) {
			return nil, fmt.Errorf("CPU IDs must be unique and within 0..65535")
		}
	}
	return isolated, nil
}

// AllowedCPUSet observes the current thread's effective cpuset without writing
// affinity. Failed/unsupported observations carry no invented CPU identities.
func AllowedCPUSet(ctx context.Context) ([]int, ProbeResult) {
	if ctx == nil {
		return nil, failedProbe("allowed CPU set", fmt.Errorf("context required"))
	}
	if err := ctx.Err(); err != nil {
		return nil, failedProbe("allowed CPU set", err)
	}
	return allowedCPUSet(ctx)
}

// RunCPUAffinity joins the real callback and restores placement before return.
// The caller owns capacity admission and retains it for the callback's full
// lifetime. A process-start callback passes its mask to fork/exec children.
func RunCPUAffinity(ctx context.Context, cpus []int, operation func(context.Context) error) (ProbeResult, error) {
	if ctx == nil || operation == nil {
		err := fmt.Errorf("CPU affinity context and operation required")
		return failedProbe("CPU affinity", err), err
	}
	if err := ctx.Err(); err != nil {
		return failedProbe("CPU affinity", err), err
	}
	if len(cpus) == 0 {
		return ProbeResult{State: StateUnavailable, Source: "CPU affinity", Reason: "no placement requested"}, invokeAffinityOperation(ctx, operation)
	}
	isolated, err := validateAffinityCPUs(cpus)
	if err != nil {
		return failedProbe("CPU affinity", err), err
	}
	return runCPUAffinity(ctx, isolated, operation)
}

func invokeAffinityOperation(ctx context.Context, operation func(context.Context) error) (returnErr error) {
	defer func() {
		if recover() != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("CPU affinity operation panicked"))
		}
	}()
	returnErr = operation(ctx)
	return errors.Join(returnErr, ctx.Err())
}
