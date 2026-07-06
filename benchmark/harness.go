package benchmark

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// HarnessConfig controls the benchmark harness behavior.
type HarnessConfig struct {
	// Competitor controls the competitor binary download and execution.
	Competitor CompetitorConfig
	// OutputDir is where scorecard files are written.
	// Default: benchmark/results
	OutputDir string
	// Iterations is the number of times each scenario is run.
	// The median wall time is recorded. Default: 5.
	Iterations int
	// SkipCompetitor disables the competitor side entirely.
	// Use this to run Artemis-only benchmarks.
	SkipCompetitor bool
}

// DefaultHarnessConfig returns the default harness configuration.
func DefaultHarnessConfig() HarnessConfig {
	return HarnessConfig{
		Competitor: DefaultCompetitorConfig(),
		OutputDir:  filepath.Join("benchmark", "results"),
		Iterations: 5,
	}
}

// Harness runs the full head-to-head benchmark: Artemis vs competitor
// across the versioned scenario matrix.
type Harness struct {
	cfg        HarnessConfig
	scenarios  []Scenario
	artemis    *ArtemisRunner
	competitor *CompetitorRunner
}

// NewHarness creates a harness with the default scenario matrix.
func NewHarness(cfg HarnessConfig) *Harness {
	if cfg.OutputDir == "" {
		cfg = DefaultHarnessConfig()
	}
	if cfg.Iterations <= 0 {
		cfg.Iterations = 5
	}
	return &Harness{
		cfg:       cfg,
		scenarios: DefaultScenarios(),
	}
}

// Run executes the full benchmark and writes the scorecard.
// It returns the scorecard and any error that prevented completion.
func (h *Harness) Run(ctx context.Context) (*Scorecard, error) {
	sc := NewScorecard()

	// Set up Artemis runner
	h.artemis = NewArtemisRunner(h.scenarios)
	defer h.artemis.Close()

	// Run Artemis side
	for _, s := range h.scenarios {
		results := make([]ScenarioResult, 0, h.cfg.Iterations)
		for i := 0; i < h.cfg.Iterations; i++ {
			r := h.artemis.RunScenario(ctx, s)
			results = append(results, r)
		}
		median := medianResult(results)
		sc.AddResult(median)
	}

	// Run competitor side (if not skipped and binary available)
	if !h.cfg.SkipCompetitor {
		h.competitor = NewCompetitorRunner(h.cfg.Competitor)
		defer h.competitor.Close()

		if err := h.competitor.EnsureBinary(ctx); err != nil {
			// Report honestly: competitor unavailable
			for _, s := range h.scenarios {
				sc.AddResult(ScenarioResult{
					ScenarioID: s.ID,
					Engine:     EngineCompetitor,
					Timestamp:  time.Now().UTC(),
					OK:         false,
					Error:      err.Error(),
				})
			}
		} else {
			if err := h.competitor.Start(ctx); err != nil {
				for _, s := range h.scenarios {
					sc.AddResult(ScenarioResult{
						ScenarioID: s.ID,
						Engine:     EngineCompetitor,
						Timestamp:  time.Now().UTC(),
						OK:         false,
						Error:      fmt.Sprintf("competitor start: %v", err),
					})
				}
			} else {
				for _, s := range h.scenarios {
					scenarioURL := h.artemis.BaseURL() + "/" + s.ID
					results := make([]ScenarioResult, 0, h.cfg.Iterations)
					for i := 0; i < h.cfg.Iterations; i++ {
						r := h.competitor.RunScenario(ctx, s, scenarioURL)
						results = append(results, r)
					}
					median := medianResult(results)
					sc.AddResult(median)
				}
			}
		}
	}

	// Write scorecard
	jsonPath := filepath.Join(h.cfg.OutputDir, "scorecard.json")
	mdPath := filepath.Join(h.cfg.OutputDir, "scorecard.md")

	if err := sc.WriteJSON(jsonPath); err != nil {
		return sc, fmt.Errorf("write json scorecard: %w", err)
	}
	if err := sc.WriteMarkdown(mdPath); err != nil {
		return sc, fmt.Errorf("write md scorecard: %w", err)
	}

	return sc, nil
}

// medianResult returns the result with the median wall time from a
// slice of results. If the slice is empty, a zero-value result is
// returned.
func medianResult(results []ScenarioResult) ScenarioResult {
	if len(results) == 0 {
		return ScenarioResult{}
	}
	if len(results) == 1 {
		return results[0]
	}

	// Copy and sort by wall time
	sorted := make([]ScenarioResult, len(results))
	copy(sorted, results)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j].WallMs < sorted[j-1].WallMs; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}

	mid := (len(sorted) - 1) / 2
	return sorted[mid]
}

// PrintSummary writes a human-readable summary to stdout.
func PrintSummary(sc *Scorecard) {
	summaries := sc.Summarize()
	fmt.Fprintf(os.Stdout, "\n=== Benchmark Summary ===\n")
	for _, su := range summaries {
		fmt.Fprintf(os.Stdout, "  %s: %d scenarios, %d wins, %d losses, %d errors, avg %.2f ms\n",
			su.Engine, su.Scenarios, su.Wins, su.Losses, su.Errors, su.AvgMs)
	}
	fmt.Fprintf(os.Stdout, "\nScorecard written to benchmark/results/\n")
}
