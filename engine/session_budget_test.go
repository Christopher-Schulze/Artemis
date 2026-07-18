package engine

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestSessionBudgetDefaultsAndValidation(t *testing.T) {
	eng, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	cfg := eng.Config()
	if cfg.SessionBudget.MaxTabs != DefaultSessionMaxTabs || cfg.SessionBudget.MaxRequests != DefaultSessionMaxRequests || cfg.SessionBudget.MaxConcurrency != DefaultSessionMaxConcurrency {
		t.Fatalf("unexpected defaults: %+v", cfg.SessionBudget)
	}
	if cfg.MaxDownloadDiskBytes != cfg.SessionBudget.MaxDiskBytes {
		t.Fatalf("download limit %d does not follow session disk limit %d", cfg.MaxDownloadDiskBytes, cfg.SessionBudget.MaxDiskBytes)
	}
	_ = eng.Close()
	if _, err := New(Config{SessionBudget: SessionBudget{MaxTabs: -1}}); err == nil {
		t.Fatal("negative session budget accepted")
	}
}

func TestSessionBudgetRequestBreachCancelsSession(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "<html></html>") }))
	defer srv.Close()
	cfg := testConfig(srv)
	cfg.SessionBudget = SessionBudget{MaxRequests: 1}
	eng, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	page, err := eng.Fetch(context.Background(), srv.URL, FetchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	defer page.Close()
	_, err = eng.Fetch(context.Background(), srv.URL, FetchOpts{})
	if !IsBudgetExceeded(err) {
		t.Fatalf("request breach error=%v", err)
	}
	if _, err := page.Eval(context.Background(), "1+1"); !IsBudgetExceeded(err) {
		t.Fatalf("existing page was not cancelled: %v", err)
	}
	usage := eng.SessionUsage()
	if usage.Requests != 1 || !usage.Cancelled {
		t.Fatalf("usage=%+v", usage)
	}
}

func TestSessionBudgetResponseBytesFailClosed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "123456789") }))
	defer srv.Close()
	cfg := testConfig(srv)
	cfg.SessionBudget = SessionBudget{MaxResponseBytes: 8}
	eng, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	if _, err := eng.Fetch(context.Background(), srv.URL, FetchOpts{}); !IsBudgetExceeded(err) {
		t.Fatalf("response breach error=%v", err)
	}
	usage := eng.SessionUsage()
	if usage.ResponseBytes > 8 || !usage.Cancelled {
		t.Fatalf("usage exceeded hard limit: %+v", usage)
	}
}

func TestSessionBudgetTabLeaseIsReleased(t *testing.T) {
	eng, err := New(Config{SessionBudget: SessionBudget{MaxTabs: 1}})
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	fetch := func() (*Page, error) {
		return eng.Fetch(context.Background(), "https://example.test/", FetchOpts{OnRequest: func(req *RequestInfo) (*ResponseInfo, error) {
			return &ResponseInfo{Status: http.StatusOK, Body: []byte("<html></html>"), FinalURL: req.URL}, nil
		}})
	}
	first, err := fetch()
	if err != nil {
		t.Fatal(err)
	}
	if usage := eng.SessionUsage(); usage.ActiveTabs != 1 {
		t.Fatalf("active tabs=%d", usage.ActiveTabs)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := fetch()
	if err != nil {
		t.Fatalf("released tab was not reusable: %v", err)
	}
	_ = second.Close()
}

func TestSessionBudgetConcurrencyBreachCancelsInflightRequest(t *testing.T) {
	entered := make(chan struct{})
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(entered) })
		<-r.Context().Done()
	}))
	defer srv.Close()
	cfg := testConfig(srv)
	cfg.SessionBudget = SessionBudget{MaxConcurrency: 1}
	eng, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	firstDone := make(chan error, 1)
	go func() {
		_, fetchErr := eng.Fetch(context.Background(), srv.URL, FetchOpts{})
		firstDone <- fetchErr
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first request did not start")
	}
	if _, err := eng.Fetch(context.Background(), srv.URL, FetchOpts{}); !IsBudgetExceeded(err) {
		t.Fatalf("concurrency breach error=%v", err)
	}
	select {
	case err := <-firstDone:
		if err == nil || (!IsBudgetExceeded(err) && !errors.Is(err, context.Canceled)) {
			t.Fatalf("inflight cancellation error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("inflight request survived session cancellation")
	}
}

func TestSessionBudgetTimeoutCancelsPage(t *testing.T) {
	eng, err := New(Config{SessionBudget: SessionBudget{Timeout: 20 * time.Millisecond}})
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	page, err := eng.Fetch(context.Background(), "https://example.test/", FetchOpts{OnRequest: func(req *RequestInfo) (*ResponseInfo, error) {
		return &ResponseInfo{Status: http.StatusOK, Body: []byte("<html></html>"), FinalURL: req.URL}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer page.Close()
	time.Sleep(40 * time.Millisecond)
	if _, err := page.Eval(context.Background(), "1+1"); !IsBudgetExceeded(err) {
		t.Fatalf("timeout error=%v", err)
	}
}

func TestSessionBudgetExternalScriptBreachAbortsFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/script.js" {
			fmt.Fprint(w, "globalThis.loaded = true")
			return
		}
		fmt.Fprint(w, `<html><script src="/script.js"></script></html>`)
	}))
	defer srv.Close()
	cfg := testConfig(srv)
	cfg.SessionBudget = SessionBudget{MaxRequests: 1}
	eng, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	if page, err := eng.Fetch(context.Background(), srv.URL, FetchOpts{RunScripts: true}); !IsBudgetExceeded(err) || page != nil {
		t.Fatalf("page=%v script budget error=%v", page, err)
	}
}
