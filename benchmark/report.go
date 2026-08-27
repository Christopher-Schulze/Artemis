package benchmark

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// RawArtifacts is the set of input files used to generate documentation.
type RawArtifacts struct {
	ScorecardPath string `json:"scorecardPath"`
	ScorecardJSON string `json:"scorecardJSON"`
	ScorecardMD   string `json:"scorecardMD"`
}

// ReadRawArtifacts reads the committed scorecard files from the output directory.
func ReadRawArtifacts(outputDir string) (RawArtifacts, error) {
	var a RawArtifacts
	jsonPath := filepath.Join(outputDir, "scorecard.json")
	mdPath := filepath.Join(outputDir, "scorecard.md")

	jsonData, err := os.ReadFile(jsonPath)
	if err != nil {
		return a, fmt.Errorf("read scorecard json: %w", err)
	}
	mdData, err := os.ReadFile(mdPath)
	if err != nil {
		return a, fmt.Errorf("read scorecard md: %w", err)
	}

	a.ScorecardPath = outputDir
	a.ScorecardJSON = string(jsonData)
	a.ScorecardMD = string(mdData)
	return a, nil
}

// GenerateReport creates a human-readable markdown report from raw artifacts and returns the rendered string.
func GenerateReport(artifacts RawArtifacts) (string, error) {
	var sc Scorecard
	if err := json.Unmarshal([]byte(artifacts.ScorecardJSON), &sc); err != nil {
		return "", fmt.Errorf("parse scorecard json: %w", err)
	}

	var b []byte
	b = append(b, "# Artemis Benchmark Report\n\n"...)
	b = fmt.Appendf(b, "Generated: %s\n\n", time.Now().UTC().Format(time.RFC3339))

	b = append(b, "## Reproducibility\n\n"...)
	b = fmt.Appendf(b, "- Scorecard path: `%s`\n", artifacts.ScorecardPath)
	b = fmt.Appendf(b, "- Scorecard version: %s\n", sc.Version)
	b = fmt.Appendf(b, "- Matrix version: %s\n", sc.MatrixVersion)
	b = fmt.Appendf(b, "- Go: %s, OS: %s, Arch: %s, CPUs: %d\n",
		sc.Environment.GoVersion, sc.Environment.OS, sc.Environment.Arch, sc.Environment.NumCPU)
	b = fmt.Appendf(b, "- Mode: %s, Honest: %v\n", sc.Mode, sc.Honest)
	if sc.HonestReason != "" {
		b = fmt.Appendf(b, "- Honest reason: %s\n", sc.HonestReason)
	}
	b = append(b, '\n')

	b = append(b, "## Command\n\n"...)
	b = append(b, "```bash\n"...)
	b = append(b, "go run ./cmd/benchmark"...)
	if sc.Mode == ModeHeadToHead {
		b = append(b, " --require-head-to-head"...)
	} else {
		b = append(b, " --skip-competitor"...)
	}
	if sc.Environment.BenchmarkTag != "" {
		b = fmt.Appendf(b, " --benchmark-tag=%s", sc.Environment.BenchmarkTag)
	}
	b = append(b, "\n"...)
	b = append(b, "```\n\n"...)

	b = append(b, "## Per-Metric Aggregates\n\n"...)
	byEngine := map[EngineName][]float64{}
	byMetric := map[MetricKind][]float64{}
	for _, r := range sc.Results {
		if !r.OK {
			continue
		}
		byEngine[r.Engine] = append(byEngine[r.Engine], r.WallMs)
		for _, sample := range r.ToMetricSet().ToSamples() {
			byMetric[sample.Kind] = append(byMetric[sample.Kind], sample.Value)
		}
	}

	b = append(b, "### Engine Wall-Time Distributions\n\n"...)
	b = append(b, "| Engine | Count | Mean (ms) | StdDev | Min | Max | Median | P95 | P99 |\n"...)
	b = append(b, "|--------|-------|-----------|--------|-----|-----|--------|-----|-----|\n"...)
	for engine, vals := range byEngine {
		agg := ComputeAggregate(vals)
		b = fmt.Appendf(b, "| %s | %d | %.3f | %.3f | %.3f | %.3f | %.3f | %.3f | %.3f |\n",
			engine, agg.Count, agg.Mean, agg.StdDev, agg.Min, agg.Max, agg.Median, agg.P95, agg.P99)
	}

	b = append(b, "\n### Metric Distributions\n\n"...)
	b = append(b, "| Metric | Count | Mean | StdDev | Min | Max | Median | P95 | P99 |\n"...)
	b = append(b, "|--------|-------|------|--------|-----|-----|--------|-----|-----|\n"...)
	for _, kind := range []MetricKind{MetricWallMs, MetricCPUMs, MetricAllocBytes, MetricAllocCount, MetricRSSBytes, MetricThroughput, MetricErrorRate} {
		vals := byMetric[kind]
		if len(vals) == 0 {
			continue
		}
		agg := ComputeAggregate(vals)
		b = fmt.Appendf(b, "| %s | %d | %s | %s | %s | %s | %s | %s | %s |\n",
			kind, agg.Count,
			FormatMetric(kind, agg.Mean),
			FormatMetric(kind, agg.StdDev),
			FormatMetric(kind, agg.Min),
			FormatMetric(kind, agg.Max),
			FormatMetric(kind, agg.Median),
			FormatMetric(kind, agg.P95),
			FormatMetric(kind, agg.P99),
		)
	}

	b = append(b, "\n## Methodology Notes\n\n"...)
	b = append(b, "All numbers are produced from the same source scorecard artifact. "+
		"Wall time is the median across iterations. "+
		"Allocations are measured from the Go runtime. "+
		"The scorecard is marked honest only when both engines complete all scenarios with matching semantic success criteria.\n"...)

	return string(b), nil
}

// WriteReport writes a report to the output directory alongside the scorecard.
func WriteReport(outputDir string) error {
	artifacts, err := ReadRawArtifacts(outputDir)
	if err != nil {
		return err
	}
	report, err := GenerateReport(artifacts)
	if err != nil {
		return err
	}
	path := filepath.Join(outputDir, "report.md")
	return os.WriteFile(path, []byte(report), 0o644)
}
