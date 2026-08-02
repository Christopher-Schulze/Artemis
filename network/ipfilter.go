package network

import (
	"context"
	"errors"
	"net"
	"net/url"
)

// ErrPrivateIP is returned when a request targets a private/internal
// address while BlockPrivateIPs is enabled.
var ErrPrivateIP = errors.New("ipfilter: target is a private/internal address")

// IsPrivateOrLocal reports whether ip is loopback, link-local,
// multicast, unspecified, or in one of the RFC1918 ranges.
func IsPrivateOrLocal(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsPrivate() {
		return true
	}
	// Carrier-grade NAT (RFC 6598)
	if ip4 := ip.To4(); ip4 != nil && ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
		return true
	}
	return false
}

func allPorts() []int {
	ports := make([]int, 65535)
	for i := range ports {
		ports[i] = i + 1
	}
	return ports
}

// CheckHostPublic returns ErrPrivateIP if the URL host resolves to a
// private, local, or otherwise non-public address, or if resolution
// fails. It is fail-closed: DNS failures and malformed inputs are
// treated as private and are denied. This function is retained as a
// compatibility wrapper around the unified Policy engine.
func CheckHostPublic(u *url.URL) error {
	if u == nil || u.Host == "" {
		return ErrPrivateIP
	}
	raw := u.String()
	if u.Scheme == "" {
		raw = "http://" + u.Host
	}
	policy, err := NewPolicy(PolicyConfig{AllowPrivateNetworks: false, AllowedPorts: allPorts()}, nil, nil)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), DefaultPolicyConfig().DialTimeout)
	defer cancel()
	if _, err := policy.ResolveURL(ctx, raw, TargetNavigation, ""); err != nil {
		return ErrPrivateIP
	}
	return nil
}
