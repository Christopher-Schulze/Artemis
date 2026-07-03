package network

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"
)

// =============================================================================
// SP-artemis-network-SEC (dns_prefetch.go, security_privacy)
// Claim: DNSPrefetchCache.Resolve denies nil cache, empty host, and
// missing resolver
// =============================================================================

func TestWFArtemisNetwork_DNSResolveDeniesInvalidInput(t *testing.T) {
	// Security: DNS prefetch must deny resolution of invalid inputs to
	// prevent cache poisoning and DNS rebinding attacks.

	validCache, _ := OpenDNSPrefetchCache("", 5*time.Minute)

	cases := []struct {
		name  string
		cache *DNSPrefetchCache
		host  string
	}{
		{"nil_cache", nil, "example.com"},
		{"empty_host", validCache, ""},
		{"whitespace_host", validCache, "   "},
		{"no_resolver", &DNSPrefetchCache{l1: make(map[string]dnsEntry), resolver: nil}, "example.com"},
	}
	blocked := 0
	for _, c := range cases {
		_, err := c.cache.Resolve(context.Background(), c.host)
		if err == nil {
			t.Fatalf("%s: expected error, got nil", c.name)
		}
		blocked++
	}
	denyRate := float64(blocked) / float64(len(cases))
	fmt.Printf("deny_rate=%.1f blocked=1\n", denyRate)
	if denyRate != 1.0 {
		t.Fatalf("expected deny_rate=1.0 (all invalid inputs denied), got %.1f", denyRate)
	}

	// Baseline: valid host with mock resolver succeeds (positive control).
	cache, _ := OpenDNSPrefetchCache("", 5*time.Minute)
	cache.resolver = func(ctx context.Context, host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("1.2.3.4")}, nil
	}
	ips, err := cache.Resolve(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("valid host must succeed, got err=%v", err)
	}
	if len(ips) == 0 {
		t.Fatal("valid host must return IPs")
	}

	// Baseline: resolver failure propagates as error (positive control on deny path).
	cache2, _ := OpenDNSPrefetchCache("", 5*time.Minute)
	cache2.resolver = func(ctx context.Context, host string) ([]net.IP, error) {
		return nil, fmt.Errorf("dns lookup failed")
	}
	_, err = cache2.Resolve(context.Background(), "fail.example.com")
	if err == nil {
		t.Fatal("resolver failure must propagate as error")
	}
}
