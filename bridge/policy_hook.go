package bridge

import (
	"context"
	"fmt"
	"sync"
)

// policy_hook.go (spec L4247: Policy Engine integration ss6).
//
// Browser requests go through the host policy engine before
// execution. This hook intercepts browser navigation/action requests
// and routes them through the Policy Engine for authorization.
//
// Reference: research/webstack/agent-browser-main/cli/src/native/policy.rs:1-217

// PolicyDecision is the Policy Engine's authorization decision.
type PolicyDecision string

const (
	PolicyDecisionAllow     PolicyDecision = "allow"
	PolicyDecisionDeny      PolicyDecision = "deny"
	PolicyDecisionChallenge PolicyDecision = "challenge"
)

// PolicyRequest represents a browser request to be evaluated by the
// Policy Engine (spec L4247).
type PolicyRequest struct {
	URL     string            `json:"url"`
	Method  string            `json:"method"`
	Action  string            `json:"action"`
	Headers map[string]string `json:"headers,omitempty"`
}

// PolicyResponse is the Policy Engine's response to a browser request.
type PolicyResponse struct {
	Decision     PolicyDecision `json:"decision"`
	Reason       string         `json:"reason"`
	RewrittenURL string         `json:"rewritten_url,omitempty"`
}

// PolicyEngine is the interface for the host policy engine.
// The real implementation is provided by the embedding host; this interface
// allows artemis/ to depend on the abstraction.
type PolicyEngine interface {
	Evaluate(ctx context.Context, req PolicyRequest) (PolicyResponse, error)
}

// PolicyHook routes browser requests through the Policy Engine (ss6)
// before execution (spec L4247).
type PolicyHook struct {
	mu      sync.RWMutex
	engine  PolicyEngine
	enabled bool
	stats   PolicyHookStats
}

// PolicyHookStats tracks policy hook decisions.
type PolicyHookStats struct {
	Total      int `json:"total"`
	Allowed    int `json:"allowed"`
	Denied     int `json:"denied"`
	Challenged int `json:"challenged"`
}

// NewPolicyHook creates a new policy hook with the given engine.
func NewPolicyHook(engine PolicyEngine) *PolicyHook {
	return &PolicyHook{
		engine:  engine,
		enabled: true,
	}
}

// Enabled returns whether the hook is active.
func (h *PolicyHook) Enabled() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.enabled
}

// SetEnabled enables or disables the hook.
func (h *PolicyHook) SetEnabled(enabled bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.enabled = enabled
}

// Stats returns the current statistics.
func (h *PolicyHook) Stats() PolicyHookStats {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.stats
}

// ResetStats resets statistics (for testing).
func (h *PolicyHook) ResetStats() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.stats = PolicyHookStats{}
}

// Check routes a browser request through the Policy Engine (spec L4247).
func (h *PolicyHook) Check(ctx context.Context, req PolicyRequest) (PolicyResponse, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.stats.Total++

	if !h.enabled {
		h.stats.Allowed++
		return PolicyResponse{Decision: PolicyDecisionAllow, Reason: "hook disabled"}, nil
	}

	if h.engine == nil {
		h.stats.Allowed++
		return PolicyResponse{Decision: PolicyDecisionAllow, Reason: "no policy engine configured"}, nil
	}

	resp, err := h.engine.Evaluate(ctx, req)
	if err != nil {
		h.stats.Denied++
		return PolicyResponse{
			Decision: PolicyDecisionDeny,
			Reason:   fmt.Sprintf("policy engine error: %v", err),
		}, err
	}

	switch resp.Decision {
	case PolicyDecisionAllow:
		h.stats.Allowed++
	case PolicyDecisionDeny:
		h.stats.Denied++
	case PolicyDecisionChallenge:
		h.stats.Challenged++
	}

	return resp, nil
}
