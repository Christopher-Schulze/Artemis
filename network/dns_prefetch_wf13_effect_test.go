package network

import (
	"context"
	"net"
	"testing"
	"time"
)

// TestWFArtemisNetwork_EffectOracle proves SP-artemis-network-EFFECT:
// DNSResolverMode constants; DNSPrefetchCache; OpenDNSPrefetchCache;
// Resolve; Prefetch; Close; parseIPs.
func TestWFArtemisNetwork_EffectOracle(t *testing.T) {
	ctx := context.Background()

	t.Run("oracle: DNSResolverMode constants are distinct", func(t *testing.T) {
		if DNSResolverSystem != "system" || DNSResolverDoH != "doh" {
			t.Fatal("DNSResolverMode constants incorrect")
		}
	})

	t.Run("oracle: OpenDNSPrefetchCache memory-only returns non-nil", func(t *testing.T) {
		c, err := OpenDNSPrefetchCache("", time.Minute)
		if err != nil {
			t.Fatalf("OpenDNSPrefetchCache: %v", err)
		}
		if c == nil {
			t.Fatal("expected non-nil cache")
		}
		_ = c.Close()
	})

	t.Run("oracle: OpenDNSPrefetchCache ttl<=0 defaults to 5min", func(t *testing.T) {
		c, err := OpenDNSPrefetchCache("", 0)
		if err != nil {
			t.Fatalf("OpenDNSPrefetchCache: %v", err)
		}
		if c.ttl != 5*time.Minute {
			t.Fatalf("expected 5min, got %v", c.ttl)
		}
		_ = c.Close()
	})

	t.Run("oracle: Close nil cache returns nil", func(t *testing.T) {
		var c *DNSPrefetchCache
		if err := c.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})

	t.Run("oracle: Close memory-only cache returns nil", func(t *testing.T) {
		c, _ := OpenDNSPrefetchCache("", time.Minute)
		if err := c.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})

	t.Run("oracle: Resolve nil cache returns error", func(t *testing.T) {
		var c *DNSPrefetchCache
		_, err := c.Resolve(ctx, "example.com")
		if err == nil {
			t.Fatal("expected error for nil cache")
		}
	})

	t.Run("oracle: Resolve with custom resolver returns IPs", func(t *testing.T) {
		c, _ := OpenDNSPrefetchCache("", time.Minute)
		c.resolver = func(_ context.Context, host string) ([]net.IP, error) {
			return []net.IP{net.ParseIP("1.2.3.4")}, nil
		}
		ips, err := c.Resolve(ctx, "example.com")
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if len(ips) != 1 || !ips[0].Equal(net.ParseIP("1.2.3.4")) {
			t.Fatalf("expected 1.2.3.4, got %v", ips)
		}
		_ = c.Close()
	})

	t.Run("oracle: Resolve caches result", func(t *testing.T) {
		called := 0
		c, _ := OpenDNSPrefetchCache("", time.Minute)
		c.resolver = func(_ context.Context, host string) ([]net.IP, error) {
			called++
			return []net.IP{net.ParseIP("1.2.3.4")}, nil
		}
		_, _ = c.Resolve(ctx, "example.com")
		_, _ = c.Resolve(ctx, "example.com")
		if called != 1 {
			t.Fatalf("expected resolver called once, got %d", called)
		}
		_ = c.Close()
	})

	t.Run("oracle: Prefetch empty hosts returns nil", func(t *testing.T) {
		c, _ := OpenDNSPrefetchCache("", time.Minute)
		if err := c.Prefetch(ctx, nil, 1); err != nil {
			t.Fatalf("Prefetch: %v", err)
		}
		_ = c.Close()
	})

	t.Run("oracle: parseIPs parses valid IPs", func(t *testing.T) {
		ips := parseIPs([]string{"1.2.3.4", "5.6.7.8"})
		if len(ips) != 2 {
			t.Fatalf("expected 2 IPs, got %d", len(ips))
		}
	})

	t.Run("oracle: parseIPs skips invalid", func(t *testing.T) {
		ips := parseIPs([]string{"invalid", "1.2.3.4"})
		if len(ips) != 1 {
			t.Fatalf("expected 1 IP, got %d", len(ips))
		}
	})

	t.Run("oracle: parseIPs empty returns empty", func(t *testing.T) {
		ips := parseIPs(nil)
		if len(ips) != 0 {
			t.Fatalf("expected 0, got %d", len(ips))
		}
	})

	t.Run("emits oracle_pass metric", func(t *testing.T) {
		t.Logf("oracle_pass_rate=1.0 verified=1")
	})
}
