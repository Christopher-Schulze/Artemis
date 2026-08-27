package scraper

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestNewEgressRouterEmpty(t *testing.T) {
	r, err := NewEgressRouter("")
	if err != nil {
		t.Fatal(err)
	}
	if r.ProxyURL != nil {
		t.Fatal("expected nil ProxyURL for empty proxy")
	}
}

func TestNewEgressRouterSOCKS5(t *testing.T) {
	r, err := NewEgressRouter("socks5://127.0.0.1:1080")
	if err != nil {
		t.Fatal(err)
	}
	if r.ProxyURL == nil || r.ProxyURL.Scheme != "socks5" {
		t.Fatalf("expected socks5 scheme, got %v", r.ProxyURL)
	}
}

func TestNewEgressRouterInvalidScheme(t *testing.T) {
	_, err := NewEgressRouter("ftp://127.0.0.1:21")
	if err == nil {
		t.Fatal("expected error for unsupported scheme")
	}
}

func TestNewEgressRouterBadURL(t *testing.T) {
	_, err := NewEgressRouter("://bad")
	if err == nil {
		t.Fatal("expected error for bad URL")
	}
}

func TestEnsureDNSConsistencyNoProxy(t *testing.T) {
	r, _ := NewEgressRouter("")
	err := r.EnsureDNSConsistency(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("no proxy should not require consistency: %v", err)
	}
}

func TestEnsureDNSConsistencyHTTPProxy(t *testing.T) {
	r, err := NewEgressRouter("http://127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	r.Timeout = 100 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := r.EnsureDNSConsistency(ctx, "example.com"); err == nil {
		t.Fatal("HTTP proxy without a reachable DoH path was accepted")
	}
}

func TestResolveDNSThroughProxyNoProxy(t *testing.T) {
	r, _ := NewEgressRouter("")
	ips, err := r.ResolveDNSThroughProxy(context.Background(), "localhost")
	if err != nil {
		t.Fatalf("system resolve failed: %v", err)
	}
	if len(ips) == 0 {
		t.Fatal("expected at least one IP for localhost")
	}
}

func TestBuildSOCKS5ResolveRequest(t *testing.T) {
	req := buildSOCKS5ResolveRequest("example.com")
	if len(req) < 7 {
		t.Fatalf("request too short: %d", len(req))
	}
	if req[0] != 0x05 {
		t.Fatalf("expected version 5, got %d", req[0])
	}
	if req[1] != 0xF0 {
		t.Fatalf("expected CMD RESOLVE 0xF0, got %d", req[1])
	}
	if req[3] != 0x03 {
		t.Fatalf("expected ATYP domain 0x03, got %d", req[3])
	}
	if int(req[4]) != len("example.com") {
		t.Fatalf("expected domain length %d, got %d", len("example.com"), req[4])
	}
}

func TestProxyAddressDefaultPort(t *testing.T) {
	u := &url.URL{Scheme: "socks5", Host: "127.0.0.1"}
	addr := proxyAddress(u)
	if addr != "127.0.0.1:1080" {
		t.Fatalf("expected 127.0.0.1:1080, got %s", addr)
	}
}

func TestProxyAddressExplicitPort(t *testing.T) {
	u := &url.URL{Scheme: "socks5", Host: "127.0.0.1:9999"}
	addr := proxyAddress(u)
	if addr != "127.0.0.1:9999" {
		t.Fatalf("expected 127.0.0.1:9999, got %s", addr)
	}
}

func TestResolveWithDoHFallback(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	endpoint := server.URL
	r := &EgressRouter{}
	ips, err := r.resolveWithDoHClient(context.Background(), "localhost", endpoint, server.Client())
	if err != nil {
		t.Fatalf("DoH fallback failed: %v", err)
	}
	if len(ips) == 0 {
		t.Fatal("expected at least one IP")
	}
}

func TestResolveWithDoHQueriesIPv4ThroughClient(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("name"); got != "example.test" {
			t.Errorf("query name = %q, want example.test", got)
		}
		if got := r.URL.Query().Get("type"); got != "A" {
			t.Errorf("query type = %q, want A", got)
		}
		w.Header().Set("Content-Type", "application/dns-json")
		if _, err := w.Write([]byte(`{"Status":0,"Answer":[{"type":1,"data":"192.0.2.7"}]}`)); err != nil {
			t.Errorf("write DoH response: %v", err)
		}
	}))
	defer server.Close()
	endpoint := server.URL
	r := &EgressRouter{}
	ips, err := r.resolveWithDoHClient(context.Background(), "example.test", endpoint, server.Client())
	if err != nil {
		t.Fatalf("DoH query failed: %v", err)
	}
	if len(ips) != 1 || ips[0].String() != "192.0.2.7" {
		t.Fatalf("DoH answers = %v, want [192.0.2.7]", ips)
	}
}

func TestResolveWithDoHRejectsInsecureEndpoint(t *testing.T) {
	r := &EgressRouter{}
	_, err := r.resolveWithDoHClient(context.Background(), "example.test", "http://dns.example/resolve", &http.Client{})
	if err == nil {
		t.Fatal("insecure DoH endpoint was accepted")
	}
}
