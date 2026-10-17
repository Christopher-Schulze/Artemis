package benchmark

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/fixtureasset"
)

func benchmarkManifestForTest(scenario Scenario) fixtureasset.Manifest {
	return fixtureasset.Manifest{ID: scenario.ID, OwnerSubsystem: "browser-benchmark", FixtureKind: fixtureasset.KindBenchmark,
		PrivacyClass: fixtureasset.PrivacyPublic, ReplayMode: fixtureasset.ReplayIsolated,
		RunnerRef: FixtureRunnerRef, QualityGateRef: "benchmark:semantic-equivalence", Retention: "benchmark-evidence"}
}

func TestBenchmarkFixturePreservesCompleteActualMatrix(t *testing.T) {
	for _, scenario := range DefaultScenarios() {
		t.Run(scenario.ID, func(t *testing.T) {
			encoded, err := EncodeFixture(scenario, benchmarkManifestForTest(scenario))
			if err != nil {
				t.Fatal(err)
			}
			actual, err := DecodeFixture(encoded)
			if err != nil || actual != scenario {
				t.Fatalf("complete input/expectation roundtrip: %+v %v", actual, err)
			}
			changed := scenario
			changed.ExpectLinks++
			other, err := EncodeFixture(changed, benchmarkManifestForTest(changed))
			if err != nil || other.Manifest.AssetHash == encoded.Manifest.AssetHash {
				t.Fatal("semantic requirements omitted from actual asset identity")
			}
		})
	}
}

func TestBenchmarkFixtureRejectsChangedBindingsAndInvalidScenario(t *testing.T) {
	for _, name := range []string{"id", "kind", "runner", "hash", "path", "mode", "negative expectation", "oversized body", "incorrect byte count"} {
		t.Run(name, func(t *testing.T) {
			scenario := DefaultScenarios()[0]
			manifest := benchmarkManifestForTest(scenario)
			switch name {
			case "id":
				manifest.ID = "different"
			case "kind":
				manifest.FixtureKind = fixtureasset.KindReplay
			case "runner":
				manifest.RunnerRef = "different"
			case "hash":
				manifest.AssetHash = strings.Repeat("0", 64)
			case "path":
				manifest.AssetPath = "/different"
			case "mode":
				scenario.EngineMode = ModeChromium
			case "negative expectation":
				scenario.ExpectLinks = -1
			case "oversized body":
				scenario.HTML = strings.Repeat("x", (8<<20)+1)
			case "incorrect byte count":
				scenario.BodyBytes = 1
			}
			if _, err := EncodeFixture(scenario, manifest); err == nil {
				t.Fatal("invalid benchmark fixture acquired a manifest")
			}
		})
	}
}

func TestBenchmarkFixtureRejectsNoncanonicalResealedInput(t *testing.T) {
	scenario := DefaultScenarios()[0]
	original, err := EncodeFixture(scenario, benchmarkManifestForTest(scenario))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ name, content string }{
		{"whitespace", string(original.Content) + "\n"},
		{"trailing", string(original.Content) + "{}"},
		{"duplicate", strings.Replace(string(original.Content), `"version":"2.0.0"`, `"version":"2.0.0","version":"2.0.0"`, 1)},
		{"unknown", strings.Replace(string(original.Content), `"version":"2.0.0"`, `"unknown":true,"version":"2.0.0"`, 1)},
		{"version", strings.Replace(string(original.Content), `"version":"2.0.0"`, `"version":"different"`, 1)},
		{"omitted expectation", strings.Replace(string(original.Content), `,"expectLinks":3`, "", 1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.content == string(original.Content) {
				t.Fatal("negative control did not change actual input")
			}
			changed := EncodedFixture{Manifest: original.Manifest.Detached(), Content: []byte(test.content)}
			changed.Manifest.AssetHash = fmt.Sprintf("%x", sha256.Sum256(changed.Content))
			changed.Manifest.AssetPath = "content://sha256/" + changed.Manifest.AssetHash
			if err := changed.Manifest.Validate(changed.Content); err != nil {
				t.Fatalf("negative input must pass transport hash admission: %v", err)
			}
			if _, err := DecodeFixture(changed); err == nil {
				t.Fatal("resealed invalid content weakened canonical input")
			}
		})
	}
}

func TestBenchmarkRunnerExecutesBoundInputAndRejectsCallerMutation(t *testing.T) {
	scenario := DefaultScenarios()[0]
	encoded, err := EncodeFixture(scenario, benchmarkManifestForTest(scenario))
	if err != nil {
		t.Fatal(err)
	}
	runner, err := NewArtemisRunnerFromFixtures([]EncodedFixture{encoded}, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := runner.Close(); err != nil {
			t.Error(err)
		}
	})
	actual := runner.RunScenario(t.Context(), scenario)
	if !actual.OK || !actual.Validated || actual.Validation.Title != scenario.ExpectTitle {
		t.Fatalf("actual bound engine execution: %+v", actual)
	}
	for _, name := range []string{"html", "expectation", "scripts", "unregistered"} {
		t.Run(name, func(t *testing.T) {
			changed := scenario
			switch name {
			case "html":
				changed.HTML += "altered"
			case "expectation":
				changed.ExpectLinks = 0
			case "scripts":
				changed.ScriptCount++
			case "unregistered":
				changed.ID = "other"
			}
			result := runner.RunScenario(t.Context(), changed)
			if result.OK || result.Validated || result.Error != "benchmark scenario differs from registered fixture input" {
				t.Fatalf("mutated input reached sample execution: %+v", result)
			}
		})
	}
	if err := runner.Close(); err != nil {
		t.Fatal(err)
	}
	if err := runner.Reset(false); err == nil {
		t.Fatal("closed fixture owner restarted an engine")
	}
}

func TestBenchmarkFixtureBatchBoundsPrecedeDecodingAndEngineCreation(t *testing.T) {
	for _, test := range []struct {
		name     string
		fixtures []EncodedFixture
	}{
		{"empty", nil}, {"entry count", make([]EncodedFixture, 1025)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if runner, err := NewArtemisRunnerFromFixtures(test.fixtures, false); err == nil || runner != nil {
				t.Fatal("invalid batch created a fixture or engine owner")
			}
		})
	}
	content := make([]byte, 8<<20)
	fixtures := make([]EncodedFixture, 9)
	for index := range fixtures {
		fixtures[index].Content = content
	}
	if runner, err := NewArtemisRunnerFromFixtures(fixtures, false); err == nil || runner != nil || err.Error() != "benchmark fixture: aggregate input byte limit exceeded" {
		t.Fatalf("aggregate admission did not precede content decoding: %v", err)
	}
}
