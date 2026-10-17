package corpus

import (
	"context"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/internal/fixture"
)

func TestRunAgentExecutesEveryActualEvaluationFixture(t *testing.T) {
	manifests, content, err := DefaultAssets("agent")
	if err != nil {
		t.Fatal(err)
	}
	var executed int
	for _, manifest := range manifests {
		scenario, err := fixture.CorpusScenario(manifest, content)
		if err != nil {
			t.Fatal(err)
		}
		if scenario.Expect.Eval == "" {
			continue
		}
		executed++
		t.Run(scenario.ID, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			result, err := RunAgent(ctx, manifest, content)
			if err != nil || result.StatusCode != scenario.Expect.Status || result.EvalResult == "" || result.FixtureManifest.ID != manifest.ID {
				t.Fatalf("actual requested Agent evaluation failed: status=%d value=%q err=%v", result.StatusCode, result.EvalResult, err)
			}
		})
	}
	if executed != 10 {
		t.Fatalf("seven original and three CSS evaluation fixtures were not all executed: %d", executed)
	}
}
