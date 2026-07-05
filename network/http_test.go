package network

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newTestClient(t *testing.T, cfg HTTPClientConfig) *HTTPClient {
	t.Helper()
	c, err := NewHTTPClient(cfg)
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestDoStatusHeadersBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom", "yes")
		w.WriteHeader(http.StatusTeapot)
		fmt.Fprint(w, "hello")
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
		fmt.Fprint(w, "ok")
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
	var final *httptest.Server
	final = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "landed")
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

func TestDoMaxBodyBytes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, strings.Repeat("x", 1024))
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
		fmt.Fprint(w, strings.Repeat("y", 4096))
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

// silence unused imports in case future refactors drop one
var _ = io.Discard
