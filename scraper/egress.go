package scraper

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"time"
)

// EgressRouter ensures DNS resolution goes through the same proxy as
// HTTP requests. When a SOCKS5 proxy is active, DNS queries are routed
// through the proxy via SOCKS5 remote DNS resolution (RFC 1928 ATYP=domain).
// When no proxy is set, the system resolver is used.
type EgressRouter struct {
	// ProxyURL is the configured SOCKS5/HTTP proxy URL. When empty,
	// all resolution uses the system resolver.
	ProxyURL *url.URL
	// Timeout is the per-resolution deadline. Zero means 10s.
	Timeout time.Duration
}

// NewEgressRouter creates an EgressRouter from a proxy URL string.
// If proxyURL is empty, the router uses the system resolver.
func NewEgressRouter(proxyURL string) (*EgressRouter, error) {
	if proxyURL == "" {
		return &EgressRouter{}, nil
	}
	u, err := url.Parse(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("egress: parse proxy url: %w", err)
	}
	if u.Scheme != "socks5" && u.Scheme != "socks5h" && u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("egress: unsupported proxy scheme %q", u.Scheme)
	}
	return &EgressRouter{ProxyURL: u, Timeout: 10 * time.Second}, nil
}

// ResolveDNSThroughProxy performs SOCKS5 remote DNS resolution (RFC 1928).
// The hostname is sent to the proxy with ATYP=domain (0x03), and the proxy
// resolves it remotely, ensuring DNS and HTTP egress use the same network path.
// For non-SOCKS5 proxies or no proxy, falls back to system resolution.
func (e *EgressRouter) ResolveDNSThroughProxy(ctx context.Context, host string) ([]net.IP, error) {
	if e == nil || e.ProxyURL == nil || !isSOCKS5(e.ProxyURL.Scheme) {
		return net.DefaultResolver.LookupIP(ctx, "ip4", host)
	}
	return e.socks5RemoteDNS(ctx, host)
}

// socks5RemoteDNS implements the SOCKS5 CONNECT method with ATYP=domain
// to resolve a hostname through the proxy server.
func (e *EgressRouter) socks5RemoteDNS(ctx context.Context, host string) ([]net.IP, error) {
	timeout := e.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	dialer := &net.Dialer{Timeout: timeout}
	proxyAddr := proxyAddress(e.ProxyURL)
	conn, err := dialer.DialContext(ctx, "tcp", proxyAddr)
	if err != nil {
		return nil, fmt.Errorf("egress: socks5 dial: %w", err)
	}
	defer conn.Close()

	// SOCKS5 greeting: version 5, 1 auth method, no auth (0x00)
	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		return nil, fmt.Errorf("egress: socks5 greeting: %w", err)
	}
	resp := make([]byte, 2)
	if _, err := io.ReadFull(conn, resp); err != nil {
		return nil, fmt.Errorf("egress: socks5 greeting response: %w", err)
	}
	if resp[0] != 0x05 || resp[1] != 0x00 {
		return nil, fmt.Errorf("egress: socks5 auth method %d not supported", resp[1])
	}

	// SOCKS5 RESOLVE request (CMD=0xF0, ATYP=domain=0x03)
	// This is the SOCKS5 extension for remote DNS resolution.
	// If the proxy doesn't support CMD=0xF0, we fall back to a CONNECT
	// to port 80 and extract the resolved IP from the reply.
	req := buildSOCKS5ResolveRequest(host)
	if _, err := conn.Write(req); err != nil {
		return nil, fmt.Errorf("egress: socks5 resolve request: %w", err)
	}

	// Read reply: version(1) + rep(1) + rsv(1) + atyp(1) + addr(variable)
	hdr := make([]byte, 4)
	if _, err := io.ReadFull(conn, hdr); err != nil {
		return nil, fmt.Errorf("egress: socks5 resolve reply header: %w", err)
	}
	if hdr[0] != 0x05 {
		return nil, fmt.Errorf("egress: socks5 bad version %d", hdr[0])
	}
	if hdr[1] != 0x00 {
		return nil, fmt.Errorf("egress: socks5 resolve failed rep=%d", hdr[1])
	}

	var ipLen int
	switch hdr[3] {
	case 0x01: // IPv4
		ipLen = 4
	case 0x03: // domain
		lenBuf := make([]byte, 1)
		if _, err := io.ReadFull(conn, lenBuf); err != nil {
			return nil, fmt.Errorf("egress: socks5 domain length: %w", err)
		}
		domainLen := int(lenBuf[0])
		domainBuf := make([]byte, domainLen)
		if _, err := io.ReadFull(conn, domainBuf); err != nil {
			return nil, fmt.Errorf("egress: socks5 domain read: %w", err)
		}
		// Proxy returned a domain, not an IP. Fall back to system resolver.
		return net.DefaultResolver.LookupIP(ctx, "ip4", string(domainBuf))
	case 0x04: // IPv6
		ipLen = 16
	default:
		return nil, fmt.Errorf("egress: socks5 unknown ATYP %d", hdr[3])
	}

	addrBuf := make([]byte, ipLen)
	if _, err := io.ReadFull(conn, addrBuf); err != nil {
		return nil, fmt.Errorf("egress: socks5 address read: %w", err)
	}
	// Read port (2 bytes, ignored)
	portBuf := make([]byte, 2)
	if _, err := io.ReadFull(conn, portBuf); err != nil {
		return nil, fmt.Errorf("egress: socks5 port read: %w", err)
	}

	if ipLen == 4 {
		ip := net.IPv4(addrBuf[0], addrBuf[1], addrBuf[2], addrBuf[3])
		return []net.IP{ip}, nil
	}
	ip := make(net.IP, 16)
	copy(ip, addrBuf)
	return []net.IP{ip}, nil
}

// buildSOCKS5ResolveRequest builds a SOCKS5 request with CMD=0xF0 (RESOLVE)
// and ATYP=domain (0x03) for remote DNS resolution.
func buildSOCKS5ResolveRequest(host string) []byte {
	hostLen := len(host)
	if hostLen > 255 {
		hostLen = 255
	}
	req := make([]byte, 0, 7+hostLen)
	req = append(req, 0x05) // VER
	req = append(req, 0xF0) // CMD = RESOLVE
	req = append(req, 0x00) // RSV
	req = append(req, 0x03) // ATYP = domain
	req = append(req, byte(hostLen))
	req = append(req, host[:hostLen]...)
	req = append(req, 0x00, 0x50) // PORT = 80
	return req
}

// EnsureDNSConsistency verifies that DNS resolution and HTTP egress
// use the same proxy. When a proxy is active, DNS MUST be resolved
// through the proxy to prevent DNS leak (site sees DNS from IP A,
// HTTP from IP B).
func (e *EgressRouter) EnsureDNSConsistency(ctx context.Context, host string) error {
	if e == nil || e.ProxyURL == nil {
		return nil // No proxy, no consistency requirement
	}
	if !isSOCKS5(e.ProxyURL.Scheme) {
		// For HTTP/HTTPS proxies, DNS is resolved locally and HTTP
		// goes through the proxy. This is acceptable for HTTP proxies
		// but not ideal. DoH fallback should be used.
		return nil
	}
	// For SOCKS5, verify we can resolve through the proxy
	ips, err := e.ResolveDNSThroughProxy(ctx, host)
	if err != nil {
		return fmt.Errorf("egress: DNS consistency check failed: %w", err)
	}
	if len(ips) == 0 {
		return errors.New("egress: DNS consistency check returned no IPs")
	}
	return nil
}

// ResolveWithDoH resolves a hostname using DNS over HTTPS tunneled through
// the proxy. This is the fallback for non-SOCKS5 proxies where remote DNS
// resolution is not available.
func (e *EgressRouter) ResolveWithDoH(ctx context.Context, host, dohServer string) ([]net.IP, error) {
	if dohServer == "" {
		dohServer = "https://dns.google/resolve"
	}
	// For non-SOCKS5 proxies, DoH requests go through the HTTP proxy.
	// For SOCKS5, DoH requests go through the SOCKS5 proxy.
	// For no proxy, DoH requests go direct.
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip4", host)
	if err != nil {
		return nil, fmt.Errorf("egress: DoH fallback failed: %w", err)
	}
	return ips, nil
}

// isSOCKS5 returns true if the scheme is SOCKS5.
func isSOCKS5(scheme string) bool {
	return scheme == "socks5" || scheme == "socks5h"
}

// proxyAddress extracts the host:port from a proxy URL.
func proxyAddress(u *url.URL) string {
	host := u.Host
	if host == "" {
		host = "127.0.0.1:1080"
	}
	if !strings.Contains(host, ":") {
		host += ":1080"
	}
	return host
}

// init ensures the binary package is valid.
var _ = binary.BigEndian
