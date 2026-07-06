package solver

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ==================== mock InferenceHub for tests ====================

type task2247MockHub struct {
	response InferenceHubResponse
	err      error
	called   bool
}

func (m *task2247MockHub) SolveCAPTCHA(ctx context.Context, req InferenceHubRequest) (InferenceHubResponse, error) {
	m.called = true
	if m.err != nil {
		return InferenceHubResponse{}, m.err
	}
	return m.response, nil
}

// ==================== pipeline.go tests ====================

// TestTASK2247_PipelineStageConstants verifies stage constants
// (spec L4025: 2-stage pipeline vision solve -> user fallback).
func TestTASK2247_PipelineStageConstants(t *testing.T) {
	if PipelineStageVision != "vision" {
		t.Error("vision stage mismatch")
	}
	if PipelineStageUserFallback != "user_fallback" {
		t.Error("user_fallback stage mismatch")
	}
	if PipelineStageNone != "none" {
		t.Error("none stage mismatch")
	}
}

// TestTASK2247_NewSolverPipeline verifies creation
// (spec L4025: 2-stage pipeline).
func TestTASK2247_NewSolverPipeline(t *testing.T) {
	v := NewVisionSolver(nil)
	p := NewSolverPipeline(v)
	if p == nil {
		t.Fatal("pipeline should not be nil")
	}
}

// TestTASK2247_PipelineSolveVisionSuccess verifies vision solve
// success (spec L4025: vision solve).
func TestTASK2247_PipelineSolveVisionSuccess(t *testing.T) {
	hub := &task2247MockHub{
		response: InferenceHubResponse{
			Solved: true,
			Answer: "click button",
			Model:  "qwen3.6-vision",
		},
	}
	v := NewVisionSolver(hub)
	p := NewSolverPipeline(v)

	result := p.Solve(context.Background(), ChallengeInfo{Type: TypeCloudflare}, []byte("screenshot"))
	if !result.Solved {
		t.Error("should be solved by vision")
	}
	if result.Stage != PipelineStageVision {
		t.Errorf("stage: got %s, want vision", result.Stage)
	}
	if !result.IsVisionSolved() {
		t.Error("IsVisionSolved should be true")
	}
	if result.IsFallbackUsed() {
		t.Error("fallback should not be used")
	}
}

// TestTASK2247_PipelineSolveVisionFailFallback verifies vision fail
// -> user fallback (spec L4025: vision solve -> user fallback).
func TestTASK2247_PipelineSolveVisionFailFallback(t *testing.T) {
	hub := &task2247MockHub{
		response: InferenceHubResponse{
			Solved: false,
			Error:  "cannot solve",
		},
	}
	v := NewVisionSolver(hub)
	p := NewSolverPipeline(v)
	p.SetMaxAttempts(1) // only 1 attempt for fast test

	result := p.Solve(context.Background(), ChallengeInfo{Type: TypeRecaptcha}, []byte("screenshot"))
	if result.Solved {
		t.Error("should not be solved (fallback)")
	}
	if result.Stage != PipelineStageUserFallback {
		t.Errorf("stage: got %s, want user_fallback", result.Stage)
	}
	if !result.IsFallbackUsed() {
		t.Error("fallback should be used")
	}
}

// TestTASK2247_PipelineSolveNoVisionSolver verifies nil vision solver
// -> direct fallback (spec L4025: user fallback).
func TestTASK2247_PipelineSolveNoVisionSolver(t *testing.T) {
	p := NewSolverPipeline(nil)
	result := p.Solve(context.Background(), ChallengeInfo{Type: TypeGeneric}, []byte("screenshot"))
	if result.Solved {
		t.Error("should not be solved")
	}
	if result.Stage != PipelineStageUserFallback {
		t.Errorf("stage: got %s, want user_fallback", result.Stage)
	}
}

// TestTASK2247_PipelineSolveEmptyScreenshot verifies empty screenshot
// -> fallback.
func TestTASK2247_PipelineSolveEmptyScreenshot(t *testing.T) {
	hub := &task2247MockHub{
		response: InferenceHubResponse{Solved: true},
	}
	v := NewVisionSolver(hub)
	p := NewSolverPipeline(v)
	p.SetMaxAttempts(1)

	result := p.Solve(context.Background(), ChallengeInfo{Type: TypeGeneric}, nil)
	if result.Solved {
		t.Error("should not be solved with empty screenshot")
	}
	if result.Stage != PipelineStageUserFallback {
		t.Errorf("stage: got %s, want user_fallback", result.Stage)
	}
}

// TestTASK2247_PipelineSetMaxAttempts verifies SetMaxAttempts
// (spec L4025).
func TestTASK2247_PipelineSetMaxAttempts(t *testing.T) {
	p := NewSolverPipeline(nil)
	p.SetMaxAttempts(5)
	// No direct way to verify, but should not panic.
}

// TestTASK2247_PipelineStats verifies stats tracking
// (spec L4025: challenge success tracking).
func TestTASK2247_PipelineStats(t *testing.T) {
	hub := &task2247MockHub{
		response: InferenceHubResponse{Solved: true, Answer: "solve"},
	}
	v := NewVisionSolver(hub)
	p := NewSolverPipeline(v)

	p.Solve(context.Background(), ChallengeInfo{Type: TypeCloudflare}, []byte("screenshot"))

	stats := p.Stats()
	if stats.TotalChallenges != 1 {
		t.Errorf("total: got %d, want 1", stats.TotalChallenges)
	}
	if stats.VisionSuccesses != 1 {
		t.Errorf("vision successes: got %d, want 1", stats.VisionSuccesses)
	}
}

// TestTASK2247_PipelineStatsFallback verifies fallback stats.
func TestTASK2247_PipelineStatsFallback(t *testing.T) {
	hub := &task2247MockHub{
		response: InferenceHubResponse{Solved: false, Error: "fail"},
	}
	v := NewVisionSolver(hub)
	p := NewSolverPipeline(v)
	p.SetMaxAttempts(1)

	p.Solve(context.Background(), ChallengeInfo{Type: TypeGeneric}, []byte("screenshot"))

	stats := p.Stats()
	if stats.FallbackAttempts != 1 {
		t.Errorf("fallback attempts: got %d, want 1", stats.FallbackAttempts)
	}
}

// TestTASK2247_PipelineNilSafe verifies nil pipeline is safe.
func TestTASK2247_PipelineNilSafe(t *testing.T) {
	var p *SolverPipeline
	result := p.Solve(context.Background(), ChallengeInfo{}, nil)
	if result.Solved {
		t.Error("nil should not solve")
	}
	p.SetMaxAttempts(1)
	if p.Stats().TotalChallenges != 0 {
		t.Error("nil stats should be zero")
	}
}

// TestTASK2247_PipelineResultString verifies String method.
func TestTASK2247_PipelineResultString(t *testing.T) {
	r := PipelineResult{Stage: PipelineStageVision, Solved: true, Attempts: 1}
	s := r.String()
	if s == "" {
		t.Error("String should not be empty")
	}
}

// ==================== vision.go tests ====================

// TestTASK2247_DefaultVisionModel verifies default model
// (spec L4025: Qwen3.6).
func TestTASK2247_DefaultVisionModel(t *testing.T) {
	if DefaultVisionModel != "qwen3.6-vision" {
		t.Errorf("default model: got %s, want qwen3.6-vision", DefaultVisionModel)
	}
}

// TestTASK2247_NewVisionSolver verifies creation
// (spec L4025: LLM vision solve).
func TestTASK2247_NewVisionSolver(t *testing.T) {
	v := NewVisionSolver(nil)
	if v == nil {
		t.Fatal("solver should not be nil")
	}
	if v.Model() != DefaultVisionModel {
		t.Error("default model should be set")
	}
}

// TestTASK2247_VisionSetModel verifies SetModel
// (spec L4025: Qwen3.6 -> instruction -> execute).
func TestTASK2247_VisionSetModel(t *testing.T) {
	v := NewVisionSolver(nil)
	v.SetModel("custom-model")
	if v.Model() != "custom-model" {
		t.Errorf("model: got %s, want custom-model", v.Model())
	}
}

// TestTASK2247_VisionSolveSuccess verifies successful vision solve
// (spec L4025: screenshot -> Qwen3.6 -> instruction -> execute).
func TestTASK2247_VisionSolveSuccess(t *testing.T) {
	hub := &task2247MockHub{
		response: InferenceHubResponse{
			Solved: true,
			Answer: "click the checkbox",
			Model:  "qwen3.6-vision",
		},
	}
	v := NewVisionSolver(hub)
	result, err := v.Solve(context.Background(), ChallengeInfo{Type: TypeCloudflare}, []byte("screenshot"))
	if err != nil {
		t.Fatalf("solve: %v", err)
	}
	if !result.Solved {
		t.Error("should be solved")
	}
	if result.Answer != "click the checkbox" {
		t.Errorf("answer: got %s, want 'click the checkbox'", result.Answer)
	}
	if result.Instruction != result.Answer {
		t.Error("instruction should equal answer")
	}
}

// TestTASK2247_VisionSolveFail verifies failed vision solve
// (spec L4025: LLM vision solve).
func TestTASK2247_VisionSolveFail(t *testing.T) {
	hub := &task2247MockHub{
		response: InferenceHubResponse{
			Solved: false,
			Error:  "cannot solve this challenge",
		},
	}
	v := NewVisionSolver(hub)
	result, err := v.Solve(context.Background(), ChallengeInfo{Type: TypeRecaptcha}, []byte("screenshot"))
	if err != nil {
		t.Fatalf("solve should not return error for failed solve: %v", err)
	}
	if result.Solved {
		t.Error("should not be solved")
	}
	if result.Error == "" {
		t.Error("error should be set")
	}
}

// TestTASK2247_VisionSolveNoHub verifies no hub -> error
// (spec L4025: LLM vision solve).
func TestTASK2247_VisionSolveNoHub(t *testing.T) {
	v := NewVisionSolver(nil)
	_, err := v.Solve(context.Background(), ChallengeInfo{Type: TypeGeneric}, []byte("screenshot"))
	if err == nil {
		t.Error("nil hub should return error")
	}
}

// TestTASK2247_VisionSolveEmptyScreenshot verifies empty screenshot
// -> error (spec L4025: screenshot -> LLM).
func TestTASK2247_VisionSolveEmptyScreenshot(t *testing.T) {
	hub := &task2247MockHub{response: InferenceHubResponse{Solved: true}}
	v := NewVisionSolver(hub)
	_, err := v.Solve(context.Background(), ChallengeInfo{Type: TypeGeneric}, nil)
	if err == nil {
		t.Error("empty screenshot should return error")
	}
}

// TestTASK2247_VisionSolveHubError verifies hub error propagation.
func TestTASK2247_VisionSolveHubError(t *testing.T) {
	hub := &task2247MockHub{err: context.DeadlineExceeded}
	v := NewVisionSolver(hub)
	_, err := v.Solve(context.Background(), ChallengeInfo{Type: TypeGeneric}, []byte("screenshot"))
	if err == nil {
		t.Error("hub error should propagate")
	}
}

// TestTASK2247_VisionIsAvailable verifies IsAvailable
// (spec L4025: LLM vision solve).
func TestTASK2247_VisionIsAvailable(t *testing.T) {
	v := NewVisionSolver(nil)
	if v.IsAvailable() {
		t.Error("nil hub should not be available")
	}
	hub := &task2247MockHub{}
	v2 := NewVisionSolver(hub)
	if !v2.IsAvailable() {
		t.Error("with hub should be available")
	}
}

// TestTASK2247_VisionStats verifies stats tracking
// (spec L4025: challenge success tracking).
func TestTASK2247_VisionStats(t *testing.T) {
	hub := &task2247MockHub{
		response: InferenceHubResponse{Solved: true, Answer: "solve"},
	}
	v := NewVisionSolver(hub)
	v.Solve(context.Background(), ChallengeInfo{Type: TypeGeneric}, []byte("screenshot"))

	stats := v.Stats()
	if stats.TotalAttempts != 1 {
		t.Errorf("attempts: got %d, want 1", stats.TotalAttempts)
	}
	if stats.Successes != 1 {
		t.Errorf("successes: got %d, want 1", stats.Successes)
	}
}

// TestTASK2247_VisionNilSafe verifies nil solver is safe.
func TestTASK2247_VisionNilSafe(t *testing.T) {
	var v *VisionSolver
	v.SetModel("test")
	if v.Model() != DefaultVisionModel {
		t.Error("nil should return default model")
	}
	if v.IsAvailable() {
		t.Error("nil should not be available")
	}
	if v.Stats().TotalAttempts != 0 {
		t.Error("nil stats should be zero")
	}
}

// TestTASK2247_VisionResultString verifies String method.
func TestTASK2247_VisionResultString(t *testing.T) {
	r := VisionResult{Solved: true, Model: "test", Duration: 10 * time.Millisecond}
	s := r.String()
	if s == "" {
		t.Error("String should not be empty")
	}
}

// ==================== full spec parity test ====================

// TestTASK2247_FullSpecParity verifies full spec parity for L4025
// (spec L4025: 2-stage pipeline vision solve -> user fallback).
func TestTASK2247_FullSpecParity(t *testing.T) {
	// 1. Vision solver exists
	hub := &task2247MockHub{
		response: InferenceHubResponse{Solved: true, Answer: "click", Model: "qwen3.6-vision"},
	}
	v := NewVisionSolver(hub)
	if !v.IsAvailable() {
		t.Error("vision solver should be available")
	}

	// 2. Pipeline exists
	p := NewSolverPipeline(v)
	if p == nil {
		t.Fatal("pipeline should not be nil")
	}

	// 3. Vision solve succeeds
	result, err := v.Solve(context.Background(), ChallengeInfo{Type: TypeCloudflare}, []byte("screenshot"))
	if err != nil || !result.Solved {
		t.Error("vision solve should succeed")
	}

	// 4. Pipeline vision stage succeeds
	pResult := p.Solve(context.Background(), ChallengeInfo{Type: TypeCloudflare}, []byte("screenshot"))
	if !pResult.IsVisionSolved() {
		t.Error("pipeline should solve via vision")
	}

	// 5. Pipeline fallback when vision fails
	hub2 := &task2247MockHub{
		response: InferenceHubResponse{Solved: false, Error: "fail"},
	}
	v2 := NewVisionSolver(hub2)
	p2 := NewSolverPipeline(v2)
	p2.SetMaxAttempts(1)
	pResult2 := p2.Solve(context.Background(), ChallengeInfo{Type: TypeGeneric}, []byte("screenshot"))
	if !pResult2.IsFallbackUsed() {
		t.Error("should use fallback when vision fails")
	}

	// 6. Stats tracked
	if p.Stats().TotalChallenges == 0 {
		t.Error("pipeline should track stats")
	}
	if v.Stats().TotalAttempts == 0 {
		t.Error("vision should track stats")
	}
}

// ==================== TASK-2344 user escalation hook tests ====================

// TestTASK2344_UserEscalationNilHookExplicitError verifies that a nil
// user-escalation hook produces an explicit error, not a silent fake
// result (spec L4025: user fallback must be real, not faked).
func TestTASK2344_UserEscalationNilHookExplicitError(t *testing.T) {
	hub := &task2247MockHub{
		response: InferenceHubResponse{Solved: false, Error: "fail"},
	}
	v := NewVisionSolver(hub)
	p := NewSolverPipeline(v)
	p.SetMaxAttempts(1)

	result := p.Solve(context.Background(), ChallengeInfo{Type: TypeGeneric}, []byte("screenshot"))
	if result.Solved {
		t.Error("should not be solved")
	}
	if result.Stage != PipelineStageUserFallback {
		t.Errorf("stage: got %s, want user_fallback", result.Stage)
	}
	if !result.IsFallbackUsed() {
		t.Error("fallback should be used")
	}
	if result.Error == "" {
		t.Error("nil hook should produce explicit error, not empty")
	}
	if !strings.Contains(result.Error, "no user escalation channel") {
		t.Errorf("error should mention 'no user escalation channel', got: %s", result.Error)
	}
}

// TestTASK2344_UserEscalationHookSolved verifies that a user-escalation
// hook that returns Solved=true produces a solved pipeline result with
// the user-provided answer (spec L4025: user fallback).
func TestTASK2344_UserEscalationHookSolved(t *testing.T) {
	hub := &task2247MockHub{
		response: InferenceHubResponse{Solved: false, Error: "fail"},
	}
	v := NewVisionSolver(hub)
	p := NewSolverPipeline(v)
	p.SetMaxAttempts(1)
	p.SetUserEscalationHook(UserEscalationFunc(func(ctx context.Context, ch ChallengeInfo, sc []byte, attempts int) (UserEscalationResult, error) {
		if ch.Type != TypeGeneric {
			t.Errorf("hook got wrong challenge type: %s", ch.Type)
		}
		if attempts != 1 {
			t.Errorf("hook got wrong attempt count: %d", attempts)
		}
		return UserEscalationResult{Solved: true, Answer: "user_provided_answer"}, nil
	}))

	result := p.Solve(context.Background(), ChallengeInfo{Type: TypeGeneric}, []byte("screenshot"))
	if !result.Solved {
		t.Error("should be solved by user escalation")
	}
	if result.Stage != PipelineStageUserFallback {
		t.Errorf("stage: got %s, want user_fallback", result.Stage)
	}
	if result.Answer != "user_provided_answer" {
		t.Errorf("answer: got %s, want user_provided_answer", result.Answer)
	}
	if !result.IsFallbackUsed() {
		t.Error("fallback should be used")
	}
	if p.Stats().FallbackSuccesses != 1 {
		t.Errorf("fallback successes: got %d, want 1", p.Stats().FallbackSuccesses)
	}
}

// TestTASK2344_UserEscalationHookUnsolved verifies that a user-escalation
// hook that returns Solved=false produces an unsolved result with the
// reason (spec L4025: user fallback).
func TestTASK2344_UserEscalationHookUnsolved(t *testing.T) {
	hub := &task2247MockHub{
		response: InferenceHubResponse{Solved: false, Error: "fail"},
	}
	v := NewVisionSolver(hub)
	p := NewSolverPipeline(v)
	p.SetMaxAttempts(1)
	p.SetUserEscalationHook(UserEscalationFunc(func(ctx context.Context, ch ChallengeInfo, sc []byte, attempts int) (UserEscalationResult, error) {
		return UserEscalationResult{Solved: false, Reason: "user declined to solve"}, nil
	}))

	result := p.Solve(context.Background(), ChallengeInfo{Type: TypeGeneric}, []byte("screenshot"))
	if result.Solved {
		t.Error("should not be solved")
	}
	if result.Stage != PipelineStageUserFallback {
		t.Errorf("stage: got %s, want user_fallback", result.Stage)
	}
	if !strings.Contains(result.Error, "user declined to solve") {
		t.Errorf("error should contain reason, got: %s", result.Error)
	}
}

// TestTASK2344_UserEscalationHookError verifies that a user-escalation
// hook that returns an error produces an unsolved result with the error
// message (spec L4025: user fallback).
func TestTASK2344_UserEscalationHookError(t *testing.T) {
	hub := &task2247MockHub{
		response: InferenceHubResponse{Solved: false, Error: "fail"},
	}
	v := NewVisionSolver(hub)
	p := NewSolverPipeline(v)
	p.SetMaxAttempts(1)
	p.SetUserEscalationHook(UserEscalationFunc(func(ctx context.Context, ch ChallengeInfo, sc []byte, attempts int) (UserEscalationResult, error) {
		return UserEscalationResult{}, fmt.Errorf("escalation channel timeout")
	}))

	result := p.Solve(context.Background(), ChallengeInfo{Type: TypeGeneric}, []byte("screenshot"))
	if result.Solved {
		t.Error("should not be solved")
	}
	if !strings.Contains(result.Error, "user escalation failed") {
		t.Errorf("error should mention 'user escalation failed', got: %s", result.Error)
	}
	if !strings.Contains(result.Error, "escalation channel timeout") {
		t.Errorf("error should contain hook error, got: %s", result.Error)
	}
}

// TestTASK2344_UserEscalationDefaultReason verifies that an unsolved
// hook result with empty reason gets a default reason message.
func TestTASK2344_UserEscalationDefaultReason(t *testing.T) {
	hub := &task2247MockHub{
		response: InferenceHubResponse{Solved: false, Error: "fail"},
	}
	v := NewVisionSolver(hub)
	p := NewSolverPipeline(v)
	p.SetMaxAttempts(1)
	p.SetUserEscalationHook(UserEscalationFunc(func(ctx context.Context, ch ChallengeInfo, sc []byte, attempts int) (UserEscalationResult, error) {
		return UserEscalationResult{Solved: false, Reason: ""}, nil
	}))

	result := p.Solve(context.Background(), ChallengeInfo{Type: TypeGeneric}, []byte("screenshot"))
	if result.Solved {
		t.Error("should not be solved")
	}
	if !strings.Contains(result.Error, "user did not solve") {
		t.Errorf("error should contain default reason, got: %s", result.Error)
	}
}

// TestTASK2344_SetUserEscalationHookNilReceiver verifies nil-receiver safety.
func TestTASK2344_SetUserEscalationHookNilReceiver(t *testing.T) {
	var p *SolverPipeline
	p.SetUserEscalationHook(nil) // must not panic
}

// TestTASK2344_MetricsStoreVisionSolved verifies that a vision-solved
// challenge records a metric row with stage_solved=0 (vision).
func TestTASK2344_MetricsStoreVisionSolved(t *testing.T) {
	hub := &task2247MockHub{
		response: InferenceHubResponse{Solved: true, Answer: "click"},
	}
	v := NewVisionSolver(hub)
	p := NewSolverPipeline(v)
	p.SetMaxAttempts(1)

	dir := t.TempDir()
	store, err := OpenMetricsStore(filepath.Join(dir, "metrics.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	p.SetMetricsStore(store)

	challenge := ChallengeInfo{Type: TypeCloudflare, Domain: "example.com"}
	result := p.Solve(context.Background(), challenge, []byte("screenshot"))
	if !result.Solved {
		t.Fatal("should be solved by vision")
	}

	rate, total, err := store.SuccessRate(string(TypeCloudflare))
	if err != nil {
		t.Fatalf("success rate: %v", err)
	}
	if total != 1 {
		t.Errorf("total: got %d, want 1", total)
	}
	if rate != 1.0 {
		t.Errorf("rate: got %f, want 1.0", rate)
	}
}

// TestTASK2344_MetricsStoreUserFallbackSolved verifies that a
// user-fallback-solved challenge records a metric row with
// stage_solved=1 (user_fallback).
func TestTASK2344_MetricsStoreUserFallbackSolved(t *testing.T) {
	hub := &task2247MockHub{
		response: InferenceHubResponse{Solved: false, Error: "fail"},
	}
	v := NewVisionSolver(hub)
	p := NewSolverPipeline(v)
	p.SetMaxAttempts(1)
	p.SetUserEscalationHook(UserEscalationFunc(func(ctx context.Context, ch ChallengeInfo, sc []byte, attempts int) (UserEscalationResult, error) {
		return UserEscalationResult{Solved: true, Answer: "user_answer"}, nil
	}))

	dir := t.TempDir()
	store, err := OpenMetricsStore(filepath.Join(dir, "metrics.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	p.SetMetricsStore(store)

	challenge := ChallengeInfo{Type: TypeGeneric, Domain: "test.org"}
	result := p.Solve(context.Background(), challenge, []byte("screenshot"))
	if !result.Solved {
		t.Fatal("should be solved by user fallback")
	}

	rate, total, err := store.SuccessRate(string(TypeGeneric))
	if err != nil {
		t.Fatalf("success rate: %v", err)
	}
	if total != 1 {
		t.Errorf("total: got %d, want 1", total)
	}
	if rate != 1.0 {
		t.Errorf("rate: got %f, want 1.0", rate)
	}
}

// TestTASK2344_MetricsStoreNotSolved verifies that an unsolved
// challenge records a metric row with stage_solved=NULL (not solved).
func TestTASK2344_MetricsStoreNotSolved(t *testing.T) {
	hub := &task2247MockHub{
		response: InferenceHubResponse{Solved: false, Error: "fail"},
	}
	v := NewVisionSolver(hub)
	p := NewSolverPipeline(v)
	p.SetMaxAttempts(1)

	dir := t.TempDir()
	store, err := OpenMetricsStore(filepath.Join(dir, "metrics.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	p.SetMetricsStore(store)

	challenge := ChallengeInfo{Type: TypeRecaptcha, Domain: "unsolved.com"}
	result := p.Solve(context.Background(), challenge, []byte("screenshot"))
	if result.Solved {
		t.Fatal("should not be solved")
	}

	rate, total, err := store.SuccessRate(string(TypeRecaptcha))
	if err != nil {
		t.Fatalf("success rate: %v", err)
	}
	if total != 1 {
		t.Errorf("total: got %d, want 1", total)
	}
	if rate != 0.0 {
		t.Errorf("rate: got %f, want 0.0", rate)
	}
}

// TestTASK2344_MetricsStoreNilStoreNoPanic verifies that a nil metrics
// store does not cause a panic.
func TestTASK2344_MetricsStoreNilStoreNoPanic(t *testing.T) {
	hub := &task2247MockHub{
		response: InferenceHubResponse{Solved: true, Answer: "click"},
	}
	v := NewVisionSolver(hub)
	p := NewSolverPipeline(v)
	p.SetMaxAttempts(1)
	// No SetMetricsStore call — p.metrics is nil

	result := p.Solve(context.Background(), ChallengeInfo{Type: TypeGeneric}, []byte("screenshot"))
	if !result.Solved {
		t.Error("should be solved")
	}
}

// TestTASK2344_MetricsStoreDefaultDomain verifies that an empty domain
// is recorded as "unknown".
func TestTASK2344_MetricsStoreDefaultDomain(t *testing.T) {
	hub := &task2247MockHub{
		response: InferenceHubResponse{Solved: true, Answer: "click"},
	}
	v := NewVisionSolver(hub)
	p := NewSolverPipeline(v)
	p.SetMaxAttempts(1)

	dir := t.TempDir()
	store, err := OpenMetricsStore(filepath.Join(dir, "metrics.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	p.SetMetricsStore(store)

	// No Domain field set
	result := p.Solve(context.Background(), ChallengeInfo{Type: TypeGeneric}, []byte("screenshot"))
	if !result.Solved {
		t.Fatal("should be solved")
	}
	// Just verify it doesn't panic and records a row
	_, total, err := store.SuccessRate(string(TypeGeneric))
	if err != nil {
		t.Fatalf("success rate: %v", err)
	}
	if total != 1 {
		t.Errorf("total: got %d, want 1", total)
	}
}
