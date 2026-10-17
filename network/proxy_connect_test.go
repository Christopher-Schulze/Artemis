package network

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"net"
	"net/url"
	"strings"
	"testing"
	"time"
)

// connectTestProxy serves exactly one CONNECT conversation with respond and
// reports the request header block it received.
func connectTestProxy(t *testing.T, respond func(net.Conn)) (*url.URL, <-chan string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			t.Errorf("close proxy listener: %v", err)
		}
	})
	headers := make(chan string, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		var block strings.Builder
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			if line == "\r\n" {
				break
			}
			block.WriteString(line)
		}
		headers <- block.String()
		respond(conn)
	}()
	return &url.URL{Scheme: "http", Host: listener.Addr().String(), User: url.UserPassword("agent", "p@ss:w/rd")}, headers
}

func plainProxyDial(ctx context.Context, network, address string) (net.Conn, error) {
	var dialer net.Dialer
	return dialer.DialContext(ctx, network, address)
}

func TestProxyConnectStallIsBoundedByCallerContext(t *testing.T) {
	stalled := make(chan struct{})
	t.Cleanup(func() { close(stalled) })
	proxyURL, _ := connectTestProxy(t, func(net.Conn) { <-stalled })
	transport := newChromeTransport(plainProxyDial, proxyURL, true)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	conn, err := transport.dialTarget(ctx, "example.test:443")
	if err == nil || conn != nil || time.Since(start) > 2*time.Second {
		t.Fatalf("stalled CONNECT was not bounded by the caller: conn=%v err=%v after %s", conn, err, time.Since(start))
	}
}

func TestProxyConnectSendsDecodedCredentialsAndRejectsTrailingData(t *testing.T) {
	proxyURL, headers := connectTestProxy(t, func(conn net.Conn) {
		if _, err := conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\nleaked-bytes")); err != nil {
			t.Errorf("write CONNECT response: %v", err)
		}
	})
	transport := newChromeTransport(plainProxyDial, proxyURL, true)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if conn, err := transport.dialTarget(ctx, "example.test:443"); err == nil || conn != nil || !strings.Contains(err.Error(), "unexpected data") {
		t.Fatalf("CONNECT response with trailing stream bytes was accepted: conn=%v err=%v", conn, err)
	}
	want := "Proxy-Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte("agent:p@ss:w/rd"))
	if block := <-headers; !strings.HasPrefix(block, "CONNECT example.test:443 HTTP/1.1\r\n") || !strings.Contains(block, want+"\r\n") {
		t.Fatalf("CONNECT request lost target or decoded credentials: %q", block)
	}
}

func TestProxyDialAddressUsesSchemeDefaultPort(t *testing.T) {
	for raw, want := range map[string]string{
		"http://proxy.test":       "proxy.test:80",
		"https://proxy.test":      "proxy.test:443",
		"http://proxy.test:3128":  "proxy.test:3128",
		"http://[2001:db8::1]":    "[2001:db8::1]:80",
		"https://[2001:db8::1]:8": "[2001:db8::1]:8",
	} {
		proxyURL, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := proxyDialAddress(proxyURL); got != want {
			t.Fatalf("proxyDialAddress(%s) = %s, want %s", raw, got, want)
		}
	}
}
