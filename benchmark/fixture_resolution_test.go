package benchmark

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func harnessFixtureConfigForTest(t *testing.T) HarnessConfig {
	t.Helper()
	scenario := DefaultScenarios()[0]
	return HarnessConfig{OutputDir: filepath.Join(t.TempDir(), "run"), Iterations: 1, SkipCompetitor: true,
		Workloads: WorkloadsWithIterations([]Scenario{scenario}, 1)[:1]}
}

func TestHarnessRecordsTheActualExecutedFixtureManifest(t *testing.T) {
	config := harnessFixtureConfigForTest(t)
	scorecard, err := NewHarness(config).Run(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(scorecard.Scenarios) != 1 || scorecard.Scenarios[0].FixtureManifest == nil || len(scorecard.Results) != 1 || !scorecard.Results[0].Validated {
		t.Fatalf("actual selected fixture proof missing: %+v", scorecard)
	}
	manifest := *scorecard.Scenarios[0].FixtureManifest
	if manifest.ID != "benchmark:"+ScenarioMatrixVersion+":"+manifest.AssetHash+":"+DefaultScenarios()[0].ID {
		t.Fatal("actual standalone producer did not retain a payload-version identity")
	}
	input, err := EncodeFixture(DefaultScenarios()[0], manifest)
	if err != nil || input.Manifest.AssetHash != manifest.AssetHash || manifest.Retention == "" {
		t.Fatalf("actual complete input/manifest binding: %+v %v", manifest, err)
	}
	sealed, err := readSealedArtifacts(config.OutputDir)
	if err != nil || !strings.Contains(sealed.ScorecardJSON, `"fixtureManifest"`) {
		t.Fatalf("existing cohort did not seal fixture metadata: %v", err)
	}
	var persisted Scorecard
	if err := json.Unmarshal([]byte(sealed.ScorecardJSON), &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.Scenarios[0].FixtureManifest == nil || persisted.Scenarios[0].FixtureManifest.AssetHash != manifest.AssetHash {
		t.Fatal("persisted scorecard lost actual fixture identity")
	}
}

func TestHarnessResolverDenialCannotFallBackOrStartSideEffects(t *testing.T) {
	config := harnessFixtureConfigForTest(t)
	config.Profile.CPUProfilePath = filepath.Join(config.OutputDir, "cpu.pprof")
	denied := errors.New("actual admission denied")
	calls := 0
	config.FixtureResolver = func(context.Context, Scenario) (EncodedFixture, error) {
		calls++
		return EncodedFixture{}, denied
	}
	if scorecard, err := NewHarness(config).Run(t.Context()); !errors.Is(err, denied) || scorecard != nil || calls != 1 {
		t.Fatalf("denied authority fell back: scorecard=%+v calls=%d err=%v", scorecard, calls, err)
	}
	if _, err := os.Stat(config.OutputDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("denial created profile/output side effects: %v", err)
	}
}

func TestHarnessRejectsCanonicalButWeakenedRequestedInput(t *testing.T) {
	config := harnessFixtureConfigForTest(t)
	config.FixtureResolver = func(ctx context.Context, requested Scenario) (EncodedFixture, error) {
		requested.ExpectLinks = 0
		return EncodeFixture(requested, benchmarkManifestForTest(requested))
	}
	if scorecard, err := NewHarness(config).Run(t.Context()); err == nil || scorecard != nil || !strings.Contains(err.Error(), "requested matrix input") {
		t.Fatalf("weakened valid input reached execution: %+v %v", scorecard, err)
	}
	if _, err := os.Stat(config.OutputDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("rejected input created output")
	}
}

func TestHarnessRechecksAuthorityAndMetadataBeforeEachWorkload(t *testing.T) {
	for _, name := range []string{"revoked", "changed metadata"} {
		t.Run(name, func(t *testing.T) {
			config := harnessFixtureConfigForTest(t)
			config.Workloads = WorkloadsWithIterations(DefaultScenarios()[:1], 1)
			calls := 0
			config.FixtureResolver = func(ctx context.Context, requested Scenario) (EncodedFixture, error) {
				calls++
				if calls == 3 && name == "revoked" {
					return EncodedFixture{}, errors.New("fixture permission revoked")
				}
				manifest := benchmarkManifestForTest(requested)
				if calls == 3 {
					manifest.Retention = "changed-profile"
				}
				return EncodeFixture(requested, manifest)
			}
			scorecard, err := NewHarness(config).Run(t.Context())
			if err == nil || scorecard == nil || len(scorecard.Results) != 1 || !scorecard.Results[0].Validated || len(scorecard.Priming) != 0 || calls != 3 {
				t.Fatalf("stale authority/metadata reached next workload: %+v calls=%d err=%v", scorecard, calls, err)
			}
			if _, err := os.Stat(config.OutputDir); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("incomplete workload published a scorecard")
			}
		})
	}
}

func TestHarnessRequiresCurrentFixturesBeforePublishingActualSamples(t *testing.T) {
	for _, change := range []string{"unchanged", "revoked", "metadata", "source", "cancelled"} {
		t.Run(change, func(t *testing.T) {
			config := harnessFixtureConfigForTest(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			denied := errors.New("fixture revoked before publication")
			config.FixtureResolver = func(ctx context.Context, requested Scenario) (EncodedFixture, error) {
				calls++
				manifest := benchmarkManifestForTest(requested)
				if calls == 3 {
					switch change {
					case "revoked":
						return EncodedFixture{}, denied
					case "metadata":
						manifest.Retention = "changed-after-execution"
					case "source":
						requested.HTML += "<p>changed-after-execution</p>"
						requested.BodyBytes = len(requested.HTML)
					case "cancelled":
						cancel()
					}
				}
				return EncodeFixture(requested, manifest)
			}
			scorecard, err := NewHarness(config).Run(ctx)
			if calls != 3 || scorecard == nil || len(scorecard.Results) != 1 || !scorecard.Results[0].Validated || len(scorecard.Samples) != 1 || !scorecard.Samples[0].Validated {
				t.Fatalf("missing actual execution or publication admission: calls=%d scorecard_present=%t err=%v", calls, scorecard != nil, err)
			}
			if change == "unchanged" {
				if err != nil {
					t.Fatal(err)
				}
				if _, err := readSealedArtifacts(config.OutputDir); err != nil {
					t.Fatalf("unchanged actual evidence was not published: %v", err)
				}
				return
			}
			if err == nil || change == "revoked" && !errors.Is(err, denied) || change == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatalf("invalid publication authority did not fail: %v", err)
			}
			if _, err := os.Stat(config.OutputDir); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("invalid authority published benchmark evidence: %v", err)
			}
		})
	}
}

func TestHarnessPublicationRechecksEverySelectedFixture(t *testing.T) {
	scenarios := DefaultScenarios()[:2]
	for _, revoked := range scenarios {
		t.Run(revoked.ID, func(t *testing.T) {
			config := harnessFixtureConfigForTest(t)
			config.Workloads = nil
			for _, scenario := range scenarios {
				config.Workloads = append(config.Workloads, WorkloadsWithIterations([]Scenario{scenario}, 1)[0])
			}
			calls := make(map[string]int)
			denied := errors.New("selected fixture revoked before publication")
			config.FixtureResolver = func(ctx context.Context, requested Scenario) (EncodedFixture, error) {
				calls[requested.ID]++
				if requested.ID == revoked.ID && calls[requested.ID] == 3 {
					return EncodedFixture{}, denied
				}
				return EncodeFixture(requested, benchmarkManifestForTest(requested))
			}
			scorecard, err := NewHarness(config).Run(t.Context())
			if !errors.Is(err, denied) || calls[revoked.ID] != 3 || scorecard == nil || len(scorecard.Results) != len(scenarios) {
				t.Fatalf("selected fixture omitted from publication admission: calls=%v scorecard_present=%t err=%v", calls, scorecard != nil, err)
			}
			for _, result := range scorecard.Results {
				if !result.Validated {
					t.Fatalf("publication test lacked actual valid samples: %s %s", result.ScenarioID, result.Error)
				}
			}
			if _, err := os.Stat(config.OutputDir); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("revoked selected fixture published benchmark evidence")
			}
		})
	}
}
