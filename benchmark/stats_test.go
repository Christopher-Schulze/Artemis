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

func TestRegressionBudgetAdversarialCases(t *testing.T) {
	cases := []struct {
		name                            string
		kind                            MetricKind
		baseline, current, delta, ratio float64
		want                            bool
	}{
		{"zero relative baseline", MetricWallMs, 0, 1, 0, 0.05, false},
		{"zero unchanged baseline", MetricWallMs, 0, 0, 0, 0.05, true},
		{"zero absolute allowed", MetricWallMs, 0, 1, 2, 0, true},
		{"zero absolute exceeded", MetricWallMs, 0, 3, 2, 0, false},
		{"throughput improvement", MetricThroughput, 100, 120, 0, 0.05, true},
		{"throughput regression", MetricThroughput, 100, 80, 0, 0.05, false},
		{"non-finite metric", MetricWallMs, 100, math.Inf(1), 1, 0, false},
		{"invalid metric", "unknown", 100, 100, 1, 0, false},
		{"negative bound", MetricWallMs, 100, 100, -1, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			budget := RegressionBudget{MetricKind: tc.kind, MaxDelta: tc.delta, MaxPercent: tc.ratio}
			got, reason := budget.Check(tc.baseline, tc.current)
			if got != tc.want || (!got && reason == "") {
				t.Fatalf("budget = %v, %q; want %v", got, reason, tc.want)
			}
		})
	}
}
