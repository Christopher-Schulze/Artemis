package benchmark

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestArtifactPublicationPreservesExistingBytes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "scorecard.json")
	original := []byte("retained evidence")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeArtifact(path, []byte("replacement")); err == nil {
		t.Fatal("existing artifact replaced")
	}
	actual, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(original, actual) {
		t.Fatal("original evidence lost")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("publication residue: %v, %v", entries, err)
	}
}

func TestReportRecomputesClaimsAndIsDeterministic(t *testing.T) {
	sc := comparisonScorecard(t, 1)
	sc.Samples = sc.Samples[1:]
	sc.Honest = true
	payload, err := json.Marshal(sc)
	if err != nil {
		t.Fatal(err)
	}
	artifacts := RawArtifacts{ScorecardJSON: string(payload)}
	first, err := GenerateReport(artifacts)
	if err != nil {
		t.Fatal(err)
	}
	second, err := GenerateReport(artifacts)
	if err != nil || first != second {
		t.Fatal("same raw proof produced nondeterministic report")
	}
	if !bytes.Contains([]byte(first), []byte("honest comparison: false")) {
		t.Fatal("report trusted forged honest flag")
	}
}
