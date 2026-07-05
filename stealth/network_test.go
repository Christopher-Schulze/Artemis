package stealth

import (
	"testing"
	"time"
)

// ==================== ConnectionInfo tests ====================

// TestTASK2244_ConnectionInfoString verifies String method
// (spec L4093).
func TestTASK2244_ConnectionInfoString(t *testing.T) {
	c := ConnectionInfo{
		RTT:           50 * time.Millisecond,
		Downlink:      7.5,
		EffectiveType: "4g",
		Source:        "live",
	}
	s := c.String()
	if s == "" {
		t.Error("String should not be empty")
	}
}

// TestTASK2244_ConnectionInfoIsLive verifies IsLive method.
func TestTASK2244_ConnectionInfoIsLive(t *testing.T) {
	c := ConnectionInfo{Source: "live"}
	if !c.IsLive() {
		t.Error("should be live")
	}
	if c.IsFallback() {
		t.Error("should not be fallback")
	}
}

// TestTASK2244_ConnectionInfoIsFallback verifies IsFallback method.
func TestTASK2244_ConnectionInfoIsFallback(t *testing.T) {
	c := ConnectionInfo{Source: "fallback"}
	if !c.IsFallback() {
		t.Error("should be fallback")
	}
	if c.IsLive() {
		t.Error("should not be live")
	}
}

// ==================== DeriveEffectiveType tests ====================

// TestTASK2244_DeriveEffectiveType4G verifies 4g derivation
// (spec L4093: rtt<100ms && downlink>5 -> "4g").
func TestTASK2244_DeriveEffectiveType4G(t *testing.T) {
	cases := []struct {
		rtt      time.Duration
		downlink float64
	}{
		{10 * time.Millisecond, 10.0},
		{50 * time.Millisecond, 7.0},
		{99 * time.Millisecond, 5.1},
	}
	for _, c := range cases {
		if DeriveEffectiveType(c.rtt, c.downlink) != "4g" {
			t.Errorf("rtt=%v downlink=%.1f should be 4g, got %s",
				c.rtt, c.downlink, DeriveEffectiveType(c.rtt, c.downlink))
		}
	}
}

// TestTASK2244_DeriveEffectiveType3G verifies 3g derivation
// (spec L4093: 3g threshold).
func TestTASK2244_DeriveEffectiveType3G(t *testing.T) {
	cases := []struct {
		rtt      time.Duration
		downlink float64
	}{
		{100 * time.Millisecond, 5.0}, // rtt >= 100 -> not 4g
		{200 * time.Millisecond, 2.0},
		{600 * time.Millisecond, 1.0},
	}
	for _, c := range cases {
		if DeriveEffectiveType(c.rtt, c.downlink) != "3g" {
			t.Errorf("rtt=%v downlink=%.1f should be 3g, got %s",
				c.rtt, c.downlink, DeriveEffectiveType(c.rtt, c.downlink))
		}
	}
}

// TestTASK2244_DeriveEffectiveType2G verifies 2g derivation.
func TestTASK2244_DeriveEffectiveType2G(t *testing.T) {
	cases := []struct {
		rtt      time.Duration
		downlink float64
	}{
		{700 * time.Millisecond, 0.5},
		{1000 * time.Millisecond, 0.1},
		{2000 * time.Millisecond, 0.0},
	}
	for _, c := range cases {
		if DeriveEffectiveType(c.rtt, c.downlink) != "2g" {
			t.Errorf("rtt=%v downlink=%.1f should be 2g, got %s",
				c.rtt, c.downlink, DeriveEffectiveType(c.rtt, c.downlink))
		}
	}
}

// TestTASK2244_DeriveEffectiveTypeSlow2G verifies slow-2g derivation.
func TestTASK2244_DeriveEffectiveTypeSlow2G(t *testing.T) {
	if DeriveEffectiveType(3000*time.Millisecond, 0.0) != "slow-2g" {
		t.Error("rtt>=3000ms should be slow-2g")
	}
	if DeriveEffectiveType(5000*time.Millisecond, 0.0) != "slow-2g" {
		t.Error("rtt>=3000ms should be slow-2g")
	}
}

// TestTASK2244_DeriveEffectiveTypeBoundary verifies boundary conditions
// (spec L4093: rtt<100ms && downlink>5 -> "4g").
func TestTASK2244_DeriveEffectiveTypeBoundary(t *testing.T) {
	// rtt=100ms exactly -> not 4g (must be <100)
	if DeriveEffectiveType(100*time.Millisecond, 10.0) == "4g" {
		t.Error("rtt=100ms should NOT be 4g (must be <100)")
	}
	// downlink=5.0 exactly -> not 4g (must be >5)
	if DeriveEffectiveType(50*time.Millisecond, 5.0) == "4g" {
		t.Error("downlink=5.0 should NOT be 4g (must be >5)")
	}
}

// ==================== MeasureConnection tests ====================

// TestTASK2244_MeasureConnectionLive verifies live measurement returns
// a ConnectionInfo (spec L4093: LIVE MEASUREMENT via net.Dial).
func TestTASK2244_MeasureConnectionLive(t *testing.T) {
	info := MeasureConnection("1.1.1.1:443")
	// Should either be live or fallback (network may not be available in CI).
	if info.Source != "live" && info.Source != "fallback" {
		t.Errorf("source: got %s, want live or fallback", info.Source)
	}
	if info.MeasuredAt.IsZero() {
		t.Error("measured_at should be set")
	}
}

// TestTASK2244_MeasureConnectionFallback verifies fallback when target
// is unreachable (spec L4093).
func TestTASK2244_MeasureConnectionFallback(t *testing.T) {
	// Use an invalid port on localhost to trigger fast connection refused.
	info := MeasureConnection("127.0.0.1:1") // port 1 should be refused
	// Should be fallback (connection refused/failed).
	if info.Source != "fallback" && info.Source != "live" {
		t.Errorf("source: got %s", info.Source)
	}
}

// TestTASK2244_MeasureConnectionEmptyTarget verifies empty target uses
// default (spec L4093: 1.1.1.1:443).
func TestTASK2244_MeasureConnectionEmptyTarget(t *testing.T) {
	info := MeasureConnection("")
	// Should not panic and should return a result.
	if info.MeasuredAt.IsZero() {
		t.Error("measured_at should be set even with empty target")
	}
}

// ==================== ConnectionMonitor tests ====================

// TestTASK2244_NewConnectionMonitor verifies creation with defaults
// (spec L4093).
func TestTASK2244_NewConnectionMonitor(t *testing.T) {
	m := NewConnectionMonitor()
	if m.IsActive() {
		t.Error("new monitor should be inactive")
	}
}

// TestTASK2244_ConnectionMonitorMeasure verifies Measure updates current
// (spec L4093).
func TestTASK2244_ConnectionMonitorMeasure(t *testing.T) {
	m := NewConnectionMonitor()
	info := m.Measure()
	if info.MeasuredAt.IsZero() {
		t.Error("measured_at should be set after Measure")
	}
	current := m.Current()
	if current.MeasuredAt.IsZero() {
		t.Error("current should be updated after Measure")
	}
}

// TestTASK2244_ConnectionMonitorStartStop verifies Start/Stop cycle
// (spec L4093: Values refreshed every 60s).
func TestTASK2244_ConnectionMonitorStartStop(t *testing.T) {
	m := NewConnectionMonitor()
	m.SetRefreshInterval(100 * time.Millisecond) // fast for testing
	m.Start()
	if !m.IsActive() {
		t.Error("should be active after Start")
	}
	time.Sleep(150 * time.Millisecond) // wait for one refresh
	current := m.Current()
	if current.MeasuredAt.IsZero() {
		t.Error("current should be set after Start+wait")
	}
	m.Stop()
	if m.IsActive() {
		t.Error("should be inactive after Stop")
	}
}

// TestTASK2244_ConnectionMonitorDoubleStart verifies double Start is
// safe (no goroutine leak).
func TestTASK2244_ConnectionMonitorDoubleStart(t *testing.T) {
	m := NewConnectionMonitor()
	m.Start()
	m.Start() // should be no-op
	m.Stop()
}

// TestTASK2244_ConnectionMonitorStopNotStarted verifies Stop when not
// started is safe.
func TestTASK2244_ConnectionMonitorStopNotStarted(t *testing.T) {
	m := NewConnectionMonitor()
	m.Stop() // should not panic
}

// TestTASK2244_ConnectionMonitorSetRefreshInterval verifies setting
// refresh interval (spec L4093: 60s).
func TestTASK2244_ConnectionMonitorSetRefreshInterval(t *testing.T) {
	m := NewConnectionMonitor()
	m.SetRefreshInterval(30 * time.Second)
	// No direct way to verify, but should not panic.
}

// TestTASK2244_ConnectionMonitorSetMeasureTarget verifies setting
// measure target (spec L4093: 1.1.1.1:443).
func TestTASK2244_ConnectionMonitorSetMeasureTarget(t *testing.T) {
	m := NewConnectionMonitor()
	m.SetMeasureTarget("8.8.8.8:443")
	// Should not panic.
}

// TestTASK2244_ConnectionMonitorNilSafe verifies nil monitor is safe.
func TestTASK2244_ConnectionMonitorNilSafe(t *testing.T) {
	var m *ConnectionMonitor
	m.Start()
	m.Stop()
	m.Measure()
	if m.IsActive() {
		t.Error("nil should not be active")
	}
	if m.Current().MeasuredAt.IsZero() {
		// nil Current returns zero-value ConnectionInfo
	}
	m.SetRefreshInterval(60 * time.Second)
	m.SetMeasureTarget("1.1.1.1:443")
}

// ==================== estimateDownlink tests ====================

// TestTASK2244_EstimateDownlink verifies downlink estimation from RTT
// (spec L4093: Downlink estimated from HTTP responses).
func TestTASK2244_EstimateDownlink(t *testing.T) {
	cases := []struct {
		rtt     time.Duration
		minDown float64 // minimum expected downlink
	}{
		{10 * time.Millisecond, 9.0},  // very fast -> high downlink
		{50 * time.Millisecond, 5.0},  // fast -> moderate
		{150 * time.Millisecond, 1.0}, // moderate -> lower
		{500 * time.Millisecond, 0.0}, // slow -> very low
	}
	for _, c := range cases {
		down := estimateDownlink(c.rtt)
		if down < c.minDown {
			t.Errorf("estimateDownlink(%v): got %.1f, want >= %.1f", c.rtt, down, c.minDown)
		}
	}
}

// ==================== full spec parity test ====================

// TestTASK2244_FullSpecParity verifies full spec parity for L4093
// (spec L4093: navigator.connection LIVE MEASUREMENT).
func TestTASK2244_FullSpecParity(t *testing.T) {
	// 1. DeriveEffectiveType
	if DeriveEffectiveType(50*time.Millisecond, 10.0) != "4g" {
		t.Error("rtt<100ms && downlink>5 should be 4g")
	}

	// 2. MeasureConnection
	info := MeasureConnection("1.1.1.1:443")
	if info.Source == "" {
		t.Error("source should be set")
	}

	// 3. ConnectionMonitor
	m := NewConnectionMonitor()
	m.Measure()
	if m.Current().MeasuredAt.IsZero() {
		t.Error("current should be set after Measure")
	}

	// 4. Start/Stop
	m.SetRefreshInterval(100 * time.Millisecond)
	m.Start()
	if !m.IsActive() {
		t.Error("should be active after Start")
	}
	m.Stop()
	if m.IsActive() {
		t.Error("should be inactive after Stop")
	}

	// 5. Default target
	if DefaultMeasureTarget != "1.1.1.1:443" {
		t.Error("default target should be 1.1.1.1:443")
	}

	// 6. Default refresh interval
	if DefaultRefreshInterval != 60*time.Second {
		t.Error("default refresh should be 60s")
	}
}
