package bridge

import (
	"context"
	"errors"
	"testing"
)

// mockPolicyEngine for testing.
type mockPolicyEngine struct {
	decision PolicyDecision
	err      error
	calls    int
}

func (m *mockPolicyEngine) Evaluate(ctx context.Context, req PolicyRequest) (PolicyResponse, error) {
	m.calls++
	if m.err != nil {
		return PolicyResponse{}, m.err
	}
	return PolicyResponse{Decision: m.decision, Reason: "mock decision"}, nil
}

func TestPolicyHook_Check_Allow(t *testing.T) {
	engine := &mockPolicyEngine{decision: PolicyDecisionAllow}
	hook := NewPolicyHook(engine)
	resp, err := hook.Check(context.Background(), PolicyRequest{URL: "https://example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Decision != PolicyDecisionAllow {
		t.Errorf("expected allow, got %s", resp.Decision)
	}
	if hook.Stats().Allowed != 1 {
		t.Errorf("expected allowed=1, got %d", hook.Stats().Allowed)
	}
}

func TestPolicyHook_Check_Deny(t *testing.T) {
	engine := &mockPolicyEngine{decision: PolicyDecisionDeny}
	hook := NewPolicyHook(engine)
	resp, _ := hook.Check(context.Background(), PolicyRequest{URL: "https://evil.com"})
	if resp.Decision != PolicyDecisionDeny {
		t.Errorf("expected deny, got %s", resp.Decision)
	}
	if hook.Stats().Denied != 1 {
		t.Errorf("expected denied=1, got %d", hook.Stats().Denied)
	}
}

func TestPolicyHook_Check_Challenge(t *testing.T) {
	engine := &mockPolicyEngine{decision: PolicyDecisionChallenge}
	hook := NewPolicyHook(engine)
	resp, _ := hook.Check(context.Background(), PolicyRequest{URL: "https://suspicious.com"})
	if resp.Decision != PolicyDecisionChallenge {
		t.Errorf("expected challenge, got %s", resp.Decision)
	}
	if hook.Stats().Challenged != 1 {
		t.Errorf("expected challenged=1, got %d", hook.Stats().Challenged)
	}
}

func TestPolicyHook_Check_Disabled(t *testing.T) {
	engine := &mockPolicyEngine{decision: PolicyDecisionDeny}
	hook := NewPolicyHook(engine)
	hook.SetEnabled(false)
	resp, err := hook.Check(context.Background(), PolicyRequest{URL: "https://evil.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Decision != PolicyDecisionAllow {
		t.Errorf("expected allow when disabled, got %s", resp.Decision)
	}
	if engine.calls != 0 {
		t.Errorf("expected 0 engine calls when disabled, got %d", engine.calls)
	}
}

func TestPolicyHook_Check_NoEngine(t *testing.T) {
	hook := NewPolicyHook(nil)
	resp, err := hook.Check(context.Background(), PolicyRequest{URL: "https://example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Decision != PolicyDecisionAllow {
		t.Errorf("expected allow with no engine, got %s", resp.Decision)
	}
}

func TestPolicyHook_Check_EngineError(t *testing.T) {
	engine := &mockPolicyEngine{err: errors.New("engine failure")}
	hook := NewPolicyHook(engine)
	resp, err := hook.Check(context.Background(), PolicyRequest{URL: "https://example.com"})
	if err == nil {
		t.Error("expected error from engine")
	}
	if resp.Decision != PolicyDecisionDeny {
		t.Errorf("expected deny on error, got %s", resp.Decision)
	}
}

func TestPolicyHook_Stats(t *testing.T) {
	engine := &mockPolicyEngine{decision: PolicyDecisionAllow}
	hook := NewPolicyHook(engine)
	hook.Check(context.Background(), PolicyRequest{URL: "https://a.com"})
	hook.Check(context.Background(), PolicyRequest{URL: "https://b.com"})
	if hook.Stats().Total != 2 {
		t.Errorf("expected total=2, got %d", hook.Stats().Total)
	}
}

func TestPolicyHook_ResetStats(t *testing.T) {
	hook := NewPolicyHook(&mockPolicyEngine{decision: PolicyDecisionAllow})
	hook.Check(context.Background(), PolicyRequest{URL: "https://a.com"})
	hook.ResetStats()
	if hook.Stats().Total != 0 {
		t.Error("expected total=0 after reset")
	}
}

func TestPolicyHook_Enabled(t *testing.T) {
	hook := NewPolicyHook(&mockPolicyEngine{decision: PolicyDecisionAllow})
	if !hook.Enabled() {
		t.Error("expected enabled by default")
	}
	hook.SetEnabled(false)
	if hook.Enabled() {
		t.Error("expected disabled after SetEnabled(false)")
	}
}
