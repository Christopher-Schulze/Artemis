package fixture

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/Christopher-Schulze/Artemis/engine"
)

func TestCorpusReplayRetainsDetachedBoundManifestThroughActualCaptureAndLoad(t *testing.T) {
	server := newDefaultTestServer(t)
	defer server.Close()
	scenario := server.ScenarioByID("html-001")
	if scenario == nil {
		t.Fatal("actual source scenario missing")
	}
	manifest, err := server.CorpusManifest("renderless", *scenario)
	if err != nil {
		t.Fatal(err)
	}
	eng := newTestEngine(t, server)
	defer closeTestResource(t, "engine close", eng.Close)
	page, err := eng.Fetch(t.Context(), server.URL(scenario.Path), engine.FetchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "page close", page.Close)
	observed := resultFromEnginePage(t.Context(), t, page, *scenario)
	observed.FixtureManifest = &manifest
	directory := t.TempDir()
	record, err := CaptureReplay(*scenario, "renderless", observed, 0, SystemMetadata(), DefaultRedactionConfig(), directory)
	if err != nil {
		t.Fatal(err)
	}
	originalID := manifest.ID
	manifest.Retention = "caller-mutated"
	manifest.CapabilityRefs[0] = "caller-mutated"
	if record.Result.FixtureManifest == nil || record.Result.FixtureManifest.ID != originalID || record.Result.FixtureManifest.Retention == "caller-mutated" || record.Result.FixtureManifest.CapabilityRefs[0] == "caller-mutated" {
		t.Fatal("captured provenance aliases caller metadata")
	}
	loaded, err := LoadReplay(filepath.Join(directory, record.filename()))
	if err != nil || loaded.Result.FixtureManifest == nil || loaded.Result.FixtureManifest.ID != originalID || loaded.Result.Title != "HTML Fixture" {
		t.Fatalf("actual stored replay lost source provenance or observation: %v", err)
	}
	if err := ValidateCrossResult(*scenario, loaded.Result); err != nil {
		t.Fatal(err)
	}
	wrong := cloneScenario(*scenario)
	wrong.Expect.Title = "weakened capture source"
	if _, err := CaptureReplay(wrong, "renderless", loaded.Result, 0, SystemMetadata(), DefaultRedactionConfig(), ""); err == nil {
		t.Fatal("bound manifest accepted a changed source descriptor")
	}
	policy := DefaultRedactionConfig()
	policy.Patterns = []string{"artemis-fixture"}
	if _, err := CaptureReplay(*scenario, "renderless", loaded.Result, 0, SystemMetadata(), policy, ""); err == nil {
		t.Fatal("inert provenance bypassed the configured privacy profile")
	}
}

func TestCorpusReplayKeepsUnreferencedRecordEncodingCompatible(t *testing.T) {
	data, err := json.Marshal(CrossResult{Title: "actual title"})
	if err != nil || bytes.Contains(data, []byte("fixture_manifest")) || bytes.Contains(data, []byte("FixtureManifest")) {
		t.Fatalf("absent optional provenance changed legacy record payload: %s %v", data, err)
	}
}
