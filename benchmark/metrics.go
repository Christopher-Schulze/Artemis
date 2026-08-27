package benchmark

import (
	"fmt"
)

// MetricKind names the dimensions measured for each scenario.
type MetricKind string

const (
	MetricWallMs     MetricKind = "wall_ms"
	MetricCPUMs      MetricKind = "cpu_ms"
	MetricAllocBytes MetricKind = "alloc_bytes"
	MetricAllocCount MetricKind = "alloc_count"
	MetricRSSBytes   MetricKind = "rss_bytes"
	MetricThroughput MetricKind = "throughput"
	MetricErrorRate  MetricKind = "error_rate"
)

// MetricSample is one scalar measurement for a given metric kind.
type MetricSample struct {
	Kind  MetricKind `json:"kind"`
	Value float64    `json:"value"`
	Unit  string     `json:"unit"`
}

// MetricSet is the collection of measurements for a single scenario run.
type MetricSet struct {
	WallMs     float64 `json:"wallMs"`
	CPUMs      float64 `json:"cpuMs"`
	AllocBytes int64   `json:"allocBytes"`
	AllocCount int64   `json:"allocCount"`
	RSSBytes   int64   `json:"rssBytes"`
	Throughput float64 `json:"throughput"` // pages/second
	ErrorRate  float64 `json:"errorRate"`  // 0..1
}

// sampleValue returns the scalar value for the requested metric kind.
func (m MetricSet) sampleValue(kind MetricKind) float64 {
	switch kind {
	case MetricWallMs:
		return m.WallMs
	case MetricCPUMs:
		return m.CPUMs
	case MetricAllocBytes:
		return float64(m.AllocBytes)
	case MetricAllocCount:
		return float64(m.AllocCount)
	case MetricRSSBytes:
		return float64(m.RSSBytes)
	case MetricThroughput:
		return m.Throughput
	case MetricErrorRate:
		return m.ErrorRate
	}
	return 0
}

// ToSamples returns the metric set as a slice of labeled samples.
func (m MetricSet) ToSamples() []MetricSample {
	return []MetricSample{
		{Kind: MetricWallMs, Value: m.WallMs, Unit: "ms"},
		{Kind: MetricCPUMs, Value: m.CPUMs, Unit: "ms"},
		{Kind: MetricAllocBytes, Value: float64(m.AllocBytes), Unit: "bytes"},
		{Kind: MetricAllocCount, Value: float64(m.AllocCount), Unit: "count"},
		{Kind: MetricRSSBytes, Value: float64(m.RSSBytes), Unit: "bytes"},
		{Kind: MetricThroughput, Value: m.Throughput, Unit: "pages/s"},
		{Kind: MetricErrorRate, Value: m.ErrorRate, Unit: "ratio"},
	}
}

// Throughput computes pages per second from wall time.
func (m MetricSet) ThroughputForPages(pages int) float64 {
	if m.WallMs <= 0 || pages <= 0 {
		return 0
	}
	return float64(pages) / (m.WallMs / 1000.0)
}

// MetricErrorRateFromBool returns 0 for success, 1 for failure.
func MetricErrorRateFromBool(ok bool) float64 {
	if ok {
		return 0
	}
	return 1
}

// Float64Ptr returns a pointer to a float64.
func Float64Ptr(v float64) *float64 {
	return &v
}

// Int64Ptr returns a pointer to an int64.
func Int64Ptr(v int64) *int64 {
	return &v
}

// FormatMetric returns a human-readable string for a metric value.
func FormatMetric(k MetricKind, v float64) string {
	switch k {
	case MetricWallMs, MetricCPUMs:
		return fmt.Sprintf("%.3f ms", v)
	case MetricAllocBytes, MetricRSSBytes:
		if v == 0 {
			return "0 B"
		}
		if v < 1024 {
			return fmt.Sprintf("%.0f B", v)
		}
		if v < 1024*1024 {
			return fmt.Sprintf("%.2f KB", v/1024)
		}
		return fmt.Sprintf("%.2f MB", v/(1024*1024))
	case MetricThroughput:
		return fmt.Sprintf("%.2f pages/s", v)
	case MetricErrorRate:
		return fmt.Sprintf("%.2f%%", v*100)
	default:
		return fmt.Sprintf("%.3f", v)
	}
}

// MetricKindFromString parses a metric kind string, returning an error for unknown kinds.
func MetricKindFromString(s string) (MetricKind, error) {
	switch s {
	case string(MetricWallMs):
		return MetricWallMs, nil
	case string(MetricCPUMs):
		return MetricCPUMs, nil
	case string(MetricAllocBytes):
		return MetricAllocBytes, nil
	case string(MetricAllocCount):
		return MetricAllocCount, nil
	case string(MetricRSSBytes):
		return MetricRSSBytes, nil
	case string(MetricThroughput):
		return MetricThroughput, nil
	case string(MetricErrorRate):
		return MetricErrorRate, nil
	default:
		return "", fmt.Errorf("unknown metric kind: %s", s)
	}
}
