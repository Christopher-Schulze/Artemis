package fixture

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/Christopher-Schulze/Artemis/engine"
	"github.com/Christopher-Schulze/Artemis/router"
)

func TestHybridObservesActualDeclaredErrorStatusesBeforeEligibilityPromotion(t *testing.T) {
	server := NewServer()
	defer server.Close()
	runtime, err := engine.New(engine.Config{PolicyConfig: server.PolicyConfig()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := runtime.Close(); err != nil {
			t.Error(err)
		}
	}()
	for _, status := range []int{400, 401, 403, 418, 429, 500, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			scenario := Scenario{ID: fmt.Sprintf("actual-status-%d", status), Path: fmt.Sprintf("/actual-status-%d", status), Kind: KindHTML,
				Status: status, HTML: "<!doctype html><title>Observed Status</title><p>actual error response</p>",
				Expect: Expect{Status: status, Title: "Observed Status", Contains: []string{"actual error response"}}}
			if err := server.Register(scenario); err != nil {
				t.Fatal(err)
			}
			executor := router.RuntimeExecutor{Runtime: runtime}
			owner, err := router.New(router.Config{Executors: map[router.Mode]router.Executor{router.ModeStaticFetch: executor, router.ModeRenderlessJS: executor}})
			if err != nil {
				t.Fatal(err)
			}
			observed, err := RunHybridScenario(t.Context(), owner, nil, scenario, server)
			if err != nil || observed.StatusCode != status {
				t.Fatalf("real declared non-200 response was replaced or dropped: observed=%d err=%v", observed.StatusCode, err)
			}
			if err := ValidateCrossResult(scenario, observed); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, status := range []int{200, 403, 429} {
		t.Run(fmt.Sprintf("challenge-%d", status), func(t *testing.T) {
			scenario := Scenario{ID: fmt.Sprintf("actual-challenge-%d", status), Path: fmt.Sprintf("/actual-challenge-%d", status), Kind: KindChallenge,
				Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("X-Challenge", "1+1")
					w.WriteHeader(status)
				})}
			if err := server.Register(scenario); err != nil {
				t.Fatal(err)
			}
			executor := router.RuntimeExecutor{Runtime: runtime}
			owner, err := router.New(router.Config{Executors: map[router.Mode]router.Executor{router.ModeStaticFetch: executor}})
			if err != nil {
				t.Fatal(err)
			}
			observed, err := RunHybridScenario(t.Context(), owner, nil, scenario, server)
			if !errors.Is(err, router.ErrChallengeDetected) || observed.StatusCode != 0 {
				t.Fatalf("actual challenge was admitted as page output: observed=%+v err=%v", observed, err)
			}
		})
	}
}
