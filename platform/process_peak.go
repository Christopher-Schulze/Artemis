package platform

import (
	"context"
	"fmt"
	"math"
)

// ProcessPeakMemory reports the kernel-observed lifetime resident-memory high
// water of this process. It includes other concurrent work and is not an
// operation-attributed allocation, reservation, Go-heap or GPU-memory metric.
func ProcessPeakMemory(ctx context.Context) ProbeResult {
	if ctx == nil {
		return failedProbe("process lifetime peak RSS", fmt.Errorf("context required"))
	}
	if err := ctx.Err(); err != nil {
		return failedProbe("process lifetime peak RSS", err)
	}
	return processPeakMemory()
}

func peakRSSBytes(value int64, unit uint64) (uint64, error) {
	if value <= 0 || unit == 0 || uint64(value) > math.MaxUint64/unit {
		return 0, fmt.Errorf("invalid or overflowing process peak RSS")
	}
	return uint64(value) * unit, nil
}
