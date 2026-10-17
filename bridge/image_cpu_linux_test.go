//go:build linux

package bridge

import (
	"context"
	"fmt"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/Christopher-Schulze/Artemis/platform"
)

func TestImageWorkersBindActualLinuxThreads(t *testing.T) {
	allowed, proof := platform.AllowedCPUSet(t.Context())
	if proof.State != platform.StateAvailable || len(allowed) == 0 {
		t.Fatalf("actual Linux cpuset unavailable: %+v", proof)
	}
	mask := allowed[:min(2, len(allowed))]
	var observed atomic.Int32
	err := runImageWorkers(t.Context(), 2, 2, mask, func(ctx context.Context, first, step int) error {
		actual, observation := platform.AllowedCPUSet(ctx)
		if observation.State != platform.StateAvailable || !slices.Equal(actual, mask) {
			return fmt.Errorf("worker %d mask %v differs from admitted %v: %+v", first, actual, mask, observation)
		}
		observed.Add(1)
		return nil
	})
	if err != nil || observed.Load() != 2 {
		t.Fatalf("actual worker placement failed: %v observations=%d", err, observed.Load())
	}
}
