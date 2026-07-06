package solver

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"
)

// pipeline.go (spec L4025: solver/pipeline.go - 2-stage pipeline:
// vision solve -> user fallback).
//
// Challenge/CAPTCHA handling: 2-stage pipeline that first attempts
// vision-based solving, then falls back to user intervention if the
// vision solver fails or is unavailable.

// PipelineStage enumerates the pipeline stages
// (spec L4025: 2-stage pipeline vision solve -> user fallback).
type PipelineStage string

const (
	// PipelineStageVision is the first stage: LLM vision solve.
	PipelineStageVision PipelineStage = "vision"
	// PipelineStageUserFallback is the second stage: user intervention.
	PipelineStageUserFallback PipelineStage = "user_fallback"
	// PipelineStageNone indicates no stage was executed.
	PipelineStageNone PipelineStage = "none"
)

// PipelineResult is the result of the 2-stage pipeline
// (spec L4025: 2-stage pipeline).
type PipelineResult struct {
	Stage        PipelineStage `json:"stage"`
	Solved       bool          `json:"solved"`
	Answer       string        `json:"answer,omitempty"`
	Error        string        `json:"error,omitempty"`
	Duration     time.Duration `json:"duration"`
	Attempts     int           `json:"attempts"`
	FallbackUsed bool          `json:"fallback_used"`
}

// UserEscalationHook is invoked when vision solve fails and the
// pipeline escalates to user fallback (spec L4025: user fallback).
// The hook receives the challenge info, the screenshot bytes, and the
// number of failed vision attempts; it returns either a user-provided
// answer (Solved=true) or an error/timeout (Solved=false). A nil hook
// means no user escalation channel is configured, in which case the
// pipeline returns an explicit "no user escalation channel configured"
// error rather than silently faking a result.
type UserEscalationHook interface {
	Escalate(ctx context.Context, challenge ChallengeInfo, screenshot []byte, visionAttempts int) (UserEscalationResult, error)
}

// UserEscalationResult is the outcome of a user-escalation request.
type UserEscalationResult struct {
	Solved bool
	Answer string
	Reason string // empty on success; on failure explains why user could not solve
}

// UserEscalationFunc is a function adapter for UserEscalationHook.
type UserEscalationFunc func(ctx context.Context, challenge ChallengeInfo, screenshot []byte, visionAttempts int) (UserEscalationResult, error)

func (f UserEscalationFunc) Escalate(ctx context.Context, challenge ChallengeInfo, screenshot []byte, visionAttempts int) (UserEscalationResult, error) {
	return f(ctx, challenge, screenshot, visionAttempts)
}

// SolverPipeline implements the 2-stage pipeline
// (spec L4025: 2-stage pipeline vision solve -> user fallback).
type SolverPipeline struct {
	mu           sync.RWMutex
	visionSolver *VisionSolver
	userHook     UserEscalationHook
	metrics      *MetricsStore
	maxAttempts  int
	stats        PipelineStats
}

// PipelineStats tracks pipeline execution statistics
// (spec L4025: challenge success tracking).
type PipelineStats struct {
	VisionAttempts    int `json:"vision_attempts"`
	VisionSuccesses   int `json:"vision_successes"`
	FallbackAttempts  int `json:"fallback_attempts"`
	FallbackSuccesses int `json:"fallback_successes"`
	TotalChallenges   int `json:"total_challenges"`
}

// NewSolverPipeline creates a new 2-stage pipeline
// (spec L4025: 2-stage pipeline vision solve -> user fallback).
// The user escalation hook defaults to nil; install one via
// SetUserEscalationHook to enable real user fallback.
func NewSolverPipeline(visionSolver *VisionSolver) *SolverPipeline {
	return &SolverPipeline{
		visionSolver: visionSolver,
		maxAttempts:  3,
	}
}

// SetUserEscalationHook installs the user-escalation hook invoked when
// vision solve fails (spec L4025: user fallback). Pass nil to disable
// user escalation (the pipeline will return an explicit error instead
// of silently faking a result).
func (p *SolverPipeline) SetUserEscalationHook(hook UserEscalationHook) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.userHook = hook
}

// SetMetricsStore installs the SQLite metrics store for challenge
// success tracking (spec L4025: challenge success tracking in SQLite).
// Pass nil to disable persistence. When set, every Solve call records
// a row with the domain, challenge type, solved stage, and duration.
func (p *SolverPipeline) SetMetricsStore(store *MetricsStore) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.metrics = store
}

// SetMaxAttempts sets the maximum number of vision solve attempts
// before falling back to user intervention (spec L4025).
func (p *SolverPipeline) SetMaxAttempts(n int) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.maxAttempts = n
}

// Solve executes the 2-stage pipeline for a challenge
// (spec L4025: 2-stage pipeline vision solve -> user fallback).
// Stage 1: attempt vision-based solving (up to maxAttempts times).
// Stage 2: if vision fails, fall back to user intervention.
func (p *SolverPipeline) Solve(ctx context.Context, challenge ChallengeInfo, screenshot []byte) PipelineResult {
	if p == nil {
		return PipelineResult{Stage: PipelineStageNone, Error: "nil pipeline"}
	}
	p.mu.Lock()
	p.stats.TotalChallenges++
	store := p.metrics
	p.mu.Unlock()

	start := time.Now()

	// Stage 1: Vision solve (spec L4025: vision solve).
	if p.visionSolver != nil {
		for attempt := 1; attempt <= p.maxAttempts; attempt++ {
			p.mu.Lock()
			p.stats.VisionAttempts++
			p.mu.Unlock()

			result, err := p.visionSolver.Solve(ctx, challenge, screenshot)
			if err == nil && result.Solved {
				p.mu.Lock()
				p.stats.VisionSuccesses++
				p.mu.Unlock()
				p.recordMetric(store, challenge, 0, result.Duration)
				return PipelineResult{
					Stage:    PipelineStageVision,
					Solved:   true,
					Answer:   result.Answer,
					Duration: time.Since(start),
					Attempts: attempt,
				}
			}
		}
	}

	// Stage 2: User fallback (spec L4025: user fallback).
	p.mu.Lock()
	p.stats.FallbackAttempts++
	hook := p.userHook
	p.mu.Unlock()

	if hook == nil {
		p.recordMetric(store, challenge, -1, time.Since(start))
		return PipelineResult{
			Stage:        PipelineStageUserFallback,
			Solved:       false,
			Error:        "vision solve failed and no user escalation channel configured",
			Duration:     time.Since(start),
			Attempts:     p.maxAttempts,
			FallbackUsed: true,
		}
	}

	escResult, err := hook.Escalate(ctx, challenge, screenshot, p.maxAttempts)
	if err != nil {
		p.recordMetric(store, challenge, -1, time.Since(start))
		return PipelineResult{
			Stage:        PipelineStageUserFallback,
			Solved:       false,
			Error:        fmt.Sprintf("user escalation failed: %s", err),
			Duration:     time.Since(start),
			Attempts:     p.maxAttempts,
			FallbackUsed: true,
		}
	}
	if escResult.Solved {
		p.mu.Lock()
		p.stats.FallbackSuccesses++
		p.mu.Unlock()
		p.recordMetric(store, challenge, 1, time.Since(start))
		return PipelineResult{
			Stage:        PipelineStageUserFallback,
			Solved:       true,
			Answer:       escResult.Answer,
			Duration:     time.Since(start),
			Attempts:     p.maxAttempts,
			FallbackUsed: true,
		}
	}
	reason := escResult.Reason
	if reason == "" {
		reason = "user did not solve the challenge"
	}
	p.recordMetric(store, challenge, -1, time.Since(start))
	return PipelineResult{
		Stage:        PipelineStageUserFallback,
		Solved:       false,
		Error:        reason,
		Duration:     time.Since(start),
		Attempts:     p.maxAttempts,
		FallbackUsed: true,
	}
}

// recordMetric writes a challenge_metrics row if a store is configured.
// stageSolved: 0=vision, 1=user_fallback, -1=not solved (NULL).
func (p *SolverPipeline) recordMetric(store *MetricsStore, challenge ChallengeInfo, stageSolved int, duration time.Duration) {
	if store == nil {
		return
	}
	domain := challenge.Domain
	if domain == "" {
		domain = "unknown"
	}
	row := MetricRow{
		Domain:        domain,
		ChallengeType: string(challenge.Type),
		VisionTokens:  0,
		DurationMS:    int(duration.Milliseconds()),
		CreatedAt:     time.Now().UTC(),
	}
	if stageSolved >= 0 {
		row.StageSolved = sql.NullInt64{Int64: int64(stageSolved), Valid: true}
	}
	_ = store.Record(row)
}

// Stats returns the current pipeline statistics
// (spec L4025: challenge success tracking).
func (p *SolverPipeline) Stats() PipelineStats {
	if p == nil {
		return PipelineStats{}
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.stats
}

// String returns a diagnostic summary.
func (r PipelineResult) String() string {
	return fmt.Sprintf("PipelineResult{stage:%s solved:%v attempts:%d fallback:%v duration:%v}",
		r.Stage, r.Solved, r.Attempts, r.FallbackUsed, r.Duration)
}

// IsVisionSolved reports whether the challenge was solved by vision.
func (r PipelineResult) IsVisionSolved() bool {
	return r.Stage == PipelineStageVision && r.Solved
}

// IsFallbackUsed reports whether user fallback was used.
func (r PipelineResult) IsFallbackUsed() bool {
	return r.FallbackUsed
}
