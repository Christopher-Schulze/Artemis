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
//
// All entries MUST be lowercase; the matcher relies on this and does
// not re-lowercase them per call (TASK-2344 hot-path optimization).
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

// adTrackerPatternSet is a pre-built set of the builtin patterns for
// O(1) exact-match lookup. Initialized once at package load; all
// entries are already lowercase (see BuiltinAdTrackerPatterns).
var adTrackerPatternSet = func() map[string]struct{} {
	m := make(map[string]struct{}, len(BuiltinAdTrackerPatterns))
	for _, p := range BuiltinAdTrackerPatterns {
		m[p] = struct{}{}
	}
	return m
}()

// IsAdTrackerDomain returns true if the domain matches any of the 40+
// built-in ad/tracker patterns (spec L4027). Matching is suffix-based:
// a domain matches if it equals a pattern or ends with "."+pattern.
//
// Implementation (TASK-2344 optimization): instead of a linear scan
// over 48 patterns with a per-call ToLower on each pattern, we walk
// up the domain labels and do O(1) map lookups. For a domain like
// "ads.fls.doubleclick.net" we check "ads.fls.doubleclick.net",
// "fls.doubleclick.net", "doubleclick.net" — at most ~4 lookups for
// a typical domain, versus 48 HasSuffix calls previously.
func IsAdTrackerDomain(domain string) bool {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return false
	}
	// Walk up the domain labels: check the full domain, then each
	// parent domain (drop the leftmost label each step).
	for {
		if _, ok := adTrackerPatternSet[domain]; ok {
			return true
		}
		dot := strings.IndexByte(domain, '.')
		if dot < 0 || dot >= len(domain)-1 {
			return false
		}
		domain = domain[dot+1:]
	}
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
