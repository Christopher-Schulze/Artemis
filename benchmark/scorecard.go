package benchmark

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sort"
	"time"
)

// EngineName identifies which engine produced a result.
type EngineName string

const (
	EngineArtemis    EngineName = "artemis"
	EngineCompetitor EngineName = "lightpanda"
)

// CurrentEnvironment returns the platform and software environment manifest
// for a benchmark run. The benchmarkTag is a caller-supplied discriminator
// (e.g. "cold", "warm", "renderless", "chromium").
func CurrentEnvironment(benchmarkTag string) Environment {
	host, _ := os.Hostname()
	return Environment{
		GoVersion:      runtime.Version(),
		OS:             runtime.GOOS,
		Arch:           runtime.GOARCH,
		NumCPU:         runtime.NumCPU(),
		ScenarioMatrix: ScenarioMatrixVersion,
		BenchmarkTag:   benchmarkTag,
		Host:           host,
	}
}

// ScenarioResult is the measured outcome of running one engine against
// one scenario. Times are wall-clock; memory is peak RSS delta if
// available, otherwise total bytes allocated.
type ScenarioResult struct {
	ScenarioID    string             `json:"scenarioId"`
	Workload      string             `json:"workload,omitempty"`
	Engine        EngineName         `json:"engine"`
	EngineMode    string             `json:"engineMode"`
	WallMs        float64            `json:"wallMs"`
	CPUMs         float64            `json:"cpuMs"`
	AllocBytes    int64              `json:"allocBytes"`
	AllocCount    int64              `json:"allocCount"`
	RSSBytes      int64              `json:"rssBytes"`
	Throughput    float64            `json:"throughput"`
	OK            bool               `json:"ok"`
	Validated     bool               `json:"validated"`
	Validation    ValidationEvidence `json:"validation"`
	Error         string             `json:"error,omitempty"`
	Timestamp     time.Time          `json:"timestamp"`
	Execution     Workload           `json:"execution"`
	Sample        int                `json:"sample"`
	SampleCount   int                `json:"sampleCount"`
	FailedSamples int                `json:"failedSamples"`
}

// ScorecardMode reports whether the scorecard can be compared fairly.
type ScorecardMode string

const (
	ModeHeadToHead  ScorecardMode = "head-to-head"
	ModeArtemisOnly ScorecardMode = "artemis-only"
	ModeError       ScorecardMode = "error"
)

// Environment captures the software/hardware context for reproducibility.
type Environment struct {
	GoVersion      string `json:"goVersion"`
	OS             string `json:"os"`
	Arch           string `json:"arch"`
	NumCPU         int    `json:"numCPU"`
	ScenarioMatrix string `json:"scenarioMatrix"`
	BenchmarkTag   string `json:"benchmarkTag"`
	Host           string `json:"host"`
}

// Scorecard is the full head-to-head result set across all scenarios.
type Scorecard struct {
	Version           string             `json:"version"`
	MatrixVersion     string             `json:"matrixVersion"`
	Date              time.Time          `json:"date"`
	Host              string             `json:"host"`
	Environment       Environment        `json:"environment"`
	Mode              ScorecardMode      `json:"mode"`
	Honest            bool               `json:"honest"`
	HonestReason      string             `json:"honestReason,omitempty"`
	ArtemisVersion    string             `json:"artemisVersion,omitempty"`
	CompetitorVersion string             `json:"competitorVersion,omitempty"`
	Results           []ScenarioResult   `json:"results"`
	Aggregates        []MetricAggregate  `json:"aggregates,omitempty"`
	BudgetChecks      []BudgetCheck      `json:"budgetChecks,omitempty"`
	Options           ExecutionOptions   `json:"options"`
	Workloads         []Workload         `json:"workloads"`
	Samples           []ScenarioResult   `json:"samples"`
	Priming           []ScenarioResult   `json:"priming"`
	CompetitorSHA256  string             `json:"competitorSHA256,omitempty"`
	Provenance        RunProvenance      `json:"provenance"`
	Scenarios         []ScenarioEvidence `json:"scenarios"`
	Claim             ClaimEvidence      `json:"claim"`
}

// NewScorecard creates an empty scorecard with the current timestamp.
func NewScorecard() *Scorecard {
	host, _ := os.Hostname()
	return &Scorecard{
		Version:       "2.0.0",
		MatrixVersion: ScenarioMatrixVersion,
		Date:          time.Now().UTC(),
		Host:          host,
		Environment:   CurrentEnvironment(""),
		Mode:          ModeArtemisOnly,
		Honest:        false,
		HonestReason:  "no executed observations",
		Results:       []ScenarioResult{},
	}
}

// AddResult appends a scenario result to the scorecard.
func (s *Scorecard) AddResult(r ScenarioResult) {
	s.Results = append(s.Results, r)
}

// ToMetricSet returns the result as a MetricSet.
func (r ScenarioResult) ToMetricSet() MetricSet {
	return MetricSet{
		WallMs:     r.WallMs,
		CPUMs:      r.CPUMs,
		AllocBytes: r.AllocBytes,
		AllocCount: r.AllocCount,
		RSSBytes:   r.RSSBytes,
		Throughput: r.Throughput,
		ErrorRate:  MetricErrorRateFromBool(r.OK && r.Validated),
	}
}

// Summary returns per-engine aggregate stats.
type EngineSummary struct {
	Engine    EngineName `json:"engine"`
	TotalMs   float64    `json:"totalMs"`
	AvgMs     float64    `json:"avgMs"`
	Wins      int        `json:"wins"`
	Losses    int        `json:"losses"`
	Errors    int        `json:"errors"`
	Scenarios int        `json:"scenarios"`
}

// MetricAggregate captures statistics per metric kind per engine.
type MetricAggregate struct {
	Engine    EngineName `json:"engine"`
	Kind      MetricKind `json:"kind"`
	Aggregate `json:"aggregate"`
}

// BudgetCheck records a regression budget evaluation.
type BudgetCheck struct {
	Workload string     `json:"workload,omitempty"`
	Kind     MetricKind `json:"kind"`
	Engine   EngineName `json:"engine"`
	Baseline float64    `json:"baseline"`
	Current  float64    `json:"current"`
	OK       bool       `json:"ok"`
	Reason   string     `json:"reason,omitempty"`
}

// Summarize computes per-engine aggregate stats and win/loss counts.
// Win/loss counts are only reported when the scorecard mode is head-to-head
// and honest; otherwise all wins/losses are zero.
func (s *Scorecard) Summarize() []EngineSummary {
	byEngine := map[EngineName]*EngineSummary{}

	for _, r := range s.Results {
		if _, ok := byEngine[r.Engine]; !ok {
			byEngine[r.Engine] = &EngineSummary{Engine: r.Engine}
		}
		su := byEngine[r.Engine]
		su.Scenarios++
		if !r.OK || !r.Validated {
			su.Errors++
			continue
		}
		su.TotalMs += r.WallMs
	}

	pairs, pairErr := s.comparisonPairs()
	if s.Honest && pairErr == nil {
		for _, pair := range pairs {
			if pair.artemis.WallMs == pair.competitor.WallMs {
				continue
			}
			winner, loser := EngineArtemis, EngineCompetitor
			if pair.competitor.WallMs < pair.artemis.WallMs {
				winner, loser = loser, winner
			}
			byEngine[winner].Wins++
			byEngine[loser].Losses++
		}
	}

	for _, su := range byEngine {
		completed := su.Scenarios - su.Errors
		if completed > 0 {
			su.AvgMs = su.TotalMs / float64(completed)
		}
	}

	out := make([]EngineSummary, 0, len(byEngine))
	for _, su := range byEngine {
		out = append(out, *su)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Engine < out[j].Engine })
	return out
}

// ComputeAggregates computes per-engine, per-metric statistics from the results.
func (s *Scorecard) ComputeAggregates() []MetricAggregate {
	byEngineMetric := map[EngineName]map[MetricKind][]float64{}
	for _, r := range s.Samples {
		if _, ok := byEngineMetric[r.Engine]; !ok {
			byEngineMetric[r.Engine] = map[MetricKind][]float64{}
		}
		for _, sample := range r.ToMetricSet().ToSamples() {
			if sample.Kind != MetricErrorRate && (!r.OK || !r.Validated) {
				continue
			}
			if !r.metricAvailable(sample.Kind) {
				continue
			}
			byEngineMetric[r.Engine][sample.Kind] = append(byEngineMetric[r.Engine][sample.Kind], sample.Value)
		}
	}

	out := []MetricAggregate{}
	engines := make([]EngineName, 0, len(byEngineMetric))
	for e := range byEngineMetric {
		engines = append(engines, e)
	}
	sort.Slice(engines, func(i, j int) bool { return engines[i] < engines[j] })
	for _, e := range engines {
		kinds := make([]MetricKind, 0, len(byEngineMetric[e]))
		for k := range byEngineMetric[e] {
			kinds = append(kinds, k)
		}
		sort.Slice(kinds, func(i, j int) bool { return kinds[i] < kinds[j] })
		for _, k := range kinds {
			agg := ComputeAggregate(byEngineMetric[e][k])
			out = append(out, MetricAggregate{Engine: e, Kind: k, Aggregate: agg})
		}
	}
	return out
}

// CheckBudgets evaluates the scorecard against the provided regression budgets.
// For head-to-head scorecards, the competitor engine is the baseline for Artemis.
func (s *Scorecard) CheckBudgets(budgets []RegressionBudget) []BudgetCheck {
	checks := []BudgetCheck{}
	if len(budgets) == 0 {
		return checks
	}
	pairs, err := s.comparisonPairs()
	if err != nil || !s.Honest {
		for _, budget := range budgets {
			checks = append(checks, BudgetCheck{Kind: budget.MetricKind, Engine: EngineArtemis, Reason: "eligible paired baseline required"})
		}
		return checks
	}
	for _, budget := range budgets {
		for _, pair := range pairs {
			check := BudgetCheck{Kind: budget.MetricKind, Engine: EngineArtemis, Workload: pair.workload.Identity()}
			if !pair.artemis.metricAvailable(budget.MetricKind) || !pair.competitor.metricAvailable(budget.MetricKind) {
				check.Reason = "metric unavailable for equivalent engine measurements"
			} else {
				check.Baseline = pair.competitor.ToMetricSet().sampleValue(budget.MetricKind)
				check.Current = pair.artemis.ToMetricSet().sampleValue(budget.MetricKind)
				check.OK, check.Reason = budget.Check(check.Baseline, check.Current)
			}
			checks = append(checks, check)
		}
	}
	return checks
}

// Finalize computes aggregates and evaluates regression budgets. Call before
// writing a scorecard to ensure all derived fields are populated.
func (s *Scorecard) Finalize(budgets []RegressionBudget) {
	s.Options.RegressionBudgets = append([]RegressionBudget(nil), budgets...)
	if s.Mode == ModeHeadToHead {
		_, err := s.comparisonPairs()
		s.Honest = err == nil
		s.HonestReason = "all workloads have matching semantic evidence and complete paired samples"
		if err != nil {
			s.HonestReason = err.Error()
		}
	} else {
		err := s.validateArtemisObservations()
		s.Honest = err == nil
		s.HonestReason = "Artemis-only observations; no comparative claim"
		if err != nil {
			s.HonestReason = err.Error()
		}
	}
	s.Aggregates = s.ComputeAggregates()
	s.BudgetChecks = s.CheckBudgets(budgets)
	s.Claim = s.claimEvidence()
}

// WriteJSON writes the scorecard as JSON to the given path.
func (s *Scorecard) WriteJSON(path string) error {
	s.Finalize(s.Options.RegressionBudgets)
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("scorecard json: %w", err)
	}
	return writeArtifact(path, data)
}

// WriteMarkdown writes the scorecard as a human-readable markdown report.
func (s *Scorecard) WriteMarkdown(path string) error {
	s.Finalize(s.Options.RegressionBudgets)
	var b []byte
	b = append(b, "# Artemis Benchmark Scorecard\n\n"...)
	b = fmt.Appendf(b, "- **Date:** %s\n", s.Date.Format(time.RFC3339))
	b = fmt.Appendf(b, "- **Host:** %s\n", s.Host)
	b = fmt.Appendf(b, "- **Matrix Version:** %s\n", s.MatrixVersion)
	b = fmt.Appendf(b, "- **Scorecard Version:** %s\n", s.Version)
	b = fmt.Appendf(b, "- **Mode:** %s\n", s.Mode)
	b = fmt.Appendf(b, "- **Honest:** %v\n", s.Honest)
	b = fmt.Appendf(b, "- **Claim status:** %s; evidence expiry: %s\n", s.Claim.Status, s.Claim.ExpiresAt.Format(time.RFC3339))
	if s.HonestReason != "" {
		b = fmt.Appendf(b, "- **Honest Reason:** %s\n", s.HonestReason)
	}
	b = fmt.Appendf(b, "- **Environment:** Go %s, %s/%s, %d CPU, tag=%q\n\n",
		s.Environment.GoVersion, s.Environment.OS, s.Environment.Arch, s.Environment.NumCPU, s.Environment.BenchmarkTag)

	// Summary table
	summaries := s.Summarize()
	b = append(b, "## Summary\n\n"...)
	b = append(b, "| Engine | Scenarios | Wins | Losses | Errors | Total ms | Avg ms |\n"...)
	b = append(b, "|--------|-----------|------|--------|--------|----------|--------|\n"...)
	for _, su := range summaries {
		b = fmt.Appendf(b, "| %s | %d | %d | %d | %d | %.2f | %.2f |\n",
			su.Engine, su.Scenarios, su.Wins, su.Losses, su.Errors, su.TotalMs, su.AvgMs)
	}
	b = append(b, '\n')

	// Per-scenario results
	b = append(b, "## Per-Scenario Results\n\n"...)
	b = append(b, "| Scenario | Engine | Mode | Wall ms | CPU ms | RSS | Allocs | Bytes | OK | Valid | Error |\n"...)
	b = append(b, "|----------|--------|------|---------|--------|-----|--------|-------|----|-------|-------|\n"...)

	// Sort results by scenario ID then engine
	sorted := make([]ScenarioResult, len(s.Results))
	copy(sorted, s.Results)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Workload != sorted[j].Workload {
			return sorted[i].Workload < sorted[j].Workload
		}
		return sorted[i].Engine < sorted[j].Engine
	})

	for _, r := range sorted {
		errMsg := r.Error
		if errMsg == "" {
			errMsg = "-"
		}
		b = fmt.Appendf(b, "| %s | %s | %s | %.3f | %.3f | %d | %d | %d | %v | %v | %s |\n",
			r.ScenarioID, r.Engine, r.EngineMode, r.WallMs, r.CPUMs, r.RSSBytes, r.AllocCount, r.AllocBytes, r.OK, r.Validated, errMsg)
	}
	b = append(b, '\n')

	// Comparative detail always rechecks the raw proof, including ties.
	pairs, pairErr := s.comparisonPairs()
	if s.Honest && pairErr == nil {
		b = append(b, "## Win/Loss Detail\n\n"...)
		for _, pair := range pairs {
			winner := "tie"
			if pair.artemis.WallMs < pair.competitor.WallMs {
				winner = string(EngineArtemis)
			}
			if pair.competitor.WallMs < pair.artemis.WallMs {
				winner = string(EngineCompetitor)
			}
			b = fmt.Appendf(b, "- **%s**: winner = %s\n", pair.workload.Identity(), winner)
		}
	}
	return writeArtifact(path, b)
}
