package solver

import (
	"context"
	"errors"
	"testing"
)

type governedExecutor struct {
	strategies []SolverStrategy
	err        error
}

func (e *governedExecutor) Execute(_ context.Context, strategy SolverStrategy, _ ChallengeInfo) error {
	e.strategies = append(e.strategies, strategy)
	return e.err
}

type budgetedGovernedExecutor struct {
	governedExecutor
	used  int
	limit int
}

func (e *budgetedGovernedExecutor) ExecuteWithinBudget(_ context.Context, strategy SolverStrategy, _ ChallengeInfo, limit int) (int, error) {
	e.strategies = append(e.strategies, strategy)
	e.limit = limit
	return e.used, e.err
}

func TestChallengeResolverRequiresPolicyAndPostcondition(t *testing.T) {
	executor := &governedExecutor{}
	resolver := ChallengeResolver{
		Policy:   ChallengePolicy{AllowedDomains: []string{"example.com"}, AllowedTypes: []ChallengeType{TypeCloudflare}, AllowedStrategies: []SolverStrategy{StrategyWait}},
		Executor: executor,
		Verify:   func(context.Context) (bool, error) { return true, nil },
	}
	outcome, err := resolver.Resolve(context.Background(), ChallengeInfo{Type: TypeCloudflare, Domain: "example.com", Confidence: .95})
	if err != nil || outcome.Status != ChallengeStatusSolved || outcome.Strategy != StrategyWait {
		t.Fatalf("outcome=%+v err=%v", outcome, err)
	}
	if len(executor.strategies) != 1 {
		t.Fatalf("strategies=%v", executor.strategies)
	}
	resolver.Verify = func(context.Context) (bool, error) { return false, nil }
	outcome, err = resolver.Resolve(context.Background(), ChallengeInfo{Type: TypeCloudflare, Domain: "example.com"})
	if err != nil || outcome.Status != ChallengeStatusFailed {
		t.Fatalf("unverified outcome=%+v err=%v", outcome, err)
	}
}

func TestChallengeResolverDeniesUnallowlistedAndUnsupported(t *testing.T) {
	resolver := ChallengeResolver{Policy: ChallengePolicy{AllowedDomains: []string{"example.com"}, AllowedTypes: []ChallengeType{TypeCloudflare}, AllowedStrategies: []SolverStrategy{StrategyWait}}}
	outcome, err := resolver.Resolve(context.Background(), ChallengeInfo{Type: TypeCloudflare, Domain: "evil.example"})
	if err != nil || outcome.Status != ChallengeStatusDisallowed {
		t.Fatalf("denial outcome=%+v err=%v", outcome, err)
	}
	outcome, err = resolver.Resolve(context.Background(), ChallengeInfo{Type: TypeGeneric, Domain: "example.com"})
	if err != nil || outcome.Status != ChallengeStatusDisallowed {
		t.Fatalf("type denial outcome=%+v err=%v", outcome, err)
	}
}

func TestChallengeResolverEnforcesMeasuredVisionBudget(t *testing.T) {
	executor := &budgetedGovernedExecutor{used: 4}
	resolver := ChallengeResolver{
		Policy:   ChallengePolicy{AllowedDomains: []string{"example.com"}, AllowedTypes: []ChallengeType{TypeCloudflare}, AllowedStrategies: []SolverStrategy{StrategyLocalVision}, MaxVisionTokens: 8},
		Executor: executor,
		Verify:   func(context.Context) (bool, error) { return true, nil },
	}
	outcome, err := resolver.Resolve(context.Background(), ChallengeInfo{Type: TypeCloudflare, Domain: "example.com"})
	if err != nil || outcome.Status != ChallengeStatusSolved || executor.limit != 8 {
		t.Fatalf("budgeted outcome=%+v err=%v limit=%d", outcome, err, executor.limit)
	}

	executor.used = 9
	outcome, err = resolver.Resolve(context.Background(), ChallengeInfo{Type: TypeCloudflare, Domain: "example.com"})
	if err != nil || outcome.Status != ChallengeStatusBudget || outcome.Reason != "vision_token_budget_exhausted" {
		t.Fatalf("over-budget outcome=%+v err=%v", outcome, err)
	}

	resolver.Executor = &governedExecutor{}
	outcome, err = resolver.Resolve(context.Background(), ChallengeInfo{Type: TypeCloudflare, Domain: "example.com"})
	if err != nil || outcome.Status != ChallengeStatusUnsupported || outcome.Reason != "vision_token_budget_unmeasured" {
		t.Fatalf("unmeasured outcome=%+v err=%v", outcome, err)
	}
}

func TestChallengeResolverPreservesCallerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	resolver := ChallengeResolver{Policy: ChallengePolicy{AllowedDomains: []string{"example.com"}, AllowedTypes: []ChallengeType{TypeCloudflare}, AllowedStrategies: []SolverStrategy{StrategyWait}}, Executor: &governedExecutor{}, Verify: func(context.Context) (bool, error) { return true, nil }}
	outcome, err := resolver.Resolve(ctx, ChallengeInfo{Type: TypeCloudflare, Domain: "example.com"})
	if !errors.Is(err, context.Canceled) || outcome.Status != ChallengeStatusCancelled {
		t.Fatalf("cancellation outcome=%+v err=%v", outcome, err)
	}
}
