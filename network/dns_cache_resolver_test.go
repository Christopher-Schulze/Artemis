package network

import (
	"context"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestDNSCacheServesPolicyLookupsAndFiltersNetworks(t *testing.T) {
	cache, err := OpenDNSPrefetchCache("", 0)
	if err != nil {
		t.Fatal(err)
	}
	var lookups atomic.Int64
	cache.resolver = func(context.Context, string) ([]net.IP, error) {
		lookups.Add(1)
		return []net.IP{net.ParseIP("93.184.216.34"), net.ParseIP("2606:2800:220:1::248")}, nil
	}
	policy, err := NewPolicy(PolicyConfig{}, cache, nil)
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err := policy.ResolveURL(context.Background(), "https://cached.example/", TargetNavigation, "s"); err != nil {
			t.Fatal(err)
		}
	}
	if lookups.Load() != 1 {
		t.Fatalf("policy bypassed the cache: %d upstream lookups", lookups.Load())
	}
	v4, err := cache.LookupNetIP(context.Background(), "ip4", "cached.example")
	if err != nil || len(v4) != 1 || !v4[0].Is4() {
		t.Fatalf("ip4 filter failed: %v %v", v4, err)
	}
	v6, err := cache.LookupNetIP(context.Background(), "ip6", "cached.example")
	if err != nil || len(v6) != 1 || !v6[0].Is6() {
		t.Fatalf("ip6 filter failed: %v %v", v6, err)
	}
}

func TestDNSCacheBoundsMemoryEntries(t *testing.T) {
	cache, err := OpenDNSPrefetchCache("", 0)
	if err != nil {
		t.Fatal(err)
	}
	cache.resolver = func(context.Context, string) ([]net.IP, error) { return []net.IP{net.ParseIP("93.184.216.34")}, nil }
	for i := range maxDNSCacheEntries + 10 {
		if _, err := cache.Resolve(context.Background(), fmt.Sprintf("host-%d.example", i)); err != nil {
			t.Fatal(err)
		}
	}
	if size := len(cache.l1); size > maxDNSCacheEntries {
		t.Fatalf("L1 cache grew past its bound: %d", size)
	}
}

func TestDNSCachePersistsAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "browser", "dns_cache.db")
	first, err := OpenDNSPrefetchCache(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	first.resolver = func(context.Context, string) ([]net.IP, error) { return []net.IP{net.ParseIP("93.184.216.34")}, nil }
	if _, err := first.Resolve(context.Background(), "warm.example"); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := OpenDNSPrefetchCache(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := second.Close(); err != nil {
			t.Error(err)
		}
	}()
	second.resolver = func(context.Context, string) ([]net.IP, error) {
		return nil, errors.New("upstream resolver must not be used")
	}
	addresses, err := second.LookupNetIP(context.Background(), "ip", "warm.example")
	if err != nil || len(addresses) != 1 || addresses[0].String() != "93.184.216.34" {
		t.Fatalf("persistent cache was not used after restart: %v %v", addresses, err)
	}
}
