package security

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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

// NewAdBlockerDetection opens the persistent site whitelist at
// whitelistPath (spec L4208: persistent across updates, no auto-expiry). A
// missing file starts empty; an unreadable or corrupt file is an error so
// operator decisions are never silently dropped.
func NewAdBlockerDetection(whitelistPath string) (*AdBlockerDetection, error) {
	d := &AdBlockerDetection{
		whitelist: make(map[string]bool),
		filePath:  whitelistPath,
	}
	if err := d.loadWhitelist(); err != nil {
		return nil, err
	}
	return d, nil
}

// DefaultAdBlockerDetection opens the whitelist under
// ~/.artemis/browser/adblock_whitelist.json; embedding hosts pass their own home.
func DefaultAdBlockerDetection() (*AdBlockerDetection, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("adblock whitelist: %w", err)
	}
	return NewAdBlockerDetection(filepath.Join(home, ".artemis", "browser", "adblock_whitelist.json"))
}

// AdBlockerOverlaySignals are the detection signals for Case A
// (explicit overlay detection, spec L4208).
type AdBlockerOverlaySignals struct {
	// TextPatterns: "adblock", "ad blocker", "werbeblocker"
	TextMatches []string
	// CSS class patterns: .adblock-notice, .adblock-overlay
	CSSClassMatches []string
	// Overlay: position:fixed + z-index:9999
	HasFixedOverlay bool
}

// AdBlockerBreakageSignals are the detection signals for Case B
// (subtle page breakage, spec L4208).
type AdBlockerBreakageSignals struct {
	// BlockedRequestCount: number of requests blocked by adblocker
	BlockedRequestCount int
	// JSErrors: number of JS errors after page load
	JSErrors int
	// MissingCriticalElements: count of expected elements that are absent
	MissingCriticalElements int
}

// DetectExplicitOverlay detects Case A: explicit "disable your
// adblocker" overlay (spec L4208).
// Returns true if an adblocker notice overlay is detected.
func DetectExplicitOverlay(signals AdBlockerOverlaySignals) bool {
	// Must have text match AND (CSS class match OR overlay)
	if len(signals.TextMatches) == 0 {
		return false
	}
	return len(signals.CSSClassMatches) > 0 || signals.HasFixedOverlay
}

// DetectSubtleBreakage detects Case B: subtle page breakage without
// overlay (spec L4208).
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
// notices (spec L4208: "adblock"/"ad blocker"/"werbeblocker").
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
// adblocker notices (spec L4208: .adblock-notice/.adblock-overlay).
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
// notice text patterns (spec L4208).
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
// adblocker notice CSS patterns (spec L4208).
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

// IsWhitelisted reports whether domain or one of its parent domains is in
// the persistent whitelist (spec L4208).
func (d *AdBlockerDetection) IsWhitelisted(domain string) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	for domain = strings.ToLower(strings.TrimSpace(domain)); domain != ""; {
		if d.whitelist[domain] {
			return true
		}
		dot := strings.IndexByte(domain, '.')
		if dot < 0 {
			return false
		}
		domain = domain[dot+1:]
	}
	return false
}

// AddToWhitelist adds a domain to the persistent whitelist
// (spec L4208: whitelist domain + reload).
func (d *AdBlockerDetection) AddToWhitelist(domain string) error {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" || strings.ContainsAny(domain, "/:@ ") {
		return fmt.Errorf("adblock whitelist: invalid domain %q", domain)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.whitelist[domain] = true
	return d.saveWhitelist()
}

// RemoveFromWhitelist removes a domain from the whitelist
// (spec L4208: operator can clean via maintenance API).
func (d *AdBlockerDetection) RemoveFromWhitelist(domain string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.whitelist, strings.ToLower(strings.TrimSpace(domain)))
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
	sort.Strings(out)
	return out
}

// loadWhitelist loads the whitelist from the JSON file
// (spec L4208: persistent across updates).
func (d *AdBlockerDetection) loadWhitelist() error {
	if d.filePath == "" {
		return nil
	}
	data, err := os.ReadFile(d.filePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("adblock whitelist: %w", err)
	}
	var domains []string
	if err := json.Unmarshal(data, &domains); err != nil {
		return fmt.Errorf("adblock whitelist %s: %w", d.filePath, err)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, domain := range domains {
		d.whitelist[strings.ToLower(strings.TrimSpace(domain))] = true
	}
	return nil
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
	sort.Strings(domains)
	if err := os.MkdirAll(filepath.Dir(d.filePath), 0o750); err != nil {
		return fmt.Errorf("adblock whitelist: %w", err)
	}
	data, err := json.Marshal(domains)
	if err != nil {
		return fmt.Errorf("adblock whitelist: %w", err)
	}
	return writeFileAtomic(d.filePath, data)
}
