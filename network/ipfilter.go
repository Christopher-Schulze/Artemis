package network

import (
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

// CheckHostPublic resolves the URL host and returns ErrPrivateIP if any
// resolved address is private/local. A host that fails to resolve
// returns nil (let the actual request error normally).
func CheckHostPublic(u *url.URL) error {
	if u == nil || u.Host == "" {
		return nil
	}
	host := u.Hostname()
	// numeric host: check directly
	if ip := net.ParseIP(host); ip != nil {
		if IsPrivateOrLocal(ip) {
			return ErrPrivateIP
		}
		return nil
	}
	addrs, err := net.LookupIP(host)
	if err != nil {
		return nil
	}
	for _, ip := range addrs {
		if IsPrivateOrLocal(ip) {
			return ErrPrivateIP
		}
	}
	return nil
}
