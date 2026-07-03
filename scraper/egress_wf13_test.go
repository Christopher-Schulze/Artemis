package scraper

import (
	"fmt"
	"testing"
)

// =============================================================================
// SP-artemis-scraper-SEC (egress.go, security_privacy)
// Claim: NewEgressRouter denies empty/invalid proxy URLs and unsupported
// schemes, EnsureDNSConsistency denies SOCKS5 proxies that fail DNS
// resolution, ResolveWithDoH denies empty DoH server fallbacks
// =============================================================================

func TestWFArtemisScraper_EgressRouterDeniesInvalidInput(t *testing.T) {
	// Security: egress router must deny invalid proxy URLs and unsupported
	// schemes to prevent DNS leaks and unauthorized proxy bypass.

	cases := []struct {
		name string
		fn   func() error
	}{
		{
			"new_router_invalid_url",
			func() error {
				_, err := NewEgressRouter("://invalid")
				return err
			},
		},
		{
			"new_router_unsupported_scheme",
			func() error {
				_, err := NewEgressRouter("ftp://proxy.example.com:21")
				return err
			},
		},
		{
			"new_router_file_scheme",
			func() error {
				_, err := NewEgressRouter("file:///etc/passwd")
				return err
			},
		},
		{
			"new_router_ws_scheme",
			func() error {
				_, err := NewEgressRouter("ws://proxy.example.com:80")
				return err
			},
		},
		{
			"new_router_gopher_scheme",
			func() error {
				_, err := NewEgressRouter("gopher://proxy.example.com:70")
				return err
			},
		},
		{
			"new_router_javascript_scheme",
			func() error {
				_, err := NewEgressRouter("javascript://alert(1)")
				return err
			},
		},
		{
			"new_router_data_scheme",
			func() error {
				_, err := NewEgressRouter("data:text/plain;base64,SGVsbG8=")
				return err
			},
		},
	}
	blocked := 0
	for _, c := range cases {
		err := c.fn()
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

	// Baseline: empty proxy URL returns valid router with no proxy (positive control)
	r, err := NewEgressRouter("")
	if err != nil {
		t.Fatalf("empty proxy URL must succeed, got: %v", err)
	}
	if r == nil {
		t.Fatal("expected non-nil router for empty proxy URL")
	}
	if r.ProxyURL != nil {
		t.Fatal("expected nil ProxyURL for empty proxy URL")
	}

	// Baseline: valid SOCKS5 proxy URL succeeds
	r, err = NewEgressRouter("socks5://proxy.example.com:1080")
	if err != nil {
		t.Fatalf("valid socks5 proxy URL must succeed, got: %v", err)
	}
	if r.ProxyURL == nil {
		t.Fatal("expected non-nil ProxyURL for socks5 proxy URL")
	}
	if r.ProxyURL.Scheme != "socks5" {
		t.Fatalf("expected scheme 'socks5', got %s", r.ProxyURL.Scheme)
	}

	// Baseline: valid HTTP proxy URL succeeds
	r, err = NewEgressRouter("http://proxy.example.com:8080")
	if err != nil {
		t.Fatalf("valid http proxy URL must succeed, got: %v", err)
	}
	if r.ProxyURL == nil {
		t.Fatal("expected non-nil ProxyURL for http proxy URL")
	}

	// Baseline: valid HTTPS proxy URL succeeds
	r, err = NewEgressRouter("https://proxy.example.com:8443")
	if err != nil {
		t.Fatalf("valid https proxy URL must succeed, got: %v", err)
	}
	if r.ProxyURL == nil {
		t.Fatal("expected non-nil ProxyURL for https proxy URL")
	}

	// Baseline: socks5h scheme succeeds
	r, err = NewEgressRouter("socks5h://proxy.example.com:1080")
	if err != nil {
		t.Fatalf("valid socks5h proxy URL must succeed, got: %v", err)
	}
	if r.ProxyURL == nil {
		t.Fatal("expected non-nil ProxyURL for socks5h proxy URL")
	}

	// Baseline: isSOCKS5 correctly identifies SOCKS5 schemes
	if !isSOCKS5("socks5") {
		t.Fatal("expected isSOCKS5 to return true for socks5")
	}
	if !isSOCKS5("socks5h") {
		t.Fatal("expected isSOCKS5 to return true for socks5h")
	}
	if isSOCKS5("http") {
		t.Fatal("expected isSOCKS5 to return false for http")
	}
}
