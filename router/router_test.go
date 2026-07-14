package router

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type executorFunc func(context.Context, ExecutionRequest) (ExecutionOutput, error)

func (f executorFunc) Execute(ctx context.Context, request ExecutionRequest) (ExecutionOutput, error) {
	return f(ctx, request)
}

type telemetryFunc func(RouteEvidence)

func (f telemetryFunc) Record(evidence RouteEvidence) { f(evidence) }

type chromiumPageFunc struct {
	navigate func(context.Context, string) error
	call     func(context.Context, string, any, any) error
}

type closeCounter struct{ closed atomic.Int32 }

func (r *closeCounter) Close() error {
	r.closed.Add(1)
	return nil
}

func (p chromiumPageFunc) Navigate(ctx context.Context, target string) (string, string, error) {
	if err := p.navigate(ctx, target); err != nil {
		return "", "", err
	}
	return "frame-1", "loader-1", nil
}

func (p chromiumPageFunc) Call(ctx context.Context, method string, params, result any) error {
	return p.call(ctx, method, params, result)
}

func verifiedOutput(request ExecutionRequest, body string) ExecutionOutput {
	return ExecutionOutput{
		Page:  PageOutput{URL: request.URL, StatusCode: http.StatusOK, HTML: body, Text: body},
		State: request.State.Clone(), Quality: ResultQualityVerified, Verified: true,
	}
}

func TestHybridRouterStaticUsesRealRenderlessExecutor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Add("Set-Cookie", "route_state=preserved; Path=/")
		_, _ = fmt.Fprint(w, "<!doctype html><title>Static</title><p>real fixture</p>")
	}))
	defer server.Close()

	eng := testEngineConfig(t, time.Second, server)
	defer eng.Close()
	r, err := New(Config{Executors: map[Mode]Executor{
		ModeStaticFetch: RenderlessExecutor{Engine: eng},
	}})
	if err != nil {
		t.Fatalf("router.New: %v", err)
	}
	result, err := r.Execute(context.Background(), RouteRequest{
		URL: server.URL, Signals: Signals{IsHTML: true}, TraceID: "trace-static",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	defer result.Close()
	if !result.Success || result.Evidence.FinalMode != ModeStaticFetch {
		t.Fatalf("result=%+v", result)
	}
	if result.Output.Title != "Static" || !strings.Contains(result.Output.Text, "real fixture") {
		t.Fatalf("output=%+v", result.Output)
	}
	if len(result.Evidence.Fallbacks) != 0 || result.Evidence.ResultQuality != ResultQualityVerified {
		t.Fatalf("evidence=%+v", result.Evidence)
	}
}

func TestChromiumExecutorRequiresCommittedDOMAndPreservesState(t *testing.T) {
	page := chromiumPageFunc{
		navigate: func(_ context.Context, target string) error {
			if target != "https://fixture.test/dynamic" {
				return fmt.Errorf("unexpected target %s", target)
			}
			return nil
		},
		call: func(_ context.Context, method string, _ any, result any) error {
			if method != "Runtime.evaluate" {
				return fmt.Errorf("unexpected method %s", method)
			}
			response, ok := result.(*evaluateResponse)
			if !ok {
				return fmt.Errorf("unexpected result type %T", result)
			}
			response.Result.Type = "object"
			response.Result.Value = chromiumPageOutput{URL: "https://fixture.test/dynamic", Title: "Dynamic", HTML: "<html><body>ready</body></html>"}
			return nil
		},
	}
	state := BrowserState{SessionID: "s1", ProfileID: "p1", Cookies: []*http.Cookie{{Name: "auth", Value: "secret"}}}
	output, err := (ChromiumExecutor{Page: page}).Execute(context.Background(), ExecutionRequest{URL: "https://fixture.test/dynamic", State: state})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !output.Verified || output.Page.Title != "Dynamic" || output.Page.HTML == "" {
		t.Fatalf("output=%+v", output)
	}
	if len(output.State.Cookies) != 1 || output.State.Cookies[0].Value != "secret" {
		t.Fatalf("state=%+v", output.State)
	}
}

func TestHybridRouterEscalatesOnceAndPreservesStateWithoutSecretEvidence(t *testing.T) {
	var renderlessCalls, chromiumCalls atomic.Int32
	secret := "cookie-secret-value"
	state := BrowserState{
		SessionID: "session-1", ProfileID: "profile-1", OwnerUserRef: "owner-1",
		CookieScope: "https://fixture.test", StorageScope: "profile-1",
		Cookies:      []*http.Cookie{{Name: "auth", Value: secret}},
		LocalStorage: map[string]string{"authenticated": "true"},
	}
	r, err := New(Config{Executors: map[Mode]Executor{
		ModeRenderlessJS: executorFunc(func(_ context.Context, request ExecutionRequest) (ExecutionOutput, error) {
			renderlessCalls.Add(1)
			if request.State.Cookies[0].Value != secret || request.State.LocalStorage["authenticated"] != "true" {
				return ExecutionOutput{}, errors.New("state was not transferred")
			}
			return ExecutionOutput{}, &RouteFailure{Reason: "layout_query_reliance", Retryable: true, Cause: errors.New("renderless cannot provide layout")}
		}),
		ModeChromiumCDP: executorFunc(func(_ context.Context, request ExecutionRequest) (ExecutionOutput, error) {
			chromiumCalls.Add(1)
			if request.State.Cookies[0].Value != secret || request.State.ProfileID != "profile-1" {
				return ExecutionOutput{}, errors.New("auth state was not preserved")
			}
			return verifiedOutput(request, "<html>authenticated</html>"), nil
		}),
	}, Telemetry: telemetryFunc(func(evidence RouteEvidence) {
		encoded, marshalErr := json.Marshal(evidence)
		if marshalErr != nil {
			t.Errorf("marshal evidence: %v", marshalErr)
		}
		if strings.Contains(string(encoded), secret) {
			t.Errorf("route evidence leaked cookie secret: %s", encoded)
		}
	})})
	if err != nil {
		t.Fatalf("router.New: %v", err)
	}
	result, err := r.Execute(context.Background(), RouteRequest{
		URL: "https://fixture.test/dashboard", Signals: Signals{IsHTML: true, ScriptCount: 1},
		Auth:  AuthState{},
		State: state, TraceID: "trace-escalate", EvidenceID: "evidence-escalate",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.Success || result.Evidence.FinalMode != ModeChromiumCDP {
		t.Fatalf("result=%+v", result)
	}
	if renderlessCalls.Load() != 1 || chromiumCalls.Load() != 1 {
		t.Fatalf("calls renderless=%d chromium=%d", renderlessCalls.Load(), chromiumCalls.Load())
	}
	if len(result.Evidence.Fallbacks) != 1 || result.Evidence.Fallbacks[0].Reason != "layout_query_reliance" {
		t.Fatalf("fallbacks=%+v", result.Evidence.Fallbacks)
	}
	if result.Evidence.State.CookieCount != 1 || result.Evidence.State.Authenticated {
		t.Fatalf("state evidence=%+v", result.Evidence.State)
	}
}

func TestHybridRouterRejectsForcedAuthDowngrade(t *testing.T) {
	r, err := New(Config{Executors: map[Mode]Executor{
		ModeRenderlessJS: executorFunc(func(_ context.Context, request ExecutionRequest) (ExecutionOutput, error) {
			return verifiedOutput(request, "<html>wrong</html>"), nil
		}),
	}})
	if err != nil {
		t.Fatalf("router.New: %v", err)
	}
	_, err = r.Decide(RouteRequest{
		URL: "https://fixture.test/account", ForceMode: ModeRenderlessJS,
		Auth: AuthState{Authenticated: true, SessionID: "s", ProfileID: "p"},
	})
	var routeErr *RouteError
	if !errors.As(err, &routeErr) || routeErr.Code != ErrorPolicyDenied || !errors.Is(err, ErrAuthSemanticDowngrade) {
		t.Fatalf("err=%v", err)
	}
}

func TestHybridRouterExplicitDowngradeRequiresCleanState(t *testing.T) {
	r, err := New(Config{Executors: map[Mode]Executor{ModeRenderlessJS: executorFunc(func(_ context.Context, request ExecutionRequest) (ExecutionOutput, error) {
		return verifiedOutput(request, "<html></html>"), nil
	})}})
	if err != nil {
		t.Fatalf("router.New: %v", err)
	}
	decision, err := r.Downgrade(RouteRequest{URL: "https://fixture.test/public"}, ModeChromiumCDP, ModeRenderlessJS)
	if err != nil || decision.Reason != "explicit_safe_downgrade" {
		t.Fatalf("decision=%+v err=%v", decision, err)
	}
	_, err = r.Downgrade(RouteRequest{URL: "https://fixture.test/account", State: BrowserState{Cookies: []*http.Cookie{{Name: "auth", Value: "secret"}}}}, ModeChromiumCDP, ModeRenderlessJS)
	if !errors.Is(err, ErrAuthSemanticDowngrade) {
		t.Fatalf("cookie-bearing downgrade err=%v", err)
	}
}

func TestHybridRouterCircuitBreakerBlocksAfterThreshold(t *testing.T) {
	var calls atomic.Int32
	r, err := New(Config{Executors: map[Mode]Executor{
		ModeStaticFetch: executorFunc(func(_ context.Context, _ ExecutionRequest) (ExecutionOutput, error) {
			calls.Add(1)
			return ExecutionOutput{}, &RouteFailure{Reason: "network", Retryable: false, Cause: errors.New("fixture failure")}
		}),
	}, Now: time.Now})
	if err != nil {
		t.Fatalf("router.New: %v", err)
	}
	request := RouteRequest{URL: "https://fixture.test/failure", Signals: Signals{IsHTML: true}, Policy: Policy{AllowedModes: map[Mode]bool{ModeStaticFetch: true}, CircuitFailureThreshold: 1}}
	if _, err := r.Execute(context.Background(), request); err == nil {
		t.Fatal("first execution unexpectedly succeeded")
	}
	_, err = r.Execute(context.Background(), request)
	if err == nil {
		t.Fatal("second execution unexpectedly succeeded")
	}
	if calls.Load() != 1 {
		t.Fatalf("executor calls=%d, want 1 after circuit opened", calls.Load())
	}
}

func TestHybridRouterEnforcesCostBudgetAndClosesResource(t *testing.T) {
	resource := &closeCounter{}
	r, err := New(Config{Executors: map[Mode]Executor{
		ModeStaticFetch: executorFunc(func(_ context.Context, request ExecutionRequest) (ExecutionOutput, error) {
			output := verifiedOutput(request, "<html>budget</html>")
			output.CostUnit = 2
			output.Resource = resource
			return output, nil
		}),
	}})
	if err != nil {
		t.Fatalf("router.New: %v", err)
	}
	_, err = r.Execute(context.Background(), RouteRequest{
		URL: "https://fixture.test/budget", Signals: Signals{IsHTML: true},
		Policy: Policy{MaxCostUnit: 1},
	})
	var routeErr *RouteError
	if !errors.As(err, &routeErr) || routeErr.Code != ErrorResourceBudget {
		t.Fatalf("err=%v", err)
	}
	if resource.closed.Load() != 1 {
		t.Fatalf("resource close count=%d", resource.closed.Load())
	}
}

func TestHybridRouterDecisionIsStable(t *testing.T) {
	r, err := New(Config{Executors: map[Mode]Executor{ModeStaticFetch: executorFunc(func(_ context.Context, request ExecutionRequest) (ExecutionOutput, error) {
		return verifiedOutput(request, "<html></html>"), nil
	})}})
	if err != nil {
		t.Fatalf("router.New: %v", err)
	}
	request := RouteRequest{URL: "https://fixture.test/static", Signals: Signals{IsHTML: true}}
	first, err := r.Decide(request)
	if err != nil {
		t.Fatalf("first decision: %v", err)
	}
	for i := 0; i < 20; i++ {
		got, decideErr := r.Decide(request)
		if decideErr != nil || got != first {
			t.Fatalf("decision %d=%+v err=%v first=%+v", i, got, decideErr, first)
		}
	}
}

func TestHybridRouterAllowsDeclaredExecutorDivergenceWithoutFalseParity(t *testing.T) {
	r, err := New(Config{Executors: map[Mode]Executor{
		ModeStaticFetch: executorFunc(func(_ context.Context, request ExecutionRequest) (ExecutionOutput, error) {
			return verifiedOutput(request, "<html><body>static</body></html>"), nil
		}),
		ModeChromiumCDP: executorFunc(func(_ context.Context, request ExecutionRequest) (ExecutionOutput, error) {
			return verifiedOutput(request, "<html><body>browser-layout</body></html>"), nil
		}),
	}})
	if err != nil {
		t.Fatalf("router.New: %v", err)
	}
	staticResult, err := r.Execute(context.Background(), RouteRequest{URL: "https://fixture.test/divergent", Signals: Signals{IsHTML: true}})
	if err != nil {
		t.Fatalf("static route: %v", err)
	}
	defer staticResult.Close()
	chromiumResult, err := r.Execute(context.Background(), RouteRequest{URL: "https://fixture.test/divergent", ForceMode: ModeChromiumCDP})
	if err != nil {
		t.Fatalf("Chromium route: %v", err)
	}
	if staticResult.Output.Text == chromiumResult.Output.Text || !chromiumResult.Success {
		t.Fatalf("divergence was hidden: static=%q chromium=%q", staticResult.Output.Text, chromiumResult.Output.Text)
	}
}

func TestHybridRouterCancellationIsTyped(t *testing.T) {
	r, err := New(Config{Executors: map[Mode]Executor{ModeStaticFetch: executorFunc(func(ctx context.Context, _ ExecutionRequest) (ExecutionOutput, error) {
		<-ctx.Done()
		return ExecutionOutput{}, ctx.Err()
	})}})
	if err != nil {
		t.Fatalf("router.New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = r.Execute(ctx, RouteRequest{URL: "https://fixture.test/cancel", Signals: Signals{IsHTML: true}})
	var routeErr *RouteError
	if !errors.As(err, &routeErr) || routeErr.Code != ErrorCancelled {
		t.Fatalf("err=%v", err)
	}
}
