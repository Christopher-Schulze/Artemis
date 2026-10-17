package benchmark

import (
	"context"
	"crypto/sha256"
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
	// harness uses the default local cold/warm matrix.
	Workloads []Workload
	// Profile controls optional pprof output.
	Profile ProfileConfig
	// Timeout supplies the deadline for execution and acquisition contexts.
	Timeout time.Duration
	// OutputFormat records CLI output selection; empty for library-only runs.
	OutputFormat string
	// FixtureResolver supplies currently admitted canonical inputs. A configured
	// authority failure never falls back to standalone fixture production.
	FixtureResolver FixtureResolver
}

// DefaultHarnessConfig returns the default harness configuration.
func DefaultHarnessConfig() HarnessConfig {
	return HarnessConfig{
		Competitor: DefaultCompetitorConfig(),
		OutputDir:  filepath.Join("benchmark", "results"),
		Iterations: 5,
		Timeout:    DefaultTimeout,
	}
}

// Harness runs the full head-to-head benchmark: Artemis vs competitor
// across the versioned scenario matrix.
type Harness struct {
	cfg        HarnessConfig
	scenarios  []Scenario
	competitor *CompetitorRunner
	fixtures   map[string]EncodedFixture
}

// NewHarness creates a harness with the default scenario matrix.
func NewHarness(cfg HarnessConfig) *Harness {
	if cfg.OutputDir == "" {
		cfg.OutputDir = DefaultHarnessConfig().OutputDir
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = DefaultTimeout
	}
	return &Harness{
		cfg:       cfg,
		scenarios: DefaultScenarios(),
	}
}

// Run executes the full benchmark and writes the scorecard.
// It returns the scorecard and any error that prevented completion.
func (h *Harness) Run(ctx context.Context) (scorecard *Scorecard, runErr error) {
	if ctx == nil {
		return nil, fmt.Errorf("benchmark context required")
	}
	if err := ValidateConfig(h.cfg, h.scenarios); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, h.cfg.Timeout)
	defer cancel()
	workloads := append([]Workload(nil), h.cfg.Workloads...)
	if len(workloads) == 0 {
		workloads = WorkloadsWithIterations(h.scenarios, h.cfg.Iterations)
	}
	if err := h.prepareFixtures(ctx, workloads); err != nil {
		return nil, err
	}
	stopProfile, err := StartProfile(h.cfg.Profile)
	if err != nil {
		return nil, err
	}
	profileStopped := false
	defer func() {
		if !profileStopped {
			runErr = errors.Join(runErr, stopProfile())
		}
	}()
	sc := NewScorecard()
	sc.Environment = CurrentEnvironment(h.cfg.BenchmarkTag)
	sc.ArtemisVersion = artemisVersion()
	sc.Provenance, err = currentProvenance()
	if err != nil {
		return sc, fmt.Errorf("artemis provenance: %w", err)
	}
	sc.Scenarios = h.fixtureEvidence()
	sc.Workloads = workloads
	sc.Options = ExecutionOptions{
		Iterations: h.cfg.Iterations, Timeout: h.cfg.Timeout.String(), OutputDir: h.cfg.OutputDir,
		SkipCompetitor: h.cfg.SkipCompetitor, RequireHeadToHead: h.cfg.RequireHeadToHead,
		BenchmarkTag: h.cfg.BenchmarkTag, CPUProfile: h.cfg.Profile.CPUProfilePath,
		MemProfile: h.cfg.Profile.MemProfilePath, RegressionBudgets: h.cfg.RegressionBudgets,
		OutputFormat: h.cfg.OutputFormat,
	}
	competitorConfig := NewCompetitorRunner(h.cfg.Competitor).cfg
	sc.Options.CompetitorDirectory, sc.Options.DownloadOrigin = competitorConfig.BinaryDir, downloadOrigin(competitorConfig.DownloadURL)
	if competitorConfig.DownloadURL != "" {
		sc.Options.DownloadURLSHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte(competitorConfig.DownloadURL)))
	}
	sc.Options.ExpectedSHA256, sc.Options.CompetitorPort = competitorConfig.ExpectedSHA256, competitorConfig.Port
	sc.Options.CompetitorTimeout, sc.Options.MaxDownloadBytes = competitorConfig.Timeout.String(), competitorConfig.MaxDownloadBytes
	sc.Options.VersionArgs = append([]string(nil), competitorConfig.VersionArgs...)
	var competitorErr error
	if !h.cfg.SkipCompetitor {
		sc.Mode = ModeHeadToHead
		h.competitor = NewCompetitorRunner(h.cfg.Competitor)
		defer func() { runErr = errors.Join(runErr, h.competitor.Close()) }()
		competitorErr = h.competitor.EnsureBinary(ctx)
		if competitorErr == nil {
			sc.CompetitorVersion, competitorErr = h.competitor.Version(ctx)
			sc.CompetitorSHA256 = h.competitor.VerifiedSHA256()
		}
	}
	for _, workload := range sc.Workloads {
		if err := ctx.Err(); err != nil {
			return sc, err
		}
		if err := h.executeWorkload(ctx, sc, workload, competitorErr); err != nil {
			return sc, err
		}
	}
	if h.competitor != nil {
		if err := h.competitor.Close(); err != nil {
			return sc, err
		}
	}
	profileStopped = true
	if err := stopProfile(); err != nil {
		return sc, err
	}
	if err := ctx.Err(); err != nil {
		return sc, err
	}
	for _, scenario := range h.scenarios {
		if _, selected := h.fixtures[scenario.ID]; !selected {
			continue
		}
		if _, err := h.fixtureForWorkload(ctx, scenario); err != nil {
			return sc, fmt.Errorf("benchmark publication fixture admission: %w", err)
		}
	}
	sc.Finalize(h.cfg.RegressionBudgets)
	if err := sc.WriteJSON(filepath.Join(h.cfg.OutputDir, "scorecard.json")); err != nil {
		return sc, err
	}
	if err := sc.WriteMarkdown(filepath.Join(h.cfg.OutputDir, "scorecard.md")); err != nil {
		return sc, err
	}
	if err := WriteReport(h.cfg.OutputDir); err != nil {
		return sc, err
	}
	if err := sealArtifacts(ctx, h.cfg, sc); err != nil {
		return sc, err
	}
	if h.cfg.RequireHeadToHead && !sc.Honest {
		return sc, fmt.Errorf("head-to-head required: %s", sc.HonestReason)
	}
	for _, check := range sc.BudgetChecks {
		if !check.OK {
			return sc, fmt.Errorf("regression budget failed: %s", check.Reason)
		}
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
	if info.Main.Sum != "" {
		return info.Main.Sum
	}
	return "development"
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
	if _, err := fmt.Fprintf(os.Stdout, "\nClaim status: %s. Scorecard written to %s\n", sc.Claim.Status, sc.Options.OutputDir); err != nil {
		return fmt.Errorf("write benchmark summary footer: %w", err)
	}
	return nil
}
