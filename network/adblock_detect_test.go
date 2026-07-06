package network

import (
	"strings"
	"sync"
	"testing"
)

// --- DetectOverlay ---------------------------------------------------------

func TestDetectOverlay_CleanContent(t *testing.T) {
	d := NewAdBlockDetector(DefaultAdBlockDetectionConfig())
	r := d.DetectOverlay("<html><body><h1>Hello World</h1></body></html>")
	if r.Detected {
		t.Fatalf("expected no detection on clean content, got %+v", r)
	}
	if r.DetectionType != "" {
		t.Fatalf("expected empty detection type, got %q", r.DetectionType)
	}
}

func TestDetectOverlay_TextDetected(t *testing.T) {
	d := NewAdBlockDetector(DefaultAdBlockDetectionConfig())
	r := d.DetectOverlay(`<html><body><div>Adblock detected! Please turn it off.</div></body></html>`)
	if !r.Detected {
		t.Fatal("expected detection for overlay text")
	}
	if r.DetectionType != DetectionTypeOverlay {
		t.Fatalf("expected overlay type, got %q", r.DetectionType)
	}
	if r.Pattern == "" {
		t.Fatal("expected non-empty pattern")
	}
	if !r.ShouldDisable {
		t.Fatal("expected ShouldDisable true with default auto-disable")
	}
}

func TestDetectOverlay_CSSDetected(t *testing.T) {
	d := NewAdBlockDetector(DefaultAdBlockDetectionConfig())
	r := d.DetectOverlay(`<html><body><div class="adblock-overlay">blocked</div></body></html>`)
	if !r.Detected {
		t.Fatal("expected detection for overlay CSS class")
	}
	if r.DetectionType != DetectionTypeOverlay {
		t.Fatalf("expected overlay type, got %q", r.DetectionType)
	}
	if !strings.Contains(strings.ToLower(r.Pattern), "overlay") && !strings.Contains(strings.ToLower(r.Pattern), "adblock") {
		t.Fatalf("expected CSS pattern, got %q", r.Pattern)
	}
}

func TestDetectOverlay_CaseInsensitive(t *testing.T) {
	d := NewAdBlockDetector(DefaultAdBlockDetectionConfig())
	r := d.DetectOverlay(`<div>PLEASE DISABLE ADBLOCK TO CONTINUE</div>`)
	if !r.Detected {
		t.Fatal("expected case-insensitive detection")
	}
	if r.DetectionType != DetectionTypeOverlay {
		t.Fatalf("expected overlay type, got %q", r.DetectionType)
	}
}

func TestDetectOverlay_CSSIdPattern(t *testing.T) {
	d := NewAdBlockDetector(DefaultAdBlockDetectionConfig())
	r := d.DetectOverlay(`<div id="ad-block-notice">no ads for you</div>`)
	if !r.Detected {
		t.Fatal("expected detection for ad-block-notice id")
	}
	if r.DetectionType != DetectionTypeOverlay {
		t.Fatalf("expected overlay type, got %q", r.DetectionType)
	}
}

// --- DetectSubtle ----------------------------------------------------------

func TestDetectSubtle_NoBlockedRequests(t *testing.T) {
	d := NewAdBlockDetector(DefaultAdBlockDetectionConfig())
	r := d.DetectSubtle(0, nil)
	if r.Detected {
		t.Fatalf("expected no detection with 0 blocked requests, got %+v", r)
	}
}

func TestDetectSubtle_HighBlockedRequests(t *testing.T) {
	d := NewAdBlockDetector(DefaultAdBlockDetectionConfig())
	r := d.DetectSubtle(10, nil)
	if !r.Detected {
		t.Fatal("expected detection with high blocked request count")
	}
	if r.DetectionType != DetectionTypeSubtle {
		t.Fatalf("expected subtle type, got %q", r.DetectionType)
	}
	if r.BlockedRequests != 10 {
		t.Fatalf("expected BlockedRequests=10, got %d", r.BlockedRequests)
	}
}

func TestDetectSubtle_BelowThreshold(t *testing.T) {
	d := NewAdBlockDetector(DefaultAdBlockDetectionConfig())
	r := d.DetectSubtle(3, nil)
	if r.Detected {
		t.Fatal("expected no detection below threshold")
	}
}

func TestDetectSubtle_JSErrorDetected(t *testing.T) {
	d := NewAdBlockDetector(DefaultAdBlockDetectionConfig())
	r := d.DetectSubtle(0, []string{"Uncaught Error: ad resource blocked at line 3"})
	if !r.Detected {
		t.Fatal("expected detection for JS error pattern")
	}
	if r.DetectionType != DetectionTypeJSError {
		t.Fatalf("expected js_error type, got %q", r.DetectionType)
	}
	if r.JSErrorMatch == "" {
		t.Fatal("expected non-empty JSErrorMatch")
	}
}

func TestDetectSubtle_JSErrorTakesPrecedence(t *testing.T) {
	d := NewAdBlockDetector(DefaultAdBlockDetectionConfig())
	// Both signals present; JS error should win.
	r := d.DetectSubtle(20, []string{"failed to load ad unit"})
	if !r.Detected {
		t.Fatal("expected detection")
	}
	if r.DetectionType != DetectionTypeJSError {
		t.Fatalf("expected js_error precedence, got %q", r.DetectionType)
	}
}

func TestDetectSubtle_NoJSErrorMatch(t *testing.T) {
	d := NewAdBlockDetector(DefaultAdBlockDetectionConfig())
	r := d.DetectSubtle(0, []string{"some unrelated TypeError"})
	if r.Detected {
		t.Fatal("expected no detection for unrelated JS error")
	}
}

// --- Detect (combined) -----------------------------------------------------

func TestDetect_OverlayOnly(t *testing.T) {
	d := NewAdBlockDetector(DefaultAdBlockDetectionConfig())
	r := d.Detect(`<div>adblock detected</div>`, 0, nil)
	if !r.Detected {
		t.Fatal("expected detection")
	}
	if r.DetectionType != DetectionTypeOverlay {
		t.Fatalf("expected overlay type, got %q", r.DetectionType)
	}
}

func TestDetect_SubtleOnly(t *testing.T) {
	d := NewAdBlockDetector(DefaultAdBlockDetectionConfig())
	r := d.Detect(`<div>hello</div>`, 8, nil)
	if !r.Detected {
		t.Fatal("expected detection")
	}
	if r.DetectionType != DetectionTypeSubtle {
		t.Fatalf("expected subtle type, got %q", r.DetectionType)
	}
}

func TestDetect_BothOverlayAndSubtle(t *testing.T) {
	d := NewAdBlockDetector(DefaultAdBlockDetectionConfig())
	// Overlay takes precedence in combined detection.
	r := d.Detect(`<div class="adblock-overlay">blocked</div>`, 12, []string{"failed to load ad"})
	if !r.Detected {
		t.Fatal("expected detection")
	}
	if r.DetectionType != DetectionTypeOverlay {
		t.Fatalf("expected overlay precedence, got %q", r.DetectionType)
	}
}

func TestDetect_NothingDetected(t *testing.T) {
	d := NewAdBlockDetector(DefaultAdBlockDetectionConfig())
	r := d.Detect(`<div>hello world</div>`, 1, []string{"unrelated error"})
	if r.Detected {
		t.Fatal("expected no detection")
	}
}

// --- ShouldAutoDisable -----------------------------------------------------

func TestShouldAutoDisable_TrueWhenDetectedAndEnabled(t *testing.T) {
	d := NewAdBlockDetector(DefaultAdBlockDetectionConfig())
	r := DetectionResult{Detected: true, DetectionType: DetectionTypeOverlay}
	if !d.ShouldAutoDisable(r) {
		t.Fatal("expected true when detected and auto-disable enabled")
	}
}

func TestShouldAutoDisable_FalseWhenNotDetected(t *testing.T) {
	d := NewAdBlockDetector(DefaultAdBlockDetectionConfig())
	r := DetectionResult{Detected: false}
	if d.ShouldAutoDisable(r) {
		t.Fatal("expected false when not detected")
	}
}

func TestShouldAutoDisable_FalseWhenDisabled(t *testing.T) {
	cfg := DefaultAdBlockDetectionConfig()
	cfg.AutoDisable = false
	d := NewAdBlockDetector(cfg)
	r := DetectionResult{Detected: true, DetectionType: DetectionTypeOverlay}
	if d.ShouldAutoDisable(r) {
		t.Fatal("expected false when auto-disable disabled")
	}
}

// --- IsWhitelisted ---------------------------------------------------------

func TestIsWhitelisted_True(t *testing.T) {
	cfg := DefaultAdBlockDetectionConfig()
	cfg.WhitelistDomains = []string{"example.com"}
	d := NewAdBlockDetector(cfg)
	if !d.IsWhitelisted("example.com") {
		t.Fatal("expected exact match whitelisted")
	}
}

func TestIsWhitelisted_SubdomainTrue(t *testing.T) {
	cfg := DefaultAdBlockDetectionConfig()
	cfg.WhitelistDomains = []string{"example.com"}
	d := NewAdBlockDetector(cfg)
	if !d.IsWhitelisted("sub.example.com") {
		t.Fatal("expected subdomain whitelisted via suffix match")
	}
}

func TestIsWhitelisted_False(t *testing.T) {
	cfg := DefaultAdBlockDetectionConfig()
	cfg.WhitelistDomains = []string{"example.com"}
	d := NewAdBlockDetector(cfg)
	if d.IsWhitelisted("other.com") {
		t.Fatal("expected non-whitelisted domain false")
	}
}

func TestIsWhitelisted_CaseInsensitive(t *testing.T) {
	cfg := DefaultAdBlockDetectionConfig()
	cfg.WhitelistDomains = []string{"Example.com"}
	d := NewAdBlockDetector(cfg)
	if !d.IsWhitelisted("EXAMPLE.com") {
		t.Fatal("expected case-insensitive whitelist match")
	}
}

func TestIsWhitelisted_EmptyDomain(t *testing.T) {
	d := NewAdBlockDetector(DefaultAdBlockDetectionConfig())
	if d.IsWhitelisted("") {
		t.Fatal("expected false for empty domain")
	}
}

func TestAddWhitelistDomain_Idempotent(t *testing.T) {
	d := NewAdBlockDetector(DefaultAdBlockDetectionConfig())
	d.AddWhitelistDomain("newdomain.com")
	if !d.IsWhitelisted("newdomain.com") {
		t.Fatal("expected newly added domain whitelisted")
	}
	// Adding again should not duplicate.
	d.AddWhitelistDomain("newdomain.com")
	cfg := d.Config()
	count := 0
	for _, w := range cfg.WhitelistDomains {
		if w == "newdomain.com" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected single entry, got %d", count)
	}
}

// --- Disabled config -------------------------------------------------------

func TestDetectOverlay_DisabledNoDetection(t *testing.T) {
	cfg := DefaultAdBlockDetectionConfig()
	cfg.Enabled = false
	d := NewAdBlockDetector(cfg)
	r := d.DetectOverlay(`<div>adblock detected</div>`)
	if r.Detected {
		t.Fatal("expected no detection when disabled")
	}
}

func TestDetectSubtle_DisabledNoDetection(t *testing.T) {
	cfg := DefaultAdBlockDetectionConfig()
	cfg.Enabled = false
	d := NewAdBlockDetector(cfg)
	r := d.DetectSubtle(100, []string{"failed to load ad"})
	if r.Detected {
		t.Fatal("expected no detection when disabled")
	}
}

func TestDetect_DisabledNoDetection(t *testing.T) {
	cfg := DefaultAdBlockDetectionConfig()
	cfg.Enabled = false
	d := NewAdBlockDetector(cfg)
	r := d.Detect(`<div>adblock detected</div>`, 100, []string{"failed to load ad"})
	if r.Detected {
		t.Fatal("expected no detection when disabled")
	}
}

// --- Performance -----------------------------------------------------------

func TestPerformance_MeasureImprovement(t *testing.T) {
	p := NewAdBlockPerformance(DefaultAdBlockPerformanceConfig())
	before := PerformanceMetrics{AXTreeNodesBefore: 1000, NetworkRequestsBefore: 200}
	after := PerformanceMetrics{AXTreeNodesAfter: 600, NetworkRequestsAfter: 120, BlockedRequests: 80}
	r := p.Measure(before, after)
	// AX: (1000-600)/1000 = 40%, Network: (200-120)/200 = 40% => avg 40%
	if r.ImprovementPercent < 39.99 || r.ImprovementPercent > 40.01 {
		t.Fatalf("expected ~40%% improvement, got %f", r.ImprovementPercent)
	}
	if r.BlockedRequests != 80 {
		t.Fatalf("expected BlockedRequests=80, got %d", r.BlockedRequests)
	}
}

func TestPerformance_AXTreeReductionOnly(t *testing.T) {
	cfg := DefaultAdBlockPerformanceConfig()
	cfg.MeasureNetwork = false
	p := NewAdBlockPerformance(cfg)
	before := PerformanceMetrics{AXTreeNodesBefore: 500, NetworkRequestsBefore: 100}
	after := PerformanceMetrics{AXTreeNodesAfter: 250, NetworkRequestsAfter: 100}
	r := p.Measure(before, after)
	// Only AX: (500-250)/500 = 50%
	if r.ImprovementPercent < 49.99 || r.ImprovementPercent > 50.01 {
		t.Fatalf("expected ~50%% AX improvement, got %f", r.ImprovementPercent)
	}
}

func TestPerformance_NetworkReductionOnly(t *testing.T) {
	cfg := DefaultAdBlockPerformanceConfig()
	cfg.MeasureAXTree = false
	p := NewAdBlockPerformance(cfg)
	before := PerformanceMetrics{AXTreeNodesBefore: 500, NetworkRequestsBefore: 100}
	after := PerformanceMetrics{AXTreeNodesAfter: 500, NetworkRequestsAfter: 25}
	r := p.Measure(before, after)
	// Only network: (100-25)/100 = 75%
	if r.ImprovementPercent < 74.99 || r.ImprovementPercent > 75.01 {
		t.Fatalf("expected ~75%% network improvement, got %f", r.ImprovementPercent)
	}
}

func TestPerformance_NoReductionZeroImprovement(t *testing.T) {
	p := NewAdBlockPerformance(DefaultAdBlockPerformanceConfig())
	before := PerformanceMetrics{AXTreeNodesBefore: 500, NetworkRequestsBefore: 100}
	after := PerformanceMetrics{AXTreeNodesAfter: 500, NetworkRequestsAfter: 100}
	r := p.Measure(before, after)
	if r.ImprovementPercent != 0 {
		t.Fatalf("expected 0%% improvement, got %f", r.ImprovementPercent)
	}
}

func TestPerformance_ZeroBeforeNoDivision(t *testing.T) {
	p := NewAdBlockPerformance(DefaultAdBlockPerformanceConfig())
	before := PerformanceMetrics{AXTreeNodesBefore: 0, NetworkRequestsBefore: 0}
	after := PerformanceMetrics{AXTreeNodesAfter: 0, NetworkRequestsAfter: 0}
	r := p.Measure(before, after)
	if r.ImprovementPercent != 0 {
		t.Fatalf("expected 0%% improvement with zero before, got %f", r.ImprovementPercent)
	}
}

func TestPerformance_DisabledNoMeasurement(t *testing.T) {
	cfg := DefaultAdBlockPerformanceConfig()
	cfg.Enabled = false
	p := NewAdBlockPerformance(cfg)
	before := PerformanceMetrics{AXTreeNodesBefore: 1000, NetworkRequestsBefore: 200}
	after := PerformanceMetrics{AXTreeNodesAfter: 600, NetworkRequestsAfter: 120}
	r := p.Measure(before, after)
	if r.ImprovementPercent != 0 {
		t.Fatalf("expected 0%% improvement when disabled, got %f", r.ImprovementPercent)
	}
	_, measured := p.Stats()
	if measured != 0 {
		t.Fatalf("expected 0 measured when disabled, got %d", measured)
	}
}

func TestPerformance_IsEnabledSetEnabled(t *testing.T) {
	p := NewAdBlockPerformance(DefaultAdBlockPerformanceConfig())
	if !p.IsEnabled() {
		t.Fatal("expected enabled by default")
	}
	p.SetEnabled(false)
	if p.IsEnabled() {
		t.Fatal("expected disabled after SetEnabled(false)")
	}
	p.SetEnabled(true)
	if !p.IsEnabled() {
		t.Fatal("expected enabled after SetEnabled(true)")
	}
}

func TestPerformance_DefaultOn(t *testing.T) {
	cfg := DefaultAdBlockPerformanceConfig()
	if !cfg.DefaultOn {
		t.Fatal("expected DefaultOn=true")
	}
	if !cfg.Enabled {
		t.Fatal("expected Enabled=true in default config")
	}
}

// --- Stats -----------------------------------------------------------------

func TestDetector_Stats(t *testing.T) {
	d := NewAdBlockDetector(DefaultAdBlockDetectionConfig())
	d.DetectOverlay(`<div>adblock detected</div>`)
	d.DetectSubtle(10, nil)
	d.DetectOverlay(`clean content`)
	total, detected, autoDisabled := d.Stats()
	if total < 2 {
		t.Fatalf("expected total>=2, got %d", total)
	}
	if detected < 2 {
		t.Fatalf("expected detected>=2, got %d", detected)
	}
	if autoDisabled < 2 {
		t.Fatalf("expected autoDisabled>=2, got %d", autoDisabled)
	}
}

func TestDetector_ResetStats(t *testing.T) {
	d := NewAdBlockDetector(DefaultAdBlockDetectionConfig())
	d.DetectOverlay(`<div>adblock detected</div>`)
	d.ResetStats()
	total, detected, autoDisabled := d.Stats()
	if total != 0 || detected != 0 || autoDisabled != 0 {
		t.Fatalf("expected all zero after reset, got total=%d detected=%d autoDisabled=%d", total, detected, autoDisabled)
	}
}

func TestPerformance_Stats(t *testing.T) {
	p := NewAdBlockPerformance(DefaultAdBlockPerformanceConfig())
	before := PerformanceMetrics{AXTreeNodesBefore: 100, NetworkRequestsBefore: 50}
	after := PerformanceMetrics{AXTreeNodesAfter: 50, NetworkRequestsAfter: 25}
	p.Measure(before, after)
	total, measured := p.Stats()
	if total != 1 {
		t.Fatalf("expected total=1, got %d", total)
	}
	if measured != 1 {
		t.Fatalf("expected measured=1, got %d", measured)
	}
}

func TestPerformance_ResetStats(t *testing.T) {
	p := NewAdBlockPerformance(DefaultAdBlockPerformanceConfig())
	before := PerformanceMetrics{AXTreeNodesBefore: 100, NetworkRequestsBefore: 50}
	after := PerformanceMetrics{AXTreeNodesAfter: 50, NetworkRequestsAfter: 25}
	p.Measure(before, after)
	p.ResetStats()
	total, measured := p.Stats()
	if total != 0 || measured != 0 {
		t.Fatalf("expected zero after reset, got total=%d measured=%d", total, measured)
	}
}

// --- Concurrency / thread-safety ------------------------------------------

func TestAdBlockDetector_Concurrent(t *testing.T) {
	d := NewAdBlockDetector(DefaultAdBlockDetectionConfig())
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			d.DetectOverlay(`<div>adblock detected</div>`)
		}()
		go func() {
			defer wg.Done()
			d.DetectSubtle(10, []string{"failed to load ad"})
		}()
		go func() {
			defer wg.Done()
			d.Detect(`<div>adblock detected</div>`, 10, nil)
		}()
	}
	wg.Wait()
	total, detected, _ := d.Stats()
	if total == 0 {
		t.Fatal("expected nonzero total after concurrent runs")
	}
	if detected == 0 {
		t.Fatal("expected nonzero detected after concurrent runs")
	}
}

func TestAdBlockDetector_ConcurrentConfig(t *testing.T) {
	d := NewAdBlockDetector(DefaultAdBlockDetectionConfig())
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			d.SetConfig(DefaultAdBlockDetectionConfig())
		}()
		go func() {
			defer wg.Done()
			_ = d.Config()
		}()
	}
	wg.Wait()
	// Final state should be a valid config; no panic / race is the main goal.
	_ = d.Config()
}

func TestAdBlockDetector_ConcurrentWhitelist(t *testing.T) {
	d := NewAdBlockDetector(AdBlockDetectionConfig{
		Enabled:                 true,
		AutoDisable:             true,
		DetectionPatterns:       DefaultAdBlockDetectionConfig().DetectionPatterns,
		OverlayCSSPatterns:      DefaultAdBlockDetectionConfig().OverlayCSSPatterns,
		JSErrorPatterns:         DefaultAdBlockDetectionConfig().JSErrorPatterns,
		BlockedRequestThreshold: 5,
	})
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			d.AddWhitelistDomain("newdomain.com")
		}()
		go func() {
			defer wg.Done()
			_ = d.IsWhitelisted("newdomain.com")
		}()
	}
	wg.Wait()
	if !d.IsWhitelisted("newdomain.com") {
		t.Fatal("expected whitelisted domain after concurrent adds")
	}
}

func TestAdBlockPerformance_Concurrent(t *testing.T) {
	p := NewAdBlockPerformance(DefaultAdBlockPerformanceConfig())
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			p.SetEnabled(true)
		}()
		go func() {
			defer wg.Done()
			before := PerformanceMetrics{AXTreeNodesBefore: 100, NetworkRequestsBefore: 50}
			after := PerformanceMetrics{AXTreeNodesAfter: 50, NetworkRequestsAfter: 25}
			p.Measure(before, after)
		}()
	}
	wg.Wait()
	total, measured := p.Stats()
	if total == 0 {
		t.Fatal("expected nonzero total after concurrent runs")
	}
	if measured == 0 {
		t.Fatal("expected nonzero measured after concurrent runs")
	}
}
