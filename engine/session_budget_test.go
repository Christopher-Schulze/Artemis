package engine

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/network"
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
	closeTestResource(t, "engine close", eng.Close)
	if _, err := New(Config{SessionBudget: SessionBudget{MaxTabs: -1}}); err == nil {
		t.Fatal("negative session budget accepted")
	}
}

func TestSessionBudgetRequestBreachCancelsSession(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeTestBody(t, w, "<html></html>")
	}))
	defer srv.Close()
	cfg := testConfig(srv)
	cfg.SessionBudget = SessionBudget{MaxRequests: 1}
	eng, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "engine close", eng.Close)
	page, err := eng.Fetch(context.Background(), srv.URL, FetchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "page close", page.Close)
	_, err = eng.Fetch(context.Background(), srv.URL, FetchOpts{})
	if !IsBudgetExceeded(err) {
		t.Fatalf("request breach error=%v", err)
	}
	if _, err := page.Eval(context.Background(), "1+1"); !IsBudgetExceeded(err) {
		t.Fatalf("existing page was not cancelled: %v", err)
	}
	usage := eng.SessionUsageFor(eng.Config().SessionID)
	if usage.Requests != 1 || !usage.Cancelled {
		t.Fatalf("usage=%+v", usage)
	}
	other, err := eng.Fetch(network.WithSessionID(context.Background(), "other-session"), srv.URL, FetchOpts{})
	if err != nil {
		t.Fatalf("a breach of one session cancelled another session: %v", err)
	}
	closeTestResource(t, "other page close", other.Close)
	if eng.SessionUsage().Cancelled {
		t.Fatal("one session's breach cancelled the shared engine")
	}
}

func TestSessionBudgetResponseBytesFailClosed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeTestBody(t, w, "123456789")
	}))
	defer srv.Close()
	cfg := testConfig(srv)
	cfg.SessionBudget = SessionBudget{MaxResponseBytes: 8}
	eng, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "engine close", eng.Close)
	if _, err := eng.Fetch(context.Background(), srv.URL, FetchOpts{}); !IsBudgetExceeded(err) {
		t.Fatalf("response breach error=%v", err)
	}
	usage := eng.SessionUsageFor(eng.Config().SessionID)
	if usage.ResponseBytes > 8 || !usage.Cancelled {
		t.Fatalf("usage exceeded hard limit: %+v", usage)
	}
}

func TestSessionBudgetTabLeaseIsReleased(t *testing.T) {
	eng, err := New(Config{SessionBudget: SessionBudget{MaxTabs: 1}})
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "engine close", eng.Close)
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
	if closeErr := first.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	second, err := fetch()
	if err != nil {
		t.Fatalf("released tab was not reusable: %v", err)
	}
	closeTestResource(t, "second page close", second.Close)
}

func TestSessionBudgetConcurrencyBreachRefusesOnlyTheNewRequest(t *testing.T) {
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
	defer closeTestResource(t, "engine close", eng.Close)
	firstCtx, cancelFirst := context.WithCancel(context.Background())
	defer cancelFirst()
	firstDone := make(chan error, 1)
	go func() {
		_, fetchErr := eng.Fetch(firstCtx, srv.URL, FetchOpts{})
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
		t.Fatalf("a refused concurrent admission ended the in-flight request: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	cancelFirst()
	if err := <-firstDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("in-flight request ended with %v, want its own cancellation", err)
	}
	if eng.SessionUsage().Cancelled {
		t.Fatal("refused admission cancelled the engine")
	}
}

func TestSessionBudgetTimeoutCancelsPage(t *testing.T) {
	eng, err := New(Config{SessionBudget: SessionBudget{Timeout: 20 * time.Millisecond}})
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "engine close", eng.Close)
	page, err := eng.Fetch(context.Background(), "https://example.test/", FetchOpts{OnRequest: func(req *RequestInfo) (*ResponseInfo, error) {
		return &ResponseInfo{Status: http.StatusOK, Body: []byte("<html></html>"), FinalURL: req.URL}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "page close", page.Close)
	time.Sleep(40 * time.Millisecond)
	if _, err := page.Eval(context.Background(), "1+1"); !IsBudgetExceeded(err) {
		t.Fatalf("timeout error=%v", err)
	}
}

func TestSessionBudgetExternalScriptBreachAbortsFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/script.js" {
			writeTestBody(t, w, "globalThis.loaded = true")
			return
		}
		writeTestBody(t, w, `<html><script src="/script.js"></script></html>`)
	}))
	defer srv.Close()
	cfg := testConfig(srv)
	cfg.SessionBudget = SessionBudget{MaxRequests: 1}
	eng, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "engine close", eng.Close)
	if page, err := eng.Fetch(context.Background(), srv.URL, FetchOpts{RunScripts: true}); !IsBudgetExceeded(err) || page != nil {
		t.Fatalf("page=%v script budget error=%v", page, err)
	}
}

func TestSessionBudgetLiveAdmissionLimitsApplyWithoutRestart(t *testing.T) {
	var liveTabs, liveConcurrency atomic.Int64
	liveTabs.Store(1)
	liveConcurrency.Store(1)
	var entered atomic.Int64
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/hold" {
			entered.Add(1)
			select {
			case <-release:
			case <-r.Context().Done():
			}
		}
		writeTestBody(t, w, "<html></html>")
	}))
	defer srv.Close()
	defer close(release)
	cfg := testConfig(srv)
	cfg.SessionBudget = SessionBudget{MaxTabs: 4, MaxConcurrency: 4}
	cfg.AdmissionLimits = func() AdmissionLimits {
		return AdmissionLimits{MaxTabs: int(liveTabs.Load()), MaxConcurrency: int(liveConcurrency.Load())}
	}
	eng, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "engine close", eng.Close)

	first, err := eng.Fetch(context.Background(), srv.URL, FetchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "first page close", first.Close)
	_, err = eng.Fetch(context.Background(), srv.URL, FetchOpts{})
	var budgetErr *BudgetError
	if !errors.As(err, &budgetErr) || budgetErr.Resource != BudgetTabs || budgetErr.Limit != 1 {
		t.Fatalf("live tab ceiling 1 not enforced: %v", err)
	}
	if _, err := first.Eval(context.Background(), "1+1"); err != nil {
		t.Fatalf("a refused admission disturbed the admitted page: %v", err)
	}
	liveTabs.Store(2)
	second, err := eng.Fetch(context.Background(), srv.URL, FetchOpts{})
	if err != nil {
		t.Fatalf("raised live tab ceiling was not applied: %v", err)
	}
	closeTestResource(t, "second page close", second.Close)
	liveTabs.Store(64)
	pages := []*Page{}
	for range 3 {
		page, fetchErr := eng.Fetch(context.Background(), srv.URL, FetchOpts{})
		if fetchErr != nil {
			if !errors.As(fetchErr, &budgetErr) || budgetErr.Limit != 4 {
				t.Fatalf("static ceiling 4 must bound a looser live value: %v", fetchErr)
			}
			break
		}
		pages = append(pages, page)
	}
	if len(pages) != 3 {
		t.Fatalf("admitted %d extra pages under static ceiling 4 with one open", len(pages))
	}
	if _, err := eng.Fetch(context.Background(), srv.URL, FetchOpts{}); !errors.As(err, &budgetErr) || budgetErr.Limit != 4 {
		t.Fatalf("live value above the static ceiling loosened it: %v", err)
	}
	for _, page := range pages {
		closeTestResource(t, "page close", page.Close)
	}

	held := make(chan error, 1)
	go func() {
		page, fetchErr := eng.Fetch(context.Background(), srv.URL+"/hold", FetchOpts{})
		if fetchErr == nil {
			fetchErr = page.Close()
		}
		held <- fetchErr
	}()
	deadline := time.Now().Add(2 * time.Second)
	for entered.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if entered.Load() == 0 {
		t.Fatal("held request did not start")
	}
	if _, err := eng.Fetch(context.Background(), srv.URL, FetchOpts{}); !errors.As(err, &budgetErr) || budgetErr.Resource != BudgetConcurrency || budgetErr.Limit != 1 {
		t.Fatalf("live concurrency ceiling 1 not enforced: %v", err)
	}
	liveConcurrency.Store(2)
	page, err := eng.Fetch(context.Background(), srv.URL, FetchOpts{})
	if err != nil {
		t.Fatalf("raised live concurrency ceiling was not applied: %v", err)
	}
	closeTestResource(t, "page close", page.Close)
	release <- struct{}{}
	if err := <-held; err != nil {
		t.Fatalf("held request: %v", err)
	}
}
