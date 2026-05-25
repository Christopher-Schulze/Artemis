package network

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"
)

func TestDNSPrefetchCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dns.db")
	cache, err := OpenDNSPrefetchCache(path, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
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
	defer cache2.Close()
	cache2.resolver = cache.resolver
	ips3, err := cache2.Resolve(context.Background(), "example.com")
	if err != nil || len(ips3) == 0 {
		t.Fatalf("L2 reload failed: %v", err)
	}
}
