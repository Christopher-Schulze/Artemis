package benchmark

import (
	"fmt"
	"math"
	"sort"
)

// Aggregate holds basic descriptive statistics for a slice of samples.
type Aggregate struct {
	Count  int     `json:"count"`
	Mean   float64 `json:"mean"`
	StdDev float64 `json:"stddev"`
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
	Median float64 `json:"median"`
	P95    float64 `json:"p95"`
	P99    float64 `json:"p99"`
}

// ComputeAggregate returns descriptive statistics for values.
func ComputeAggregate(values []float64) Aggregate {
	if len(values) == 0 {
		return Aggregate{}
	}
	if len(values) == 1 {
		return Aggregate{
			Count:  1,
			Mean:   values[0],
			StdDev: 0,
			Min:    values[0],
			Max:    values[0],
			Median: values[0],
			P95:    values[0],
			P99:    values[0],
		}
	}

	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)

	sum := 0.0
	min := sorted[0]
	max := sorted[len(sorted)-1]
	for _, v := range sorted {
		sum += v
	}
	mean := sum / float64(len(sorted))

	var varianceSum float64
	for _, v := range sorted {
		d := v - mean
		varianceSum += d * d
	}
	stddev := math.Sqrt(varianceSum / float64(len(sorted)))

	return Aggregate{
		Count:  len(sorted),
		Mean:   mean,
		StdDev: stddev,
		Min:    min,
		Max:    max,
		Median: percentile(sorted, 0.5),
		P95:    percentile(sorted, 0.95),
		P99:    percentile(sorted, 0.99),
	}
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}
	idx := p * float64(len(sorted)-1)
	lower := int(math.Floor(idx))
	upper := int(math.Ceil(idx))
	if lower == upper {
		return sorted[lower]
	}
	frac := idx - float64(lower)
	return sorted[lower] + frac*(sorted[upper]-sorted[lower])
}

// RegressionBudget defines a percentage of allowable regression for a metric.
type RegressionBudget struct {
	MetricKind MetricKind `json:"metricKind"`
	MaxDelta   float64    `json:"maxDelta"`   // absolute delta
	MaxPercent float64    `json:"maxPercent"` // relative delta as ratio, e.g. 0.05 = 5%
}

// Check returns true if the new value is within the budget relative to the baseline.
func (b RegressionBudget) Check(baseline, current float64) (bool, string) {
	if baseline == 0 {
		if b.MaxDelta == 0 {
			return true, "no baseline and no max delta"
		}
		if current <= b.MaxDelta {
			return true, ""
		}
		return false, fmt.Sprintf("%s current=%s exceeds maxDelta=%s with zero baseline", b.MetricKind, FormatMetric(b.MetricKind, current), FormatMetric(b.MetricKind, b.MaxDelta))
	}

	delta := current - baseline
	if b.MaxDelta != 0 && delta > b.MaxDelta {
		return false, fmt.Sprintf("%s delta=%s exceeds maxDelta=%s", b.MetricKind, FormatMetric(b.MetricKind, delta), FormatMetric(b.MetricKind, b.MaxDelta))
	}

	if b.MaxPercent != 0 && current/baseline > 1+b.MaxPercent {
		return false, fmt.Sprintf("%s regressed %.2f%%, budget %.2f%%", b.MetricKind, (current/baseline-1)*100, b.MaxPercent*100)
	}
	return true, ""
}

// PercentChange returns the relative change from baseline to current.
func PercentChange(baseline, current float64) float64 {
	if baseline == 0 {
		return math.Inf(1)
	}
	return (current - baseline) / baseline
}
