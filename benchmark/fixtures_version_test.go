package benchmark

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
)

func TestBenchmarkFixtureDerivedIdentityBindsEveryActualCaseVersion(t *testing.T) {
	scenarios := DefaultScenarios()
	maximum := scenarios[0]
	maximum.ID = strings.Repeat("a", 128)
	scenarios = append(scenarios, maximum)
	for _, scenario := range scenarios {
		t.Run(scenario.ID, func(t *testing.T) {
			manifest := benchmarkManifestForTest(scenario)
			manifest.ID = ""
			encoded, err := EncodeFixture(scenario, manifest)
			if err != nil {
				t.Fatal(err)
			}
			want := "benchmark:" + ScenarioMatrixVersion + ":" + encoded.Manifest.AssetHash + ":" + scenario.ID
			if encoded.Manifest.ID != want || manifest.ID != "" || len(want) > 256 {
				t.Fatal("derived identity changed its input or exceeded the portable bound")
			}
			decoded, err := DecodeFixture(encoded)
			if err != nil || decoded != scenario {
				t.Fatalf("versioned source/expectation roundtrip: %v", err)
			}
			again, err := EncodeFixture(scenario, encoded.Manifest)
			if err != nil || again.Manifest.ID != encoded.Manifest.ID || string(again.Content) != string(encoded.Content) {
				t.Fatalf("same version changed its identity or bytes: %v", err)
			}
			for _, field := range []string{"html", "expectation"} {
				changed := scenario
				if field == "html" {
					changed.HTML += "<p>new retained version</p>"
					changed.BodyBytes = len(changed.HTML)
				} else {
					changed.ExpectLinks++
				}
				other, err := EncodeFixture(changed, manifest)
				if err != nil || other.Manifest.ID == encoded.Manifest.ID || other.Manifest.AssetHash == encoded.Manifest.AssetHash {
					t.Fatalf("changed %s reused an immutable version identity: %v", field, err)
				}
				if _, err := EncodeFixture(changed, encoded.Manifest); err == nil {
					t.Fatalf("changed %s silently rebound existing identity", field)
				}
			}
		})
	}
}

func TestBenchmarkFixtureVersionIdentityRejectsResealedOtherSource(t *testing.T) {
	scenario := DefaultScenarios()[0]
	manifest := benchmarkManifestForTest(scenario)
	manifest.ID = ""
	original, err := EncodeFixture(scenario, manifest)
	if err != nil {
		t.Fatal(err)
	}
	scenario.HTML += "<p>changed actual source</p>"
	scenario.BodyBytes = len(scenario.HTML)
	changed, err := EncodeFixture(scenario, manifest)
	if err != nil {
		t.Fatal(err)
	}
	changed.Manifest.ID = original.Manifest.ID
	if err := changed.Manifest.Validate(changed.Content); err != nil {
		t.Fatalf("negative control must retain valid transport/hash admission: %v", err)
	}
	if _, err := DecodeFixture(changed); err == nil {
		t.Fatal("resealed changed source reused the old version identity")
	}
	invalid := original.Manifest.Detached()
	invalid.ID = fmt.Sprintf("benchmark:%s:%s:%s", ScenarioMatrixVersion, strings.Repeat("0", sha256.Size*2), scenario.ID)
	if _, err := EncodeFixture(DefaultScenarios()[0], invalid); err == nil {
		t.Fatal("wrong version digest acquired actual source identity")
	}
}
