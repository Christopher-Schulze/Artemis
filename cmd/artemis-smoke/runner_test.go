package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/engine"
	"github.com/Christopher-Schulze/Artemis/serve"
)

// startServeServer starts an in-process artemis serve.Server on a free
// port and returns the address. This lets the runner tests exercise the
// full WS path without spawning a subprocess or hitting the internet.
func startServeServer(t *testing.T) (addr string, cleanup func()) {
	t.Helper()
	eng, err := engine.New(engine.Config{})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	srv := serve.New(eng, serve.Opts{})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr = ln.Addr().String()
	httpSrv := &http.Server{Handler: http.HandlerFunc(srv.HandleWSForTest)}
	go func() { _ = httpSrv.Serve(ln) }()
	cleanup = func() {
		_ = httpSrv.Close()
		_ = ln.Close()
		_ = eng.Close()
	}
	return addr, cleanup
}

func TestRunnerScenarioAgainstLocalServer(t *testing.T) {
	// Local page that the scenario will fetch.
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `<!doctype html><html><head><title>LocalTest</title></head><body><h1>Hello Smoke</h1></body></html>`)
	}))
	defer page.Close()

	addr, cleanup := startServeServer(t)
	defer cleanup()

	// Build a runner that points at the in-process server by overriding
	// the host/port to match addr. We bypass BuildArtemis by setting a
	// dummy binPath that we never invoke (we call runScenario directly
	// after swapping the dial target).
	host, port := splitAddr(addr)
	r := NewRunner(RunnerConfig{
		ArtemisBinPath: "/dev/null", // never invoked because we call runScenario directly
		OutDir:         t.TempDir(),
		StepTimeout:    10 * time.Second,
		Host:           host,
		Port:           port,
	})

	scenario := Scenario{
		ID:          "local-smoke",
		Site:        page.URL,
		Description: "local httptest smoke",
		Steps: []Step{
			{Name: "open session", Cmd: "session.new"},
			{Name: "open page", Cmd: "page.open", Params: map[string]any{
				"url":        page.URL,
				"runScripts": false,
			}},
			{Name: "assert title", Cmd: "page.assert", Params: map[string]any{
				"mode":      "title_contains",
				"substring": "LocalTest",
			}},
			{Name: "dump markdown", Cmd: "page.dump", Params: map[string]any{
				"format": "markdown",
			}},
			{Name: "close page", Cmd: "page.close", Params: map[string]any{}},
			{Name: "close session", Cmd: "session.close", Params: map[string]any{}},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res := r.runScenarioWithServeAddr(ctx, &scenario, addr)

	if res.Skipped {
		t.Fatalf("scenario skipped: %s", res.SkipReason)
	}
	if !res.Pass {
		t.Errorf("scenario did not pass; steps:")
		for i, sr := range res.Steps {
			t.Errorf("  step[%d] %s cmd=%s ok=%v err=%s", i, sr.Name, sr.Cmd, sr.OK, sr.Error)
		}
	}
	if len(res.Artifacts) == 0 {
		t.Errorf("expected at least one artifact from page.dump")
	}
}

func TestRunnerScenarioAssertFailureFailsScenario(t *testing.T) {
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `<!doctype html><html><head><title>Wrong</title></head><body></body></html>`)
	}))
	defer page.Close()

	addr, cleanup := startServeServer(t)
	defer cleanup()

	host, port := splitAddr(addr)
	r := NewRunner(RunnerConfig{
		ArtemisBinPath: "/dev/null",
		OutDir:         t.TempDir(),
		StepTimeout:    10 * time.Second,
		Host:           host,
		Port:           port,
	})

	scenario := Scenario{
		ID:   "assert-fail",
		Site: page.URL,
		Steps: []Step{
			{Name: "open session", Cmd: "session.new"},
			{Name: "open page", Cmd: "page.open", Params: map[string]any{
				"url":        page.URL,
				"runScripts": false,
			}},
			{Name: "assert wrong title", Cmd: "page.assert", Params: map[string]any{
				"mode":      "title_contains",
				"substring": "ExpectedButNotPresent",
			}},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res := r.runScenarioWithServeAddr(ctx, &scenario, addr)
	if res.Pass {
		t.Errorf("expected scenario to fail due to assertion mismatch, got pass=true")
	}
}

func TestRunnerScenarioNetworkFailureTolerant(t *testing.T) {
	addr, cleanup := startServeServer(t)
	defer cleanup()

	host, port := splitAddr(addr)
	r := NewRunner(RunnerConfig{
		ArtemisBinPath: "/dev/null",
		OutDir:         t.TempDir(),
		StepTimeout:    10 * time.Second,
		Host:           host,
		Port:           port,
	})

	// Use a port that is almost certainly closed to force a fetch error.
	scenario := Scenario{
		ID:   "net-fail",
		Site: "http://127.0.0.1:1/nope",
		Steps: []Step{
			{Name: "open session", Cmd: "session.new"},
			{Name: "open dead page", Cmd: "page.open", Params: map[string]any{
				"url":        "http://127.0.0.1:1/nope",
				"runScripts": false,
			}},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res := r.runScenarioWithServeAddr(ctx, &scenario, addr)
	if res.Pass {
		t.Errorf("expected scenario to fail (dead URL)")
	}
	if !res.NetworkFail {
		t.Errorf("expected NetworkFail=true for fetch_failed on page.open")
	}
}

func TestScorecardJSONRoundTrip(t *testing.T) {
	sc := &Scorecard{
		Version:    "1",
		StartedAt:  time.Now(),
		FinishedAt: time.Now(),
		Total:      3,
		Passed:     2,
		Failed:     1,
		Results: []ScenarioResult{
			{ID: "a", Site: "https://a", Pass: true},
			{ID: "b", Site: "https://b", Pass: true},
			{ID: "c", Site: "https://c", Pass: false, NetworkFail: true},
		},
	}
	data, err := json.MarshalIndent(sc, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back Scorecard
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Total != 3 || back.Passed != 2 || back.Failed != 1 {
		t.Errorf("round-trip mismatch: %+v", back)
	}
}

func TestInjectIDs(t *testing.T) {
	st := &Step{Cmd: "page.dump", Params: map[string]any{"format": "markdown"}}
	injectIDs(st, "s1", "p1")
	if st.Params["sessionId"] != "s1" {
		t.Errorf("sessionId = %v, want s1", st.Params["sessionId"])
	}
	if st.Params["pageId"] != "p1" {
		t.Errorf("pageId = %v, want p1", st.Params["pageId"])
	}
	// Explicit values should not be overwritten.
	st2 := &Step{Cmd: "page.dump", Params: map[string]any{"sessionId": "explicit", "pageId": "pp"}}
	injectIDs(st2, "s1", "p1")
	if st2.Params["sessionId"] != "explicit" {
		t.Errorf("sessionId = %v, want explicit", st2.Params["sessionId"])
	}
	// session.new needs neither.
	st3 := &Step{Cmd: "session.new", Params: map[string]any{}}
	injectIDs(st3, "s1", "p1")
	if _, ok := st3.Params["sessionId"]; ok {
		t.Error("session.new should not get sessionId injected")
	}
}

func TestStepNeedsSession(t *testing.T) {
	cases := map[string]bool{
		"session.new":        false,
		"session.close":      true,
		"page.open":          true,
		"page.dump":          true,
		"page.assert":        true,
		"page.type":          true,
		"page.wait_idle":     true,
		"page.click_by_text": true,
		"page.eval":          true,
		"page.close":         true,
		"unknown":            false,
	}
	for cmd, want := range cases {
		if got := stepNeedsSession(cmd); got != want {
			t.Errorf("stepNeedsSession(%q) = %v, want %v", cmd, got, want)
		}
	}
}

func TestStepNeedsPage(t *testing.T) {
	cases := map[string]bool{
		"session.new":        false,
		"session.close":      false,
		"page.open":          false,
		"page.dump":          true,
		"page.assert":        true,
		"page.type":          true,
		"page.wait_idle":     true,
		"page.click_by_text": true,
		"page.eval":          true,
		"page.close":         true,
	}
	for cmd, want := range cases {
		if got := stepNeedsPage(cmd); got != want {
			t.Errorf("stepNeedsPage(%q) = %v, want %v", cmd, got, want)
		}
	}
}

func TestSanitize(t *testing.T) {
	cases := map[string]string{
		"open page":     "open_page",
		"dump markdown": "dump_markdown",
		"":              "step",
		"a-b_c.d":       "a-b_c_d",
	}
	for in, want := range cases {
		if got := sanitize(in); got != want {
			t.Errorf("sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestExtractString(t *testing.T) {
	v := map[string]any{"data": "hello", "n": 42}
	s, ok := extractString(v, "data")
	if !ok || s != "hello" {
		t.Errorf("data = %q ok=%v", s, ok)
	}
	if _, ok := extractString(v, "missing"); ok {
		t.Error("expected missing key to return false")
	}
	if _, ok := extractString(v, "n"); ok {
		t.Error("expected non-string value to return false")
	}
}

func splitAddr(addr string) (host string, port int) {
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		panic("splitAddr: " + err.Error())
	}
	var pi int
	fmt.Sscanf(p, "%d", &pi)
	return h, pi
}
