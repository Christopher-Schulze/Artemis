package benchmark

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCohortRequiresCompleteExactArtifacts(t *testing.T) {
	cfg := DefaultHarnessConfig()
	cfg.SkipCompetitor, cfg.Iterations, cfg.OutputDir = true, 1, t.TempDir()
	harness := NewHarness(cfg)
	harness.scenarios = DefaultScenarios()[:1]
	sc, err := harness.Run(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	artifacts, err := ReadRawArtifacts(cfg.OutputDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []struct{ name, captured string }{{"scorecard.json", artifacts.ScorecardJSON}, {"scorecard.md", artifacts.ScorecardMD}} {
		data, err := os.ReadFile(filepath.Join(cfg.OutputDir, input.name))
		if err != nil || input.captured != string(data) {
			t.Fatalf("verified bytes not returned for %s: %v", input.name, err)
		}
	}
	if !sc.Honest || sc.Claim.Status != "observed" || !validSHA256(sc.Provenance.ArtemisSHA256) {
		t.Fatalf("valid observation provenance missing: %+v", sc.Claim)
	}
	file, err := os.OpenFile(filepath.Join(cfg.OutputDir, "scorecard.md"), os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := file.WriteString("\nmodified evidence")
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatalf("fixture mutation: %v, %v", writeErr, closeErr)
	}
	if _, err := ReadRawArtifacts(cfg.OutputDir); err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("changed artifact admitted: %v", err)
	}
}

func TestCohortRefusesIncompleteAndEscapingManifest(t *testing.T) {
	dir := t.TempDir()
	if _, err := ReadRawArtifacts(dir); err == nil {
		t.Fatal("unsealed cohort admitted")
	}
	receipt := ArtifactReceipt{Version: "1.0.0", Artifacts: []ArtifactDigest{{Path: "../outside"}, {Path: "scorecard.md"}, {Path: "report.md"}}}
	data, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadRawArtifacts(dir); err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("escaping receipt admitted: %v", err)
	}
}

func TestRelativeOutputAndAbsoluteProfileSealTheSameCohort(t *testing.T) {
	output := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(cwd, output)
	if err != nil {
		t.Fatal(err)
	}
	cfg := DefaultHarnessConfig()
	cfg.OutputDir, cfg.Iterations, cfg.SkipCompetitor = relative, 1, true
	cfg.Profile.MemProfilePath = filepath.Join(output, "heap.pprof")
	h := NewHarness(cfg)
	h.scenarios = DefaultScenarios()[:1]
	if _, err := h.Run(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadRawArtifacts(relative); err != nil {
		t.Fatal(err)
	}
}
