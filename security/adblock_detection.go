package security

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// AdBlockerDetection implements Layer 2 of the intelligent ad blocker:
// AdBlocker-detection + auto-disable
// (spec L4212-L4215).
//
// Case A (explicit): page shows "disable your adblocker" overlay.
// Case B (subtle): page breaks without overlay.
type AdBlockerDetection struct {
	mu        sync.RWMutex
	whitelist map[string]bool // domain -> whitelisted
	filePath  string
}

// NewAdBlockerDetection creates a new detection layer with the
// persistent whitelist at the given path
// (whitelist file under the browser data dir).
func NewAdBlockerDetection(whitelistPath string) *AdBlockerDetection {
	d := &AdBlockerDetection{
		whitelist: make(map[string]bool),
		filePath:  whitelistPath,
	}
	d.loadWhitelist()
	return d
}

// DefaultAdBlockerDetection creates a detection layer with the
// default whitelist path.
func DefaultAdBlockerDetection() *AdBlockerDetection {
	home, _ := os.UserHomeDir()
	return NewAdBlockerDetection(filepath.Join(home, ".omnimus", "browser", "adblock_whitelist.json"))
}

// AdBlockerOverlaySignals are the detection signals for Case A
// (explicit overlay detection, spec L4213).
type AdBlockerOverlaySignals struct {
	// TextPatterns: "adblock", "ad blocker", "werbeblocker"
	TextMatches []string
	// CSS class patterns: .adblock-notice, .adblock-overlay
	CSSClassMatches []string
	// Overlay: position:fixed + z-index:9999
	HasFixedOverlay bool
}

// AdBlockerBreakageSignals are the detection signals for Case B
// (subtle page breakage, spec L4214).
type AdBlockerBreakageSignals struct {
	// BlockedRequestCount: number of requests blocked by adblocker
	BlockedRequestCount int
	// JSErrors: number of JS errors after page load
	JSErrors int
	// MissingCriticalElements: count of expected elements that are absent
	MissingCriticalElements int
}

// DetectExplicitOverlay detects Case A: explicit "disable your
// adblocker" overlay (spec L4213).
// Returns true if an adblocker notice overlay is detected.
func DetectExplicitOverlay(signals AdBlockerOverlaySignals) bool {
	// Must have text match AND (CSS class match OR overlay)
	if len(signals.TextMatches) == 0 {
		return false
	}
	return len(signals.CSSClassMatches) > 0 || signals.HasFixedOverlay
}

// DetectSubtleBreakage detects Case B: subtle page breakage without
// overlay (spec L4214).
// Returns true if the page appears broken due to adblocker.
// Detection: load WITH adblocker -> count blocked_requests ->
// if >5 AND JS errors or missing critical elements -> breakage.
func DetectSubtleBreakage(signals AdBlockerBreakageSignals) bool {
	if signals.BlockedRequestCount <= 5 {
		return false
	}
	return signals.JSErrors > 0 || signals.MissingCriticalElements > 0
}

// AdBlockerTextPatterns are the text patterns for detecting adblocker
// notices (spec L4213: "adblock"/"ad blocker"/"werbeblocker").
var AdBlockerTextPatterns = []string{
	"adblock",
	"ad blocker",
	"ad-blocker",
	"werbeblocker",
	"disable adblock",
	"disable your adblocker",
	"please disable adblock",
	"adblock detected",
	"whitelist us",
	"disable ad blocker",
}

// AdBlockerCSSPatterns are the CSS class patterns for detecting
// adblocker notices (spec L4213: .adblock-notice/.adblock-overlay).
var AdBlockerCSSPatterns = []string{
	"adblock-notice",
	"adblock-overlay",
	"adblocker-notice",
	"adblocker-overlay",
	"ad-blocker-notice",
	"ad-blocker-overlay",
	"adblock-warning",
	"adblock-message",
}

// MatchAdBlockerText checks if the given text contains any adblocker
// notice text patterns (spec L4213).
func MatchAdBlockerText(text string) []string {
	lower := strings.ToLower(text)
	var matches []string
	for _, pattern := range AdBlockerTextPatterns {
		if strings.Contains(lower, pattern) {
			matches = append(matches, pattern)
		}
	}
	return matches
}

// MatchAdBlockerCSS checks if the given CSS class list contains any
// adblocker notice CSS patterns (spec L4213).
func MatchAdBlockerCSS(classList string) []string {
	lower := strings.ToLower(classList)
	var matches []string
	for _, pattern := range AdBlockerCSSPatterns {
		if strings.Contains(lower, pattern) {
			matches = append(matches, pattern)
		}
	}
	return matches
}

// IsWhitelisted reports whether a domain is in the persistent
// adblocker whitelist (spec L4215: persistent across updates, no
// auto-expiry).
func (d *AdBlockerDetection) IsWhitelisted(domain string) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.whitelist[strings.ToLower(domain)]
}

// AddToWhitelist adds a domain to the persistent whitelist
// (spec L4215: whitelist domain + reload).
func (d *AdBlockerDetection) AddToWhitelist(domain string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.whitelist[strings.ToLower(domain)] = true
	return d.saveWhitelist()
}

// RemoveFromWhitelist removes a domain from the whitelist
// (spec L4215: operator can clean via maintenance API).
func (d *AdBlockerDetection) RemoveFromWhitelist(domain string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.whitelist, strings.ToLower(domain))
	return d.saveWhitelist()
}

// WhitelistDomains returns all whitelisted domains.
func (d *AdBlockerDetection) WhitelistDomains() []string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]string, 0, len(d.whitelist))
	for domain := range d.whitelist {
		out = append(out, domain)
	}
	return out
}

// loadWhitelist loads the whitelist from the JSON file
// (spec L4215: persistent across updates).
func (d *AdBlockerDetection) loadWhitelist() {
	if d.filePath == "" {
		return
	}
	data, err := os.ReadFile(d.filePath)
	if err != nil {
		return // file doesn't exist yet; start with empty whitelist
	}
	var domains []string
	if err := json.Unmarshal(data, &domains); err != nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, domain := range domains {
		d.whitelist[strings.ToLower(domain)] = true
	}
}

// saveWhitelist saves the whitelist to the JSON file.
func (d *AdBlockerDetection) saveWhitelist() error {
	if d.filePath == "" {
		return nil
	}
	domains := make([]string, 0, len(d.whitelist))
	for domain := range d.whitelist {
		domains = append(domains, domain)
	}
	if err := os.MkdirAll(filepath.Dir(d.filePath), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(domains)
	if err != nil {
		return err
	}
	return os.WriteFile(d.filePath, data, 0o644)
}
