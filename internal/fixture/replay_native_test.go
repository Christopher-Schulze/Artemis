package fixture

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestReplayRegressionRejectsExecutionOverrides(t *testing.T) {
	replay, err := CaptureReplay(Scenario{ID: "snapshot", Path: "/snapshot", Kind: KindHTML}, "renderless", CrossResult{StatusCode: 200, HTML: "<html><body>snapshot</body></html>"}, 0, SystemMetadata(), DefaultRedactionConfig(), "")
	if err != nil {
		t.Fatal(err)
	}
	regression, err := replay.AsRegression()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*Scenario)
	}{
		{"handler", func(sc *Scenario) {
			sc.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) })
		}},
		{"scripts", func(sc *Scenario) { sc.RunScripts = true }},
		{"async fetch", func(sc *Scenario) { sc.AsyncFetch = true }},
		{"idle execution", func(sc *Scenario) { sc.WaitForIdle = true }},
		{"script count", func(sc *Scenario) { sc.ScriptCount = 1 }},
		{"evaluation", func(sc *Scenario) { sc.Expect = Expect{Eval: "document.title='Executed'", EvalContains: "Executed"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := NewServer()
			defer server.Close()
			modified := regression
			tc.change(&modified)
			if err := server.Register(modified); err == nil || len(server.Scenarios()) != 0 {
				t.Fatalf("execution override published: %v", err)
			}
		})
	}
}

func newReplayIsolationServer(t *testing.T) (*Server, *atomic.Int64) {
	t.Helper()
	server := NewServer()
	t.Cleanup(server.Close)
	requests := &atomic.Int64{}
	if err := server.RegisterHandler("/probe/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "text/html")
		if _, err := io.WriteString(w, "<html><title>Probe</title><body>external probe</body></html>"); err != nil {
			t.Error(err)
		}
	})); err != nil {
		t.Fatal(err)
	}
	if err := server.RegisterHTML("/control", "<html><head><title>Control</title></head><body><script>document.title='Executed';fetch('/probe/control')</script>live control</body></html>"); err != nil {
		t.Fatal(err)
	}
	return server, requests
}

func newReplayIsolationProjection(t *testing.T) Scenario {
	t.Helper()
	const body = `<html><head><title>Snapshot</title>
<meta http-equiv="Content-Security-Policy" content="default-src * 'unsafe-inline'">
<meta http-equiv="refresh" content="0;url=/probe/navigation">
<base href="/probe/"><link rel="stylesheet" href="/probe/style.css">
<style>body{background-image:url('/probe/css-image')}</style></head>
<body onload="document.title='Changed';fetch('/probe/load')"><p>captured snapshot</p>
<script>document.title='Changed';fetch('/probe/script');navigator.sendBeacon('/probe/beacon','data')</script>
<script src="/probe/external.js"></script><img src="/probe/image" onerror="fetch('/probe/error')">
<iframe src="/probe/frame"></iframe><iframe srcdoc="<script>fetch('/probe/srcdoc')</script>"></iframe>
<object data="/probe/object"></object><video src="/probe/media" autoplay></video>
<form action="/probe/form"><input name="value" value="captured"></form></body></html>`
	replay, err := CaptureReplay(Scenario{ID: "snapshot", Path: "/snapshot", Kind: KindHTML}, "renderless", CrossResult{StatusCode: 200, HTML: body, Title: "Snapshot", Text: "captured snapshot"}, 0, SystemMetadata(), DefaultRedactionConfig(), "")
	if err != nil {
		t.Fatal(err)
	}
	path, err := replay.Save(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	replay, err = LoadReplay(path)
	if err != nil {
		t.Fatal(err)
	}
	regression, err := replay.AsRegression()
	if err != nil {
		t.Fatal(err)
	}
	if regression.HTML != body {
		t.Fatal("stored replay projection changed captured HTML")
	}
	return regression
}

func TestReplayRegressionNativeContentRemainsInert(t *testing.T) {
	server, requests := newReplayIsolationServer(t)
	regression := newReplayIsolationProjection(t)
	if err := server.Register(regression); err != nil {
		t.Fatal(err)
	}
	response, err := http.Get(server.URL(regression.Path))
	if err != nil {
		t.Fatal(err)
	}
	bytes, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil {
		t.Fatalf("read replay response: %v; close: %v", readErr, closeErr)
	}
	if string(bytes) != regression.HTML || response.Header.Get("Content-Security-Policy") != replayContentSecurityPolicy || response.Header.Get("Referrer-Policy") != "no-referrer" || response.Header.Get("X-Content-Type-Options") != "nosniff" || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("inert replay response changed snapshot bytes or omitted its enforced headers")
	}
	for _, runner := range []runner{&bridgeRunner{}, &artemisRunner{}} {
		t.Run(runner.name(), func(t *testing.T) {
			runReplayNativeIsolation(t, runner, server, regression, requests)
		})
	}
}

func runReplayNativeIsolation(t *testing.T, runner runner, server *Server, regression Scenario, requests *atomic.Int64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), nativeRunnerCleanupBudget)
		defer cancel()
		runner.stop(cleanup, t)
	}()
	runner.start(ctx, t, server)
	before := requests.Load()
	control := runner.run(ctx, t, Scenario{ID: "control", Path: "/control", Kind: KindHTML}, server)
	if control.Title != "Executed" {
		t.Fatalf("ordinary fixture script did not execute: %q", control.Title)
	}
	waitForReplayProbe(t, ctx, requests, before)
	before = requests.Load()
	actual := runner.run(ctx, t, regression, server)
	if err := ValidateCrossResult(regression, actual); err != nil || !strings.HasSuffix(actual.URL, regression.Path) {
		t.Fatalf("snapshot executed or navigated: %v %+v", err, actual)
	}
	if requests.Load() != before {
		t.Fatal("inert replay requested an active or external resource")
	}
}

func waitForReplayProbe(t *testing.T, ctx context.Context, requests *atomic.Int64, before int64) {
	t.Helper()
	deadline, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	for requests.Load() == before {
		select {
		case <-deadline.Done():
			t.Fatal("ordinary fixture did not reach the live probe")
		case <-time.After(10 * time.Millisecond):
		}
	}
}
