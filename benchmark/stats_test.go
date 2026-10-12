package benchmark

import (
	"math"
	"testing"
)

func TestComputeAggregate(t *testing.T) {
	vals := []float64{1, 2, 3, 4, 5}
	agg := ComputeAggregate(vals)
	if agg.Count != 5 {
		t.Errorf("count = %d", agg.Count)
	}
	if math.Abs(agg.Mean-3.0) > 1e-9 {
		t.Errorf("mean = %v", agg.Mean)
	}
	if agg.Min != 1 || agg.Max != 5 {
		t.Errorf("min/max = %v %v", agg.Min, agg.Max)
	}
	if agg.Median != 3 {
		t.Errorf("median = %v", agg.Median)
	}
}

func TestComputeAggregateEmpty(t *testing.T) {
	agg := ComputeAggregate(nil)
	if agg.Count != 0 {
		t.Errorf("count = %d", agg.Count)
	}
}

func TestRegressionBudgetCheck(t *testing.T) {
	b := RegressionBudget{MetricKind: MetricWallMs, MaxPercent: 0.05}
	ok, msg := b.Check(100, 103)
	if !ok {
		t.Errorf("expected ok, got %s", msg)
	}
	ok, msg = b.Check(100, 110)
	if ok {
		t.Errorf("expected fail, got %s", msg)
	}
}

func TestRegressionBudgetMaxDelta(t *testing.T) {
	b := RegressionBudget{MetricKind: MetricAllocBytes, MaxDelta: 10}
	ok, msg := b.Check(100, 105)
	if !ok {
		t.Errorf("expected ok, got %s", msg)
	}
	ok, msg = b.Check(100, 120)
	if ok {
		t.Errorf("expected fail, got %s", msg)
	}
}

func TestPercentChange(t *testing.T) {
	if got := PercentChange(100, 110); math.Abs(got-0.1) > 1e-9 {
		t.Errorf("percent change = %v", got)
	}
}
