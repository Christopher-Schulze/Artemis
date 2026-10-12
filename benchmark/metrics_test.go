package benchmark

import "testing"

func TestMetricSetToSamples(t *testing.T) {
	m := MetricSet{
		WallMs:     10,
		CPUMs:      5,
		AllocBytes: 1024,
		AllocCount: 10,
		RSSBytes:   2048,
		Throughput: 100,
		ErrorRate:  0.05,
	}
	samples := m.ToSamples()
	if len(samples) != 7 {
		t.Fatalf("samples = %d, want 7", len(samples))
	}
	if samples[0].Value != 10 {
		t.Errorf("wall ms sample = %v, want 10", samples[0].Value)
	}
}

func TestMetricSetThroughputForPages(t *testing.T) {
	m := MetricSet{WallMs: 1000}
	if got := m.ThroughputForPages(1); got != 1.0 {
		t.Errorf("throughput = %v, want 1.0", got)
	}
	if got := m.ThroughputForPages(10); got != 10.0 {
		t.Errorf("throughput = %v, want 10.0", got)
	}
	if got := (MetricSet{WallMs: 0}.ThroughputForPages(1)); got != 0 {
		t.Error("zero wall time should yield zero throughput")
	}
}

func TestFormatMetric(t *testing.T) {
	if got := FormatMetric(MetricWallMs, 1.5); got != "1.500 ms" {
		t.Errorf("format wall ms = %q", got)
	}
	if got := FormatMetric(MetricAllocBytes, 1536); got != "1.50 KB" {
		t.Errorf("format bytes = %q", got)
	}
	if got := FormatMetric(MetricErrorRate, 0.05); got != "5.00%" {
		t.Errorf("format error rate = %q", got)
	}
}

func TestMetricKindFromString(t *testing.T) {
	kind, err := MetricKindFromString("wall_ms")
	if err != nil || kind != MetricWallMs {
		t.Fatalf("kind = %s, err = %v", kind, err)
	}
	if _, err := MetricKindFromString("unknown"); err == nil {
		t.Fatal("expected error for unknown metric kind")
	}
}
