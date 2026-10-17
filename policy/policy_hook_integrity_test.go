package policy

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type policyEngineFunction func(context.Context, PolicyRequest) (PolicyResponse, error)

func (evaluate policyEngineFunction) Evaluate(ctx context.Context, request PolicyRequest) (PolicyResponse, error) {
	return evaluate(ctx, request)
}

func TestPolicyHookRejectsTypedNilHostWithoutInvokingIt(t *testing.T) {
	var engine policyEngineFunction
	hook := NewPolicyHook(engine)
	response, err := hook.Check(t.Context(), PolicyRequest{Action: "navigate", URL: "https://example.com"})
	if err == nil || response.Decision != PolicyDecisionDeny || hook.Stats().Denied != 1 {
		t.Fatalf("typed nil host admitted: %+v %v", response, err)
	}
}

func TestPolicyHookRejectsUnknownRewriteAndExpiredHostDecision(t *testing.T) {
	for _, test := range []struct {
		name     string
		response PolicyResponse
	}{
		{"unknown", PolicyResponse{Decision: "oops"}},
		{"empty", PolicyResponse{}},
		{"rewrite", PolicyResponse{Decision: PolicyDecisionAllow, RewrittenURL: "https://evil.example"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			hook := NewPolicyHook(policyEngineFunction(func(context.Context, PolicyRequest) (PolicyResponse, error) { return test.response, nil }))
			response, err := hook.Check(context.Background(), PolicyRequest{URL: "https://example.com"})
			if err == nil || response.Decision != PolicyDecisionDeny || hook.Stats().Denied != 1 {
				t.Fatalf("invalid host response admitted: %+v %v %+v", response, err, hook.Stats())
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	hook := NewPolicyHook(policyEngineFunction(func(context.Context, PolicyRequest) (PolicyResponse, error) {
		cancel()
		return PolicyResponse{Decision: PolicyDecisionAllow}, nil
	}))
	response, err := hook.Check(ctx, PolicyRequest{URL: "https://example.com"})
	if !errors.Is(err, context.Canceled) || response.Decision != PolicyDecisionDeny {
		t.Fatalf("expired host decision admitted: %+v %v", response, err)
	}
}

func TestPolicyHookDoesNotHoldLockAcrossHostAndOwnsHeaders(t *testing.T) {
	policy, err := NewStaticNavigationPolicy([]byte(`{"allow":["navigate"]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer policy.Stop()
	var hook *PolicyHook
	hook = NewPolicyHook(policyEngineFunction(func(ctx context.Context, request PolicyRequest) (PolicyResponse, error) {
		if !hook.Enabled() || hook.Stats().Total != 1 {
			return PolicyResponse{}, errors.New("invalid hook state")
		}
		request.Headers["Authorization"] = "changed by host"
		response, err := policy.Evaluate(ctx, request)
		hook.SetEnabled(false)
		return response, err
	}))
	headers := map[string]string{"Authorization": "caller owned"}
	done := make(chan struct{})
	var response PolicyResponse
	var checkErr error
	var callers sync.WaitGroup
	callers.Go(func() {
		response, checkErr = hook.Check(context.Background(), PolicyRequest{URL: "https://example.com", Action: "navigate", Headers: headers})
		close(done)
	})
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("host callback deadlocked while inspecting/disabling the hook")
	}
	callers.Wait()
	if checkErr == nil || response.Decision != PolicyDecisionDeny || headers["Authorization"] != "caller owned" || hook.Stats().Denied != 1 {
		t.Fatalf("disable/header boundary failed: %+v %v %+v", response, checkErr, hook.Stats())
	}
}

func TestPolicyHookConcurrentActualPolicyDecisionsRetainCounters(t *testing.T) {
	policy, err := NewStaticNavigationPolicy([]byte(`{"allow":["click"],"deny":["evaluate"],"confirm":["submit"]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer policy.Stop()
	hook := NewPolicyHook(policy)
	var callers sync.WaitGroup
	for range 32 {
		for _, kind := range []string{"click", "evaluate", "submit"} {
			callers.Go(func() {
				response, err := hook.Check(context.Background(), PolicyRequest{URL: "https://example.com", Action: kind})
				if err != nil || response.Decision == "" {
					t.Errorf("actual concurrent policy decision: %+v %v", response, err)
				}
			})
		}
	}
	callers.Wait()
	if got := hook.Stats(); got != (PolicyHookStats{Total: 96, Allowed: 32, Denied: 32, Challenged: 32}) {
		t.Fatalf("lost/doubled policy counters: %+v", got)
	}
}
