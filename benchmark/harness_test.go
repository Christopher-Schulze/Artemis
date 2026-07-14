package benchmark

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/agent"
)

// TestHarnessArtemisOnly runs the harness in Artemis-only mode (no
// competitor download) and verifies it produces a valid scorecard.
// This is the network-free same-TASK test required by the acceptance
// criteria.
func TestHarnessArtemisOnly(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := HarnessConfig{
		OutputDir:      tmpDir,
		Iterations:     3,
		SkipCompetitor: true,
	}

	h := NewHarness(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	sc, err := h.Run(ctx)
	if err != nil {
		t.Fatalf("harness run: %v", err)
	}

	if sc == nil {
		t.Fatal("scorecard is nil")
	}

	if sc.MatrixVersion != ScenarioMatrixVersion {
		t.Errorf("matrix version = %s, want %s", sc.MatrixVersion, ScenarioMatrixVersion)
	}

	// Should have results for all scenarios (Artemis side only)
	scenarios := DefaultScenarios()
	artemisResults := 0
	for _, r := range sc.Results {
		if r.Engine == EngineArtemis {
			artemisResults++
		}
	}
	if artemisResults != len(scenarios) {
		t.Errorf("artemis results = %d, want %d", artemisResults, len(scenarios))
	}

	// All Artemis results should be OK
	for _, r := range sc.Results {
		if r.Engine != EngineArtemis {
			continue
		}
		if !r.OK {
			t.Errorf("scenario %s: artemis not OK: %s", r.ScenarioID, r.Error)
		}
		if r.WallMs <= 0 {
			t.Errorf("scenario %s: wall ms = %.3f, want > 0", r.ScenarioID, r.WallMs)
		}
	}

	// Scorecard files should exist
	jsonPath := filepath.Join(tmpDir, "scorecard.json")
	mdPath := filepath.Join(tmpDir, "scorecard.md")

	if _, err := os.Stat(jsonPath); err != nil {
		t.Errorf("scorecard.json not written: %v", err)
	}
	if _, err := os.Stat(mdPath); err != nil {
		t.Errorf("scorecard.md not written: %v", err)
	}

	// Verify JSON is valid by reading it back
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("read scorecard.json: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("scorecard.json is empty")
	}

	// Verify markdown contains expected sections
	mdData, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("read scorecard.md: %v", err)
	}
	mdStr := string(mdData)
	if !contains(mdStr, "# Artemis Benchmark Scorecard") {
		t.Error("scorecard.md missing title")
	}
	if !contains(mdStr, "## Summary") {
		t.Error("scorecard.md missing summary")
	}
	if !contains(mdStr, "## Per-Scenario Results") {
		t.Error("scorecard.md missing per-scenario results")
	}
}

func TestHarnessCompetitorUnavailable(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := HarnessConfig{
		OutputDir:      tmpDir,
		Iterations:     2,
		SkipCompetitor: false,
		Competitor: CompetitorConfig{
			BinaryDir:   filepath.Join(tmpDir, "bin"),
			DownloadURL: "", // no download URL -> unavailable
			Port:        19222,
			Timeout:     5 * time.Second,
		},
	}

	h := NewHarness(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	sc, err := h.Run(ctx)
	if err != nil {
		t.Fatalf("harness run: %v", err)
	}

	// Should have Artemis results (OK) and competitor results (not OK)
	artemisOK := 0
	competitorErrors := 0
	for _, r := range sc.Results {
		if r.Engine == EngineArtemis && r.OK {
			artemisOK++
		}
		if r.Engine == EngineCompetitor && !r.OK && r.Error != "" {
			competitorErrors++
		}
	}

	scenarios := DefaultScenarios()
	if artemisOK != len(scenarios) {
		t.Errorf("artemis OK = %d, want %d", artemisOK, len(scenarios))
	}
	if competitorErrors != len(scenarios) {
		t.Errorf("competitor errors = %d, want %d", competitorErrors, len(scenarios))
	}
}

func TestScenarioMatrix(t *testing.T) {
	scenarios := DefaultScenarios()

	// Verify all scenarios have required fields
	ids := map[string]bool{}
	for _, s := range scenarios {
		if s.ID == "" {
			t.Error("scenario has empty ID")
		}
		if ids[s.ID] {
			t.Errorf("duplicate scenario ID: %s", s.ID)
		}
		ids[s.ID] = true

		if s.HTML == "" {
			t.Errorf("scenario %s has empty HTML", s.ID)
		}
		if s.Description == "" {
			t.Errorf("scenario %s has empty description", s.ID)
		}
		if s.Kind == "" {
			t.Errorf("scenario %s has empty kind", s.ID)
		}
		if s.ExpectTitle == "" {
			t.Errorf("scenario %s has empty expectTitle", s.ID)
		}
	}

	// Verify all 5 kinds are represented
	kinds := map[ScenarioKind]bool{}
	for _, s := range scenarios {
		kinds[s.Kind] = true
	}
	expectedKinds := []ScenarioKind{KindNavigation, KindScrape, KindDOM, KindMarkdown, KindScriptHeavy}
	for _, k := range expectedKinds {
		if !kinds[k] {
			t.Errorf("missing scenario kind: %s", k)
		}
	}
}

func TestScenarioByID(t *testing.T) {
	s := ScenarioByID("nav-001")
	if s == nil {
		t.Fatal("ScenarioByID(nav-001) returned nil")
	}
	if s.Kind != KindNavigation {
		t.Errorf("kind = %s, want %s", s.Kind, KindNavigation)
	}

	if s := ScenarioByID("nonexistent"); s != nil {
		t.Error("ScenarioByID(nonexistent) should return nil")
	}
}

func TestScorecardSummary(t *testing.T) {
	sc := NewScorecard()
	sc.Mode = ModeHeadToHead
	sc.AddResult(ScenarioResult{ScenarioID: "s1", Engine: EngineArtemis, WallMs: 1.0, OK: true})
	sc.AddResult(ScenarioResult{ScenarioID: "s1", Engine: EngineCompetitor, WallMs: 2.0, OK: true})
	sc.AddResult(ScenarioResult{ScenarioID: "s2", Engine: EngineArtemis, WallMs: 0.5, OK: true})
	sc.AddResult(ScenarioResult{ScenarioID: "s2", Engine: EngineCompetitor, WallMs: 3.0, OK: true})

	summaries := sc.Summarize()
	if len(summaries) != 2 {
		t.Fatalf("summaries = %d, want 2", len(summaries))
	}

	for _, su := range summaries {
		if su.Engine == EngineArtemis {
			if su.Wins != 2 {
				t.Errorf("artemis wins = %d, want 2", su.Wins)
			}
			if su.Losses != 0 {
				t.Errorf("artemis losses = %d, want 0", su.Losses)
			}
		}
		if su.Engine == EngineCompetitor {
			if su.Wins != 0 {
				t.Errorf("competitor wins = %d, want 0", su.Wins)
			}
			if su.Losses != 2 {
				t.Errorf("competitor losses = %d, want 2", su.Losses)
			}
		}
	}
}

func TestScorecardWriteFiles(t *testing.T) {
	tmpDir := t.TempDir()
	sc := NewScorecard()
	sc.AddResult(ScenarioResult{ScenarioID: "s1", Engine: EngineArtemis, WallMs: 1.0, OK: true})

	jsonPath := filepath.Join(tmpDir, "scorecard.json")
	mdPath := filepath.Join(tmpDir, "scorecard.md")

	if err := sc.WriteJSON(jsonPath); err != nil {
		t.Fatalf("write json: %v", err)
	}
	if err := sc.WriteMarkdown(mdPath); err != nil {
		t.Fatalf("write md: %v", err)
	}

	if _, err := os.Stat(jsonPath); err != nil {
		t.Errorf("json not written: %v", err)
	}
	if _, err := os.Stat(mdPath); err != nil {
		t.Errorf("md not written: %v", err)
	}
}

func TestMedianResult(t *testing.T) {
	results := []ScenarioResult{
		{WallMs: 3.0},
		{WallMs: 1.0},
		{WallMs: 2.0},
	}
	median := medianResult(results)
	if median.WallMs != 2.0 {
		t.Errorf("median = %.3f, want 2.0", median.WallMs)
	}

	// Even number of results: lower middle
	results = []ScenarioResult{
		{WallMs: 4.0},
		{WallMs: 1.0},
		{WallMs: 3.0},
		{WallMs: 2.0},
	}
	median = medianResult(results)
	if median.WallMs != 2.0 {
		t.Errorf("median = %.3f, want 2.0", median.WallMs)
	}
}

func TestCompetitorConfigDefaults(t *testing.T) {
	cfg := DefaultCompetitorConfig()
	if cfg.Port == 0 {
		t.Error("default port should not be 0")
	}
	if cfg.Timeout == 0 {
		t.Error("default timeout should not be 0")
	}
	if cfg.BinaryDir == "" {
		t.Error("default binary dir should not be empty")
	}
}

func TestCurrentEnvironment(t *testing.T) {
	env := CurrentEnvironment("warm")
	if env.GoVersion == "" {
		t.Error("GoVersion is empty")
	}
	if env.OS == "" {
		t.Error("OS is empty")
	}
	if env.Arch == "" {
		t.Error("Arch is empty")
	}
	if env.NumCPU <= 0 {
		t.Errorf("NumCPU = %d, want > 0", env.NumCPU)
	}
	if env.ScenarioMatrix != ScenarioMatrixVersion {
		t.Errorf("ScenarioMatrix = %s, want %s", env.ScenarioMatrix, ScenarioMatrixVersion)
	}
	if env.BenchmarkTag != "warm" {
		t.Errorf("BenchmarkTag = %s, want warm", env.BenchmarkTag)
	}
}

func TestScorecardHonestyHeadToHead(t *testing.T) {
	sc := NewScorecard()
	sc.Mode = ModeHeadToHead
	sc.Honest = true
	sc.AddResult(ScenarioResult{ScenarioID: "s1", Engine: EngineArtemis, WallMs: 1.0, OK: true})
	sc.AddResult(ScenarioResult{ScenarioID: "s1", Engine: EngineCompetitor, WallMs: 2.0, OK: true})

	su := sc.Summarize()
	if len(su) != 2 {
		t.Fatalf("summaries = %d, want 2", len(su))
	}
	for _, s := range su {
		if s.Engine == EngineArtemis && s.Wins != 1 {
			t.Errorf("artemis wins = %d, want 1", s.Wins)
		}
		if s.Engine == EngineCompetitor && s.Losses != 1 {
			t.Errorf("competitor losses = %d, want 1", s.Losses)
		}
	}
}

func TestScorecardHonestyArtemisOnlyNoWins(t *testing.T) {
	sc := NewScorecard()
	sc.Mode = ModeArtemisOnly
	sc.Honest = true
	sc.AddResult(ScenarioResult{ScenarioID: "s1", Engine: EngineArtemis, WallMs: 1.0, OK: true})
	sc.AddResult(ScenarioResult{ScenarioID: "s1", Engine: EngineCompetitor, WallMs: 2.0, OK: true})

	su := sc.Summarize()
	for _, s := range su {
		if s.Wins != 0 || s.Losses != 0 {
			t.Errorf("wins/losses should be 0 in artemis-only mode, got wins=%d losses=%d", s.Wins, s.Losses)
		}
	}
}

func TestHarnessRequireHeadToHeadFails(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := HarnessConfig{
		OutputDir:         tmpDir,
		Iterations:        2,
		SkipCompetitor:    false,
		RequireHeadToHead: true,
		Competitor: CompetitorConfig{
			BinaryDir:   filepath.Join(tmpDir, "bin"),
			DownloadURL: "",
			Port:        19222,
			Timeout:     5 * time.Second,
		},
	}

	h := NewHarness(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	sc, err := h.Run(ctx)
	if err == nil {
		t.Fatal("expected error when head-to-head is required and competitor unavailable")
	}
	if sc == nil {
		t.Fatal("scorecard is nil")
	}
	if sc.Mode != ModeHeadToHead {
		t.Errorf("mode = %s, want %s", sc.Mode, ModeHeadToHead)
	}
	if sc.Honest {
		t.Error("scorecard should be dishonest")
	}
}

func TestValidateScenario(t *testing.T) {
	s := Scenario{
		ExpectTitle:      "Home",
		ExpectLinks:      2,
		ExpectParagraphs: 1,
	}
	if !validateScenario(s, "Home", []agent.Link{{Href: "/a"}, {Href: "/b"}}, "hello world") {
		t.Error("expected valid")
	}
	if validateScenario(s, "Wrong", []agent.Link{{Href: "/a"}, {Href: "/b"}}, "hello world") {
		t.Error("expected invalid title")
	}
	if validateScenario(s, "Home", []agent.Link{{Href: "/a"}}, "hello world") {
		t.Error("expected invalid links count")
	}
	if validateScenario(s, "Home", []agent.Link{{Href: "/a"}, {Href: "/b"}}, "") {
		t.Error("expected invalid paragraphs count")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && indexOf(s, substr) >= 0
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
