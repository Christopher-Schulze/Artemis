package solver

import (
	"context"
	"errors"
	"testing"
)

// mockInferenceHub for testing.
type mockInferenceHub struct {
	resp  InferenceHubResponse
	err   error
	calls int
}

func (m *mockInferenceHub) SolveCAPTCHA(ctx context.Context, req InferenceHubRequest) (InferenceHubResponse, error) {
	m.calls++
	if m.err != nil {
		return InferenceHubResponse{}, m.err
	}
	return m.resp, nil
}

func TestInferenceHubHook_Solve_Success(t *testing.T) {
	hub := &mockInferenceHub{resp: InferenceHubResponse{Solved: true, Answer: "ABC123", Model: "local-qwen", Local: true}}
	hook := NewInferenceHubHook(hub)
	resp, err := hook.Solve(context.Background(), InferenceHubRequest{ChallengeType: "text"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Solved {
		t.Error("expected solved=true")
	}
	if resp.Answer != "ABC123" {
		t.Errorf("expected answer=ABC123, got %s", resp.Answer)
	}
	if hook.Stats().Solved != 1 {
		t.Errorf("expected solved=1, got %d", hook.Stats().Solved)
	}
	if hook.Stats().LocalUsed != 1 {
		t.Errorf("expected local_used=1, got %d", hook.Stats().LocalUsed)
	}
}

func TestInferenceHubHook_Solve_Failed(t *testing.T) {
	hub := &mockInferenceHub{resp: InferenceHubResponse{Solved: false, Error: "no solution"}}
	hook := NewInferenceHubHook(hub)
	resp, _ := hook.Solve(context.Background(), InferenceHubRequest{})
	if resp.Solved {
		t.Error("expected solved=false")
	}
	if hook.Stats().Failed != 1 {
		t.Errorf("expected failed=1, got %d", hook.Stats().Failed)
	}
}

func TestInferenceHubHook_Solve_NoHub(t *testing.T) {
	hook := NewInferenceHubHook(nil)
	_, err := hook.Solve(context.Background(), InferenceHubRequest{})
	if err == nil {
		t.Error("expected error with no hub")
	}
	if hook.Stats().Failed != 1 {
		t.Errorf("expected failed=1, got %d", hook.Stats().Failed)
	}
}

func TestInferenceHubHook_Solve_HubError(t *testing.T) {
	hub := &mockInferenceHub{err: errors.New("hub failure")}
	hook := NewInferenceHubHook(hub)
	_, err := hook.Solve(context.Background(), InferenceHubRequest{})
	if err == nil {
		t.Error("expected error from hub")
	}
}

func TestInferenceHubHook_RemoteUsed(t *testing.T) {
	hub := &mockInferenceHub{resp: InferenceHubResponse{Solved: true, Answer: "X", Local: false}}
	hook := NewInferenceHubHook(hub)
	hook.Solve(context.Background(), InferenceHubRequest{})
	if hook.Stats().RemoteUsed != 1 {
		t.Errorf("expected remote_used=1, got %d", hook.Stats().RemoteUsed)
	}
}

func TestInferenceHubHook_IsAvailable(t *testing.T) {
	hook := NewInferenceHubHook(nil)
	if hook.IsAvailable() {
		t.Error("expected not available with nil hub")
	}
	hook2 := NewInferenceHubHook(&mockInferenceHub{})
	if !hook2.IsAvailable() {
		t.Error("expected available with hub")
	}
}

func TestInferenceHubHook_ResetStats(t *testing.T) {
	hook := NewInferenceHubHook(&mockInferenceHub{resp: InferenceHubResponse{Solved: true, Answer: "X", Local: true}})
	hook.Solve(context.Background(), InferenceHubRequest{})
	hook.ResetStats()
	if hook.Stats().Total != 0 {
		t.Error("expected total=0 after reset")
	}
}

func TestInferenceHubHook_Stats(t *testing.T) {
	hub := &mockInferenceHub{resp: InferenceHubResponse{Solved: true, Answer: "X", Local: true}}
	hook := NewInferenceHubHook(hub)
	hook.Solve(context.Background(), InferenceHubRequest{})
	hook.Solve(context.Background(), InferenceHubRequest{})
	if hook.Stats().Total != 2 {
		t.Errorf("expected total=2, got %d", hook.Stats().Total)
	}
}

func TestFormatChallengePrompt(t *testing.T) {
	prompt := FormatChallengePrompt("text", "enter the code")
	if !contains(prompt, "text") {
		t.Error("expected challenge type in prompt")
	}
	if !contains(prompt, "enter the code") {
		t.Error("expected context in prompt")
	}
}

func TestValidateResponse_Solved(t *testing.T) {
	resp := InferenceHubResponse{Solved: true, Answer: "ABC"}
	if err := ValidateResponse(resp); err != nil {
		t.Errorf("expected valid, got %v", err)
	}
}

func TestValidateResponse_UnsolvedWithError(t *testing.T) {
	resp := InferenceHubResponse{Solved: false, Error: "failed"}
	if err := ValidateResponse(resp); err != nil {
		t.Errorf("expected valid, got %v", err)
	}
}

func TestValidateResponse_UnsolvedNoError(t *testing.T) {
	resp := InferenceHubResponse{Solved: false}
	if err := ValidateResponse(resp); err == nil {
		t.Error("expected error for unsolved without error message")
	}
}

func TestValidateResponse_SolvedEmptyAnswer(t *testing.T) {
	resp := InferenceHubResponse{Solved: true, Answer: "  "}
	if err := ValidateResponse(resp); err == nil {
		t.Error("expected error for solved with empty answer")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
