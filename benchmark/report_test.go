package benchmark

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteReport(t *testing.T) {
	tmpDir := t.TempDir()
	sc := NewScorecard()
	sc.Environment = CurrentEnvironment("warm")
	sc.AddResult(ScenarioResult{ScenarioID: "s1", Engine: EngineArtemis, WallMs: 1.0, OK: true, Validated: true, Throughput: 1.0})
	if err := sc.WriteJSON(filepath.Join(tmpDir, "scorecard.json")); err != nil {
		t.Fatalf("write json: %v", err)
	}
	if err := sc.WriteMarkdown(filepath.Join(tmpDir, "scorecard.md")); err != nil {
		t.Fatalf("write md: %v", err)
	}
	if err := WriteReport(tmpDir); err != nil {
		t.Fatalf("write report: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(tmpDir, "report.md"))
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	if !strings.Contains(string(data), "# Artemis Benchmark Report") {
		t.Error("report missing title")
	}
	if !strings.Contains(string(data), "warm") {
		t.Error("report missing benchmark tag")
	}
	if !strings.Contains(string(data), "Per-Metric Aggregates") {
		t.Error("report missing per-metric aggregates section")
	}
}
