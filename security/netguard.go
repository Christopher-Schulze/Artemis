package security

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// netguard.go (spec L4027: security/netguard.go - SSRF prevention:
// private IP blocking).
//
// Browser security: SSRF (Server-Side Request Forgery) prevention by
// blocking requests to private/internal IP addresses. This prevents
// the browser from being used to scan or access internal network
// resources.

// NetGuard implements SSRF prevention by blocking private IP ranges
// (spec L4027: SSRF prevention private IP blocking).
type NetGuard struct {
	allowList []string // explicitly allowed IPs/domains
	blockList []string // explicitly blocked IPs/domains
}

// NewNetGuard creates a new NetGuard with default private IP blocking
// (spec L4027: SSRF prevention).
func NewNetGuard() *NetGuard {
	return &NetGuard{}
}

// Allow adds an IP or domain to the allow list
// (spec L4027: SSRF prevention).
func (n *NetGuard) Allow(host string) {
	if n == nil {
		return
	}
	n.allowList = append(n.allowList, strings.ToLower(host))
}

// Block adds an IP or domain to the block list
// (spec L4027: SSRF prevention).
func (n *NetGuard) Block(host string) {
	if n == nil {
		return
	}
	n.blockList = append(n.blockList, strings.ToLower(host))
}

// IsPrivateIP reports whether an IP address is in a private/reserved
// range (spec L4027: private IP blocking).
// Blocks: 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, 127.0.0.0/8,
// 169.254.0.0/16 (link-local), 0.0.0.0/8, 100.64.0.0/10 (CGNAT),
// 198.18.0.0/15 (benchmark testing), ::1, fc00::/7, fe80::/10.
func IsPrivateIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	// Check IPv4 private ranges
	if ip4 := ip.To4(); ip4 != nil {
		switch {
		case ip4[0] == 10: // 10.0.0.0/8
			return true
		case ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31: // 172.16.0.0/12
			return true
		case ip4[0] == 192 && ip4[1] == 168: // 192.168.0.0/16
			return true
		case ip4[0] == 127: // 127.0.0.0/8 (loopback)
			return true
		case ip4[0] == 169 && ip4[1] == 254: // 169.254.0.0/16 (link-local)
			return true
		case ip4[0] == 0: // 0.0.0.0/8
			return true
		case ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127: // 100.64.0.0/10 (CGNAT)
			return true
		case ip4[0] == 198 && ip4[1] >= 18 && ip4[1] <= 19: // 198.18.0.0/15 (benchmark)
			return true
		}
		return false
	}
	// Check IPv6 private ranges
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	// Check fc00::/7 (unique local addresses)
	if len(ip) == 16 {
		if ip[0]&0xfe == 0xfc {
			return true
		}
	}
	return false
}

// CheckURL checks whether a URL is safe to request
// (spec L4027: SSRF prevention private IP blocking).
// Returns an error if the URL points to a private IP or blocked host.
func (n *NetGuard) CheckURL(rawURL string) error {
	if n == nil {
		return fmt.Errorf("netguard: nil guard")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("netguard: invalid URL %q: %w", rawURL, err)
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return fmt.Errorf("netguard: empty host in URL %q", rawURL)
	}
	// Check explicit allow list (overrides private IP check)
	for _, allowed := range n.allowList {
		if host == allowed {
			return nil
		}
	}
	// Check explicit block list
	for _, blocked := range n.blockList {
		if host == blocked {
			return fmt.Errorf("netguard: host %q is blocked", host)
		}
	}
	// Check if host is a private IP
	ip := net.ParseIP(host)
	if ip != nil {
		if IsPrivateIP(ip) {
			return fmt.Errorf("netguard: private IP %q blocked (SSRF prevention)", host)
		}
		return nil
	}
	// Host is a domain name - not a private IP, allow
	return nil
}

// IsBlocked reports whether a URL would be blocked
// (spec L4027: SSRF prevention).
func (n *NetGuard) IsBlocked(rawURL string) bool {
	if n == nil {
		return true
	}
	return n.CheckURL(rawURL) != nil
}

// IsAllowed reports whether a URL would be allowed
// (spec L4027: SSRF prevention).
func (n *NetGuard) IsAllowed(rawURL string) bool {
	if n == nil {
		return false
	}
	return n.CheckURL(rawURL) == nil
}

// String returns a diagnostic summary.
func (n *NetGuard) String() string {
	if n == nil {
		return "NetGuard(nil)"
	}
	return fmt.Sprintf("NetGuard{allow:%d block:%d}", len(n.allowList), len(n.blockList))
}
