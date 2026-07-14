package benchmark

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

	var b strings.Builder
	b.WriteString("# Artemis Benchmark Report\n\n")
	b.WriteString(fmt.Sprintf("Generated: %s\n\n", time.Now().UTC().Format(time.RFC3339)))

	b.WriteString("## Reproducibility\n\n")
	b.WriteString(fmt.Sprintf("- Scorecard path: `%s`\n", artifacts.ScorecardPath))
	b.WriteString(fmt.Sprintf("- Scorecard version: %s\n", sc.Version))
	b.WriteString(fmt.Sprintf("- Matrix version: %s\n", sc.MatrixVersion))
	b.WriteString(fmt.Sprintf("- Go: %s, OS: %s, Arch: %s, CPUs: %d\n",
		sc.Environment.GoVersion, sc.Environment.OS, sc.Environment.Arch, sc.Environment.NumCPU))
	b.WriteString(fmt.Sprintf("- Mode: %s, Honest: %v\n", sc.Mode, sc.Honest))
	if sc.HonestReason != "" {
		b.WriteString(fmt.Sprintf("- Honest reason: %s\n", sc.HonestReason))
	}
	b.WriteString("\n")

	b.WriteString("## Command\n\n")
	b.WriteString("```bash\n")
	b.WriteString("go run ./cmd/benchmark")
	if sc.Mode == ModeHeadToHead {
		b.WriteString(" --require-head-to-head")
	} else {
		b.WriteString(" --skip-competitor")
	}
	if sc.Environment.BenchmarkTag != "" {
		b.WriteString(fmt.Sprintf(" --benchmark-tag=%s", sc.Environment.BenchmarkTag))
	}
	b.WriteString("\n")
	b.WriteString("```\n\n")

	b.WriteString("## Per-Metric Aggregates\n\n")
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

	b.WriteString("### Engine Wall-Time Distributions\n\n")
	b.WriteString("| Engine | Count | Mean (ms) | StdDev | Min | Max | Median | P95 | P99 |\n")
	b.WriteString("|--------|-------|-----------|--------|-----|-----|--------|-----|-----|\n")
	for engine, vals := range byEngine {
		agg := ComputeAggregate(vals)
		b.WriteString(fmt.Sprintf("| %s | %d | %.3f | %.3f | %.3f | %.3f | %.3f | %.3f | %.3f |\n",
			engine, agg.Count, agg.Mean, agg.StdDev, agg.Min, agg.Max, agg.Median, agg.P95, agg.P99))
	}

	b.WriteString("\n### Metric Distributions\n\n")
	b.WriteString("| Metric | Count | Mean | StdDev | Min | Max | Median | P95 | P99 |\n")
	b.WriteString("|--------|-------|------|--------|-----|-----|--------|-----|-----|\n")
	for _, kind := range []MetricKind{MetricWallMs, MetricCPUMs, MetricAllocBytes, MetricAllocCount, MetricRSSBytes, MetricThroughput, MetricErrorRate} {
		vals := byMetric[kind]
		if len(vals) == 0 {
			continue
		}
		agg := ComputeAggregate(vals)
		b.WriteString(fmt.Sprintf("| %s | %d | %s | %s | %s | %s | %s | %s | %s |\n",
			kind, agg.Count,
			FormatMetric(kind, agg.Mean),
			FormatMetric(kind, agg.StdDev),
			FormatMetric(kind, agg.Min),
			FormatMetric(kind, agg.Max),
			FormatMetric(kind, agg.Median),
			FormatMetric(kind, agg.P95),
			FormatMetric(kind, agg.P99),
		))
	}

	b.WriteString("\n## Methodology Notes\n\n")
	b.WriteString("All numbers are produced from the same source scorecard artifact. " +
		"Wall time is the median across iterations. " +
		"Allocations are measured from the Go runtime. " +
		"The scorecard is marked honest only when both engines complete all scenarios with matching semantic success criteria.\n")

	return b.String(), nil
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
