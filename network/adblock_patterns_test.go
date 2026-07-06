package network

import (
	"strings"
	"testing"
)

// TestTASK2238_BuiltinAdTrackerPatternCount40Plus verifies there are
// 40+ built-in ad/tracker patterns (spec L4027: 40+ patterns).
func TestTASK2238_BuiltinAdTrackerPatternCount40Plus(t *testing.T) {
	count := BuiltinAdTrackerPatternCount()
	if count < 40 {
		t.Errorf("expected 40+ patterns, got %d", count)
	}
}

// TestTASK2238_IsAdTrackerDomainAdNetworks verifies major ad network
// domains are detected (spec L4027).
func TestTASK2238_IsAdTrackerDomainAdNetworks(t *testing.T) {
	adDomains := []string{
		"doubleclick.net",
		"googlesyndication.com",
		"googleadservices.com",
		"amazon-adsystem.com",
		"adnxs.com",
		"criteo.com",
		"pubmatic.com",
	}
	for _, d := range adDomains {
		if !IsAdTrackerDomain(d) {
			t.Errorf("ad domain %s should be detected", d)
		}
	}
}

// TestTASK2238_IsAdTrackerDomainTrackers verifies tracker/analytics
// domains are detected (spec L4027).
func TestTASK2238_IsAdTrackerDomainTrackers(t *testing.T) {
	trackers := []string{
		"google-analytics.com",
		"hotjar.com",
		"mixpanel.com",
		"segment.com",
		"amplitude.com",
		"fullstory.com",
		"clarity.ms",
	}
	for _, d := range trackers {
		if !IsAdTrackerDomain(d) {
			t.Errorf("tracker domain %s should be detected", d)
		}
	}
}

// TestTASK2238_IsAdTrackerDomainSocialWidgets verifies social
// widget/pixel domains are detected (spec L4027).
func TestTASK2238_IsAdTrackerDomainSocialWidgets(t *testing.T) {
	socials := []string{
		"connect.facebook.net",
		"platform.twitter.com",
		"platform.linkedin.com",
		"analytics.tiktok.com",
		"bat.bing.com",
	}
	for _, d := range socials {
		if !IsAdTrackerDomain(d) {
			t.Errorf("social domain %s should be detected", d)
		}
	}
}

// TestTASK2238_IsAdTrackerDomainSubdomain verifies subdomains of
// ad/tracker patterns are detected (spec L4027).
func TestTASK2238_IsAdTrackerDomainSubdomain(t *testing.T) {
	subdomains := []string{
		"ads.doubleclick.net",
		"stats.g.doubleclick.net",
		"www.googletagmanager.com",
		"cdn.adsystem.com",
		"tpc.googlesyndication.com",
	}
	for _, d := range subdomains {
		if !IsAdTrackerDomain(d) {
			t.Errorf("subdomain %s should be detected", d)
		}
	}
}

// TestTASK2238_IsAdTrackerDomainCleanDomain verifies non-ad/tracker
// domains are NOT detected (spec L4027).
func TestTASK2238_IsAdTrackerDomainCleanDomain(t *testing.T) {
	clean := []string{
		"example.com",
		"wikipedia.org",
		"myapp.com",
		"localhost",
		"internal.corp",
	}
	for _, d := range clean {
		if IsAdTrackerDomain(d) {
			t.Errorf("clean domain %s should NOT be detected", d)
		}
	}
}

// TestTASK2238_IsAdTrackerDomainCaseInsensitive verifies matching is
// case-insensitive (spec L4027).
func TestTASK2238_IsAdTrackerDomainCaseInsensitive(t *testing.T) {
	if !IsAdTrackerDomain("DOUBLECLICK.NET") {
		t.Error("uppercase ad domain should be detected")
	}
	if !IsAdTrackerDomain("Googlesyndication.COM") {
		t.Error("mixed-case ad domain should be detected")
	}
}

// TestTASK2238_IsAdTrackerDomainEmpty verifies empty domain returns false.
func TestTASK2238_IsAdTrackerDomainEmpty(t *testing.T) {
	if IsAdTrackerDomain("") {
		t.Error("empty domain should not be detected")
	}
}

// TestTASK2238_IsAdTrackerDomainSimilarNotMatched verifies similar-
// looking domains are NOT matched (e.g. "notdoubleclick.net" is not
// "doubleclick.net").
func TestTASK2238_IsAdTrackerDomainSimilarNotMatched(t *testing.T) {
	similar := []string{
		"notdoubleclick.net",
		"doubleclick.net.evil.com",
		"mygoogle-analytics.com",
	}
	for _, d := range similar {
		if IsAdTrackerDomain(d) {
			t.Errorf("similar domain %s should NOT be detected", d)
		}
	}
}

// TestTASK2238_FilterAdTrackerDomains verifies filtering removes
// ad/tracker domains from a list (spec L4027).
func TestTASK2238_FilterAdTrackerDomains(t *testing.T) {
	domains := []string{
		"example.com",
		"doubleclick.net",
		"wikipedia.org",
		"google-analytics.com",
		"myapp.com",
		"adnxs.com",
	}
	filtered := FilterAdTrackerDomains(domains)
	expected := []string{"example.com", "wikipedia.org", "myapp.com"}
	if len(filtered) != len(expected) {
		t.Fatalf("filtered: got %d, want %d (%v)", len(filtered), len(expected), filtered)
	}
	for i, d := range expected {
		if filtered[i] != d {
			t.Errorf("filtered[%d]: got %s, want %s", i, filtered[i], d)
		}
	}
}

// TestTASK2238_FilterAdTrackerDomainsAllAd verifies filtering when all
// domains are ad/tracker domains.
func TestTASK2238_FilterAdTrackerDomainsAllAd(t *testing.T) {
	domains := []string{"doubleclick.net", "googlesyndication.com", "adnxs.com"}
	filtered := FilterAdTrackerDomains(domains)
	if len(filtered) != 0 {
		t.Errorf("all-ad filter: got %d, want 0", len(filtered))
	}
}

// TestTASK2238_FilterAdTrackerDomainsAllClean verifies filtering when
// no domains are ad/tracker domains.
func TestTASK2238_FilterAdTrackerDomainsAllClean(t *testing.T) {
	domains := []string{"example.com", "wikipedia.org", "myapp.com"}
	filtered := FilterAdTrackerDomains(domains)
	if len(filtered) != 3 {
		t.Errorf("all-clean filter: got %d, want 3", len(filtered))
	}
}

// TestTASK2238_FilterAdTrackerDomainsEmpty verifies filtering empty list.
func TestTASK2238_FilterAdTrackerDomainsEmpty(t *testing.T) {
	filtered := FilterAdTrackerDomains(nil)
	if len(filtered) != 0 {
		t.Errorf("empty filter: got %d, want 0", len(filtered))
	}
}

// TestTASK2238_BuiltinPatternsNoDuplicates verifies no duplicate
// patterns in the built-in list (spec L4027: quality).
func TestTASK2238_BuiltinPatternsNoDuplicates(t *testing.T) {
	seen := make(map[string]bool)
	for _, p := range BuiltinAdTrackerPatterns {
		p = strings.ToLower(p)
		if seen[p] {
			t.Errorf("duplicate pattern: %s", p)
		}
		seen[p] = true
	}
}

// TestTASK2238_BuiltinPatternsAllNonEmpty verifies all patterns are
// non-empty (spec L4027: quality).
func TestTASK2238_BuiltinPatternsAllNonEmpty(t *testing.T) {
	for i, p := range BuiltinAdTrackerPatterns {
		if strings.TrimSpace(p) == "" {
			t.Errorf("pattern at index %d is empty", i)
		}
	}
}

// TestTASK2238_BuiltinPatternsAllLowercase verifies all patterns are
// stored lowercase for consistent matching (spec L4027: quality).
func TestTASK2238_BuiltinPatternsAllLowercase(t *testing.T) {
	for i, p := range BuiltinAdTrackerPatterns {
		if p != strings.ToLower(p) {
			t.Errorf("pattern at index %d is not lowercase: %s", i, p)
		}
	}
}

// TestTASK2238_FullSpecParity verifies the full spec parity for
// L4027 security surface (spec L4027).
func TestTASK2238_FullSpecParity(t *testing.T) {
	// 1. 40+ patterns
	if BuiltinAdTrackerPatternCount() < 40 {
		t.Error("must have 40+ patterns")
	}

	// 2. Ad network detection
	if !IsAdTrackerDomain("doubleclick.net") {
		t.Error("doubleclick.net must be detected")
	}

	// 3. Tracker detection
	if !IsAdTrackerDomain("google-analytics.com") {
		t.Error("google-analytics.com must be detected")
	}

	// 4. Social widget detection
	if !IsAdTrackerDomain("connect.facebook.net") {
		t.Error("connect.facebook.net must be detected")
	}

	// 5. Subdomain detection
	if !IsAdTrackerDomain("ads.doubleclick.net") {
		t.Error("ads.doubleclick.net must be detected")
	}

	// 6. Clean domain not detected
	if IsAdTrackerDomain("example.com") {
		t.Error("example.com must NOT be detected")
	}

	// 7. Case insensitive
	if !IsAdTrackerDomain("DOUBLECLICK.NET") {
		t.Error("DOUBLECLICK.NET must be detected")
	}

	// 8. Filter removes ad domains
	filtered := FilterAdTrackerDomains([]string{"example.com", "doubleclick.net"})
	if len(filtered) != 1 || filtered[0] != "example.com" {
		t.Error("filter must remove ad domains")
	}
}

// ==================== TASK-2344 label-walk matcher tests ====================

// TestTASK2344_IsAdTrackerDomainDeepSubdomain verifies the label-walk
// matcher correctly matches a deep subdomain of a builtin pattern
// (e.g. "a.b.c.doubleclick.net" matches "doubleclick.net").
func TestTASK2344_IsAdTrackerDomainDeepSubdomain(t *testing.T) {
	deep := []string{
		"a.b.c.doubleclick.net",
		"x.y.z.googlesyndication.com",
		"sub1.sub2.criteo.com",
		"deep.connect.facebook.net",
	}
	for _, d := range deep {
		if !IsAdTrackerDomain(d) {
			t.Errorf("deep subdomain %s should match", d)
		}
	}
}

// TestTASK2344_IsAdTrackerDomainLabelWalkNotPrefix verifies the
// label-walk matcher does NOT match a domain that merely has the
// pattern as a prefix without a dot separator (e.g.
// "doubleclick.net.evil.com" must NOT match "doubleclick.net").
func TestTASK2344_IsAdTrackerDomainLabelWalkNotPrefix(t *testing.T) {
	// "doubleclick.net.evil.com" — the label walk checks
	// "doubleclick.net.evil.com", "net.evil.com", "evil.com", "com"
	// — none of which are in the pattern set, so no match.
	if IsAdTrackerDomain("doubleclick.net.evil.com") {
		t.Error("doubleclick.net.evil.com must NOT match (it is a subdomain of evil.com, not doubleclick.net)")
	}
}

// TestTASK2344_IsAdTrackerDomainSingleLabel verifies a single-label
// domain (no dots) does not crash and does not match any pattern.
func TestTASK2344_IsAdTrackerDomainSingleLabel(t *testing.T) {
	if IsAdTrackerDomain("localhost") {
		t.Error("localhost should not match")
	}
	if IsAdTrackerDomain("com") {
		t.Error("com should not match (not in pattern set)")
	}
}

// TestTASK2344_IsAdTrackerDomainTrailingDot verifies a domain with a
// trailing dot does not crash and is handled correctly.
func TestTASK2344_IsAdTrackerDomainTrailingDot(t *testing.T) {
	// Trailing dot: "doubleclick.net." — after ToLower/TrimSpace,
	// the label walk checks "doubleclick.net." (not in set),
	// then "net." (not in set), then "" — no match. This is
	// correct behavior: a trailing dot is a DNS root label and
	// should not match the pattern.
	if IsAdTrackerDomain("doubleclick.net.") {
		t.Error("trailing-dot domain should not match (pattern has no trailing dot)")
	}
}

// TestTASK2344_IsAdTrackerDomainWhitespace verifies whitespace is
// trimmed before matching.
func TestTASK2344_IsAdTrackerDomainWhitespace(t *testing.T) {
	if !IsAdTrackerDomain("  doubleclick.net  ") {
		t.Error("whitespace-padded domain should match after trim")
	}
}

// TestTASK2344_IsAdTrackerDomainMixedCaseDeepSubdomain verifies
// case-insensitive matching works for deep subdomains.
func TestTASK2344_IsAdTrackerDomainMixedCaseDeepSubdomain(t *testing.T) {
	if !IsAdTrackerDomain("Ads.FLS.DoubleClick.Net") {
		t.Error("mixed-case deep subdomain should match")
	}
}
