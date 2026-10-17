package smoke

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Christopher-Schulze/Artemis/fixtureasset"
)

func writeSmokeManifest(t *testing.T, manifest fixtureasset.Manifest, content []byte) string {
	t.Helper()
	data, err := fixtureasset.Encode(manifest, content)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "fixture.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSmokeFixtureBindsExactYAMLAndDetachedGovernance(t *testing.T) {
	sf, raw := loadSmokeLifecycle(t)
	manifest, err := sf.fixtureAsset()
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	if manifest.AssetHash != hex.EncodeToString(digest[:]) || !bytes.Equal(raw, sf.source) || manifest.RunnerRef != "artemis:smoke:serve" {
		t.Fatalf("manifest differs from actual loaded input: %+v", manifest)
	}
	manifest.PrivacyClass = fixtureasset.PrivacyPersonal
	manifest.Retention = "explicit-customer-fixture-retention"
	manifest.DomainRefs = []string{"customer-domain"}
	path := writeSmokeManifest(t, manifest, raw)
	if err := sf.bindFixtureManifest(path); err != nil {
		t.Fatal(err)
	}
	got, err := sf.fixtureAsset()
	if err != nil || got.PrivacyClass != manifest.PrivacyClass || got.Retention != manifest.Retention || got.DomainRefs[0] != "customer-domain" {
		t.Fatalf("declared governance lost: %+v %v", got, err)
	}
	got.DomainRefs[0] = "mutated"
	again, err := sf.fixtureAsset()
	if err != nil || again.DomainRefs[0] != "customer-domain" {
		t.Fatal("returned manifest aliases loaded input")
	}
	sf.source[0] ^= 1
	if _, err := sf.fixtureAsset(); err == nil {
		t.Fatal("modified source retained admitted manifest")
	}
}

func TestSmokeFixtureRejectsResealedContractChangesWithoutFallback(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*fixtureasset.Manifest)
	}{
		{"identity", func(m *fixtureasset.Manifest) { m.ID = "other" }},
		{"source path", func(m *fixtureasset.Manifest) { m.AssetPath = "content://other" }},
		{"owner", func(m *fixtureasset.Manifest) { m.OwnerSubsystem = "other" }},
		{"runner", func(m *fixtureasset.Manifest) { m.RunnerRef = "artemis:benchmark:renderless" }},
		{"kind", func(m *fixtureasset.Manifest) { m.FixtureKind = fixtureasset.KindReplay }},
		{"mode", func(m *fixtureasset.Manifest) { m.ReplayMode = fixtureasset.ReplayDryRun }},
		{"omitted capabilities", func(m *fixtureasset.Manifest) { m.CapabilityRefs = nil }},
		{"gate", func(m *fixtureasset.Manifest) { m.QualityGateRef = "other" }},
		{"omitted scorecard", func(m *fixtureasset.Manifest) { m.ExpectedArtifactRefs = nil }},
	} {
		t.Run(test.name, func(t *testing.T) {
			sf, raw := loadSmokeLifecycle(t)
			original, err := sf.fixtureAsset()
			if err != nil {
				t.Fatal(err)
			}
			changed := original.Detached()
			test.edit(&changed)
			path := writeSmokeManifest(t, changed, raw)
			if err := sf.bindFixtureManifest(path); err == nil {
				t.Fatal("resealed weaker or foreign execution contract admitted")
			}
			retained, err := sf.fixtureAsset()
			if err != nil || retained.RunnerRef != original.RunnerRef || retained.QualityGateRef != original.QualityGateRef {
				t.Fatal("rejected bind damaged the original input")
			}
		})
	}
}

func TestSmokeFixtureCLIRejectsChangedBytesWithoutFallback(t *testing.T) {
	binary := buildRealSmokeArtemis(t)
	sf, raw := loadSmokeLifecycle(t)
	manifest, err := sf.fixtureAsset()
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := writeSmokeManifest(t, manifest, raw)
	scenarioPath := filepath.Join(t.TempDir(), "scenario.yaml")
	changed := append(append([]byte(nil), raw...), []byte("\n# changed bytes\n")...)
	if err := os.WriteFile(scenarioPath, changed, 0o600); err != nil {
		t.Fatal(err)
	}
	parent := t.TempDir()
	if code := Command([]string{"--scenarios", scenarioPath, "--fixture-manifest", manifestPath, "--bin", binary, "--out", parent}); code != 1 {
		t.Fatalf("changed input exit=%d", code)
	}
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) != 0 {
		t.Fatalf("rejected manifest produced output: %v %+v", err, entries)
	}
}

func TestSmokeFixtureActualCommandPublishesExactManifest(t *testing.T) {
	binary := buildRealSmokeArtemis(t)
	sf, raw := loadSmokeLifecycle(t)
	manifest, err := sf.fixtureAsset()
	if err != nil {
		t.Fatal(err)
	}
	manifest.Retention = "approved-smoke-evidence"
	manifestPath := writeSmokeManifest(t, manifest, raw)
	scenarioPath := filepath.Join(t.TempDir(), "scenarios.yaml")
	if err := os.WriteFile(scenarioPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	parent := t.TempDir()
	wrong := manifest.Detached()
	wrong.RunnerRef = "other"
	wrongPath := writeSmokeManifest(t, wrong, raw)
	if code := Command([]string{"--scenarios", scenarioPath, "--fixture-manifest", wrongPath, "--bin", binary, "--out", parent}); code != 1 {
		t.Fatalf("valid binary and YAML bypassed wrong manifest: exit=%d", code)
	}
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) != 0 {
		t.Fatalf("manifest rejection produced evidence: %v %v", entries, err)
	}
	if code := Command([]string{"--scenarios", scenarioPath, "--fixture-manifest", manifestPath, "--bin", binary, "--out", parent}); code != 0 {
		t.Fatalf("actual smoke command exit=%d", code)
	}
	paths, err := filepath.Glob(filepath.Join(parent, "run-*", "evidence", "scorecard.json"))
	if err != nil || len(paths) != 1 {
		t.Fatalf("actual completed output: %v %v", paths, err)
	}
	data, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	var scorecard Scorecard
	if err := json.Unmarshal(data, &scorecard); err != nil {
		t.Fatal(err)
	}
	expected, err := fixtureasset.Encode(manifest, raw)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := fixtureasset.Encode(scorecard.Inputs.FixtureManifest, raw)
	if err != nil || !bytes.Equal(actual, expected) || scorecard.Passed != 1 || scorecard.Failed != 0 || scorecard.Skipped != 0 {
		t.Fatalf("executed evidence lost exact fixture identity: %s %v %+v", actual, err, scorecard)
	}
}
