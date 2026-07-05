package stealth

import (
	"fmt"
	"net"
	"sync"
	"time"
)

// ConnectionInfo is the navigator.connection API response
// (spec L4093: navigator.connection LIVE MEASUREMENT).
type ConnectionInfo struct {
	RTT           time.Duration `json:"rtt"`           // round-trip time
	Downlink      float64       `json:"downlink"`      // Mbps estimated from HTTP responses
	EffectiveType string        `json:"effectiveType"` // "4g", "3g", "2g", "slow-2g"
	MeasuredAt    time.Time     `json:"measured_at"`   // when the measurement was taken
	Source        string        `json:"source"`        // "live" or "fallback"
}

// ConnectionMonitor implements the navigator.connection LIVE MEASUREMENT
// (spec L4093: LIVE MEASUREMENT via net.Dial("tcp", "1.1.1.1:443")
// -> real RTT. Downlink estimated from HTTP responses. effectiveType
// derived: rtt<100ms && downlink>5 -> "4g". Values refreshed every 60s.
// Activation: StealthStealth (Patch 16)).
type ConnectionMonitor struct {
	mu              sync.RWMutex
	current         ConnectionInfo
	active          bool
	refreshInterval time.Duration
	stopCh          chan struct{}
	measureTarget   string // host:port for RTT measurement
}

// DefaultMeasureTarget is the default target for RTT measurement
// (spec L4093: net.Dial("tcp", "1.1.1.1:443")).
const DefaultMeasureTarget = "1.1.1.1:443"

// DefaultRefreshInterval is the default refresh interval
// (spec L4093: Values refreshed every 60s).
const DefaultRefreshInterval = 60 * time.Second

// NewConnectionMonitor creates a new ConnectionMonitor
// (spec L4093).
func NewConnectionMonitor() *ConnectionMonitor {
	return &ConnectionMonitor{
		refreshInterval: DefaultRefreshInterval,
		measureTarget:   DefaultMeasureTarget,
	}
}

// MeasureConnection performs a LIVE MEASUREMENT of the network
// connection (spec L4093: LIVE MEASUREMENT via net.Dial("tcp",
// "1.1.1.1:443") -> real RTT).
func MeasureConnection(target string) ConnectionInfo {
	if target == "" {
		target = DefaultMeasureTarget
	}
	start := time.Now()
	conn, err := net.DialTimeout("tcp", target, 5*time.Second)
	rtt := time.Since(start)
	if err != nil {
		// Fallback: can't measure -> conservative values
		return ConnectionInfo{
			RTT:           0,
			Downlink:      0,
			EffectiveType: "slow-2g",
			MeasuredAt:    time.Now(),
			Source:        "fallback",
		}
	}
	defer conn.Close()
	// Downlink is estimated from HTTP responses in a real implementation.
	// For now, we estimate based on RTT (lower RTT -> higher downlink).
	downlink := estimateDownlink(rtt)
	return ConnectionInfo{
		RTT:           rtt,
		Downlink:      downlink,
		EffectiveType: DeriveEffectiveType(rtt, downlink),
		MeasuredAt:    time.Now(),
		Source:        "live",
	}
}

// estimateDownlink estimates downlink speed from RTT
// (spec L4093: Downlink estimated from HTTP responses).
// This is a rough heuristic: lower RTT generally correlates with
// higher bandwidth.
func estimateDownlink(rtt time.Duration) float64 {
	rttMs := rtt.Milliseconds()
	if rttMs <= 0 {
		return 0
	}
	if rttMs < 20 {
		return 10.0 // very fast -> ~10 Mbps
	}
	if rttMs < 50 {
		return 7.0
	}
	if rttMs < 100 {
		return 5.0
	}
	if rttMs < 200 {
		return 2.0
	}
	return 0.5 // slow connection
}

// DeriveEffectiveType derives the effective connection type from RTT
// and downlink (spec L4093: effectiveType derived: rtt<100ms &&
// downlink>5 -> "4g").
func DeriveEffectiveType(rtt time.Duration, downlink float64) string {
	rttMs := rtt.Milliseconds()
	// 4g: rtt<100ms && downlink>5
	if rttMs < 100 && downlink > 5 {
		return "4g"
	}
	// 3g: rtt<700ms && downlink>0.5
	if rttMs < 700 && downlink > 0.5 {
		return "3g"
	}
	// 2g: rtt<3000ms
	if rttMs < 3000 {
		return "2g"
	}
	// slow-2g: everything else
	return "slow-2g"
}

// Measure performs a single measurement and updates the current info
// (spec L4093).
func (m *ConnectionMonitor) Measure() ConnectionInfo {
	if m == nil {
		return ConnectionInfo{}
	}
	info := MeasureConnection(m.measureTarget)
	m.mu.Lock()
	m.current = info
	m.mu.Unlock()
	return info
}

// Current returns the current connection info
// (spec L4093).
func (m *ConnectionMonitor) Current() ConnectionInfo {
	if m == nil {
		return ConnectionInfo{}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current
}

// Start begins the periodic measurement goroutine
// (spec L4093: Values refreshed every 60s. Activation: StealthStealth
// (Patch 16)).
func (m *ConnectionMonitor) Start() {
	if m == nil {
		return
	}
	m.mu.Lock()
	if m.active {
		m.mu.Unlock()
		return
	}
	m.active = true
	m.stopCh = make(chan struct{})
	m.mu.Unlock()

	// Do an initial measurement.
	m.Measure()

	go m.refreshLoop()
}

// Stop stops the periodic measurement goroutine.
func (m *ConnectionMonitor) Stop() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.active {
		return
	}
	m.active = false
	close(m.stopCh)
}

// IsActive reports whether the monitor is actively measuring.
func (m *ConnectionMonitor) IsActive() bool {
	if m == nil {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.active
}

// SetRefreshInterval sets the refresh interval
// (spec L4093: Values refreshed every 60s).
func (m *ConnectionMonitor) SetRefreshInterval(d time.Duration) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.refreshInterval = d
}

// SetMeasureTarget sets the target for RTT measurement
// (spec L4093: net.Dial("tcp", "1.1.1.1:443")).
func (m *ConnectionMonitor) SetMeasureTarget(target string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.measureTarget = target
}

// refreshLoop periodically measures the connection
// (spec L4093: Values refreshed every 60s).
func (m *ConnectionMonitor) refreshLoop() {
	ticker := time.NewTicker(m.refreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.Measure()
		case <-m.stopCh:
			return
		}
	}
}

// String returns a diagnostic summary.
func (c ConnectionInfo) String() string {
	return fmt.Sprintf("ConnectionInfo{rtt:%v downlink:%.1fMbps effectiveType:%s source:%s}",
		c.RTT, c.Downlink, c.EffectiveType, c.Source)
}

// IsLive reports whether the measurement was from a live net.Dial.
func (c ConnectionInfo) IsLive() bool {
	return c.Source == "live"
}

// IsFallback reports whether the measurement was a fallback.
func (c ConnectionInfo) IsFallback() bool {
	return c.Source == "fallback"
}
