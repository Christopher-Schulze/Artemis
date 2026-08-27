package bridge

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/network"
)

func TestChromiumPolicyProxyAllowsConfiguredPrivateDestination(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer backend.Close()
	policy := newProxyTestPolicy(t, backend.URL, true)
	proxy, err := newChromiumPolicyProxy(context.Background(), policy)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := proxy.Close(); closeErr != nil {
			t.Errorf("close policy proxy: %v", closeErr)
		}
	})
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, backend.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := proxyHTTPClient(t, proxy.URL()).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := response.Body.Close(); closeErr != nil {
			t.Errorf("close response body: %v", closeErr)
		}
	}()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("status=%d", response.StatusCode)
	}
}

func TestChromiumPolicyProxyDeniesPrivateDestination(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer backend.Close()
	policy := newProxyTestPolicy(t, backend.URL, false)
	proxy, err := newChromiumPolicyProxy(context.Background(), policy)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := proxy.Close(); closeErr != nil {
			t.Errorf("close policy proxy: %v", closeErr)
		}
	})
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, backend.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := proxyHTTPClient(t, proxy.URL()).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := response.Body.Close(); closeErr != nil {
			t.Errorf("close response body: %v", closeErr)
		}
	}()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("status=%d", response.StatusCode)
	}
}

func TestChromiumPolicyProxyRewriteRemovesProxyHeaders(t *testing.T) {
	proxy := &chromiumPolicyProxy{}
	outbound := &http.Request{
		URL:        &url.URL{Scheme: "http", Host: "backend.example", Path: "/resource"},
		RequestURI: "/resource",
		Header: http.Header{
			"Proxy-Authorization": []string{"secret"},
			"Proxy-Connection":    []string{"keep-alive"},
			"X-Forwarded-For":     []string{"127.0.0.1"},
		},
	}
	proxy.newReverseProxy().Rewrite(&httputil.ProxyRequest{Out: outbound})

	if outbound.RequestURI != "" {
		t.Fatalf("request URI = %q, want empty", outbound.RequestURI)
	}
	if outbound.Host != "backend.example" {
		t.Fatalf("host = %q, want backend.example", outbound.Host)
	}
	for _, header := range []string{"Proxy-Authorization", "Proxy-Connection", "X-Forwarded-For"} {
		if value := outbound.Header.Get(header); value != "" {
			t.Fatalf("%s = %q, want removed", header, value)
		}
	}
}

func TestChromiumPolicyProxyConnectHonorsPolicy(t *testing.T) {
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer closeBridgeTestResource(t, "listener", listener.Close)
	policy := newProxyTestPolicy(t, "http://"+listener.Addr().String(), false)
	proxy, err := newChromiumPolicyProxy(context.Background(), policy)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := proxy.Close(); closeErr != nil {
			t.Errorf("close policy proxy: %v", closeErr)
		}
	})
	connection, err := (&net.Dialer{Timeout: time.Second}).DialContext(t.Context(), "tcp", strings.TrimPrefix(proxy.URL(), "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer closeBridgeTestResource(t, "proxy connection", connection.Close)
	if _, writeErr := fmt.Fprintf(connection, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", listener.Addr(), listener.Addr()); writeErr != nil {
		t.Fatal(writeErr)
	}
	response, err := http.ReadResponse(bufio.NewReader(connection), &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := response.Body.Close(); closeErr != nil {
			t.Errorf("close CONNECT response body: %v", closeErr)
		}
	}()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("status=%d", response.StatusCode)
	}
}

func newProxyTestPolicy(t *testing.T, target string, allowPrivate bool) *network.Policy {
	t.Helper()
	parsed, err := url.Parse(target)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}
	policy, err := network.NewPolicy(network.PolicyConfig{
		AllowedPorts: []int{port}, AllowPrivateNetworks: allowPrivate,
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

func proxyHTTPClient(t *testing.T, rawProxyURL string) *http.Client {
	t.Helper()
	proxyURL, err := url.Parse(rawProxyURL)
	if err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	t.Cleanup(transport.CloseIdleConnections)
	return &http.Client{
		Transport: transport,
		Timeout:   2 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func TestChromiumPolicyProxyClosesActiveTunnel(t *testing.T) {
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer closeBridgeTestResource(t, "listener", listener.Close)
	accepted := make(chan net.Conn, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr == nil {
			accepted <- connection
		}
	}()
	policy := newProxyTestPolicy(t, "http://"+listener.Addr().String(), true)
	proxy, err := newChromiumPolicyProxy(context.Background(), policy)
	if err != nil {
		t.Fatal(err)
	}
	connection := openProxyTunnel(t, proxy.URL(), listener.Addr().String())
	defer closeBridgeTestResource(t, "proxy connection", connection.Close)
	upstream := <-accepted
	defer closeBridgeTestResource(t, "upstream connection", upstream.Close)
	if _, err := connection.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	if err := upstream.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 4)
	if _, err := io.ReadFull(upstream, buffer); err != nil || string(buffer) != "ping" {
		t.Fatalf("tunnel payload=%q err=%v", buffer, err)
	}
	if err := proxy.Close(); err != nil {
		t.Fatal(err)
	}
	if err := connection.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(connection); err != nil {
		if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
			t.Fatal("active tunnel survived proxy close")
		}
	}
}

func openProxyTunnel(t *testing.T, rawProxyURL, target string) net.Conn {
	t.Helper()
	connection, err := (&net.Dialer{}).DialContext(context.Background(), "tcp", strings.TrimPrefix(rawProxyURL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	if _, writeErr := fmt.Fprintf(connection, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target); writeErr != nil {
		closeBridgeTestResource(t, "proxy connection", connection.Close)
		t.Fatal(writeErr)
	}
	reader := bufio.NewReader(connection)
	status, err := reader.ReadString('\n')
	if err != nil {
		closeBridgeTestResource(t, "proxy connection", connection.Close)
		t.Fatal(err)
	}
	if !strings.Contains(status, " 200 ") {
		closeBridgeTestResource(t, "proxy connection", connection.Close)
		t.Fatalf("status=%q", strings.TrimSpace(status))
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			closeBridgeTestResource(t, "proxy connection", connection.Close)
			t.Fatal(err)
		}
		if line == "\r\n" {
			break
		}
	}
	return connection
}

func closeBridgeTestResource(t *testing.T, label string, close func() error) {
	t.Helper()
	if err := close(); err != nil {
		t.Errorf("close %s: %v", label, err)
	}
}
