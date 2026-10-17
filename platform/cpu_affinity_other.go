//go:build !linux

package platform

import "context"

func allowedCPUSet(context.Context) ([]int, ProbeResult) {
	return nil, ProbeResult{State: StateUnavailable, Source: "CPU affinity backend", Reason: "this build has no CPU binding syscall backend"}
}

func runCPUAffinity(ctx context.Context, _ []int, operation func(context.Context) error) (ProbeResult, error) {
	return ProbeResult{State: StateUnavailable, Source: "CPU affinity backend", Reason: "ordinary execution without CPU placement on this build"}, invokeAffinityOperation(ctx, operation)
}
