package network

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"
)

// =============================================================================
// Claim: DNSPrefetchCache.Resolve denies nil cache, empty host, and
// missing resolver
// =============================================================================

func TestArtemisNetwork_DNSResolveDeniesInvalidInput(t *testing.T) {
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
	for _, c := range cases {
		_, err := c.cache.Resolve(context.Background(), c.host)
		if err == nil {
			t.Fatalf("%s: expected error, got nil", c.name)
		}
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
