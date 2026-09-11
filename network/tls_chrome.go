package network

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	utls "github.com/Christopher-Schulze/Artemis/third_party/utls"
	"golang.org/x/net/http2"
)

// chromeTransport serves HTTPS requests through uTLS with a real Chrome
// ClientHello (JA3/JA4 parity) and routes each host to HTTP/2 or HTTP/1.1
// based on the negotiated ALPN. Go's crypto/tls fingerprint is one of the
// strongest non-browser bot signals; ChromeLike mode must fix the wire.
type chromeTransport struct {
	dialContext func(context.Context, string, string) (net.Conn, error)
	proxyURL    *url.URL
	tlsConfig   *utls.Config

	h1 *http.Transport  // HTTPS over uTLS (ALPN http/1.1)
	h2 *http2.Transport // HTTPS over uTLS (ALPN h2)

	mu    sync.Mutex
	proto map[string]string // host:port -> negotiated ALPN, bounded
}

const chromeALPNCacheCap = 4096

// newChromeTransport builds the Chrome-like RoundTripper. dialContext is the
// policy-bound TCP dialer (egress control); proxyURL optionally tunnels
// through an HTTP CONNECT proxy.
func newChromeTransport(dialContext func(context.Context, string, string) (net.Conn, error), proxyURL *url.URL, insecureSkipVerify bool) *chromeTransport {
	t := &chromeTransport{
		dialContext: dialContext,
		proxyURL:    proxyURL,
		tlsConfig:   &utls.Config{MinVersion: utls.VersionTLS12, InsecureSkipVerify: insecureSkipVerify},
		proto:       make(map[string]string),
	}
	t.h1 = &http.Transport{
		DialTLSContext:      t.dialTLS,
		MaxIdleConns:        512,
		MaxIdleConnsPerHost: 64,
		IdleConnTimeout:     90 * time.Second,
		DisableCompression:  false,
		WriteBufferSize:     64 * 1024,
		ReadBufferSize:      64 * 1024,
	}
	t.h2 = &http2.Transport{
		DialTLSContext: func(ctx context.Context, _, addr string, _ *tls.Config) (net.Conn, error) {
			return t.dialTLS(ctx, "tcp", addr)
		},
	}
	return t
}

// RoundTrip routes the request to the negotiated protocol for this host.
func (t *chromeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme != "https" {
		return nil, fmt.Errorf("chrome transport: unsupported scheme %q", req.URL.Scheme)
	}
	host := req.URL.Host
	t.mu.Lock()
	proto := t.proto[host]
	t.mu.Unlock()
	if proto == "" {
		var err error
		proto, err = t.probe(req.Context(), host)
		if err != nil {
			return nil, err
		}
	}
	if proto == "h2" {
		return t.h2.RoundTrip(req)
	}
	return t.h1.RoundTrip(req)
}

// probe handshakes once to learn the negotiated ALPN, caches it, and closes.
func (t *chromeTransport) probe(ctx context.Context, host string) (string, error) {
	conn, err := t.dialTLS(ctx, "tcp", host)
	if err != nil {
		return "", fmt.Errorf("chrome tls probe %s: %w", host, err)
	}
	var proto string
	if uconn, ok := conn.(*utls.UConn); ok {
		proto = uconn.ConnectionState().NegotiatedProtocol
	}
	_ = conn.Close()
	if proto == "" {
		proto = "http/1.1"
	}
	t.mu.Lock()
	if len(t.proto) < chromeALPNCacheCap {
		t.proto[host] = proto
	}
	t.mu.Unlock()
	return proto, nil
}

// dialTLS performs TCP dial (policy-bound, optional CONNECT tunnel) plus a
// Chrome ClientHello uTLS handshake with Chrome's ALPN list.
func (t *chromeTransport) dialTLS(ctx context.Context, _, addr string) (net.Conn, error) {
	raw, err := t.dialTarget(ctx, addr)
	if err != nil {
		return nil, err
	}
	serverName := addr
	if i := strings.LastIndexByte(addr, ':'); i >= 0 {
		serverName = addr[:i]
	}
	cfg := t.tlsConfig.Clone()
	cfg.ServerName = serverName
	cfg.NextProtos = []string{"h2", "http/1.1"}
	uconn := utls.UClient(raw, cfg, utls.HelloChrome_Auto)
	if err := uconn.HandshakeContext(ctx); err != nil {
		_ = raw.Close()
		return nil, fmt.Errorf("chrome tls handshake %s: %w", addr, err)
	}
	return uconn, nil
}

// dialTarget dials the target TCP endpoint, tunneling through the configured
// HTTP CONNECT proxy when one is set.
func (t *chromeTransport) dialTarget(ctx context.Context, addr string) (net.Conn, error) {
	if t.proxyURL == nil {
		return t.dialContext(ctx, "tcp", addr)
	}
	proxyAddr := t.proxyURL.Host
	if !strings.Contains(proxyAddr, ":") {
		proxyAddr += ":443"
	}
	conn, err := t.dialContext(ctx, "tcp", proxyAddr)
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	b.WriteString("CONNECT " + addr + " HTTP/1.1\r\nHost: " + addr + "\r\n")
	if t.proxyURL.User != nil {
		b.WriteString("Proxy-Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(t.proxyURL.User.String())) + "\r\n")
	}
	b.WriteString("\r\n")
	if _, err := conn.Write([]byte(b.String())); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("proxy CONNECT write: %w", err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodConnect})
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("proxy CONNECT read: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = conn.Close()
		return nil, fmt.Errorf("proxy CONNECT: %s", resp.Status)
	}
	return conn, nil
}

// schemeRouter selects the Chrome-fingerprinted transport for HTTPS and the
// standard transport for everything else.
type schemeRouter struct {
	https    http.RoundTripper
	fallback http.RoundTripper
}

func (r *schemeRouter) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL != nil && req.URL.Scheme == "https" {
		return r.https.RoundTrip(req)
	}
	return r.fallback.RoundTrip(req)
}
