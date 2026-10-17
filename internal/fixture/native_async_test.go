package fixture

import (
	"context"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestNativeFixtureAsyncFetchObservesActualDelayedResponse(t *testing.T) {
	server := NewServer()
	defer server.Close()
	scenario := ScenarioByID("spa-001")
	if scenario == nil {
		t.Fatal("declared SPA fixture is missing")
	}
	if err := server.Register(*scenario); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int64
	if err := server.RegisterHandler("/api/spa-data", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		select {
		case <-r.Context().Done():
			return
		case <-time.After(150 * time.Millisecond):
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := io.WriteString(w, `{"text":"Hello SPA"}`); err != nil {
			t.Error(err)
		}
	})); err != nil {
		t.Fatal(err)
	}
	for _, runner := range []runner{&bridgeRunner{}, &artemisRunner{}} {
		t.Run(runner.name(), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			defer func() {
				cleanup, cancel := context.WithTimeout(context.Background(), nativeRunnerCleanupBudget)
				defer cancel()
				runner.stop(cleanup, t)
			}()
			runner.start(ctx, t, server)
			before := requests.Load()
			actual := runner.run(ctx, t, *scenario, server)
			if err := ValidateCrossResult(*scenario, actual); err != nil {
				t.Fatalf("actual delayed SPA outcome: %v", err)
			}
			if requests.Load() <= before {
				t.Fatal("SPA outcome passed without its actual endpoint request")
			}
		})
	}
}
