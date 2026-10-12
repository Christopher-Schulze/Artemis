package network

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func writeTestHTTPBody(t *testing.T, w http.ResponseWriter, body string) {
	t.Helper()
	if _, err := fmt.Fprint(w, body); err != nil {
		t.Errorf("write HTTP fixture response: %v", err)
	}
}

func newTestClient(t *testing.T, cfg HTTPClientConfig) *HTTPClient {
	t.Helper()
	if cfg.Policy == nil {
		policyConfig := DefaultPolicyConfig()
		policyConfig.AllowPrivateNetworks = true
		policyConfig.AllowedPorts = allTestPorts()
		cfg.Policy = mustPolicy(t, policyConfig, nil, nil)
	}
	c, err := NewHTTPClient(cfg)
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	t.Cleanup(func() {
		if err := c.Close(); err != nil {
			t.Errorf("close HTTP client: %v", err)
		}
	})
	return c
}

func allTestPorts() []int {
	// Tests allocate arbitrary loopback ports; allow the entire valid range
	// while private-network access remains an explicit test-only opt-in.
	ports := make([]int, 65535)
	for index := range ports {
		ports[index] = index + 1
	}
	return ports
}

func TestDoStatusHeadersBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom", "yes")
		w.WriteHeader(http.StatusTeapot)
		writeTestHTTPBody(t, w, "hello")
	}))
	defer srv.Close()

	c := newTestClient(t, HTTPClientConfig{UserAgent: "test/1", Timeout: 5 * time.Second})
	resp, err := c.Do(context.Background(), Request{URL: srv.URL})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if resp.StatusCode != http.StatusTeapot {
		t.Errorf("status = %d, want 418", resp.StatusCode)
	}
	if got := resp.Headers.Get("X-Custom"); got != "yes" {
		t.Errorf("X-Custom = %q, want yes", got)
	}
	if string(resp.Body) != "hello" {
		t.Errorf("body = %q, want hello", string(resp.Body))
	}
	if resp.FinalURL != srv.URL {
		t.Errorf("final url = %q, want %q", resp.FinalURL, srv.URL)
	}
}

func TestDoSendsUserAgentAndCustomHeaders(t *testing.T) {
	var (
		gotUA string
		gotXk string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		gotXk = r.Header.Get("X-K")
		writeTestHTTPBody(t, w, "ok")
	}))
	defer srv.Close()

	c := newTestClient(t, HTTPClientConfig{UserAgent: "artemis-test"})
	_, err := c.Do(context.Background(), Request{
		URL:     srv.URL,
		Headers: http.Header{"X-K": []string{"v"}},
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if gotUA != "artemis-test" {
		t.Errorf("User-Agent = %q, want artemis-test", gotUA)
	}
	if gotXk != "v" {
		t.Errorf("X-K = %q, want v", gotXk)
	}
}

func TestDoFollowsRedirects(t *testing.T) {
	final := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTestHTTPBody(t, w, "landed")
	}))
	defer final.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, final.URL, http.StatusFound)
	}))
	defer srv.Close()

	c := newTestClient(t, HTTPClientConfig{})
	resp, err := c.Do(context.Background(), Request{URL: srv.URL})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if string(resp.Body) != "landed" {
		t.Errorf("body = %q, want landed", string(resp.Body))
	}
	if resp.FinalURL != final.URL {
		t.Errorf("final url = %q, want %q", resp.FinalURL, final.URL)
	}
}

func TestDoRejectsRedirectToPrivateTarget(t *testing.T) {
	client, err := NewHTTPClient(HTTPClientConfig{})
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := client.Close(); closeErr != nil {
			t.Errorf("close HTTP client: %v", closeErr)
		}
	})
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://127.0.0.1/secret", nil)
	if err != nil {
		t.Fatalf("build redirect request: %v", err)
	}
	err = client.client.CheckRedirect(request, []*http.Request{{}})
	if !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("redirect error = %v, want policy denial", err)
	}
}

func TestDoRejectsCrossOriginBodyRedirect(t *testing.T) {
	client := newTestClient(t, HTTPClientConfig{})
	previous, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://source.example/login", strings.NewReader("password=secret"))
	if err != nil {
		t.Fatal(err)
	}
	redirect, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://target.example/collect", strings.NewReader("password=secret"))
	if err != nil {
		t.Fatal(err)
	}
	err = client.client.CheckRedirect(redirect, []*http.Request{previous})
	if !errors.Is(err, ErrPolicyDenied) || !strings.Contains(err.Error(), "cross_origin_body_redirect") {
		t.Fatalf("redirect error=%v", err)
	}
}

func TestDoAllowsSameOriginBodyRedirect(t *testing.T) {
	client := newTestClient(t, HTTPClientConfig{})
	previous, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://127.0.0.1:12345/login", strings.NewReader("credential=value"))
	if err != nil {
		t.Fatal(err)
	}
	redirect, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://127.0.0.1:12345/session", strings.NewReader("credential=value"))
	if err != nil {
		t.Fatal(err)
	}
	if err := client.client.CheckRedirect(redirect, []*http.Request{previous}); err != nil {
		t.Fatalf("same-origin redirect denied: %v", err)
	}
}

func TestDoMaxBodyBytes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTestHTTPBody(t, w, strings.Repeat("x", 1024))
	}))
	defer srv.Close()

	c := newTestClient(t, HTTPClientConfig{MaxBodyBytes: 64})
	_, err := c.Do(context.Background(), Request{URL: srv.URL})
	if !errors.Is(err, ErrBodyTooLarge) {
		t.Errorf("err = %v, want ErrBodyTooLarge", err)
	}
}

func TestDoTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// flush headers so the client starts reading and trips the body deadline
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// keep the response open longer than the per-request timeout
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer srv.Close()

	c := newTestClient(t, HTTPClientConfig{Timeout: 100 * time.Millisecond})
	_, err := c.Do(context.Background(), Request{URL: srv.URL})
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

func TestDoNilBodyAcceptsLargeUnlimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTestHTTPBody(t, w, strings.Repeat("y", 4096))
	}))
	defer srv.Close()

	c := newTestClient(t, HTTPClientConfig{}) // 0 = unlimited
	resp, err := c.Do(context.Background(), Request{URL: srv.URL})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if len(resp.Body) != 4096 {
		t.Errorf("len(body) = %d, want 4096", len(resp.Body))
	}
}

// guard against accidentally caching cookies when none are issued
func TestCookieJarPresent(t *testing.T) {
	c := newTestClient(t, HTTPClientConfig{})
	if c.CookieJar() == nil {
		t.Fatal("CookieJar nil")
	}
}

func TestHTTPClientNilReceiverFailsClosed(t *testing.T) {
	var client *HTTPClient
	if _, err := client.Do(context.Background(), Request{URL: "https://example.com"}); err == nil {
		t.Fatal("nil client executed request")
	}
	if err := client.Close(); err != nil {
		t.Fatalf("nil Close: %v", err)
	}
	if client.CookieJar() != nil {
		t.Fatal("nil client returned a cookie jar")
	}
}
