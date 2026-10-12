package router

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	bridgeobserve "github.com/Christopher-Schulze/Artemis/bridge/observe"
	"github.com/Christopher-Schulze/Artemis/observe"
	"github.com/Christopher-Schulze/Artemis/solver"
)

type challengeResolverFunc func(context.Context, solver.ChallengeInfo) (solver.ChallengeOutcome, error)

func (f challengeResolverFunc) Resolve(ctx context.Context, challenge solver.ChallengeInfo) (solver.ChallengeOutcome, error) {
	return f(ctx, challenge)
}

type challengeExecutorFunc func(context.Context, solver.SolverStrategy, solver.ChallengeInfo) error

func (f challengeExecutorFunc) Execute(ctx context.Context, strategy solver.SolverStrategy, challenge solver.ChallengeInfo) error {
	return f(ctx, strategy, challenge)
}

func TestHybridRouterBlocksDetectedChallengeBeforeSuccess(t *testing.T) {
	resource := &closeCounter{}
	router, err := New(Config{Executors: map[Mode]Executor{
		ModeStaticFetch: executorFunc(func(_ context.Context, request ExecutionRequest) (ExecutionOutput, error) {
			output := verifiedOutput(request, `<html><head><title>Just a moment...</title></head><body>checking your browser</body></html>`)
			output.Resource = resource
			return output, nil
		}),
	}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = router.Execute(context.Background(), RouteRequest{URL: "https://fixture.test/challenge", Signals: Signals{IsHTML: true}})
	var routeErr *RouteError
	if !errors.As(err, &routeErr) || routeErr.Code != ErrorChallenge || !errors.Is(err, ErrChallengeDetected) {
		t.Fatalf("err=%v", err)
	}
	if resource.closed.Load() != 1 {
		t.Fatalf("challenge resource close count=%d", resource.closed.Load())
	}
}

func TestHybridRouterRequiresVerifiedGovernedChallengeOutcome(t *testing.T) {
	resolverCalls := 0
	base := challengeResolverFunc(func(_ context.Context, challenge solver.ChallengeInfo) (solver.ChallengeOutcome, error) {
		resolverCalls++
		return solver.ChallengeOutcome{Type: challenge.Type, Status: solver.ChallengeStatusFailed, Reason: "verification failed"}, nil
	})
	router, err := New(Config{Executors: map[Mode]Executor{
		ModeRenderlessJS: executorFunc(func(_ context.Context, request ExecutionRequest) (ExecutionOutput, error) {
			return verifiedOutput(request, `<html><body><div class="g-recaptcha"></div></body></html>`), nil
		}),
	}, ChallengeResolver: base})
	if err != nil {
		t.Fatal(err)
	}
	_, err = router.Execute(context.Background(), RouteRequest{URL: "https://fixture.test/challenge", Signals: Signals{IsHTML: true}})
	var routeErr *RouteError
	if !errors.As(err, &routeErr) || routeErr.Code != ErrorChallengeSolve || !errors.Is(err, ErrChallengeUnresolved) {
		t.Fatalf("err=%v", err)
	}
	if resolverCalls != 1 {
		t.Fatalf("resolver calls=%d", resolverCalls)
	}
}

func TestHybridRouterCarriesBoundedObservationAndSolvedChallengeEvidence(t *testing.T) {
	resolver := solver.ChallengeResolver{
		Policy: solver.ChallengePolicy{
			AllowedDomains: []string{"fixture.test"}, AllowedTypes: []solver.ChallengeType{solver.TypeRecaptcha},
			AllowedStrategies: []solver.SolverStrategy{solver.StrategyWait}, MaxAttempts: 1,
		},
		Executor: challengeExecutorFunc(func(_ context.Context, strategy solver.SolverStrategy, _ solver.ChallengeInfo) error {
			if strategy != solver.StrategyWait {
				return errors.New("unexpected challenge strategy")
			}
			return nil
		}),
		Verify: func(context.Context) (bool, error) { return true, nil },
	}
	observation := &observe.ObservationEvidence{
		Schema:   "artemis.observation.v1",
		Snapshot: structSnapshot(),
		Network:  []observe.NetworkEvent{{URL: "https://fixture.test/challenge"}},
		Console:  []observe.ConsoleEntry{{Level: observe.ConsoleLevelLog, Args: []string{"ready"}}},
		Metrics:  observe.PerformanceMetrics{RequestCount: 2},
	}
	router, err := New(Config{Executors: map[Mode]Executor{
		ModeChromiumCDP: executorFunc(func(_ context.Context, request ExecutionRequest) (ExecutionOutput, error) {
			output := verifiedOutput(request, `<html><body><div class="g-recaptcha"></div></body></html>`)
			output.Page.Headers = http.Header{"Content-Type": []string{"text/html"}}
			output.Observation = observation
			return output, nil
		}),
	}, ChallengeResolver: resolver})
	if err != nil {
		t.Fatal(err)
	}
	result, err := router.Execute(context.Background(), RouteRequest{URL: "https://fixture.test/challenge", ForceMode: ModeChromiumCDP})
	if err != nil {
		t.Fatal(err)
	}
	if result.Observation == nil || result.Evidence.Observation == nil {
		t.Fatalf("observation missing result=%+v evidence=%+v", result.Observation, result.Evidence.Observation)
	}
	if result.Evidence.Observation.NetworkCount != 1 || result.Evidence.Observation.ConsoleCount != 1 || result.Evidence.Observation.RequestCount != 2 {
		t.Fatalf("observation summary=%+v", result.Evidence.Observation)
	}
	if result.Evidence.Challenge == nil || result.Evidence.Challenge.Status != solver.ChallengeStatusSolved {
		t.Fatalf("solved challenge evidence missing: %+v", result.Evidence.Challenge)
	}
}

func structSnapshot() bridgeobserve.Snapshot {
	return bridgeobserve.Snapshot{Schema: "artemis.observation.v1", Epoch: 7, Nodes: []bridgeobserve.Node{{Role: "button", Name: "ready"}}}
}

func TestHybridRouterDetectsNetworkChallengeSignals(t *testing.T) {
	observation := &observe.ObservationEvidence{
		Schema:   "artemis.observation.v1",
		Snapshot: structSnapshot(),
		Network:  []observe.NetworkEvent{{URL: "https://challenges.cloudflare.com/cdn-cgi"}},
	}
	router, err := New(Config{Executors: map[Mode]Executor{
		ModeStaticFetch: executorFunc(func(_ context.Context, request ExecutionRequest) (ExecutionOutput, error) {
			output := verifiedOutput(request, `<html><body>content</body></html>`)
			output.Observation = observation
			return output, nil
		}),
	}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = router.Execute(context.Background(), RouteRequest{URL: "https://fixture.test", Signals: Signals{IsHTML: true}})
	if err == nil || !errors.Is(err, ErrChallengeDetected) || !strings.Contains(err.Error(), "challenge") {
		t.Fatalf("network challenge was not blocked: %v", err)
	}
}
