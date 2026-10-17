package corpus

import (
	"errors"
	"testing"

	"github.com/Christopher-Schulze/Artemis/internal/fixture"
	"github.com/Christopher-Schulze/Artemis/process"
	"github.com/Christopher-Schulze/Artemis/router"
)

func TestRunHybridPreservesEveryOriginalNon200Outcome(t *testing.T) {
	manifests, content, err := DefaultAssets("hybrid")
	if err != nil {
		t.Fatal(err)
	}
	var executed int
	for _, manifest := range manifests {
		scenario, err := fixture.CorpusScenario(manifest, content)
		if err != nil {
			t.Fatal(err)
		}
		if scenario.Expect.Status == 0 || scenario.Expect.Status == 200 {
			continue
		}
		executed++
		t.Run(scenario.ID, func(t *testing.T) {
			result, err := RunHybrid(t.Context(), manifest, content, process.LaunchConfig{})
			if scenario.Kind == fixture.KindChallenge {
				if !errors.Is(err, router.ErrChallengeDetected) || result.Passed || result.StatusCode != 0 {
					t.Fatalf("actual challenge lost its fail-closed outcome: result=%+v err=%v", result, err)
				}
				return
			}
			if err != nil || !result.Passed || result.StatusCode != scenario.Expect.Status || result.FixtureManifest.ID != manifest.ID {
				t.Fatalf("actual non-200 hybrid response was lost: expected=%d observed=%d passed=%t err=%v", scenario.Expect.Status, result.StatusCode, result.Passed, err)
			}
		})
	}
	if executed != 6 {
		t.Fatalf("original six non-200 fixtures were not all executed: %d", executed)
	}
}
