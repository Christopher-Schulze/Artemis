package network

import (
	"sort"
	"testing"
)

func TestDefaultBlockedResourcesAllTenBlocked(t *testing.T) {
	b := DefaultBlockedResources()
	want := []ResourceType{
		ResourceFont, ResourceImage, ResourceMedia, ResourceBeacon,
		ResourceObject, ResourceImageset, ResourceTexttrack,
		ResourceWebsocket, ResourceCSPReport, ResourceStylesheet,
	}
	if len(b.ResourceTypes) != len(want) {
		t.Fatalf("got %d blocked types, want %d", len(b.ResourceTypes), len(want))
	}
	for _, rt := range want {
		if !b.ResourceTypes[rt] {
			t.Errorf("resource type %q not blocked by default", rt)
		}
	}
}

func TestIsBlockedBlockedType(t *testing.T) {
	b := DefaultBlockedResources()
	if !b.IsBlocked(ResourceFont, "cdn.example.com") {
		t.Error("IsBlocked(font, ...) = false, want true (type blocked, no domain rules)")
	}
	if !b.IsBlocked(ResourceStylesheet, "cdn.example.com") {
		t.Error("IsBlocked(stylesheet, ...) = false, want true")
	}
}

func TestIsBlockedAllowedType(t *testing.T) {
	b := DefaultBlockedResources()
	// Document/script types are not in the default blocked set.
	if b.IsBlocked(ResourceType("document"), "cdn.example.com") {
		t.Error("IsBlocked(document, ...) = true, want false (not blocked type)")
	}
	if b.IsBlocked(ResourceType("script"), "cdn.example.com") {
		t.Error("IsBlocked(script, ...) = true, want false (not blocked type)")
	}
}

func TestIsBlockedDomainBlockingSubdomainMatch(t *testing.T) {
	b := DefaultBlockedResources()
	b.BlockedDomains = []string{"example.com"}

	if !b.IsBlocked(ResourceImage, "example.com") {
		t.Error("IsBlocked(image, example.com) = false, want true")
	}
	if !b.IsBlocked(ResourceImage, "sub.example.com") {
		t.Error("IsBlocked(image, sub.example.com) = false, want true (subdomain match)")
	}
	if !b.IsBlocked(ResourceImage, "a.b.example.com") {
		t.Error("IsBlocked(image, a.b.example.com) = false, want true (deep subdomain)")
	}
	if b.IsBlocked(ResourceImage, "notexample.com") {
		t.Error("IsBlocked(image, notexample.com) = true, want false (not a subdomain)")
	}
	if b.IsBlocked(ResourceImage, "example.com.evil.com") {
		t.Error("IsBlocked(image, example.com.evil.com) = true, want false (suffix attack)")
	}
}

func TestIsBlockedAllowlistOverride(t *testing.T) {
	b := DefaultBlockedResources()
	b.BlockedDomains = []string{"example.com"}
	b.AllowedDomains = []string{"safe.example.com"}

	// Allowed subdomain overrides both type and domain blocking.
	if b.IsBlocked(ResourceImage, "safe.example.com") {
		t.Error("IsBlocked(image, safe.example.com) = true, want false (allowlisted)")
	}
	// Non-allowed subdomain still blocked.
	if !b.IsBlocked(ResourceImage, "bad.example.com") {
		t.Error("IsBlocked(image, bad.example.com) = false, want true")
	}
	// Allowlist overrides even when no domain rules: type blocking
	// should not apply to allowlisted domains.
	b2 := DefaultBlockedResources()
	b2.AllowedDomains = []string{"cdn.safe.com"}
	if b2.IsBlocked(ResourceFont, "cdn.safe.com") {
		t.Error("IsBlocked(font, cdn.safe.com) = true, want false (allowlist overrides type)")
	}
}

func TestIsDomainBlocked(t *testing.T) {
	b := BlockedResources{
		BlockedDomains: []string{"tracker.io"},
		AllowedDomains: []string{"ok.tracker.io"},
	}
	if !b.IsDomainBlocked("tracker.io") {
		t.Error("IsDomainBlocked(tracker.io) = false, want true")
	}
	if !b.IsDomainBlocked("ads.tracker.io") {
		t.Error("IsDomainBlocked(ads.tracker.io) = false, want true")
	}
	if b.IsDomainBlocked("ok.tracker.io") {
		t.Error("IsDomainBlocked(ok.tracker.io) = true, want false (allowlisted)")
	}
	if b.IsDomainBlocked("unrelated.com") {
		t.Error("IsDomainBlocked(unrelated.com) = true, want false")
	}
	if b.IsDomainBlocked("") {
		t.Error("IsDomainBlocked(\"\") = true, want false")
	}
}

func TestAddBlockedDomain(t *testing.T) {
	b := DefaultBlockedResources()
	if !b.AddBlockedDomain("evil.com") {
		t.Error("AddBlockedDomain(evil.com) = false, want true (new)")
	}
	if !b.IsDomainBlocked("evil.com") {
		t.Error("after AddBlockedDomain, IsDomainBlocked(evil.com) = false")
	}
	// Duplicate not added.
	if b.AddBlockedDomain("evil.com") {
		t.Error("AddBlockedDomain(evil.com) second time = true, want false (duplicate)")
	}
	// Empty rejected.
	if b.AddBlockedDomain("  ") {
		t.Error("AddBlockedDomain(whitespace) = true, want false")
	}
	// Case-insensitive dedup.
	if b.AddBlockedDomain("EVIL.com") {
		t.Error("AddBlockedDomain(EVIL.com) = true, want false (case-insensitive dup)")
	}
	// Subdomain match works after add.
	if !b.IsDomainBlocked("ads.evil.com") {
		t.Error("IsDomainBlocked(ads.evil.com) = false after adding evil.com")
	}
}

func TestAddAllowedDomain(t *testing.T) {
	b := DefaultBlockedResources()
	b.BlockedDomains = []string{"example.com"}
	if !b.AddAllowedDomain("good.example.com") {
		t.Error("AddAllowedDomain = false, want true (new)")
	}
	if b.IsBlocked(ResourceImage, "good.example.com") {
		t.Error("IsBlocked after AddAllowedDomain = true, want false")
	}
	if b.AddAllowedDomain("good.example.com") {
		t.Error("AddAllowedDomain second time = true, want false (duplicate)")
	}
	if b.AddAllowedDomain("") {
		t.Error("AddAllowedDomain(\"\") = true, want false")
	}
}

func TestToCDPPattern(t *testing.T) {
	b := DefaultBlockedResources()
	b.BlockedDomains = []string{"ads.com", "  tracker.io  "}
	patterns := b.ToCDPPattern()
	if len(patterns) != 2 {
		t.Fatalf("ToCDPPattern returned %d patterns, want 2", len(patterns))
	}
	sort.Strings(patterns)
	want := []string{"*://*.ads.com/*", "*://*.tracker.io/*"}
	for i, p := range patterns {
		if p != want[i] {
			t.Errorf("pattern[%d] = %q, want %q", i, p, want[i])
		}
	}
}

func TestToCDPPatternEmpty(t *testing.T) {
	b := DefaultBlockedResources()
	patterns := b.ToCDPPattern()
	if len(patterns) != 0 {
		t.Errorf("ToCDPPattern with no domains returned %v, want empty", patterns)
	}
}
