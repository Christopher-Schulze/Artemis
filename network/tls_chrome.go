package network

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
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
		// Session resumption: real Chrome caches tickets; resumed
		// handshakes are both faster and fingerprint-consistent.
		tlsConfig: &utls.Config{
			MinVersion:         utls.VersionTLS12,
			InsecureSkipVerify: insecureSkipVerify,
			ClientSessionCache: utls.NewLRUClientSessionCache(1024),
		},
		proto: make(map[string]string),
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
	port := req.URL.Port()
	if port == "" {
		port = "443"
	}
	host := net.JoinHostPort(req.URL.Hostname(), port)
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
	if err := conn.Close(); err != nil {
		return "", fmt.Errorf("chrome tls probe close: %w", err)
	}
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
	serverName, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("chrome tls authority: %w", errors.Join(err, raw.Close()))
	}
	cfg := t.tlsConfig.Clone()
	cfg.ServerName = serverName
	cfg.NextProtos = []string{"h2", "http/1.1"}
	uconn := utls.UClient(raw, cfg, utls.HelloChrome_Auto)
	if err := uconn.HandshakeContext(ctx); err != nil {
		return nil, fmt.Errorf("chrome tls handshake %s: %w", addr, errors.Join(err, raw.Close()))
	}
	return uconn, nil
}

// dialTarget dials the target TCP endpoint, tunneling through the configured
// HTTP CONNECT proxy when one is set.
func (t *chromeTransport) dialTarget(ctx context.Context, addr string) (net.Conn, error) {
	if t.proxyURL == nil {
		return t.dialContext(ctx, "tcp", addr)
	}
	conn, err := t.dialContext(ctx, "tcp", proxyDialAddress(t.proxyURL))
	if err != nil {
		return nil, err
	}
	// The CONNECT exchange is plain blocking socket IO: bind it to the caller's
	// deadline and cancellation so a stalled proxy cannot hold the dial forever.
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(proxyConnectTimeout)
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, fmt.Errorf("proxy CONNECT deadline: %w", errors.Join(err, conn.Close()))
	}
	stop := context.AfterFunc(ctx, func() {
		if err := conn.SetDeadline(time.Unix(1, 0)); err != nil {
			conn.Close()
		}
	})
	tunnel, err := t.connectTunnel(conn, addr)
	if !stop() || err != nil {
		return nil, errors.Join(err, ctx.Err(), conn.Close())
	}
	if err := tunnel.SetDeadline(time.Time{}); err != nil {
		return nil, fmt.Errorf("proxy CONNECT deadline reset: %w", errors.Join(err, conn.Close()))
	}
	return tunnel, nil
}

// proxyConnectTimeout bounds a CONNECT exchange whose caller has no deadline.
const proxyConnectTimeout = 30 * time.Second

// proxyDialAddress applies the proxy scheme's default port when none is set.
func proxyDialAddress(proxyURL *url.URL) string {
	if proxyURL.Port() != "" {
		return proxyURL.Host
	}
	port := "80"
	if proxyURL.Scheme == "https" {
		port = "443"
	}
	return net.JoinHostPort(proxyURL.Hostname(), port)
}

func (t *chromeTransport) connectTunnel(conn net.Conn, addr string) (net.Conn, error) {
	var b strings.Builder
	b.WriteString("CONNECT " + addr + " HTTP/1.1\r\nHost: " + addr + "\r\n")
	if user := t.proxyURL.User; user != nil {
		// Basic credentials use the decoded user and password, not the URL form.
		password, _ := user.Password()
		b.WriteString("Proxy-Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(user.Username()+":"+password)) + "\r\n")
	}
	b.WriteString("\r\n")
	if _, err := conn.Write([]byte(b.String())); err != nil {
		return nil, fmt.Errorf("proxy CONNECT write: %w", err)
	}
	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
	if err != nil {
		return nil, fmt.Errorf("proxy CONNECT read: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("proxy CONNECT: %s", resp.Status)
	}
	if reader.Buffered() > 0 {
		// Bytes after the CONNECT response would be lost from the TLS stream.
		return nil, errors.New("proxy CONNECT: unexpected data after tunnel response")
	}
	return conn, nil
}

// CloseIdleConnections drains idle conns in both sub-transports.
func (t *chromeTransport) CloseIdleConnections() {
	t.h1.CloseIdleConnections()
	t.h2.CloseIdleConnections()
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

// CloseIdleConnections forwards lifecycle teardown to both transports —
// http.Client only calls this on RoundTrippers exposing the interface.
func (r *schemeRouter) CloseIdleConnections() {
	for _, rt := range []http.RoundTripper{r.https, r.fallback} {
		if c, ok := rt.(interface{ CloseIdleConnections() }); ok {
			c.CloseIdleConnections()
		}
	}
}
