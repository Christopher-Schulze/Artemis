package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestEngineFetchEndToEnd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		writeTestBody(t, w, `<!doctype html><html><head><title>Hi</title></head><body><h1>Welcome</h1><p>Hello <b>world</b>.</p></body></html>`)
	}))
	defer srv.Close()

	eng, err := New(testConfig(srv))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer closeTestResource(t, "engine", eng.Close)

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
	defer closeTestResource(t, "engine", eng.Close)
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

func TestEngineRejectsProtectionDisablingConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{name: "timeout", cfg: Config{Timeout: -time.Second}},
		{name: "body limit", cfg: Config{MaxBodyBytes: -1}},
		{name: "download limit", cfg: Config{MaxDownloadDiskBytes: -1}},
		{name: "download headroom", cfg: Config{MinDownloadFreeBytes: -1}},
		{name: "context pool", cfg: Config{JSContextPoolSize: -1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if engine, err := New(test.cfg); err == nil {
				closeTestResource(t, "invalid configuration engine", engine.Close)
				t.Fatal("invalid configuration accepted")
			}
		})
	}
}

func TestEngineMockResponseHonorsBodyLimitAndStatus(t *testing.T) {
	engine, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "engine", engine.Close)
	for _, response := range []*ResponseInfo{
		{Status: http.StatusOK, Body: []byte("oversize")},
		{Status: 0, Body: []byte("ok")},
	} {
		_, err := engine.Fetch(context.Background(), "https://example.invalid/", FetchOpts{
			MaxBodyBytes: 2,
			OnRequest:    func(*RequestInfo) (*ResponseInfo, error) { return response, nil },
		})
		if err == nil {
			t.Fatalf("invalid mock response accepted: %#v", response)
		}
	}
}

func TestPageResponseAccessorsReturnCopies(t *testing.T) {
	headers := http.Header{"X-Proof": []string{"original"}}
	body := []byte("<html><body>original</body></html>")
	engine, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "engine", engine.Close)
	page, err := engine.Fetch(context.Background(), "https://example.invalid/", FetchOpts{OnRequest: func(*RequestInfo) (*ResponseInfo, error) {
		return &ResponseInfo{Status: http.StatusOK, Headers: headers, Body: body}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "page", page.Close)
	headers.Set("X-Proof", "mutated-source")
	body[0] = 'X'
	gotHeaders := page.Headers()
	gotBody := page.RawBody()
	gotHeaders.Set("X-Proof", "mutated-result")
	gotBody[0] = 'Y'
	if page.Headers().Get("X-Proof") != "original" || string(page.RawBody()) != "<html><body>original</body></html>" {
		t.Fatalf("page response aliases caller-owned data: headers=%v body=%q", page.Headers(), page.RawBody())
	}
}
