package benchmark

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
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
	// RequireHeadToHead makes the harness fail if a competitor run was
	// expected but could not be completed honestly.
	RequireHeadToHead bool
	// BenchmarkTag annotates the environment manifest (e.g. "cold", "warm").
	BenchmarkTag string
	// RegressionBudgets are evaluated when the scorecard is finalized.
	RegressionBudgets []RegressionBudget
	// Workloads defines the workload combinations to run. If empty, the
	// harness uses a single default workload.
	Workloads []Workload
	// Profile controls optional pprof output.
	Profile ProfileConfig
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
func (h *Harness) Run(ctx context.Context) (scorecard *Scorecard, runErr error) {
	stopProfile, err := StartProfile(h.cfg.Profile)
	if err != nil {
		return nil, err
	}
	defer func() {
		if stopErr := stopProfile(); stopErr != nil {
			runErr = errors.Join(runErr, fmt.Errorf("stop profile: %w", stopErr))
		}
	}()

	sc := NewScorecard()
	sc.Environment = CurrentEnvironment(h.cfg.BenchmarkTag)
	sc.ArtemisVersion = artemisVersion()

	workloads := h.cfg.Workloads
	if len(workloads) == 0 {
		workloads = DefaultWorkloads(h.scenarios)
	}

	// Run Artemis side
	for _, w := range workloads {
		s := scenarioByID(h.scenarios, w.ScenarioID)
		if s == nil {
			continue
		}
		s.EngineMode = w.EngineMode

		artemis := NewArtemisRunner(h.scenarios, w.Warmth == WarmthWarm)
		if w.Warmth == WarmthWarm {
			// Warm reuse: share the same runner across warm workloads
			if h.artemis == nil {
				h.artemis = artemis
			} else {
				if closeErr := artemis.Close(); closeErr != nil {
					return sc, fmt.Errorf("close superseded Artemis runner: %w", closeErr)
				}
				artemis = h.artemis
			}
		}
		if w.Warmth == WarmthCold {
			if h.artemis != nil {
				if closeErr := h.artemis.Close(); closeErr != nil {
					h.artemis = nil
					return sc, fmt.Errorf("close warm Artemis runner: %w", closeErr)
				}
				h.artemis = nil
			}
			h.artemis = artemis
		}
		if w.Locality == LocalityNetwork {
			closeErr := artemis.Close()
			h.artemis = nil
			return sc, errors.Join(fmt.Errorf("network locality not supported in this harness"), closeErr)
		}

		results := make([]ScenarioResult, 0, w.Iterations)
		for i := 0; i < w.Iterations; i++ {
			r := artemis.RunScenario(ctx, *s)
			r.Workload = w.Key()
			results = append(results, r)
		}
		median := medianResult(results)
		sc.AddResult(median)
		if w.Warmth == WarmthCold {
			if closeErr := artemis.Close(); closeErr != nil {
				h.artemis = nil
				return sc, fmt.Errorf("close cold Artemis runner: %w", closeErr)
			}
			h.artemis = nil
		}
	}
	defer func() {
		if h.artemis != nil {
			if closeErr := h.artemis.Close(); closeErr != nil {
				runErr = errors.Join(runErr, fmt.Errorf("close Artemis runner: %w", closeErr))
			}
			h.artemis = nil
		}
	}()

	// Run competitor side (if not skipped and binary available)
	if !h.cfg.SkipCompetitor {
		sc.Mode = ModeHeadToHead
		h.competitor = NewCompetitorRunner(h.cfg.Competitor)
		defer func() {
			if closeErr := h.competitor.Close(); closeErr != nil {
				runErr = errors.Join(runErr, fmt.Errorf("close competitor runner: %w", closeErr))
			}
		}()

		honestReason := ""
		if err := h.competitor.EnsureBinary(ctx); err != nil {
			// Report honestly: competitor unavailable
			honestReason = fmt.Sprintf("competitor unavailable: %v", err)
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
			if v, err := h.competitor.Version(); err == nil {
				sc.CompetitorVersion = v
			}
			if err := h.competitor.Start(ctx); err != nil {
				honestReason = fmt.Sprintf("competitor start failed: %v", err)
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

		// Determine honesty after competitor side
		if honestReason != "" {
			sc.Honest = false
			sc.HonestReason = honestReason
		} else {
			artemisOK := 0
			competitorOK := 0
			for _, r := range sc.Results {
				if r.OK {
					if r.Engine == EngineArtemis {
						artemisOK++
					} else {
						competitorOK++
					}
				}
			}
			if artemisOK == len(h.scenarios) && competitorOK == len(h.scenarios) {
				sc.Honest = true
				sc.HonestReason = "both engines completed all scenarios"
			} else {
				sc.Honest = false
				sc.HonestReason = fmt.Sprintf("incomplete results: artemis ok=%d, competitor ok=%d, expected=%d", artemisOK, competitorOK, len(h.scenarios))
			}
		}

	}

	sc.Finalize(h.cfg.RegressionBudgets)

	// Write scorecard
	jsonPath := filepath.Join(h.cfg.OutputDir, "scorecard.json")
	mdPath := filepath.Join(h.cfg.OutputDir, "scorecard.md")

	if err := sc.WriteJSON(jsonPath); err != nil {
		return sc, fmt.Errorf("write json scorecard: %w", err)
	}
	if err := sc.WriteMarkdown(mdPath); err != nil {
		return sc, fmt.Errorf("write md scorecard: %w", err)
	}

	if err := WriteReport(h.cfg.OutputDir); err != nil {
		return sc, fmt.Errorf("write report: %w", err)
	}

	if h.cfg.RequireHeadToHead && !sc.Honest {
		return sc, fmt.Errorf("head-to-head required but scorecard is not honest: %s", sc.HonestReason)
	}

	return sc, nil
}

func artemisVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	if info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return info.Main.Sum
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
func PrintSummary(sc *Scorecard) error {
	summaries := sc.Summarize()
	if _, err := fmt.Fprintf(os.Stdout, "\n=== Benchmark Summary ===\n"); err != nil {
		return fmt.Errorf("write benchmark summary header: %w", err)
	}
	for _, su := range summaries {
		if _, err := fmt.Fprintf(os.Stdout, "  %s: %d scenarios, %d wins, %d losses, %d errors, avg %.2f ms\n",
			su.Engine, su.Scenarios, su.Wins, su.Losses, su.Errors, su.AvgMs); err != nil {
			return fmt.Errorf("write benchmark summary: %w", err)
		}
	}
	if _, err := fmt.Fprintf(os.Stdout, "\nScorecard written to benchmark/results/\n"); err != nil {
		return fmt.Errorf("write benchmark summary footer: %w", err)
	}
	return nil
}
