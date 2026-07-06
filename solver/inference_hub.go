package solver

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// inference_hub.go (spec L4248: Inference Hub ss7 CAPTCHA fallback).
//
// CAPTCHA LLM fallback uses the Inference Hub (ss7) instead of external
// services. This hook connects the CAPTCHA solver to the Inference Hub
// for LLM-based challenge solving.

// InferenceHubRequest is a request to the Inference Hub for CAPTCHA
// LLM fallback (spec L4248).
type InferenceHubRequest struct {
	ChallengeType string `json:"challenge_type"`
	ImageData     string `json:"image_data"`
	Prompt        string `json:"prompt"`
	// LocalOnly forces LOCAL inference only (ss7.7 privacy routing).
	LocalOnly bool `json:"local_only"`
}

// InferenceHubResponse is the Inference Hub's response.
type InferenceHubResponse struct {
	Solved bool   `json:"solved"`
	Answer string `json:"answer"`
	Model  string `json:"model"`
	Local  bool   `json:"local"`
	Error  string `json:"error,omitempty"`
}

// InferenceHub is the interface for the host inference hub.
// The real implementation is provided by the embedding host.
type InferenceHub interface {
	SolveCAPTCHA(ctx context.Context, req InferenceHubRequest) (InferenceHubResponse, error)
}

// InferenceHubHook connects CAPTCHA LLM fallback to the Inference Hub
// (ss7) instead of external services (spec L4248).
type InferenceHubHook struct {
	mu    sync.RWMutex
	hub   InferenceHub
	stats InferenceHubStats
}

// InferenceHubStats tracks inference hub decisions.
type InferenceHubStats struct {
	Total      int `json:"total"`
	Solved     int `json:"solved"`
	Failed     int `json:"failed"`
	LocalUsed  int `json:"local_used"`
	RemoteUsed int `json:"remote_used"`
}

// NewInferenceHubHook creates a new inference hub hook.
func NewInferenceHubHook(hub InferenceHub) *InferenceHubHook {
	return &InferenceHubHook{hub: hub}
}

// Stats returns the current statistics.
func (h *InferenceHubHook) Stats() InferenceHubStats {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.stats
}

// ResetStats resets statistics (for testing).
func (h *InferenceHubHook) ResetStats() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.stats = InferenceHubStats{}
}

// Solve routes a CAPTCHA challenge to the Inference Hub (ss7) for
// LLM-based solving (spec L4248).
func (h *InferenceHubHook) Solve(ctx context.Context, req InferenceHubRequest) (InferenceHubResponse, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.stats.Total++

	if h.hub == nil {
		h.stats.Failed++
		return InferenceHubResponse{
			Error: "no inference hub configured",
		}, fmt.Errorf("no inference hub configured")
	}

	resp, err := h.hub.SolveCAPTCHA(ctx, req)
	if err != nil {
		h.stats.Failed++
		return InferenceHubResponse{
			Error: err.Error(),
		}, err
	}

	if resp.Solved {
		h.stats.Solved++
	} else {
		h.stats.Failed++
	}

	if resp.Local {
		h.stats.LocalUsed++
	} else {
		h.stats.RemoteUsed++
	}

	return resp, nil
}

// IsAvailable checks if the inference hub is configured.
func (h *InferenceHubHook) IsAvailable() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.hub != nil
}

// FormatChallengePrompt creates a prompt for the LLM to solve a
// CAPTCHA challenge (spec L4248).
func FormatChallengePrompt(challengeType, context string) string {
	return fmt.Sprintf("Solve this %s CAPTCHA challenge. Context: %s. Provide only the answer.", challengeType, context)
}

// ValidateResponse checks if an inference hub response is valid.
func ValidateResponse(resp InferenceHubResponse) error {
	if !resp.Solved && resp.Error == "" {
		return fmt.Errorf("unsolved response without error message")
	}
	if resp.Solved && strings.TrimSpace(resp.Answer) == "" {
		return fmt.Errorf("solved response with empty answer")
	}
	return nil
}
