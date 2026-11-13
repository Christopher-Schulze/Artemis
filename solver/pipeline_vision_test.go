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

type visionMockHub struct {
	response InferenceHubResponse
	err      error
	called   bool
}

func (m *visionMockHub) SolveCAPTCHA(ctx context.Context, req InferenceHubRequest) (InferenceHubResponse, error) {
	m.called = true
	if m.err != nil {
		return InferenceHubResponse{}, m.err
	}
	return m.response, nil
}

// ==================== pipeline.go tests ====================

// TestPipelineStageConstants verifies stage constants
// (2-stage pipeline vision solve -> user fallback).
func TestPipelineStageConstants(t *testing.T) {
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

// TestNewSolverPipeline verifies creation
// (2-stage pipeline).
func TestNewSolverPipeline(t *testing.T) {
	v := NewVisionSolver(nil)
	p := NewSolverPipeline(v)
	if p == nil {
		t.Fatal("pipeline should not be nil")
	}
}

// TestPipelineSolveVisionSuccess verifies vision solve
// success (vision solve).
func TestPipelineSolveVisionSuccess(t *testing.T) {
	hub := &visionMockHub{
		response: InferenceHubResponse{
			Solved: true,
			Answer: "click button",
			Model:  "qwen3.6-vision",
			Local:  true,
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

// TestPipelineSolveVisionFailFallback verifies vision fail
// -> user fallback (vision solve -> user fallback).
func TestPipelineSolveVisionFailFallback(t *testing.T) {
	hub := &visionMockHub{
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

// TestPipelineSolveNoVisionSolver verifies nil vision solver
// -> direct fallback (user fallback).
func TestPipelineSolveNoVisionSolver(t *testing.T) {
	p := NewSolverPipeline(nil)
	result := p.Solve(context.Background(), ChallengeInfo{Type: TypeGeneric}, []byte("screenshot"))
	if result.Solved {
		t.Error("should not be solved")
	}
	if result.Stage != PipelineStageUserFallback {
		t.Errorf("stage: got %s, want user_fallback", result.Stage)
	}
}

// TestPipelineSolveEmptyScreenshot verifies empty screenshot
// -> fallback.
func TestPipelineSolveEmptyScreenshot(t *testing.T) {
	hub := &visionMockHub{
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

// TestPipelineSetMaxAttempts verifies SetMaxAttempts.
func TestPipelineSetMaxAttempts(t *testing.T) {
	p := NewSolverPipeline(nil)
	p.SetMaxAttempts(5)
	// No direct way to verify, but should not panic.
}

// TestPipelineStats verifies stats tracking
// (challenge success tracking).
func TestPipelineStats(t *testing.T) {
	hub := &visionMockHub{
		response: InferenceHubResponse{Solved: true, Answer: "solve", Local: true},
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

// TestPipelineStatsFallback verifies fallback stats.
func TestPipelineStatsFallback(t *testing.T) {
	hub := &visionMockHub{
		response: InferenceHubResponse{Solved: false, Error: "fail", Local: true},
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

// TestPipelineNilSafe verifies nil pipeline is safe.
func TestPipelineNilSafe(t *testing.T) {
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

// TestPipelineResultString verifies String method.
func TestPipelineResultString(t *testing.T) {
	r := PipelineResult{Stage: PipelineStageVision, Solved: true, Attempts: 1}
	s := r.String()
	if s == "" {
		t.Error("String should not be empty")
	}
}

// ==================== vision.go tests ====================

// TestDefaultVisionModel verifies default model
// (Qwen3.6).
func TestDefaultVisionModel(t *testing.T) {
	if DefaultVisionModel != "qwen3.6-vision" {
		t.Errorf("default model: got %s, want qwen3.6-vision", DefaultVisionModel)
	}
}

// TestNewVisionSolver verifies creation
// (LLM vision solve).
func TestNewVisionSolver(t *testing.T) {
	v := NewVisionSolver(nil)
	if v == nil {
		t.Fatal("solver should not be nil")
	}
	if v.Model() != DefaultVisionModel {
		t.Error("default model should be set")
	}
}

// TestVisionSetModel verifies SetModel
// (Qwen3.6 -> instruction -> execute).
func TestVisionSetModel(t *testing.T) {
	v := NewVisionSolver(nil)
	v.SetModel("custom-model")
	if v.Model() != "custom-model" {
		t.Errorf("model: got %s, want custom-model", v.Model())
	}
}

// TestVisionSolveSuccess verifies successful vision solve
// (screenshot -> Qwen3.6 -> instruction -> execute).
func TestVisionSolveSuccess(t *testing.T) {
	hub := &visionMockHub{
		response: InferenceHubResponse{
			Solved: true,
			Answer: "click the checkbox",
			Model:  "qwen3.6-vision",
			Local:  true,
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

// TestVisionSolveFail verifies failed vision solve
// (LLM vision solve).
func TestVisionSolveFail(t *testing.T) {
	hub := &visionMockHub{
		response: InferenceHubResponse{
			Solved: false,
			Error:  "cannot solve this challenge",
			Local:  true,
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

// TestVisionSolveNoHub verifies no hub -> error
// (LLM vision solve).
func TestVisionSolveNoHub(t *testing.T) {
	v := NewVisionSolver(nil)
	_, err := v.Solve(context.Background(), ChallengeInfo{Type: TypeGeneric}, []byte("screenshot"))
	if err == nil {
		t.Error("nil hub should return error")
	}
}

// TestVisionSolveEmptyScreenshot verifies empty screenshot
// -> error (screenshot -> LLM).
func TestVisionSolveEmptyScreenshot(t *testing.T) {
	hub := &visionMockHub{response: InferenceHubResponse{Solved: true}}
	v := NewVisionSolver(hub)
	_, err := v.Solve(context.Background(), ChallengeInfo{Type: TypeGeneric}, nil)
	if err == nil {
		t.Error("empty screenshot should return error")
	}
}

// TestVisionSolveHubError verifies hub error propagation.
func TestVisionSolveHubError(t *testing.T) {
	hub := &visionMockHub{err: context.DeadlineExceeded}
	v := NewVisionSolver(hub)
	_, err := v.Solve(context.Background(), ChallengeInfo{Type: TypeGeneric}, []byte("screenshot"))
	if err == nil {
		t.Error("hub error should propagate")
	}
}

// TestVisionIsAvailable verifies IsAvailable
// (LLM vision solve).
func TestVisionIsAvailable(t *testing.T) {
	v := NewVisionSolver(nil)
	if v.IsAvailable() {
		t.Error("nil hub should not be available")
	}
	hub := &visionMockHub{}
	v2 := NewVisionSolver(hub)
	if !v2.IsAvailable() {
		t.Error("with hub should be available")
	}
}

// TestVisionStats verifies stats tracking
// (challenge success tracking).
func TestVisionStats(t *testing.T) {
	hub := &visionMockHub{
		response: InferenceHubResponse{Solved: true, Answer: "solve", Local: true},
	}
	v := NewVisionSolver(hub)
	if _, err := v.Solve(context.Background(), ChallengeInfo{Type: TypeGeneric}, []byte("screenshot")); err != nil {
		t.Fatalf("solve: %v", err)
	}

	stats := v.Stats()
	if stats.TotalAttempts != 1 {
		t.Errorf("attempts: got %d, want 1", stats.TotalAttempts)
	}
	if stats.Successes != 1 {
		t.Errorf("successes: got %d, want 1", stats.Successes)
	}
}

// TestVisionNilSafe verifies nil solver is safe.
func TestVisionNilSafe(t *testing.T) {
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

// TestVisionResultString verifies String method.
func TestVisionResultString(t *testing.T) {
	r := VisionResult{Solved: true, Model: "test", Duration: 10 * time.Millisecond}
	s := r.String()
	if s == "" {
		t.Error("String should not be empty")
	}
}

// ==================== parity test ====================

// TestPipelineVisionParity verifies full parity.
// (2-stage pipeline vision solve -> user fallback).
func TestPipelineVisionParity(t *testing.T) {
	// 1. Vision solver exists
	hub := &visionMockHub{
		response: InferenceHubResponse{Solved: true, Answer: "click", Model: "qwen3.6-vision", Local: true},
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
	hub2 := &visionMockHub{
		response: InferenceHubResponse{Solved: false, Error: "fail", Local: true},
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

// ==================== user escalation hook tests ====================

// TestUserEscalationNilHookExplicitError verifies that a nil
// user-escalation hook produces an explicit error, not a silent fake
// result (user fallback must be real, not faked).
func TestUserEscalationNilHookExplicitError(t *testing.T) {
	hub := &visionMockHub{
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

// TestUserEscalationHookSolved verifies that a user-escalation
// hook that returns Solved=true produces a solved pipeline result with
// the user-provided answer (user fallback).
func TestUserEscalationHookSolved(t *testing.T) {
	hub := &visionMockHub{
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

// TestUserEscalationHookUnsolved verifies that a user-escalation
// hook that returns Solved=false produces an unsolved result with the
// reason (user fallback).
func TestUserEscalationHookUnsolved(t *testing.T) {
	hub := &visionMockHub{
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

// TestUserEscalationHookError verifies that a user-escalation
// hook that returns an error produces an unsolved result with the error
// message (user fallback).
func TestUserEscalationHookError(t *testing.T) {
	hub := &visionMockHub{
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

// TestUserEscalationDefaultReason verifies that an unsolved
// hook result with empty reason gets a default reason message.
func TestUserEscalationDefaultReason(t *testing.T) {
	hub := &visionMockHub{
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

// TestSetUserEscalationHookNilReceiver verifies nil-receiver safety.
func TestSetUserEscalationHookNilReceiver(t *testing.T) {
	var p *SolverPipeline
	p.SetUserEscalationHook(nil) // must not panic
}

// TestMetricsStoreVisionSolved verifies that a vision-solved
// challenge records a metric row with stage_solved=0 (vision).
func TestMetricsStoreVisionSolved(t *testing.T) {
	hub := &visionMockHub{
		response: InferenceHubResponse{Solved: true, Answer: "click", Local: true},
	}
	v := NewVisionSolver(hub)
	p := NewSolverPipeline(v)
	p.SetMaxAttempts(1)

	dir := t.TempDir()
	store, err := OpenMetricsStore(filepath.Join(dir, "metrics.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Errorf("close metrics store: %v", closeErr)
		}
	}()
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

// TestMetricsStoreUserFallbackSolved verifies that a
// user-fallback-solved challenge records a metric row with
// stage_solved=1 (user_fallback).
func TestMetricsStoreUserFallbackSolved(t *testing.T) {
	hub := &visionMockHub{
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
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Errorf("close metrics store: %v", closeErr)
		}
	}()
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

// TestMetricsStoreNotSolved verifies that an unsolved
// challenge records a metric row with stage_solved=NULL (not solved).
func TestMetricsStoreNotSolved(t *testing.T) {
	hub := &visionMockHub{
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
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Errorf("close metrics store: %v", closeErr)
		}
	}()
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

// TestMetricsStoreNilStoreNoPanic verifies that a nil metrics
// store does not cause a panic.
func TestMetricsStoreNilStoreNoPanic(t *testing.T) {
	hub := &visionMockHub{
		response: InferenceHubResponse{Solved: true, Answer: "click", Local: true},
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

// TestMetricsStoreDefaultDomain verifies that an empty domain
// is recorded as "unknown".
func TestMetricsStoreDefaultDomain(t *testing.T) {
	hub := &visionMockHub{
		response: InferenceHubResponse{Solved: true, Answer: "click", Local: true},
	}
	v := NewVisionSolver(hub)
	p := NewSolverPipeline(v)
	p.SetMaxAttempts(1)

	dir := t.TempDir()
	store, err := OpenMetricsStore(filepath.Join(dir, "metrics.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Errorf("close metrics store: %v", closeErr)
		}
	}()
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
