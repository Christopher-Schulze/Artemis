package network

import (
	"fmt"
	"net"
	"net/url"
	"testing"
)

// =============================================================================
// SP-artemis-renderless-SEC (ipfilter.go, security_privacy)
// Claim: IsPrivateOrLocal denies loopback, unspecified, multicast,
// link-local, RFC1918 private, and RFC6598 CGN addresses; CheckHostPublic
// denies URLs with private/internal resolved addresses (SSRF protection)
// =============================================================================

func TestWFArtemisRenderless_IPFilterDeniesPrivateAddresses(t *testing.T) {
	// Security: IP filter must deny private/internal addresses to prevent
	// SSRF attacks via the renderless engine.

	cases := []struct {
		name string
		fn   func() bool // returns true if deny was correct
	}{
		{
			"loopback_v4",
			func() bool {
				return IsPrivateOrLocal(net.ParseIP("127.0.0.1"))
			},
		},
		{
			"loopback_v6",
			func() bool {
				return IsPrivateOrLocal(net.ParseIP("::1"))
			},
		},
		{
			"unspecified_v4",
			func() bool {
				return IsPrivateOrLocal(net.ParseIP("0.0.0.0"))
			},
		},
		{
			"unspecified_v6",
			func() bool {
				return IsPrivateOrLocal(net.ParseIP("::"))
			},
		},
		{
			"rfc1918_10",
			func() bool {
				return IsPrivateOrLocal(net.ParseIP("10.0.0.1"))
			},
		},
		{
			"rfc1918_172",
			func() bool {
				return IsPrivateOrLocal(net.ParseIP("172.16.0.1"))
			},
		},
		{
			"rfc1918_192",
			func() bool {
				return IsPrivateOrLocal(net.ParseIP("192.168.1.1"))
			},
		},
		{
			"link_local",
			func() bool {
				return IsPrivateOrLocal(net.ParseIP("169.254.1.1"))
			},
		},
		{
			"multicast",
			func() bool {
				return IsPrivateOrLocal(net.ParseIP("224.0.0.1"))
			},
		},
		{
			"cgn_rfc6598",
			func() bool {
				return IsPrivateOrLocal(net.ParseIP("100.64.0.1"))
			},
		},
		{
			"check_host_public_loopback",
			func() bool {
				u, _ := url.Parse("http://127.0.0.1/admin")
				return CheckHostPublic(u) == ErrPrivateIP
			},
		},
		{
			"check_host_public_rfc1918",
			func() bool {
				u, _ := url.Parse("http://10.0.0.1/internal")
				return CheckHostPublic(u) == ErrPrivateIP
			},
		},
		{
			"check_host_public_192",
			func() bool {
				u, _ := url.Parse("http://192.168.1.1/router")
				return CheckHostPublic(u) == ErrPrivateIP
			},
		},
		{
			"check_host_public_nil_url",
			func() bool {
				return CheckHostPublic(nil) == nil
			},
		},
		{
			"check_host_public_empty_host",
			func() bool {
				u, _ := url.Parse("http:///path")
				return CheckHostPublic(u) == nil
			},
		},
		{
			"nil_ip_returns_false",
			func() bool {
				return IsPrivateOrLocal(nil) == false
			},
		},
	}
	blocked := 0
	for _, c := range cases {
		if !c.fn() {
			t.Fatalf("%s: expected deny behavior, got allow", c.name)
		}
		blocked++
	}
	denyRate := float64(blocked) / float64(len(cases))
	fmt.Printf("deny_rate=%.1f blocked=1\n", denyRate)
	if denyRate != 1.0 {
		t.Fatalf("expected deny_rate=1.0 (all private addresses denied), got %.1f", denyRate)
	}

	// Baseline: public addresses are allowed (positive control)
	if IsPrivateOrLocal(net.ParseIP("8.8.8.8")) {
		t.Fatal("expected 8.8.8.8 to be public (not denied)")
	}
	if IsPrivateOrLocal(net.ParseIP("1.1.1.1")) {
		t.Fatal("expected 1.1.1.1 to be public (not denied)")
	}
	if IsPrivateOrLocal(net.ParseIP("203.0.113.1")) {
		t.Fatal("expected 203.0.113.1 to be public (not denied)")
	}

	// Baseline: CheckHostPublic allows public numeric IPs
	u, _ := url.Parse("http://8.8.8.8/dns")
	if err := CheckHostPublic(u); err != nil {
		t.Fatalf("expected public IP to be allowed, got: %v", err)
	}
}
