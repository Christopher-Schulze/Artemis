package network

import (
	"strings"
	"testing"
)

// BenchmarkIsAdTrackerDomainHit measures the IsAdTrackerDomain
// hot path for a matching domain (worst case: last pattern in the list).
func BenchmarkIsAdTrackerDomainHit(b *testing.B) {
	// "fls.doubleclick.net" is the last pattern in the builtin list.
	domain := "ads.fls.doubleclick.net"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !IsAdTrackerDomain(domain) {
			b.Fatal("expected match")
		}
	}
}

// BenchmarkIsAdTrackerDomainMiss measures the IsAdTrackerDomain
// hot path for a non-matching domain (worst case: scans all 48 patterns
// and never matches).
func BenchmarkIsAdTrackerDomainMiss(b *testing.B) {
	domain := "www.example.com"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if IsAdTrackerDomain(domain) {
			b.Fatal("expected no match")
		}
	}
}

// BenchmarkIsAdTrackerDomainExact measures the IsAdTrackerDomain
// hot path for an exact-match domain (best case: first pattern matches).
func BenchmarkIsAdTrackerDomainExact(b *testing.B) {
	domain := "doubleclick.net"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !IsAdTrackerDomain(domain) {
			b.Fatal("expected match")
		}
	}
}

// BenchmarkFilterEasyListRules measures the FilterEasyListRules
// hot path for a typical mix of rules (comments + real rules).
func BenchmarkFilterEasyListRules(b *testing.B) {
	rules := make([]string, 0, 200)
	for i := 0; i < 50; i++ {
		rules = append(rules, "! comment line "+strings.Repeat("x", i%20))
	}
	for i := 0; i < 150; i++ {
		rules = append(rules, "||ads.example.com^")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = FilterEasyListRules(rules)
	}
}

// BenchmarkFilterAdTrackerDomains measures the
// FilterAdTrackerDomains hot path for a batch of mixed domains.
func BenchmarkFilterAdTrackerDomains(b *testing.B) {
	domains := make([]string, 0, 100)
	for i := 0; i < 50; i++ {
		domains = append(domains, "ads.doubleclick.net")
	}
	for i := 0; i < 50; i++ {
		domains = append(domains, "www.example.com")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = FilterAdTrackerDomains(domains)
	}
}
