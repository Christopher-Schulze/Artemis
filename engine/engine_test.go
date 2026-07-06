package engine

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEngineFetchEndToEnd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><html><head><title>Hi</title></head><body><h1>Welcome</h1><p>Hello <b>world</b>.</p></body></html>`)
	}))
	defer srv.Close()

	eng, err := New(Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer eng.Close()

	page, err := eng.Fetch(context.Background(), srv.URL, FetchOpts{})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if page.StatusCode() != 200 {
		t.Errorf("status = %d, want 200", page.StatusCode())
	}
	if got := page.Title(); got != "Hi" {
		t.Errorf("title = %q, want Hi", got)
	}
	if got := page.Markdown(); !strings.Contains(got, "# Welcome") || !strings.Contains(got, "**world**") {
		t.Errorf("markdown missing expected content: %q", got)
	}
	if got := page.Text(); !strings.Contains(got, "Welcome") || !strings.Contains(got, "world") {
		t.Errorf("text missing expected content: %q", got)
	}
}

func TestEngineFetchInvalidURL(t *testing.T) {
	eng, err := New(Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer eng.Close()
	if _, err := eng.Fetch(context.Background(), "://broken", FetchOpts{}); err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

func TestConfigDefaults(t *testing.T) {
	cfg := Config{}
	cfg.applyDefaults()
	if cfg.UserAgent == "" {
		t.Error("UserAgent default not applied")
	}
	if cfg.Timeout == 0 {
		t.Error("Timeout default not applied")
	}
	if cfg.MaxBodyBytes == 0 {
		t.Error("MaxBodyBytes default not applied")
	}
}
