package solver

import (
	"context"
	"errors"
	"strings"
	"time"
)

type SolverStrategy string

const (
	StrategyWait        SolverStrategy = "wait_and_detect"
	StrategyLocalVision SolverStrategy = "local_vision"
	StrategyInteraction SolverStrategy = "interaction"
	StrategyUserHandoff SolverStrategy = "user_handoff"
)

type ChallengeOutcomeStatus string

const (
	ChallengeStatusSolved      ChallengeOutcomeStatus = "solved"
	ChallengeStatusUnsupported ChallengeOutcomeStatus = "unsupported"
	ChallengeStatusDisallowed  ChallengeOutcomeStatus = "disallowed"
	ChallengeStatusBudget      ChallengeOutcomeStatus = "budget_exhausted"
	ChallengeStatusCancelled   ChallengeOutcomeStatus = "cancelled"
	ChallengeStatusFailed      ChallengeOutcomeStatus = "failed"
)

type ChallengePolicy struct {
	AllowedDomains    []string
	AllowedTypes      []ChallengeType
	AllowedStrategies []SolverStrategy
	MaxAttempts       int
	MaxDuration       time.Duration
	MaxVisionTokens   int
}

type ChallengeOutcome struct {
	Status       ChallengeOutcomeStatus `json:"status"`
	Type         ChallengeType          `json:"type"`
	Domain       string                 `json:"domain,omitempty"`
	Strategy     SolverStrategy         `json:"strategy,omitempty"`
	Attempts     int                    `json:"attempts"`
	Confidence   float64                `json:"confidence"`
	SignalCount  int                    `json:"signal_count"`
	Reason       string                 `json:"reason"`
	EvidenceRefs []string               `json:"evidence_refs,omitempty"`
}

type ChallengeExecutor interface {
	Execute(context.Context, SolverStrategy, ChallengeInfo) error
}

// ChallengeUsageExecutor is required for local vision. The caller supplies
// the remaining token budget so the implementation can stop before crossing
// the policy boundary and returns the measured usage for accounting.
type ChallengeUsageExecutor interface {
	ExecuteWithinBudget(context.Context, SolverStrategy, ChallengeInfo, int) (int, error)
}

type ChallengeVerifier func(context.Context) (bool, error)

type ChallengeResolver struct {
	Policy   ChallengePolicy
	Executor ChallengeExecutor
	Verify   ChallengeVerifier
}

func (r ChallengeResolver) Resolve(ctx context.Context, challenge ChallengeInfo) (ChallengeOutcome, error) {
	outcome := ChallengeOutcome{Type: challenge.Type, Domain: challenge.Domain, Confidence: challenge.Confidence, SignalCount: len(challenge.Signals)}
	if err := ctx.Err(); err != nil {
		outcome.Status = ChallengeStatusCancelled
		outcome.Reason = "context_cancelled"
		return outcome, err
	}
	if challenge.Type == TypeNone {
		outcome.Status = ChallengeStatusUnsupported
		outcome.Reason = "no_challenge"
		return outcome, nil
	}
	if !domainAllowed(challenge.Domain, r.Policy.AllowedDomains) || !containsChallengeType(r.Policy.AllowedTypes, challenge.Type) {
		outcome.Status = ChallengeStatusDisallowed
		outcome.Reason = "challenge_policy_denied"
		return outcome, nil
	}
	if r.Executor == nil || r.Verify == nil {
		outcome.Status = ChallengeStatusUnsupported
		outcome.Reason = "challenge_executor_or_verifier_missing"
		return outcome, nil
	}
	strategies := admittedStrategies(r.Policy.AllowedStrategies)
	if len(strategies) == 0 {
		outcome.Status = ChallengeStatusUnsupported
		outcome.Reason = "no_admitted_strategy"
		return outcome, nil
	}
	if containsStrategy(strategies, StrategyLocalVision) && r.Policy.MaxVisionTokens <= 0 {
		outcome.Status = ChallengeStatusUnsupported
		outcome.Reason = "vision_token_budget_missing"
		return outcome, nil
	}
	var visionTokens int
	maxAttempts := r.Policy.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 1
	}
	resolveCtx := ctx
	if r.Policy.MaxDuration > 0 {
		var cancel context.CancelFunc
		resolveCtx, cancel = context.WithTimeout(ctx, r.Policy.MaxDuration)
		defer cancel()
	}
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		outcome.Attempts = attempt
		for _, strategy := range strategies {
			if err := resolveCtx.Err(); err != nil {
				outcome.Status = ChallengeStatusCancelled
				outcome.Reason = "challenge_context_cancelled"
				return outcome, err
			}
			outcome.Strategy = strategy
			if strategy == StrategyLocalVision {
				usageExecutor, ok := r.Executor.(ChallengeUsageExecutor)
				if !ok {
					outcome.Status = ChallengeStatusUnsupported
					outcome.Reason = "vision_token_budget_unmeasured"
					return outcome, nil
				}
				remaining := r.Policy.MaxVisionTokens - visionTokens
				used, err := usageExecutor.ExecuteWithinBudget(resolveCtx, strategy, challenge, remaining)
				if err != nil {
					continue
				}
				if used < 0 || used > remaining {
					outcome.Status = ChallengeStatusBudget
					outcome.Reason = "vision_token_budget_exhausted"
					return outcome, nil
				}
				visionTokens += used
			} else if err := r.Executor.Execute(resolveCtx, strategy, challenge); err != nil {
				continue
			}
			verified, err := r.Verify(resolveCtx)
			if err != nil {
				continue
			}
			if verified {
				outcome.Status = ChallengeStatusSolved
				outcome.Reason = "verified_challenge_postcondition"
				return outcome, nil
			}
		}
	}
	if resolveCtx.Err() != nil {
		if ctx.Err() != nil && errors.Is(ctx.Err(), context.Canceled) {
			outcome.Status = ChallengeStatusCancelled
			outcome.Reason = "challenge_context_cancelled"
			return outcome, ctx.Err()
		}
		outcome.Status = ChallengeStatusBudget
		outcome.Reason = "challenge_budget_exhausted"
		return outcome, resolveCtx.Err()
	}
	outcome.Status = ChallengeStatusFailed
	outcome.Reason = "challenge_postcondition_not_verified"
	return outcome, nil
}

func admittedStrategies(strategies []SolverStrategy) []SolverStrategy {
	allowed := []SolverStrategy{StrategyWait, StrategyLocalVision, StrategyInteraction, StrategyUserHandoff}
	if len(strategies) == 0 {
		return nil
	}
	result := make([]SolverStrategy, 0, len(allowed))
	for _, candidate := range allowed {
		for _, configured := range strategies {
			if candidate == configured {
				result = append(result, candidate)
				break
			}
		}
	}
	return result
}

func containsStrategy(strategies []SolverStrategy, want SolverStrategy) bool {
	for _, strategy := range strategies {
		if strategy == want {
			return true
		}
	}
	return false
}

func containsChallengeType(types []ChallengeType, want ChallengeType) bool {
	if len(types) == 0 {
		return false
	}
	for _, item := range types {
		if item == want {
			return true
		}
	}
	return false
}

func domainAllowed(domain string, allowed []string) bool {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" || len(allowed) == 0 {
		return false
	}
	for _, item := range allowed {
		item = strings.ToLower(strings.TrimSpace(item))
		if item != "" && (domain == item || strings.HasSuffix(domain, "."+item)) {
			return true
		}
	}
	return false
}
