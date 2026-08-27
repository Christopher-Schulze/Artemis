package network

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"
)

func closeTestDNSCache(t *testing.T, cache *DNSPrefetchCache) {
	t.Helper()
	if err := cache.Close(); err != nil {
		t.Errorf("close DNS cache: %v", err)
	}
}

func TestDNSPrefetchCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dns.db")
	cache, err := OpenDNSPrefetchCache(path, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestDNSCache(t, cache)
	cache.resolver = func(ctx context.Context, host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	}
	ips, err := cache.Resolve(context.Background(), "example.com")
	if err != nil || len(ips) == 0 {
		t.Fatalf("resolve: %v %v", ips, err)
	}
	ips2, err := cache.Resolve(context.Background(), "example.com")
	if err != nil || len(ips2) == 0 {
		t.Fatal(err)
	}
	cache2, err := OpenDNSPrefetchCache(path, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestDNSCache(t, cache2)
	cache2.resolver = cache.resolver
	ips3, err := cache2.Resolve(context.Background(), "example.com")
	if err != nil || len(ips3) == 0 {
		t.Fatalf("L2 reload failed: %v", err)
	}
}

func TestDNSPrefetchCachePersistError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dns.db")
	cache, err := OpenDNSPrefetchCache(path, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	resolveCalls := 0
	cache.resolver = func(ctx context.Context, host string) ([]net.IP, error) {
		resolveCalls++
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	}
	if closeErr := cache.db.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	_, err = cache.Resolve(context.Background(), "example.com")
	if err == nil {
		t.Fatal("expected persist error from closed DNS cache DB")
	}
	_, err = cache.Resolve(context.Background(), "example.com")
	if err == nil {
		t.Fatal("failed persistence must not publish a successful L1 entry")
	}
	if resolveCalls != 2 {
		t.Fatalf("resolver calls = %d, want 2 after two failed publications", resolveCalls)
	}
}

func TestDNSPrefetchRejectsMissingContext(t *testing.T) {
	cache, err := OpenDNSPrefetchCache("", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	var invalidContext context.Context
	if _, err := cache.Resolve(invalidContext, "example.com"); err == nil {
		t.Fatal("Resolve accepted nil context")
	}
	if err := cache.Prefetch(invalidContext, []string{"example.com"}, 1); err == nil {
		t.Fatal("Prefetch accepted nil context")
	}
}
