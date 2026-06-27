package solver

import (
	"context"
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
	Stage       PipelineStage `json:"stage"`
	Solved      bool          `json:"solved"`
	Answer      string        `json:"answer,omitempty"`
	Error       string        `json:"error,omitempty"`
	Duration    time.Duration `json:"duration"`
	Attempts    int           `json:"attempts"`
	FallbackUsed bool         `json:"fallback_used"`
}

// SolverPipeline implements the 2-stage pipeline
// (spec L4025: 2-stage pipeline vision solve -> user fallback).
type SolverPipeline struct {
	mu           sync.RWMutex
	visionSolver *VisionSolver
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
func NewSolverPipeline(visionSolver *VisionSolver) *SolverPipeline {
	return &SolverPipeline{
		visionSolver: visionSolver,
		maxAttempts:  3,
	}
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
	p.mu.Unlock()

	// In a real implementation, this would trigger user intervention
	// (e.g., display the challenge to the operator). Here we return
	// a fallback result.
	return PipelineResult{
		Stage:         PipelineStageUserFallback,
		Solved:        false,
		Error:         "vision solve failed, user fallback required",
		Duration:      time.Since(start),
		Attempts:      p.maxAttempts,
		FallbackUsed:  true,
	}
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
