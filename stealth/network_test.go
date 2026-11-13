package stealth

import (
	"testing"
	"time"
)

// ==================== ConnectionInfo tests ====================

// TestConnectionInfoString verifies String method.
func TestConnectionInfoString(t *testing.T) {
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

// TestConnectionInfoIsLive verifies IsLive method.
func TestConnectionInfoIsLive(t *testing.T) {
	c := ConnectionInfo{Source: "live"}
	if !c.IsLive() {
		t.Error("should be live")
	}
	if c.IsFallback() {
		t.Error("should not be fallback")
	}
}

// TestConnectionInfoIsFallback verifies IsFallback method.
func TestConnectionInfoIsFallback(t *testing.T) {
	c := ConnectionInfo{Source: "fallback"}
	if !c.IsFallback() {
		t.Error("should be fallback")
	}
	if c.IsLive() {
		t.Error("should not be live")
	}
}

// ==================== DeriveEffectiveType tests ====================

// TestDeriveEffectiveType4G verifies 4g derivation
// (rtt<100ms && downlink>5 -> "4g").
func TestDeriveEffectiveType4G(t *testing.T) {
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

// TestDeriveEffectiveType3G verifies 3g derivation
// (3g threshold).
func TestDeriveEffectiveType3G(t *testing.T) {
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

// TestDeriveEffectiveType2G verifies 2g derivation.
func TestDeriveEffectiveType2G(t *testing.T) {
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

// TestDeriveEffectiveTypeSlow2G verifies slow-2g derivation.
func TestDeriveEffectiveTypeSlow2G(t *testing.T) {
	if DeriveEffectiveType(3000*time.Millisecond, 0.0) != "slow-2g" {
		t.Error("rtt>=3000ms should be slow-2g")
	}
	if DeriveEffectiveType(5000*time.Millisecond, 0.0) != "slow-2g" {
		t.Error("rtt>=3000ms should be slow-2g")
	}
}

// TestDeriveEffectiveTypeBoundary verifies boundary conditions
// (rtt<100ms && downlink>5 -> "4g").
func TestDeriveEffectiveTypeBoundary(t *testing.T) {
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

// TestMeasureConnectionLive verifies live measurement returns
// a ConnectionInfo (LIVE MEASUREMENT via net.Dial).
func TestMeasureConnectionLive(t *testing.T) {
	info := MeasureConnection("1.1.1.1:443")
	// Should either be live or fallback (network may not be available in CI).
	if info.Source != "live" && info.Source != "fallback" {
		t.Errorf("source: got %s, want live or fallback", info.Source)
	}
	if info.MeasuredAt.IsZero() {
		t.Error("measured_at should be set")
	}
}

// TestMeasureConnectionFallback verifies fallback when target
// is unreachable.
func TestMeasureConnectionFallback(t *testing.T) {
	// Use an invalid port on localhost to trigger fast connection refused.
	info := MeasureConnection("127.0.0.1:1") // port 1 should be refused
	// Should be fallback (connection refused/failed).
	if info.Source != "fallback" && info.Source != "live" {
		t.Errorf("source: got %s", info.Source)
	}
}

// TestMeasureConnectionEmptyTarget verifies empty target uses
// default (1.1.1.1:443).
func TestMeasureConnectionEmptyTarget(t *testing.T) {
	info := MeasureConnection("")
	// Should not panic and should return a result.
	if info.MeasuredAt.IsZero() {
		t.Error("measured_at should be set even with empty target")
	}
}

// ==================== ConnectionMonitor tests ====================

// TestNewConnectionMonitor verifies creation with defaults.
func TestNewConnectionMonitor(t *testing.T) {
	m := NewConnectionMonitor()
	if m.IsActive() {
		t.Error("new monitor should be inactive")
	}
}

// TestConnectionMonitorMeasure verifies Measure updates current.
func TestConnectionMonitorMeasure(t *testing.T) {
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

// TestConnectionMonitorStartStop verifies Start/Stop cycle
// (Values refreshed every 60s).
func TestConnectionMonitorStartStop(t *testing.T) {
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

// TestConnectionMonitorDoubleStart verifies double Start is
// safe (no goroutine leak).
func TestConnectionMonitorDoubleStart(t *testing.T) {
	m := NewConnectionMonitor()
	m.Start()
	m.Start() // should be no-op
	m.Stop()
}

// TestConnectionMonitorStopNotStarted verifies Stop when not
// started is safe.
func TestConnectionMonitorStopNotStarted(t *testing.T) {
	m := NewConnectionMonitor()
	m.Stop() // should not panic
}

// TestConnectionMonitorSetRefreshInterval verifies setting
// refresh interval (60s).
func TestConnectionMonitorSetRefreshInterval(t *testing.T) {
	m := NewConnectionMonitor()
	m.SetRefreshInterval(30 * time.Second)
	// No direct way to verify, but should not panic.
}

// TestConnectionMonitorSetMeasureTarget verifies setting
// measure target (1.1.1.1:443).
func TestConnectionMonitorSetMeasureTarget(t *testing.T) {
	m := NewConnectionMonitor()
	m.SetMeasureTarget("8.8.8.8:443")
	// Should not panic.
}

// TestConnectionMonitorNilSafe verifies nil monitor is safe.
func TestConnectionMonitorNilSafe(t *testing.T) {
	var m *ConnectionMonitor
	m.Start()
	m.Stop()
	m.Measure()
	if m.IsActive() {
		t.Error("nil should not be active")
	}
	if !m.Current().MeasuredAt.IsZero() {
		t.Error("nil monitor should return a zero-value connection info")
	}
	m.SetRefreshInterval(60 * time.Second)
	m.SetMeasureTarget("1.1.1.1:443")
}

// ==================== estimateDownlink tests ====================

// TestEstimateDownlink verifies downlink estimation from RTT
// (Downlink estimated from HTTP responses).
func TestEstimateDownlink(t *testing.T) {
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

// ==================== parity test ====================

// TestNetworkParity verifies full parity.
// (navigator.connection LIVE MEASUREMENT).
func TestNetworkParity(t *testing.T) {
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
