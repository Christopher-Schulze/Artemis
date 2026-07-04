package security

import "testing"

func TestDetectExplicitOverlay(t *testing.T) {
	// Text + CSS match
	signals := AdBlockerOverlaySignals{
		TextMatches:     []string{"adblock"},
		CSSClassMatches: []string{"adblock-notice"},
	}
	if !DetectExplicitOverlay(signals) {
		t.Error("explicit overlay with text+CSS should be detected")
	}

	// Text + fixed overlay
	signals = AdBlockerOverlaySignals{
		TextMatches:     []string{"disable adblock"},
		HasFixedOverlay: true,
	}
	if !DetectExplicitOverlay(signals) {
		t.Error("explicit overlay with text+overlay should be detected")
	}

	// No text match
	signals = AdBlockerOverlaySignals{
		CSSClassMatches: []string{"adblock-notice"},
	}
	if DetectExplicitOverlay(signals) {
		t.Error("overlay without text match should not be detected")
	}
}

func TestDetectSubtleBreakage(t *testing.T) {
	// >5 blocked + JS errors
	signals := AdBlockerBreakageSignals{
		BlockedRequestCount: 10,
		JSErrors:            3,
	}
	if !DetectSubtleBreakage(signals) {
		t.Error("subtle breakage with >5 blocked + JS errors should be detected")
	}

	// >5 blocked + missing elements
	signals = AdBlockerBreakageSignals{
		BlockedRequestCount:     8,
		MissingCriticalElements: 2,
	}
	if !DetectSubtleBreakage(signals) {
		t.Error("subtle breakage with >5 blocked + missing elements should be detected")
	}

	// <=5 blocked: no breakage
	signals = AdBlockerBreakageSignals{
		BlockedRequestCount: 3,
		JSErrors:            5,
	}
	if DetectSubtleBreakage(signals) {
		t.Error("subtle breakage with <=5 blocked should not be detected")
	}

	// >5 blocked but no errors/missing: no breakage
	signals = AdBlockerBreakageSignals{
		BlockedRequestCount: 10,
	}
	if DetectSubtleBreakage(signals) {
		t.Error("subtle breakage with >5 blocked but no errors should not be detected")
	}
}

func TestMatchAdBlockerText(t *testing.T) {
	matches := MatchAdBlockerText("Please disable your adblocker to continue")
	if len(matches) == 0 {
		t.Error("should match adblocker text patterns")
	}

	matches = MatchAdBlockerText("Welcome to our website")
	if len(matches) > 0 {
		t.Error("should not match normal text")
	}
}

func TestMatchAdBlockerCSS(t *testing.T) {
	matches := MatchAdBlockerCSS("content adblock-notice footer")
	if len(matches) == 0 {
		t.Error("should match adblocker CSS patterns")
	}

	matches = MatchAdBlockerCSS("header main footer")
	if len(matches) > 0 {
		t.Error("should not match normal CSS classes")
	}
}

func TestAdBlockerDetectionWhitelist(t *testing.T) {
	d := NewAdBlockerDetection(t.TempDir() + "/whitelist.json")

	if d.IsWhitelisted("example.com") {
		t.Error("example.com should not be whitelisted initially")
	}

	if err := d.AddToWhitelist("example.com"); err != nil {
		t.Fatalf("AddToWhitelist: %v", err)
	}

	if !d.IsWhitelisted("example.com") {
		t.Error("example.com should be whitelisted after adding")
	}

	if !d.IsWhitelisted("EXAMPLE.COM") {
		t.Error("whitelist should be case-insensitive")
	}

	domains := d.WhitelistDomains()
	if len(domains) != 1 {
		t.Errorf("WhitelistDomains len = %d, want 1", len(domains))
	}

	if err := d.RemoveFromWhitelist("example.com"); err != nil {
		t.Fatalf("RemoveFromWhitelist: %v", err)
	}

	if d.IsWhitelisted("example.com") {
		t.Error("example.com should not be whitelisted after removal")
	}
}

func TestAdBlockerDetectionWhitelistPersistence(t *testing.T) {
	path := t.TempDir() + "/whitelist.json"
	d1 := NewAdBlockerDetection(path)
	d1.AddToWhitelist("persist-test.com")

	// Create a new instance that loads from the same file
	d2 := NewAdBlockerDetection(path)
	if !d2.IsWhitelisted("persist-test.com") {
		t.Error("whitelist should persist across instances")
	}
}
