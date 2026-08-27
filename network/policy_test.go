package network

import (
	"context"
	"errors"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"testing"
)

type resolverResult struct {
	addresses []netip.Addr
	err       error
}

type sequenceResolver struct {
	mu      sync.Mutex
	results []resolverResult
	calls   int
}

func (r *sequenceResolver) LookupNetIP(_ context.Context, _, _ string) ([]netip.Addr, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	if len(r.results) == 0 {
		return nil, errors.New("no resolver result")
	}
	result := r.results[0]
	if len(r.results) > 1 {
		r.results = r.results[1:]
	}
	return append([]netip.Addr(nil), result.addresses...), result.err
}

func mustPolicy(t *testing.T, config PolicyConfig, resolver Resolver, sink DecisionSink) *Policy {
	t.Helper()
	policy, err := NewPolicy(config, resolver, sink)
	if err != nil {
		t.Fatalf("NewPolicy: %v", err)
	}
	return policy
}

func TestDefaultPolicyDeniesPrivateAndMetadataTargets(t *testing.T) {
	policy := mustPolicy(t, PolicyConfig{}, nil, nil)
	for _, rawURL := range []string{
		"http://127.0.0.1/",
		"http://169.254.169.254/latest/meta-data/",
		"http://[::1]/",
		"http://2130706433/",
		"http://0177.0.0.1/",
		"http://0x7f000001/",
		"http://localhost/",
		"http://metadata.google.internal/",
	} {
		t.Run(rawURL, func(t *testing.T) {
			if _, err := policy.ResolveURL(context.Background(), rawURL, TargetNavigation, "session-a"); !errors.Is(err, ErrPolicyDenied) {
				t.Fatalf("ResolveURL(%q) error = %v, want policy denial", rawURL, err)
			}
		})
	}
}

func TestAllowPrivateNetworksStillDeniesNonDestinationAddresses(t *testing.T) {
	config := DefaultPolicyConfig()
	config.AllowPrivateNetworks = true
	policy := mustPolicy(t, config, nil, nil)
	for _, rawURL := range []string{
		"http://0.0.0.0/",
		"http://[::]/",
		"http://224.0.0.1/",
		"http://[ff02::1]/",
	} {
		t.Run(rawURL, func(t *testing.T) {
			if _, err := policy.ResolveURL(context.Background(), rawURL, TargetWebSocket, ""); !errors.Is(err, ErrPolicyDenied) {
				t.Fatalf("ResolveURL(%q) error = %v, want non-destination denial", rawURL, err)
			}
		})
	}
}

func TestPolicyDeniesMixedDNSAnswersAndResolutionFailure(t *testing.T) {
	resolver := &sequenceResolver{results: []resolverResult{
		{addresses: []netip.Addr{netip.MustParseAddr("1.1.1.1"), netip.MustParseAddr("10.0.0.1")}},
		{err: errors.New("NXDOMAIN")},
	}}
	policy := mustPolicy(t, PolicyConfig{}, resolver, nil)
	if _, err := policy.ResolveURL(context.Background(), "https://mixed.example/", TargetNavigation, ""); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("mixed DNS result error = %v, want denial", err)
	}
	if _, err := policy.ResolveURL(context.Background(), "https://missing.example/", TargetNavigation, ""); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("DNS failure error = %v, want denial", err)
	}
}

func TestPolicyRevalidatesDNSAtDialBoundary(t *testing.T) {
	resolver := &sequenceResolver{results: []resolverResult{
		{addresses: []netip.Addr{netip.MustParseAddr("1.1.1.1")}},
		{addresses: []netip.Addr{netip.MustParseAddr("127.0.0.1")}},
	}}
	policy := mustPolicy(t, PolicyConfig{}, resolver, nil)
	if _, err := policy.ResolveURL(context.Background(), "https://rebind.example/", TargetNavigation, ""); err != nil {
		t.Fatalf("initial resolution: %v", err)
	}
	if _, err := policy.DialContext(context.Background(), "tcp", "rebind.example:443"); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("rebound dial error = %v, want policy denial", err)
	}
}

func TestPolicyValidatesRequestBoundaries(t *testing.T) {
	resolver := &sequenceResolver{results: []resolverResult{{addresses: []netip.Addr{netip.MustParseAddr("1.1.1.1")}}}}
	config := DefaultPolicyConfig()
	config.AllowedDomains = []string{"api.example"}
	config.MaxRequestBodyBytes = 8
	policy := mustPolicy(t, config, resolver, nil)
	credentialURL := (&url.URL{Scheme: "https", Host: "api.example", User: url.UserPassword("user", t.Name())}).String()
	tests := []struct {
		name        string
		rawURL      string
		method      string
		contentType string
		length      int64
	}{
		{name: "credentials", rawURL: credentialURL, method: "GET"},
		{name: "scheme", rawURL: "file://api.example/etc/passwd", method: "GET"},
		{name: "domain", rawURL: "https://evil-api.example/", method: "GET"},
		{name: "port", rawURL: "https://api.example:8443/", method: "GET"},
		{name: "method", rawURL: "https://api.example/", method: "TRACE"},
		{name: "body", rawURL: "https://api.example/", method: "POST", contentType: "application/json", length: 9},
		{name: "content-type", rawURL: "https://api.example/", method: "POST", contentType: "application/octet-stream", length: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := policy.ValidateRequest(context.Background(), test.rawURL, test.method, test.contentType, test.length, TargetNavigation, ""); !errors.Is(err, ErrPolicyDenied) {
				t.Fatalf("ValidateRequest error = %v, want denial", err)
			}
		})
	}
}

func TestPolicyDomainWildcardRequiresLabelBoundary(t *testing.T) {
	resolver := &sequenceResolver{results: []resolverResult{
		{addresses: []netip.Addr{netip.MustParseAddr("1.1.1.1")}},
		{addresses: []netip.Addr{netip.MustParseAddr("1.1.1.1")}},
	}}
	config := DefaultPolicyConfig()
	config.AllowedDomains = []string{"*.example.com"}
	policy := mustPolicy(t, config, resolver, nil)
	if _, err := policy.ResolveURL(context.Background(), "https://api.example.com/", TargetNavigation, ""); err != nil {
		t.Fatalf("subdomain rejected: %v", err)
	}
	if _, err := policy.ResolveURL(context.Background(), "https://evil-example.com/", TargetNavigation, ""); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("boundary bypass error = %v, want denial", err)
	}
	if _, err := policy.ResolveURL(context.Background(), "https://example.com/", TargetNavigation, ""); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("wildcard apex error = %v, want denial", err)
	}
}

func TestPolicyNormalizesIDNAAndEmitsRedactedDecision(t *testing.T) {
	resolver := &sequenceResolver{results: []resolverResult{{addresses: []netip.Addr{netip.MustParseAddr("1.1.1.1")}}}}
	config := DefaultPolicyConfig()
	config.AllowedDomains = []string{"xn--bcher-kva.example"}
	var decisions []Decision
	policy := mustPolicy(t, config, resolver, func(decision Decision) error {
		decisions = append(decisions, decision)
		return nil
	})
	if _, err := policy.ResolveURL(context.Background(), "https://bücher.example/private?token=secret", TargetNavigation, "session-a"); err != nil {
		t.Fatalf("IDNA URL rejected: %v", err)
	}
	if len(decisions) != 1 || decisions[0].Host != "xn--bcher-kva.example" || decisions[0].SessionID != "session-a" {
		t.Fatalf("unexpected decision: %+v", decisions)
	}
	encoded := decisions[0].Host + decisions[0].Reason
	if strings.Contains(encoded, "private") || strings.Contains(encoded, "secret") {
		t.Fatalf("decision leaked path or query: %+v", decisions[0])
	}
}

func TestPolicyFailsClosedWhenDecisionAuditFails(t *testing.T) {
	resolver := &sequenceResolver{results: []resolverResult{{addresses: []netip.Addr{netip.MustParseAddr("1.1.1.1")}}}}
	policy := mustPolicy(t, PolicyConfig{}, resolver, func(Decision) error { return errors.New("ledger unavailable") })
	if _, err := policy.ResolveURL(context.Background(), "https://example.test/", TargetNavigation, "session-a"); !errors.Is(err, ErrPolicyDenied) || !errors.Is(err, ErrDecisionAudit) {
		t.Fatalf("allow audit failure error=%v", err)
	}
	if _, err := policy.ResolveURL(context.Background(), "http://127.0.0.1/", TargetNavigation, "session-a"); !errors.Is(err, ErrPolicyDenied) || !errors.Is(err, ErrDecisionAudit) {
		t.Fatalf("deny audit failure error=%v", err)
	}
}

func TestPolicyConfigurationCopyIsImmutable(t *testing.T) {
	policy := mustPolicy(t, PolicyConfig{}, nil, nil)
	copy := policy.Config()
	copy.AllowedPorts[0] = 1
	if policy.Config().AllowedPorts[0] == 1 {
		t.Fatal("Config exposed mutable policy state")
	}
}

func TestNewPolicyDoesNotMutateInputPorts(t *testing.T) {
	ports := []int{443, 80}
	if _, err := NewPolicy(PolicyConfig{AllowedPorts: ports}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if ports[0] != 443 || ports[1] != 80 {
		t.Fatalf("input ports mutated: %v", ports)
	}
}

func FuzzPolicyNeverAllowsMalformedOrPrivateHosts(f *testing.F) {
	for _, rawURL := range []string{
		"http://127.0.0.1/", "http://2130706433/", "http://0x7f000001/",
		"http://[::1]/", "file:///etc/passwd", "http://user:pass@example.com/",
	} {
		f.Add(rawURL)
	}
	policy, err := NewPolicy(PolicyConfig{}, nil, nil)
	if err != nil {
		f.Fatalf("NewPolicy: %v", err)
	}
	f.Fuzz(func(t *testing.T, rawURL string) {
		addresses, err := policy.ResolveURL(context.Background(), rawURL, TargetNavigation, "")
		if err != nil {
			return
		}
		for _, address := range addresses {
			if blockedAddress(address) {
				t.Fatalf("policy allowed blocked address %s for %q", address, rawURL)
			}
		}
	})
}
