package main

import (
	"encoding/json"
	"testing"

	"github.com/Christopher-Schulze/Artemis/benchmark"
)

func TestBenchmarkCommandSealsEveryActualFixtureManifest(t *testing.T) {
	output := t.TempDir()
	if code := cmdBenchmark([]string{"--skip-competitor", "--iterations=1", "--timeout=20s", "--format=summary", "--output=" + output}); code != 0 {
		t.Fatalf("actual benchmark command exit=%d", code)
	}
	sealed, err := benchmark.ReadRawArtifacts(output)
	if err != nil {
		t.Fatal(err)
	}
	var scorecard benchmark.Scorecard
	if err := json.Unmarshal([]byte(sealed.ScorecardJSON), &scorecard); err != nil {
		t.Fatal(err)
	}
	if len(scorecard.Scenarios) != len(benchmark.DefaultScenarios()) {
		t.Fatal("actual command omitted selected fixture inputs")
	}
	for _, evidence := range scorecard.Scenarios {
		scenario := benchmark.ScenarioByID(evidence.ID)
		if scenario == nil || evidence.FixtureManifest == nil {
			t.Fatal("actual command emitted an unknown or unbound fixture")
		}
		if _, err := benchmark.EncodeFixture(*scenario, *evidence.FixtureManifest); err != nil {
			t.Fatalf("actual command changed input or expectation identity: %v", err)
		}
	}
}
