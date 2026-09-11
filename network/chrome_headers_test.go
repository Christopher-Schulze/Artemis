package network

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

func TestChromeLikeHeaders(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.WriteHeader(200)
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	policy, _ := NewPolicy(PolicyConfig{AllowPrivateNetworks: true, AllowedPorts: []int{80, 443, port}}, nil, nil)
	c, err := NewHTTPClient(HTTPClientConfig{Policy: policy, ChromeLike: true, ChromeOS: "macos", UserAgent: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/152.0.0.0 Safari/537.36"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Do(context.Background(), Request{URL: srv.URL}); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"Sec-Ch-Ua", "Sec-Ch-Ua-Platform", "Sec-Fetch-Site", "Accept-Language", "Upgrade-Insecure-Requests"} {
		if got.Get(k) == "" {
			t.Errorf("missing %s", k)
		}
	}
	if got.Get("Sec-Ch-Ua-Platform") != `"macOS"` {
		t.Errorf("platform: %q", got.Get("Sec-Ch-Ua-Platform"))
	}
	if got.Get("User-Agent") == "" {
		t.Error("UA missing")
	}
}

func TestChromeLikeTLSHandshake(t *testing.T) {
	var got http.Header
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.WriteHeader(200)
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	policy, _ := NewPolicy(PolicyConfig{AllowPrivateNetworks: true, AllowedPorts: []int{80, 443, port}}, nil, nil)
	c, err := NewHTTPClient(HTTPClientConfig{Policy: policy, ChromeLike: true, ChromeOS: "macos", TLSInsecureSkipVerify: true, UserAgent: "Mozilla/5.0 Chrome/152.0.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	resp, err := c.Do(context.Background(), Request{URL: srv.URL})
	if err != nil {
		t.Fatalf("uTLS request: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if got.Get("Sec-Ch-Ua-Platform") != `"macOS"` {
		t.Errorf("chrome headers missing over uTLS: %q", got.Get("Sec-Ch-Ua-Platform"))
	}
	// And the verify-on path must fail closed against self-signed.
	c2, err := NewHTTPClient(HTTPClientConfig{Policy: policy, ChromeLike: true, ChromeOS: "macos", UserAgent: "x"})
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	if _, err := c2.Do(context.Background(), Request{URL: srv.URL}); err == nil {
		t.Error("self-signed cert accepted with verification enabled — must fail")
	}
}

func TestChromeLikeH2(t *testing.T) {
	var gotProto string
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotProto = r.Proto
		w.WriteHeader(200)
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	policy, _ := NewPolicy(PolicyConfig{AllowPrivateNetworks: true, AllowedPorts: []int{80, 443, port}}, nil, nil)
	c, err := NewHTTPClient(HTTPClientConfig{Policy: policy, ChromeLike: true, TLSInsecureSkipVerify: true})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	resp, err := c.Do(context.Background(), Request{URL: srv.URL})
	if err != nil {
		t.Fatalf("uTLS h2 request: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if gotProto != "HTTP/2.0" {
		t.Errorf("ALPN routing: server saw %q, want HTTP/2.0", gotProto)
	}
}

// h1-only TLS server: probe must learn http/1.1 and route via the h1 path.
func TestChromeLikeH1OnlyServer(t *testing.T) {
	var gotProto string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotProto = r.Proto
		w.WriteHeader(200)
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	policy, _ := NewPolicy(PolicyConfig{AllowPrivateNetworks: true, AllowedPorts: []int{80, 443, port}}, nil, nil)
	c, err := NewHTTPClient(HTTPClientConfig{Policy: policy, ChromeLike: true, TLSInsecureSkipVerify: true})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for i := 0; i < 2; i++ {
		resp, err := c.Do(context.Background(), Request{URL: srv.URL})
		if err != nil {
			t.Fatalf("req %d: %v", i, err)
		}
		if resp.StatusCode != 200 {
			t.Fatalf("status %d", resp.StatusCode)
		}
	}
	if gotProto != "HTTP/1.1" {
		t.Fatalf("expected HTTP/1.1 routing, got %q", gotProto)
	}
}

// CONNECT proxy: the uTLS handshake must tunnel through the proxy — the
// proxy sees only CONNECT, never the request line.
func TestChromeLikeConnectProxy(t *testing.T) {
	var connectHost string
	var tunneled atomic.Int32
	backend := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer backend.Close()

	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("proxy must only receive CONNECT, never the plain request")
	}))
	defer proxy.Close()
	// httptest can't serve CONNECT+upgrade; use a raw listener instead.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				br := bufio.NewReader(c)
				line, err := br.ReadString('\n')
				if err != nil || !strings.HasPrefix(line, "CONNECT ") {
					return
				}
				connectHost = strings.TrimSpace(strings.TrimPrefix(line, "CONNECT "))
				// drain headers
				for {
					l, err := br.ReadString('\n')
					if err != nil || l == "\r\n" {
						break
					}
				}
				up, err := net.Dial("tcp", strings.Split(connectHost, " ")[0])
				if err != nil {
					return
				}
				defer up.Close()
				io.WriteString(c, "HTTP/1.1 200 Connection Established\r\n\r\n")
				go io.Copy(up, br) // carry any buffered bytes
				io.Copy(c, up)
				tunneled.Add(1)
			}(conn)
		}
	}()
	proxyURL := "http://" + ln.Addr().String()

	u, _ := url.Parse(backend.URL)
	port, _ := strconv.Atoi(u.Port())
	policy, _ := NewPolicy(PolicyConfig{AllowPrivateNetworks: true, AllowedPorts: []int{80, 443, port, ln.Addr().(*net.TCPAddr).Port}}, nil, nil)
	c, err := NewHTTPClient(HTTPClientConfig{Policy: policy, ChromeLike: true, TLSInsecureSkipVerify: true, ProxyURL: proxyURL})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	resp, err := c.Do(context.Background(), Request{URL: backend.URL})
	if err != nil {
		t.Fatalf("tunneled request: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if !strings.HasPrefix(connectHost, u.Host) {
		t.Fatalf("CONNECT target %q, want %q", connectHost, u.Host)
	}
	if tunneled.Load() == 0 {
		t.Fatal("no tunnel established")
	}
}

// Plain http must bypass the chrome transport entirely.
func TestSchemeRouterPlainHTTP(t *testing.T) {
	var called atomic.Int32
	fallback := roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		called.Add(1)
		return &http.Response{StatusCode: 200, Body: http.NoBody, Header: make(http.Header), Request: r}, nil
	})
	r := &schemeRouter{https: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("https transport must not see http requests")
		return nil, nil
	}), fallback: fallback}
	req, _ := http.NewRequest("GET", "http://example.com/", nil)
	resp, err := r.RoundTrip(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("fallback: %v", err)
	}
	if called.Load() != 1 {
		t.Fatal("fallback not used")
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
