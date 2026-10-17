package platform

import (
	"context"
	"math"
	"runtime"
	"testing"
)

func TestProcessPeakMemoryUnitsAndBounds(t *testing.T) {
	for _, test := range []struct {
		name  string
		value int64
		unit  uint64
		want  uint64
		bad   bool
	}{
		{"bytes", 1234, 1, 1234, false}, {"KiB", 1234, 1024, 1234 * 1024, false},
		{"negative", -1, 1, 0, true}, {"zero", 0, 1, 0, true},
		{"zero_unit", 1, 0, 0, true}, {"overflow", math.MaxInt64, 1024, 0, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			value, err := peakRSSBytes(test.value, test.unit)
			if value != test.want || (err != nil) != test.bad {
				t.Fatalf("kernel units: %d %v", value, err)
			}
		})
	}
}

func TestProcessPeakMemoryUsesRealKernelObservation(t *testing.T) {
	first := ProcessPeakMemory(t.Context())
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" || runtime.GOOS == "windows" {
		if first.State != StateAvailable || first.Value == 0 {
			t.Fatalf("kernel process high-water unavailable: %+v", first)
		}
		second := ProcessPeakMemory(t.Context())
		if second.State != StateAvailable || second.Value < first.Value {
			t.Fatalf("lifetime high-water decreased: %+v -> %+v", first, second)
		}
	} else if first.State != StateUnavailable || first.Value != 0 {
		t.Fatal("unsupported process probe invented a measurement")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, ctx := range []context.Context{nil, ctx} {
		if failed := ProcessPeakMemory(ctx); failed.State != StateUnknown || failed.Value != 0 {
			t.Fatalf("invalid/canceled probe invented bytes: %+v", failed)
		}
	}
}
