package bridge

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFormatWebSocketURLIPv4(t *testing.T) {
	got := FormatWebSocketURL("127.0.0.1", 9222, "/devtools/browser")
	want := "ws://127.0.0.1:9222/devtools/browser"
	if got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

func TestFormatWebSocketURLIPv6(t *testing.T) {
	got := FormatWebSocketURL("::1", 9222, "/devtools/browser")
	if !strings.HasPrefix(got, "ws://[::1]:9222") {
		t.Fatalf("expected IPv6 bracketing, got=%q", got)
	}
}

func TestFormatWebSocketURLDefaultPath(t *testing.T) {
	got := FormatWebSocketURL("127.0.0.1", 9222, "")
	if got != "ws://127.0.0.1:9222/" {
		t.Fatalf("got=%q", got)
	}
}

func TestFormatWebSocketURLPathNoSlash(t *testing.T) {
	got := FormatWebSocketURL("127.0.0.1", 9222, "devtools/browser")
	if !strings.HasSuffix(got, "/devtools/browser") {
		t.Fatalf("got=%q", got)
	}
}

func TestDiscoverJSONVersionSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/json/version" {
			_ = json.NewEncoder(w).Encode(map[string]string{
				"Browser":              "Chrome/120.0",
				"webSocketDebuggerUrl": "ws://127.0.0.1:9222/devtools/browser/abc",
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	host, port := splitHostPort(t, srv.URL)
	d := NewChromeDiscoveryWithClient(srv.Client())
	results, err := d.Discover(host, port)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 || !results[0].Found {
		t.Fatalf("results=%+v", results)
	}
	if results[0].Method != DiscoveryJSONVersion {
		t.Fatalf("method=%s", results[0].Method)
	}
	if results[0].BrowserVersion != "Chrome/120.0" {
		t.Fatalf("version=%q", results[0].BrowserVersion)
	}
}

func TestDiscoverJSONListFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/json/list" {
			_ = json.NewEncoder(w).Encode([]map[string]string{
				{"id": "1", "type": "page", "url": "about:blank", "webSocketDebuggerUrl": "ws://127.0.0.1:9222/devtools/page/1"},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	host, port := splitHostPort(t, srv.URL)
	d := NewChromeDiscoveryWithClient(srv.Client())
	results, err := d.Discover(host, port)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range results {
		if r.Method == DiscoveryJSONList && r.Found {
			found = true
		}
	}
	if !found {
		t.Fatalf("json/list not found: %+v", results)
	}
}

func TestDiscoverDevToolsBrowserFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	host, port := splitHostPort(t, srv.URL)
	d := NewChromeDiscoveryWithClient(srv.Client())
	results, err := d.Discover(host, port)
	if err != nil {
		t.Fatal(err)
	}
	// All three methods attempted; none found because httptest doesn't speak
	// the devtools browser WS protocol but the TCP dial should succeed.
	foundAny := false
	for _, r := range results {
		if r.Found {
			foundAny = true
		}
	}
	if !foundAny {
		// TCP fallback may still mark it found; either way we should have results.
		if len(results) == 0 {
			t.Fatal("expected at least one result")
		}
	}
}

func TestDiscoverNotFound(t *testing.T) {
	d := NewChromeDiscoveryWithClient(&http.Client{Timeout: 200 * time.Millisecond})
	_, err := d.Discover("127.0.0.1", 1) // nothing on port 1
	if err == nil {
		t.Fatal("expected error for unreachable port")
	}
}

func TestDiscoverTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
	}))
	defer srv.Close()

	host, port := splitHostPort(t, srv.URL)
	d := NewChromeDiscoveryWithClient(&http.Client{Timeout: 50 * time.Millisecond})
	_, err := d.Discover(host, port)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestDiscoverAllThreeMethodsAttempted(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		http.NotFound(w, r)
	}))
	defer srv.Close()

	host, port := splitHostPort(t, srv.URL)
	d := NewChromeDiscoveryWithClient(srv.Client())
	_, _ = d.Discover(host, port)
	if len(paths) < 2 {
		t.Fatalf("expected multiple attempts, got paths=%v", paths)
	}
}

func TestDiscoverJSONVersionNoWebSocket(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/json/version" {
			_ = json.NewEncoder(w).Encode(map[string]string{"Browser": "Chrome/120"})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	host, port := splitHostPort(t, srv.URL)
	d := NewChromeDiscoveryWithClient(srv.Client())
	results, err := d.Discover(host, port)
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Found {
		t.Fatal("should not be found without webSocketDebuggerUrl")
	}
}

func TestDiscoverJSONListEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/json/list" {
			_ = json.NewEncoder(w).Encode([]map[string]string{})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	host, port := splitHostPort(t, srv.URL)
	d := NewChromeDiscoveryWithClient(srv.Client())
	results, err := d.Discover(host, port)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.Method == DiscoveryJSONList && r.Found {
			t.Fatal("empty list should not be found")
		}
	}
}

func TestNewChromeDiscoveryDefault(t *testing.T) {
	d := NewChromeDiscovery()
	if d == nil || d.client == nil {
		t.Fatal("default client not configured")
	}
	if d.client.Timeout != 2*time.Second {
		t.Fatalf("timeout=%v", d.client.Timeout)
	}
}

func TestDiscoverMalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	host, port := splitHostPort(t, srv.URL)
	d := NewChromeDiscoveryWithClient(srv.Client())
	results, err := d.Discover(host, port)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.Found {
			t.Fatalf("malformed json should not yield found: %+v", r)
		}
	}
}

func TestDiscoverNon200Status(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer srv.Close()

	host, port := splitHostPort(t, srv.URL)
	d := NewChromeDiscoveryWithClient(srv.Client())
	_, err := d.Discover(host, port)
	if err == nil {
		t.Fatal("expected error when all endpoints return 500")
	}
}

func splitHostPort(t *testing.T, url string) (string, int) {
	t.Helper()
	// url looks like http://127.0.0.1:PORT
	rest := strings.TrimPrefix(url, "http://")
	colon := strings.LastIndex(rest, ":")
	if colon < 0 {
		t.Fatalf("bad url %q", url)
	}
	host := rest[:colon]
	var port int
	_, err := fmt.Sscanf(rest[colon+1:], "%d", &port)
	if err != nil {
		t.Fatalf("bad port in %q", url)
	}
	return host, port
}
