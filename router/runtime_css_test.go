package router

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/css"
	"github.com/Christopher-Schulze/Artemis/engine"
)

func TestRenderlessExecutorsPreserveTypedCSSFailures(t *testing.T) {
	for _, test := range []struct {
		name, body string
		code       css.DiagnosticCode
		retryable  bool
	}{
		{"unsupported", `<style>@media all {p{color:red}}</style><p>partial</p>`, css.DiagnosticUnsupported, true},
		{"load", `<link rel="stylesheet" href="/missing.css"><p>partial</p>`, css.DiagnosticLoad, true},
		{"caught callback", `<p id="x" style="color:red;broken">partial</p><script>try {document.getElementById('x').style.color} catch(e) {}</script>`, css.DiagnosticSyntax, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/" {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "text/html")
				if _, err := fmt.Fprint(w, test.body); err != nil {
					t.Errorf("write fixture: %v", err)
				}
			}))
			defer server.Close()
			eng := testEngineConfig(t, 3*time.Second, server)
			defer closeRouterTestResource(t, "engine", eng.Close)
			for _, executor := range []Executor{RuntimeExecutor{Runtime: eng, RunScripts: true}, RenderlessExecutor{Engine: eng, RunScripts: true}} {
				output, err := executor.Execute(t.Context(), ExecutionRequest{URL: server.URL})
				if output.Resource != nil {
					closeRouterTestResource(t, "unexpected partial resource", output.Resource.Close)
					t.Fatal("CSS failure transferred a partial page")
				}
				var failure *RouteFailure
				if output.Verified || !errors.As(err, &failure) || failure.Reason != "renderless_execution_failed" || failure.Retryable != test.retryable {
					t.Fatalf("CSS failure lost canonical phase-aware routing evidence: %+v %v", output, err)
				}
				var value css.Diagnostic
				var pointer *css.Diagnostic
				if errors.As(err, &value) {
					pointer = &value
				} else if !errors.As(err, &pointer) {
					t.Fatalf("typed CSS diagnostic lost through real engine/router: %v", err)
				}
				if pointer.Code != test.code {
					t.Fatalf("wrong diagnostic: %+v", pointer)
				}
			}
		})
	}
}

func TestCSSFailureCannotReplayCompletedHTTPPost(t *testing.T) {
	for _, test := range []struct {
		name, method, body string
		fallback, posts    int32
	}{
		{"script effect", http.MethodGet, `<p id="x" style="color:red;broken"></p><script>fetch('/effect',{method:'POST'});try {document.getElementById('x').style.color} catch(e) {}</script>`, 0, 1},
		{"navigation effect", http.MethodPost, `<style>@media all {p{color:red}}</style><p>partial</p>`, 0, 1},
		{"pre-script positive control", http.MethodGet, `<style>@media all {p{color:red}}</style><p>partial</p>`, 1, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			var posts, fallback atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					posts.Add(1)
				}
				w.Header().Set("Content-Type", "text/html")
				body := test.body
				if r.URL.Path == "/effect" {
					body = "effect-completed"
				}
				if _, err := fmt.Fprint(w, body); err != nil {
					t.Errorf("actual HTTP response: %v", err)
				}
			}))
			defer server.Close()
			eng := testEngineConfig(t, 3*time.Second, server)
			defer closeRouterTestResource(t, "engine", eng.Close)
			router, err := New(Config{Executors: map[Mode]Executor{
				ModeRenderlessJS: RenderlessExecutor{Engine: eng, RunScripts: true},
				ModeChromiumCDP: executorFunc(func(_ context.Context, _ ExecutionRequest) (ExecutionOutput, error) {
					fallback.Add(1)
					return ExecutionOutput{}, errors.New("fallback invocation observed")
				}),
			}})
			if err != nil {
				t.Fatal(err)
			}
			result, err := router.Execute(t.Context(), RouteRequest{URL: server.URL, Method: test.method, Signals: Signals{IsHTML: true, ScriptCount: 1}})
			if err == nil || result.Success || posts.Load() != test.posts || fallback.Load() != test.fallback {
				t.Fatalf("CSS failure admitted unsafe replay or blocked the pre-script control: posts=%d fallback=%d expected=%d/%d err=%v", posts.Load(), fallback.Load(), test.posts, test.fallback, err)
			}
		})
	}
}

func TestCSSFailureReplayClassificationPreservesPhaseAndBudget(t *testing.T) {
	for _, test := range []struct {
		name      string
		request   ExecutionRequest
		cause     error
		retryable bool
	}{
		{"safe HEAD", ExecutionRequest{Method: http.MethodHead}, &css.Diagnostic{Code: css.DiagnosticLoad}, true},
		{"GET body", ExecutionRequest{Method: http.MethodGet, Body: []byte("payload")}, &css.Diagnostic{Code: css.DiagnosticLoad}, false},
		{"budget cause", ExecutionRequest{}, &css.Diagnostic{Code: css.DiagnosticLoad, Cause: &engine.BudgetError{}}, false},
		{"CSS budget", ExecutionRequest{}, &css.Diagnostic{Code: css.DiagnosticBudget}, false},
		{"joined CSS budget", ExecutionRequest{}, errors.Join(css.Diagnostic{Code: css.DiagnosticSyntax}, &css.Diagnostic{Code: css.DiagnosticBudget}), false},
		{"phase without diagnostic", ExecutionRequest{}, &css.ExecutionFailure{Cause: errors.New("execution failed")}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			failure := renderlessFetchFailure(test.request, test.cause)
			if failure.Retryable != test.retryable || !errors.Is(failure, test.cause) {
				t.Fatalf("replay classification or original failure identity changed: %+v", failure)
			}
		})
	}
}

func TestCSSIdleFailureCannotReplayCompletedHTTPPost(t *testing.T) {
	for _, runtime := range []bool{false, true} {
		t.Run(fmt.Sprintf("runtime=%t", runtime), func(t *testing.T) {
			var posts, fallback atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/pending" {
					<-r.Context().Done()
					return
				}
				if r.Method == http.MethodPost {
					posts.Add(1)
				}
				w.Header().Set("Content-Type", "text/html")
				body := `<p id="x" style="color:red;broken"></p><script>fetch('/effect',{method:'POST'}).then(()=>{setTimeout(()=>{try{document.getElementById('x').style.color}catch(e){}},100);fetch('/pending')});</script>`
				if r.URL.Path == "/effect" {
					body = "effect-completed"
				}
				if _, err := fmt.Fprint(w, body); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			eng := testEngineConfig(t, 3*time.Second, server)
			defer closeRouterTestResource(t, "engine", eng.Close)
			var executor Executor = RenderlessExecutor{Engine: eng, RunScripts: true}
			if runtime {
				executor = RuntimeExecutor{Runtime: eng, RunScripts: true}
			}
			router, err := New(Config{Executors: map[Mode]Executor{ModeRenderlessJS: executor, ModeChromiumCDP: executorFunc(func(context.Context, ExecutionRequest) (ExecutionOutput, error) {
				fallback.Add(1)
				return ExecutionOutput{}, errors.New("unexpected replay")
			})}})
			if err != nil {
				t.Fatal(err)
			}
			result, err := router.Execute(t.Context(), RouteRequest{URL: server.URL, AsyncFetch: true, Signals: Signals{IsHTML: true, ScriptCount: 1}})
			var failure *RouteFailure
			if err == nil || result.Success || posts.Load() != 1 || fallback.Load() != 0 || !css.IsDiagnostic(err) || !errors.As(err, &failure) || failure.Reason != "renderless_idle_failed" {
				t.Fatalf("idle CSS failure replayed completed effects: posts=%d fallback=%d err=%v", posts.Load(), fallback.Load(), err)
			}
		})
	}
}
