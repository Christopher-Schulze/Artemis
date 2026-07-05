package network

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
)

// ---------------------------------------------------------------------------
// Spec L4208: AdBlocker Layer 2 - Detection + Auto-Disable
// ---------------------------------------------------------------------------

// DetectionType classifies the kind of adblocker wall signal observed.
type DetectionType string

const (
	// DetectionTypeOverlay is Case A: visible overlay text / CSS patterns
	// such as "adblock detected" notices or "adblock-overlay" elements.
	DetectionTypeOverlay DetectionType = "overlay"
	// DetectionTypeSubtle is Case B: subtle signals inferred from a high
	// count of blocked requests relative to page activity.
	DetectionTypeSubtle DetectionType = "subtle"
	// DetectionTypeJSError is Case B: subtle signals inferred from
	// JavaScript error messages that reference blocked ad resources.
	DetectionTypeJSError DetectionType = "js_error"
)

// AdBlockDetectionConfig controls Layer 2 detection behaviour.
type AdBlockDetectionConfig struct {
	Enabled            bool
	DetectionPatterns  []string
	OverlayCSSPatterns []string
	AutoDisable        bool
	WhitelistDomains   []string
	// BlockedRequestThreshold is the minimum number of blocked requests
	// before a subtle (Case B) detection is emitted. Defaults to 5.
	BlockedRequestThreshold int
	// JSErrorPatterns are substrings that, when present in a JS error
	// message, indicate a blocked ad resource.
	JSErrorPatterns []string
}

// DefaultAdBlockDetectionConfig returns the production default config:
// detection enabled, auto-disable enabled, with overlay text and CSS
// patterns and JS-error patterns covering the common adblocker walls.
func DefaultAdBlockDetectionConfig() AdBlockDetectionConfig {
	return AdBlockDetectionConfig{
		Enabled:     true,
		AutoDisable: true,
		DetectionPatterns: []string{
			"adblock detected",
			"disable adblock",
			"ad blocker",
			"please disable",
			"adblocker detected",
			"turn off adblock",
			"whitelist our site",
			"adblock is enabled",
		},
		OverlayCSSPatterns: []string{
			"ad-block-notice",
			"adblock-overlay",
			"adblock-notice",
			"ad-blocker-message",
			"adblock-detected",
			"adblocker-wall",
			"please-disable-adblock",
		},
		JSErrorPatterns: []string{
			"blocked by adblock",
			"ad resource blocked",
			"failed to load ad",
			"ad script blocked",
			"adblocker blocked request",
			"net::err_blocked",
		},
		WhitelistDomains: []string{
			"localhost",
			"127.0.0.1",
		},
		BlockedRequestThreshold: 5,
	}
}

// DetectionResult is the outcome of a single detection pass.
type DetectionResult struct {
	Detected        bool
	DetectionType   DetectionType
	Pattern         string
	ShouldDisable   bool
	Reason          string
	BlockedRequests int
	JSErrorMatch    string
}

// AdBlockDetectorStats are atomic counters for detector activity.
type AdBlockDetectorStats struct {
	Total        atomic.Int64
	Detected     atomic.Int64
	AutoDisabled atomic.Int64
}

// AdBlockDetector implements Layer 2 adblocker-wall detection and the
// auto-disable / whitelist retry policy.
type AdBlockDetector struct {
	mu     sync.RWMutex
	config AdBlockDetectionConfig
	stats  AdBlockDetectorStats
}

// NewAdBlockDetector constructs a detector with the given config.
func NewAdBlockDetector(cfg AdBlockDetectionConfig) *AdBlockDetector {
	if cfg.BlockedRequestThreshold <= 0 {
		cfg.BlockedRequestThreshold = 5
	}
	return &AdBlockDetector{config: cfg}
}

// Config returns a copy of the current detection config.
func (d *AdBlockDetector) Config() AdBlockDetectionConfig {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.config
}

// SetConfig replaces the detection config atomically.
func (d *AdBlockDetector) SetConfig(cfg AdBlockDetectionConfig) {
	if cfg.BlockedRequestThreshold <= 0 {
		cfg.BlockedRequestThreshold = 5
	}
	d.mu.Lock()
	d.config = cfg
	d.mu.Unlock()
}

// DetectOverlay implements Case A: it scans rendered HTML content for
// known overlay text and CSS class/id patterns. Detection is case
// insensitive. Returns a DetectionResult with DetectionTypeOverlay when a
// pattern matches.
func (d *AdBlockDetector) DetectOverlay(htmlContent string) DetectionResult {
	d.stats.Total.Add(1)
	d.mu.RLock()
	cfg := d.config
	d.mu.RUnlock()

	if !cfg.Enabled {
		return DetectionResult{Detected: false}
	}

	lower := strings.ToLower(htmlContent)

	for _, p := range cfg.DetectionPatterns {
		if p == "" {
			continue
		}
		if strings.Contains(lower, strings.ToLower(p)) {
			res := DetectionResult{
				Detected:      true,
				DetectionType: DetectionTypeOverlay,
				Pattern:       p,
				Reason:        fmt.Sprintf("overlay text pattern matched: %q", p),
			}
			res.ShouldDisable = d.ShouldAutoDisable(res)
			if res.ShouldDisable {
				d.stats.AutoDisabled.Add(1)
			}
			if res.Detected {
				d.stats.Detected.Add(1)
			}
			return res
		}
	}

	for _, p := range cfg.OverlayCSSPatterns {
		if p == "" {
			continue
		}
		if strings.Contains(lower, strings.ToLower(p)) {
			res := DetectionResult{
				Detected:      true,
				DetectionType: DetectionTypeOverlay,
				Pattern:       p,
				Reason:        fmt.Sprintf("overlay CSS pattern matched: %q", p),
			}
			res.ShouldDisable = d.ShouldAutoDisable(res)
			if res.ShouldDisable {
				d.stats.AutoDisabled.Add(1)
			}
			if res.Detected {
				d.stats.Detected.Add(1)
			}
			return res
		}
	}

	return DetectionResult{Detected: false}
}

// DetectSubtle implements Case B: it inspects subtle signals - a high
// blocked-request count and JavaScript error messages that reference
// blocked ad resources. When blockedRequests meets the threshold a
// DetectionTypeSubtle result is returned; when a JS error pattern
// matches, a DetectionTypeJSError result is returned. JS errors take
// precedence as the stronger signal.
func (d *AdBlockDetector) DetectSubtle(blockedRequests int, jsErrors []string) DetectionResult {
	d.stats.Total.Add(1)
	d.mu.RLock()
	cfg := d.config
	d.mu.RUnlock()

	if !cfg.Enabled {
		return DetectionResult{Detected: false, BlockedRequests: blockedRequests}
	}

	// Case B - JS errors: strongest subtle signal.
	for _, err := range jsErrors {
		le := strings.ToLower(err)
		for _, p := range cfg.JSErrorPatterns {
			if p == "" {
				continue
			}
			if strings.Contains(le, strings.ToLower(p)) {
				res := DetectionResult{
					Detected:        true,
					DetectionType:   DetectionTypeJSError,
					Pattern:         p,
					JSErrorMatch:    err,
					BlockedRequests: blockedRequests,
					Reason:          fmt.Sprintf("JS error pattern matched: %q in %q", p, err),
				}
				res.ShouldDisable = d.ShouldAutoDisable(res)
				if res.ShouldDisable {
					d.stats.AutoDisabled.Add(1)
				}
				if res.Detected {
					d.stats.Detected.Add(1)
				}
				return res
			}
		}
	}

	// Case B - subtle blocked request count.
	if blockedRequests >= cfg.BlockedRequestThreshold {
		res := DetectionResult{
			Detected:        true,
			DetectionType:   DetectionTypeSubtle,
			BlockedRequests: blockedRequests,
			Reason:          fmt.Sprintf("blocked request count %d >= threshold %d", blockedRequests, cfg.BlockedRequestThreshold),
		}
		res.ShouldDisable = d.ShouldAutoDisable(res)
		if res.ShouldDisable {
			d.stats.AutoDisabled.Add(1)
		}
		if res.Detected {
			d.stats.Detected.Add(1)
		}
		return res
	}

	return DetectionResult{Detected: false, BlockedRequests: blockedRequests}
}

// Detect runs the combined overlay + subtle detection and returns the
// first positive result. Overlay (Case A) is checked first because it is
// the most explicit signal; subtle signals are checked second.
func (d *AdBlockDetector) Detect(htmlContent string, blockedRequests int, jsErrors []string) DetectionResult {
	d.mu.RLock()
	enabled := d.config.Enabled
	d.mu.RUnlock()
	if !enabled {
		d.stats.Total.Add(1)
		return DetectionResult{Detected: false, BlockedRequests: blockedRequests}
	}

	if r := d.DetectOverlay(htmlContent); r.Detected {
		return r
	}
	if r := d.DetectSubtle(blockedRequests, jsErrors); r.Detected {
		return r
	}
	// Account for the combined pass even when nothing fired but only when
	// the individual passes did not already increment (they always do, so
	// nothing extra here).
	return DetectionResult{Detected: false, BlockedRequests: blockedRequests}
}

// ShouldAutoDisable reports whether the detector should auto-disable
// blocking for the matched domain and retry via the whitelist. It returns
// true when the result is a detection and AutoDisable is enabled in the
// config.
func (d *AdBlockDetector) ShouldAutoDisable(result DetectionResult) bool {
	if !result.Detected {
		return false
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.config.AutoDisable
}

// IsWhitelisted reports whether domain matches a whitelist entry. The
// match is case insensitive and supports suffix matching so that
// "example.com" whitelists "sub.example.com" as well as "example.com".
func (d *AdBlockDetector) IsWhitelisted(domain string) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return false
	}
	for _, w := range d.config.WhitelistDomains {
		w = strings.ToLower(strings.TrimSpace(w))
		if w == "" {
			continue
		}
		if domain == w || strings.HasSuffix(domain, "."+w) {
			return true
		}
	}
	return false
}

// AddWhitelistDomain adds a domain to the whitelist idempotently.
func (d *AdBlockDetector) AddWhitelistDomain(domain string) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, w := range d.config.WhitelistDomains {
		if strings.ToLower(strings.TrimSpace(w)) == domain {
			return
		}
	}
	d.config.WhitelistDomains = append(d.config.WhitelistDomains, domain)
}

// Stats returns a snapshot of detector counters.
func (d *AdBlockDetector) Stats() (total, detected, autoDisabled int64) {
	return d.stats.Total.Load(), d.stats.Detected.Load(), d.stats.AutoDisabled.Load()
}

// ResetStats zeroes all detector counters.
func (d *AdBlockDetector) ResetStats() {
	d.stats.Total.Store(0)
	d.stats.Detected.Store(0)
	d.stats.AutoDisabled.Store(0)
}

// ---------------------------------------------------------------------------
// Spec L4210: AdBlocker Layer 3 - Performance
// ---------------------------------------------------------------------------

// AdBlockPerformanceConfig controls Layer 3 performance measurement.
type AdBlockPerformanceConfig struct {
	Enabled        bool
	MeasureAXTree  bool
	MeasureNetwork bool
	DefaultOn      bool
}

// DefaultAdBlockPerformanceConfig returns the production default: enabled
// by default, with AX tree and network measurement both active.
func DefaultAdBlockPerformanceConfig() AdBlockPerformanceConfig {
	return AdBlockPerformanceConfig{
		Enabled:        true,
		MeasureAXTree:  true,
		MeasureNetwork: true,
		DefaultOn:      true,
	}
}

// PerformanceMetrics captures before/after counts and the computed
// improvement percentage for the AX tree and network request surface.
type PerformanceMetrics struct {
	BlockedRequests       int
	AXTreeNodesBefore     int
	AXTreeNodesAfter      int
	NetworkRequestsBefore int
	NetworkRequestsAfter  int
	ImprovementPercent    float64
}

// AdBlockPerformanceStats are atomic counters for performance passes.
type AdBlockPerformanceStats struct {
	Total    atomic.Int64
	Measured atomic.Int64
}

// AdBlockPerformance implements Layer 3 performance measurement.
type AdBlockPerformance struct {
	mu     sync.RWMutex
	config AdBlockPerformanceConfig
	stats  AdBlockPerformanceStats
}

// NewAdBlockPerformance constructs a performance tracker with the given config.
func NewAdBlockPerformance(cfg AdBlockPerformanceConfig) *AdBlockPerformance {
	return &AdBlockPerformance{config: cfg}
}

// IsEnabled reports whether performance measurement is currently enabled.
func (p *AdBlockPerformance) IsEnabled() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.config.Enabled
}

// SetEnabled toggles performance measurement. Layer 3 is disableable per
// spec L4210.
func (p *AdBlockPerformance) SetEnabled(enabled bool) {
	p.mu.Lock()
	p.config.Enabled = enabled
	p.mu.Unlock()
}

// Config returns a copy of the current performance config.
func (p *AdBlockPerformance) Config() AdBlockPerformanceConfig {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.config
}

// SetConfig replaces the performance config atomically.
func (p *AdBlockPerformance) SetConfig(cfg AdBlockPerformanceConfig) {
	p.mu.Lock()
	p.config = cfg
	p.mu.Unlock()
}

// Measure computes the improvement percentage from before/after metrics.
// The improvement is the average of the AX tree reduction and network
// request reduction, each expressed as a percentage of the before value.
// Only dimensions enabled in the config contribute. The returned metrics
// combine the before counts from `before` and the after counts from
// `after`, with the computed ImprovementPercent.
func (p *AdBlockPerformance) Measure(before, after PerformanceMetrics) PerformanceMetrics {
	p.stats.Total.Add(1)

	p.mu.RLock()
	cfg := p.config
	p.mu.RUnlock()

	out := PerformanceMetrics{
		BlockedRequests:       after.BlockedRequests,
		AXTreeNodesBefore:     before.AXTreeNodesBefore,
		AXTreeNodesAfter:      after.AXTreeNodesAfter,
		NetworkRequestsBefore: before.NetworkRequestsBefore,
		NetworkRequestsAfter:  after.NetworkRequestsAfter,
	}

	if !cfg.Enabled {
		return out
	}

	var sum float64
	var dims int

	if cfg.MeasureAXTree && before.AXTreeNodesBefore > 0 {
		reduction := float64(before.AXTreeNodesBefore-after.AXTreeNodesAfter) / float64(before.AXTreeNodesBefore) * 100.0
		if reduction < 0 {
			reduction = 0
		}
		sum += reduction
		dims++
	}

	if cfg.MeasureNetwork && before.NetworkRequestsBefore > 0 {
		reduction := float64(before.NetworkRequestsBefore-after.NetworkRequestsAfter) / float64(before.NetworkRequestsBefore) * 100.0
		if reduction < 0 {
			reduction = 0
		}
		sum += reduction
		dims++
	}

	if dims > 0 {
		out.ImprovementPercent = sum / float64(dims)
	}

	p.stats.Measured.Add(1)
	return out
}

// Stats returns a snapshot of performance counters.
func (p *AdBlockPerformance) Stats() (total, measured int64) {
	return p.stats.Total.Load(), p.stats.Measured.Load()
}

// ResetStats zeroes all performance counters.
func (p *AdBlockPerformance) ResetStats() {
	p.stats.Total.Store(0)
	p.stats.Measured.Store(0)
}
