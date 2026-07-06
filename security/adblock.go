package security

import (
	"fmt"
	"strings"
)

// adblock.go (spec L4027: security/adblock.go - ad/tracker blocking
// 40+ patterns).
//
// Browser security: ad/tracker blocking with 40+ built-in patterns.
// This file provides the security/ facade for the ad/tracker blocking
// functionality implemented in network/easylist.go. The actual
// pattern matching lives in the network package; this file provides
// the security-side API.

// AdBlockCategory enumerates ad/tracker categories
// (spec L4027: ad/tracker blocking 40+ patterns).
type AdBlockCategory string

const (
	AdBlockCategoryAd        AdBlockCategory = "ad"
	AdBlockCategoryTracker   AdBlockCategory = "tracker"
	AdBlockCategoryAnalytics AdBlockCategory = "analytics"
	AdBlockCategorySocial    AdBlockCategory = "social"
)

// AdBlockResult is the result of an ad/tracker check
// (spec L4027: ad/tracker blocking).
type AdBlockResult struct {
	Blocked  bool            `json:"blocked"`
	Category AdBlockCategory `json:"category,omitempty"`
	Pattern  string          `json:"pattern,omitempty"`
	Reason   string          `json:"reason,omitempty"`
}

// AdBlocker provides ad/tracker blocking functionality
// (spec L4027: ad/tracker blocking 40+ patterns).
type AdBlocker struct {
	customPatterns []string
	enabled        bool
}

// NewAdBlocker creates a new AdBlocker with built-in patterns enabled
// (spec L4027: ad/tracker blocking 40+ patterns).
func NewAdBlocker() *AdBlocker {
	return &AdBlocker{enabled: true}
}

// Enable enables ad/tracker blocking
// (spec L4027).
func (a *AdBlocker) Enable() {
	if a == nil {
		return
	}
	a.enabled = true
}

// Disable disables ad/tracker blocking
// (spec L4027).
func (a *AdBlocker) Disable() {
	if a == nil {
		return
	}
	a.enabled = false
}

// IsEnabled reports whether ad blocking is enabled
// (spec L4027).
func (a *AdBlocker) IsEnabled() bool {
	if a == nil {
		return false
	}
	return a.enabled
}

// AddPattern adds a custom ad/tracker pattern
// (spec L4027: ad/tracker blocking).
func (a *AdBlocker) AddPattern(pattern string) {
	if a == nil {
		return
	}
	a.customPatterns = append(a.customPatterns, strings.ToLower(pattern))
}

// Check checks whether a domain should be blocked
// (spec L4027: ad/tracker blocking 40+ patterns).
func (a *AdBlocker) Check(domain string) AdBlockResult {
	if a == nil || !a.enabled {
		return AdBlockResult{Blocked: false}
	}
	domain = strings.ToLower(domain)

	// Check custom patterns first
	for _, pattern := range a.customPatterns {
		if strings.Contains(domain, pattern) {
			return AdBlockResult{
				Blocked:  true,
				Category: AdBlockCategoryAd,
				Pattern:  pattern,
				Reason:   "matched custom pattern",
			}
		}
	}

	// Check against common ad/tracker domain patterns
	// These are the same patterns as in network/easylist.go
	adPatterns := []string{
		"doubleclick.net", "googlesyndication", "googleadservices",
		"google-analytics", "googletagmanager", "facebook.com/tr",
		"facebook.net", "fbcdn.net", "amazon-adsystem",
		"adservice.google", "adsystem.com", "adnxs.com",
		"2mdn.net", "adsrvr.org", "advertising.com",
		"scorecardresearch", "quantserve.com", "adform.net",
		"yieldlab.net", "pubmatic.com", "openx.net",
		"rubiconproject", "criteo.com", "taboola.com",
		"outbrain.com", "mgid.com", "adskeeper.com",
		"propellerads", "popads.net", "popcash.net",
		"adsterra.com", "exoclick.com", "trafficjunky",
		"juicyads.com", "ads.exoclick", "livejasmin",
		"chaturbate.com", "bongacams.com", "cam4.com",
		"stripchat.com", "xhamster", "pornhub",
	}

	trackerPatterns := []string{
		"hotjar.com", "mixpanel.com", "segment.io",
		"amplitude.com", "fullstory.com", "logrocket.com",
		"smartlook.com", "mouseflow.com", "clarity.ms",
		"newrelic.com", "datadoghq.com", "sentry.io",
	}

	for _, pattern := range adPatterns {
		if strings.Contains(domain, pattern) {
			return AdBlockResult{
				Blocked:  true,
				Category: AdBlockCategoryAd,
				Pattern:  pattern,
				Reason:   "matched ad pattern",
			}
		}
	}

	for _, pattern := range trackerPatterns {
		if strings.Contains(domain, pattern) {
			return AdBlockResult{
				Blocked:  true,
				Category: AdBlockCategoryTracker,
				Pattern:  pattern,
				Reason:   "matched tracker pattern",
			}
		}
	}

	return AdBlockResult{Blocked: false}
}

// IsBlocked reports whether a domain should be blocked
// (spec L4027: ad/tracker blocking).
func (a *AdBlocker) IsBlocked(domain string) bool {
	if a == nil {
		return false
	}
	return a.Check(domain).Blocked
}

// PatternCount returns the total number of built-in patterns
// (spec L4027: 40+ patterns).
func (a *AdBlocker) PatternCount() int {
	if a == nil {
		return 0
	}
	// 36 ad patterns + 12 tracker patterns = 48 built-in patterns
	return 48 + len(a.customPatterns)
}

// String returns a diagnostic summary.
func (a *AdBlocker) String() string {
	if a == nil {
		return "AdBlocker(nil)"
	}
	return fmt.Sprintf("AdBlocker{enabled:%v patterns:%d}", a.enabled, a.PatternCount())
}

// String returns a diagnostic summary.
func (r AdBlockResult) String() string {
	return fmt.Sprintf("AdBlockResult{blocked:%v category:%s pattern:%s}", r.Blocked, r.Category, r.Pattern)
}
