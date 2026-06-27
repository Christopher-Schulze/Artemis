package network

import "strings"

// FilterEasyListRules normalizes easylist-style entries.
func FilterEasyListRules(rules []string) []string {
	out := make([]string, 0, len(rules))
	for _, r := range rules {
		r = strings.TrimSpace(r)
		if r == "" || strings.HasPrefix(r, "!") {
			continue
		}
		out = append(out, r)
	}
	return out
}

// BuiltinAdTrackerPatterns is the 40+ built-in ad/tracker domain
// patterns (spec L4027: ad/tracker blocking 40+ patterns). Covers
// major ad networks, trackers, analytics, and social widgets.
var BuiltinAdTrackerPatterns = []string{
	// --- Ad networks (20) ---
	"doubleclick.net",
	"googlesyndication.com",
	"googleadservices.com",
	"googletagmanager.com",
	"googletagservices.com",
	"amazon-adsystem.com",
	"adsystem.com",
	"facebook.net",
	"fbcdn.net",
	"adsrvr.org",
	"adnxs.com",
	"2mdn.net",
	"adform.net",
	"adtech.de",
	"adtech.com",
	"yieldlab.net",
	"pubmatic.com",
	"rubiconproject.com",
	"openx.net",
	"criteo.com",
	"criteo.net",
	// --- Trackers / analytics (15) ---
	"google-analytics.com",
	"analytics.google.com",
	"hotjar.com",
	"mixpanel.com",
	"segment.com",
	"segment.io",
	"amplitude.com",
	"fullstory.com",
	"mouseflow.com",
	"clarity.ms",
	"quantserve.com",
	"scorecardresearch.com",
	"newrelic.com",
	"pingdom.net",
	"statcounter.com",
	// --- Social widgets / pixels (8) ---
	"connect.facebook.net",
	"platform.twitter.com",
	"platform.linkedin.com",
	"analytics.tiktok.com",
	"bat.bing.com",
	"pixel.facebook.com",
	"snap.licdn.com",
	"ads.linkedin.com",
	// --- Other ad/tracker (5) ---
	"adservice.google.com",
	"ads.google.com",
	"partner.googleadservices.com",
	"tpc.googlesyndication.com",
	"fls.doubleclick.net",
}

// IsAdTrackerDomain returns true if the domain matches any of the 40+
// built-in ad/tracker patterns (spec L4027). Matching is suffix-based:
// a domain matches if it equals a pattern or ends with "."+pattern.
func IsAdTrackerDomain(domain string) bool {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return false
	}
	for _, pattern := range BuiltinAdTrackerPatterns {
		pattern = strings.ToLower(pattern)
		if domain == pattern || strings.HasSuffix(domain, "."+pattern) {
			return true
		}
	}
	return false
}

// FilterAdTrackerDomains filters a list of domains, removing any that
// match the built-in ad/tracker patterns (spec L4027).
func FilterAdTrackerDomains(domains []string) []string {
	out := make([]string, 0, len(domains))
	for _, d := range domains {
		if !IsAdTrackerDomain(d) {
			out = append(out, d)
		}
	}
	return out
}

// BuiltinAdTrackerPatternCount returns the number of built-in patterns.
// Must be >= 40 per spec L4027.
func BuiltinAdTrackerPatternCount() int {
	return len(BuiltinAdTrackerPatterns)
}
