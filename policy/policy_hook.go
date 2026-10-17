package policy

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
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
	URL        string            `json:"url"`
	Method     string            `json:"method"`
	Action     string            `json:"action"`
	Headers    map[string]string `json:"headers,omitempty"`
	TargetURLs []string          `json:"targetUrls,omitempty"`
}

// PolicyResponse is the Policy Engine's response to a browser request.
type PolicyResponse struct {
	Decision     PolicyDecision `json:"decision"`
	Reason       string         `json:"reason"`
	RewrittenURL string         `json:"rewritten_url,omitempty"`
}

// PolicyError carries denial/confirmation without parsing human error strings.
type PolicyError struct {
	Decision PolicyDecision
	Reason   string
}

func (e *PolicyError) Error() string {
	if e == nil {
		return "browser policy denied"
	}
	return "browser policy " + string(e.Decision) + ": " + e.Reason
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
	if engine != nil {
		value := reflect.ValueOf(engine)
		switch value.Kind() {
		case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
			if value.IsNil() {
				engine = nil
			}
		}
	}
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
	if h == nil {
		return PolicyResponse{Decision: PolicyDecisionDeny, Reason: "policy hook required"}, errors.New("policy hook required")
	}
	h.mu.Lock()
	h.stats.Total++
	engine, enabled := h.engine, h.enabled
	h.mu.Unlock()
	resp, err := evaluatePolicyHook(ctx, req, engine, enabled)
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.enabled && resp.Decision != PolicyDecisionDeny {
		resp = PolicyResponse{Decision: PolicyDecisionDeny, Reason: "policy hook disabled"}
		err = errors.New("policy hook disabled")
	}
	switch resp.Decision {
	case PolicyDecisionAllow:
		h.stats.Allowed++
	case PolicyDecisionDeny:
		h.stats.Denied++
	case PolicyDecisionChallenge:
		h.stats.Challenged++
	}

	return resp, err
}

func evaluatePolicyHook(ctx context.Context, req PolicyRequest, engine PolicyEngine, enabled bool) (PolicyResponse, error) {
	deny := func(err error) (PolicyResponse, error) {
		return PolicyResponse{Decision: PolicyDecisionDeny, Reason: err.Error()}, err
	}
	if ctx == nil {
		return deny(errors.New("policy context required"))
	}
	if err := ctx.Err(); err != nil {
		return deny(err)
	}
	if !enabled {
		return deny(errors.New("policy hook disabled"))
	}
	if engine == nil {
		return deny(errors.New("policy engine required"))
	}
	req.Headers = maps.Clone(req.Headers)
	req.TargetURLs = slices.Clone(req.TargetURLs)
	response, err := engine.Evaluate(ctx, req)
	if err != nil {
		return deny(fmt.Errorf("policy engine evaluate: %w", err))
	}
	if err := ctx.Err(); err != nil {
		return deny(err)
	}
	if response.RewrittenURL != "" {
		return deny(errors.New("policy URL rewriting is not supported"))
	}
	switch response.Decision {
	case PolicyDecisionAllow, PolicyDecisionDeny, PolicyDecisionChallenge:
		return response, nil
	default:
		return deny(errors.New("invalid policy decision"))
	}
}
