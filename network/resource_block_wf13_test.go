package network

import (
	"fmt"
	"testing"
)

// =============================================================================
// SP-artemis-runtime-SEC (resource_block.go, security_privacy)
// Claim: IsBlocked denies blocked resource types from non-allowed domains,
// IsDomainBlocked denies blocked domains (with subdomain matching) unless
// allowlisted, and domainMatches denies empty inputs
// =============================================================================

func TestWFArtemisRuntime_ResourceBlockDeniesInvalidInput(t *testing.T) {
	// Security: resource blocker must deny blocked resource types and
	// domains to prevent unauthorized resource loading.

	cases := []struct {
		name string
		fn   func() bool // returns true if deny was correct
	}{
		{
			"is_blocked_default_type",
			func() bool {
				b := DefaultBlockedResources()
				return b.IsBlocked(ResourceImage, "ads.example.com")
			},
		},
		{
			"is_blocked_blocked_domain",
			func() bool {
				b := DefaultBlockedResources()
				b.AddBlockedDomain("evil.com")
				return b.IsBlocked(ResourceImage, "evil.com")
			},
		},
		{
			"is_blocked_blocked_subdomain",
			func() bool {
				b := DefaultBlockedResources()
				b.AddBlockedDomain("evil.com")
				return b.IsBlocked(ResourceImage, "sub.evil.com")
			},
		},
		{
			"is_domain_blocked_explicit",
			func() bool {
				b := DefaultBlockedResources()
				b.AddBlockedDomain("tracker.com")
				return b.IsDomainBlocked("tracker.com")
			},
		},
		{
			"is_domain_blocked_subdomain",
			func() bool {
				b := DefaultBlockedResources()
				b.AddBlockedDomain("tracker.com")
				return b.IsDomainBlocked("a.b.tracker.com")
			},
		},
		{
			"is_domain_blocked_empty_domain",
			func() bool {
				b := DefaultBlockedResources()
				b.AddBlockedDomain("evil.com")
				return b.IsDomainBlocked("") == false
			},
		},
		{
			"domain_matches_empty_candidate",
			func() bool {
				return domainMatches("", "example.com") == false
			},
		},
		{
			"domain_matches_empty_pattern",
			func() bool {
				return domainMatches("example.com", "") == false
			},
		},
		{
			"domain_matches_not_subdomain",
			func() bool {
				return domainMatches("notexample.com", "example.com") == false
			},
		},
		{
			"allowlist_overrides_blocked",
			func() bool {
				b := DefaultBlockedResources()
				b.AddBlockedDomain("example.com")
				b.AddAllowedDomain("example.com")
				return b.IsDomainBlocked("example.com") == false
			},
		},
		{
			"allowlist_overrides_is_blocked",
			func() bool {
				b := DefaultBlockedResources()
				b.AddAllowedDomain("safe.com")
				return b.IsBlocked(ResourceImage, "safe.com") == false
			},
		},
		{
			"non_blocked_type_allowed",
			func() bool {
				b := DefaultBlockedResources()
				return b.IsBlocked(ResourceType("custom"), "any.com") == false
			},
		},
	}
	blocked := 0
	for _, c := range cases {
		if !c.fn() {
			t.Fatalf("%s: expected deny behavior, got allow", c.name)
		}
		blocked++
	}
	denyRate := float64(blocked) / float64(len(cases))
	fmt.Printf("deny_rate=%.1f blocked=1\n", denyRate)
	if denyRate != 1.0 {
		t.Fatalf("expected deny_rate=1.0 (all invalid inputs denied), got %.1f", denyRate)
	}

	// Baseline: valid domainMatches succeeds (positive control)
	if !domainMatches("example.com", "example.com") {
		t.Fatal("expected exact match to succeed")
	}
	if !domainMatches("sub.example.com", "example.com") {
		t.Fatal("expected subdomain match to succeed")
	}

	// Baseline: AddBlockedDomain returns true for new domain, false for duplicate
	b := DefaultBlockedResources()
	if !b.AddBlockedDomain("newblock.com") {
		t.Fatal("expected AddBlockedDomain to return true for new domain")
	}
	if b.AddBlockedDomain("newblock.com") {
		t.Fatal("expected AddBlockedDomain to return false for duplicate, but got true")
	}
}
