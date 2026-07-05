package solver

import (
	"context"
	"encoding/base64"
	"fmt"
	"sync"
	"time"
)

// vision.go (spec L4025: solver/vision.go - LLM vision solve:
// screenshot -> Qwen3.6 -> instruction -> execute).
//
// Challenge/CAPTCHA handling: vision-based solving using LLM vision
// models. The solver takes a screenshot of the challenge, sends it
// to an LLM vision model (Qwen3.6 or similar), receives an instruction,
// and executes it to solve the challenge.

// VisionResult is the result of an LLM vision solve attempt
// (spec L4025: LLM vision solve).
type VisionResult struct {
	Solved      bool          `json:"solved"`
	Answer      string        `json:"answer,omitempty"`
	Instruction string        `json:"instruction,omitempty"`
	Model       string        `json:"model,omitempty"`
	Duration    time.Duration `json:"duration"`
	Error       string        `json:"error,omitempty"`
}

// VisionSolver implements LLM vision-based challenge solving
// (spec L4025: LLM vision solve screenshot -> Qwen3.6 -> instruction
// -> execute).
type VisionSolver struct {
	mu    sync.RWMutex
	hub   InferenceHub
	model string
	stats VisionStats
}

// VisionStats tracks vision solver statistics
// (spec L4025: challenge success tracking).
type VisionStats struct {
	TotalAttempts int           `json:"total_attempts"`
	Successes     int           `json:"successes"`
	Failures      int           `json:"failures"`
	TotalDuration time.Duration `json:"total_duration"`
}

// DefaultVisionModel is the default LLM vision model
// (spec L4025: Qwen3.6).
const DefaultVisionModel = "qwen3.6-vision"

// NewVisionSolver creates a new VisionSolver with the given Inference Hub
// (spec L4025: LLM vision solve).
func NewVisionSolver(hub InferenceHub) *VisionSolver {
	return &VisionSolver{
		hub:   hub,
		model: DefaultVisionModel,
	}
}

// SetModel sets the LLM vision model to use
// (spec L4025: Qwen3.6 -> instruction -> execute).
func (v *VisionSolver) SetModel(model string) {
	if v == nil {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.model = model
}

// Model returns the current vision model
// (spec L4025).
func (v *VisionSolver) Model() string {
	if v == nil {
		return DefaultVisionModel
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.model
}

// Solve attempts to solve a challenge using LLM vision
// (spec L4025: screenshot -> Qwen3.6 -> instruction -> execute).
// The screenshot is sent to the LLM vision model, which returns an
// instruction that is executed to solve the challenge.
func (v *VisionSolver) Solve(ctx context.Context, challenge ChallengeInfo, screenshot []byte) (VisionResult, error) {
	if v == nil {
		return VisionResult{}, fmt.Errorf("nil vision solver")
	}
	if v.hub == nil {
		return VisionResult{Error: "no inference hub"}, fmt.Errorf("no inference hub configured")
	}
	if len(screenshot) == 0 {
		return VisionResult{Error: "empty screenshot"}, fmt.Errorf("empty screenshot")
	}

	start := time.Now()

	v.mu.Lock()
	v.stats.TotalAttempts++
	v.mu.Unlock()

	// Send screenshot to LLM vision model via Inference Hub
	// (spec L4025: screenshot -> Qwen3.6 -> instruction -> execute).
	req := InferenceHubRequest{
		ChallengeType: string(challenge.Type),
		ImageData:     encodeScreenshot(screenshot),
		Prompt:        buildVisionPrompt(challenge),
		LocalOnly:     false,
	}

	resp, err := v.hub.SolveCAPTCHA(ctx, req)
	if err != nil {
		v.mu.Lock()
		v.stats.Failures++
		v.mu.Unlock()
		return VisionResult{
			Duration: time.Since(start),
			Error:    err.Error(),
		}, err
	}

	result := VisionResult{
		Solved:      resp.Solved,
		Answer:      resp.Answer,
		Instruction: resp.Answer, // The answer IS the instruction to execute
		Model:       resp.Model,
		Duration:    time.Since(start),
	}
	if !resp.Solved && resp.Error != "" {
		result.Error = resp.Error
	}

	v.mu.Lock()
	if result.Solved {
		v.stats.Successes++
	} else {
		v.stats.Failures++
	}
	v.stats.TotalDuration += result.Duration
	v.mu.Unlock()

	return result, nil
}

// encodeScreenshot encodes screenshot bytes to a standard base64 string
// for the Inference Hub request (spec L4025: screenshot -> LLM).
func encodeScreenshot(screenshot []byte) string {
	if len(screenshot) == 0 {
		return ""
	}
	return base64.StdEncoding.EncodeToString(screenshot)
}

// buildVisionPrompt builds the prompt for the LLM vision model
// (spec L4025: screenshot -> Qwen3.6 -> instruction).
func buildVisionPrompt(challenge ChallengeInfo) string {
	return fmt.Sprintf("Solve this %s challenge. Provide the action needed to solve it.", challenge.Type)
}

// Stats returns the current vision solver statistics
// (spec L4025: challenge success tracking).
func (v *VisionSolver) Stats() VisionStats {
	if v == nil {
		return VisionStats{}
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.stats
}

// IsAvailable reports whether the vision solver is available
// (spec L4025: LLM vision solve).
func (v *VisionSolver) IsAvailable() bool {
	if v == nil {
		return false
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.hub != nil
}

// String returns a diagnostic summary.
func (r VisionResult) String() string {
	return fmt.Sprintf("VisionResult{solved:%v model:%s duration:%v error:%s}",
		r.Solved, r.Model, r.Duration, r.Error)
}
