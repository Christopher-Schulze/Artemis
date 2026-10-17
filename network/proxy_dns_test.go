package network

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
)

// countingResolver records every hostname the policy resolves locally.
type countingResolver struct {
	mu    sync.Mutex
	hosts []string
}

func (r *countingResolver) LookupNetIP(_ context.Context, _ string, host string) ([]netip.Addr, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hosts = append(r.hosts, host)
	if address, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{address}, nil
	}
	return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
}

func (r *countingResolver) lookups(host string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	count := 0
	for _, seen := range r.hosts {
		if seen == host {
			count++
		}
	}
	return count
}

func TestProxiedAdmissionNeverResolvesTargetsLocallyButStaysFailClosed(t *testing.T) {
	resolver := &countingResolver{}
	policy, err := NewPolicy(PolicyConfig{}, resolver, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := policy.ValidateProxiedRequest(ctx, "https://target.example/path", http.MethodGet, "", 0, TargetNavigation, "s"); err != nil {
		t.Fatalf("proxied hostname target denied: %v", err)
	}
	if resolver.lookups("target.example") != 0 {
		t.Fatal("proxied admission resolved the target locally")
	}
	for _, denied := range []string{"https://10.0.0.7/", "https://[::1]/", "https://localhost/", "ftp://target.example/", "https://target.example:2222/"} {
		if err := policy.ValidateProxiedRequest(ctx, denied, http.MethodGet, "", 0, TargetNavigation, "s"); !errors.Is(err, ErrPolicyDenied) {
			t.Fatalf("proxied admission let %s through: %v", denied, err)
		}
	}
	if err := policy.ValidateRequest(ctx, "https://target.example/path", http.MethodGet, "", 0, TargetNavigation, "s"); err != nil || resolver.lookups("target.example") != 1 {
		t.Fatalf("direct admission stopped resolving and checking addresses: err=%v lookups=%d", err, resolver.lookups("target.example"))
	}
}

func TestHTTPClientWithProxyKeepsTargetDNSAtTheProxy(t *testing.T) {
	resolver := &countingResolver{}
	proxied := make(chan string, 4)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxied <- r.URL.String()
		w.Header().Set("Content-Type", "text/plain")
		if _, err := w.Write([]byte("via-proxy")); err != nil {
			t.Errorf("write proxy response: %v", err)
		}
	}))
	defer proxy.Close()
	proxyPort := proxy.Listener.Addr().(*net.TCPAddr).Port
	policy, err := NewPolicy(PolicyConfig{AllowPrivateNetworks: true, AllowedPorts: []int{80, proxyPort}}, resolver, nil)
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewHTTPClient(HTTPClientConfig{Policy: policy, ProxyURL: proxy.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	response, err := client.Do(context.Background(), Request{URL: "http://unresolvable-target.invalid/page"})
	if err != nil {
		t.Fatalf("proxied request failed: %v", err)
	}
	if !strings.Contains(string(response.Body), "via-proxy") || <-proxied != "http://unresolvable-target.invalid/page" {
		t.Fatalf("request did not travel through the proxy: %q", response.Body)
	}
	if lookups := resolver.lookups("unresolvable-target.invalid"); lookups != 0 {
		t.Fatalf("target DNS leaked to the local resolver %d time(s)", lookups)
	}
}
