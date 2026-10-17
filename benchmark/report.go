package benchmark

import (
	"crypto/sha256"
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
	return readSealedArtifacts(outputDir)
}

func readUnsealedArtifacts(outputDir string) (RawArtifacts, error) {
	var a RawArtifacts
	cleanOutputDir := filepath.Clean(outputDir)
	jsonPath := filepath.Join(cleanOutputDir, "scorecard.json")
	mdPath := filepath.Join(cleanOutputDir, "scorecard.md")

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
	// Serialized flags are not authority: derive claims from the raw samples.
	sc.Finalize(sc.Options.RegressionBudgets)
	var b []byte
	b = append(b, "# Artemis Benchmark Report\n\n"...)
	b = fmt.Appendf(b, "Run date: %s\n\n", sc.Date.Format(time.RFC3339Nano))
	b = append(b, "## Reproducibility\n\n"...)
	b = fmt.Appendf(b, "- Scorecard version: %s; matrix: %s\n", sc.Version, sc.MatrixVersion)
	b = fmt.Appendf(b, "- Artifact SHA256: %x\n", sha256.Sum256([]byte(artifacts.ScorecardJSON)))
	b = fmt.Appendf(b, "- Claim status: %s; evidence expiry: %s\n", sc.Claim.Status, sc.Claim.ExpiresAt.Format(time.RFC3339))
	b = fmt.Appendf(b, "- Artemis binary SHA256: %s; source revision: %s; modified: %v\n", sc.Provenance.ArtemisSHA256, sc.Provenance.SourceRevision, sc.Provenance.SourceModified)
	b = fmt.Appendf(b, "- Competitor version: %s; executed SHA256: %s\n", sc.CompetitorVersion, sc.CompetitorSHA256)
	b = fmt.Appendf(b, "- Go: %s; OS/Arch: %s/%s; CPUs: %d; tag: %q\n",
		sc.Environment.GoVersion, sc.Environment.OS, sc.Environment.Arch, sc.Environment.NumCPU, sc.Environment.BenchmarkTag)
	_, comparisonErr := sc.comparisonPairs()
	b = fmt.Appendf(b, "- Mode: %s; honest comparison: %v; reason: %s\n", sc.Mode, sc.Honest && comparisonErr == nil, sc.HonestReason)
	for _, ref := range sc.Claim.ProofRefs {
		b = fmt.Appendf(b, "- Proof: %s\n", ref)
	}
	b = append(b, "\n## Command\n\n"...)
	b = append(b, "Both benchmark entry points use the recorded options below. Replays require a fresh output directory. Custom matrices use the harness Workloads API.\n\n"...)
	options, err := json.MarshalIndent(sc.Options, "", "  ")
	if err != nil {
		return "", err
	}
	b = append(b, "```json\n"...)
	b = append(b, options...)
	b = append(b, "\n```\n\n"...)
	b = append(b, "## Per-Metric Aggregates\n\n"...)
	b = append(b, "| Engine | Metric | Count | Mean | StdDev | Min | Max | Median | P95 | P99 |\n"...)
	b = append(b, "|--------|--------|-------|------|--------|-----|-----|--------|-----|-----|\n"...)
	for _, metric := range sc.Aggregates {
		agg := metric.Aggregate
		b = fmt.Appendf(b, "| %s | %s | %d | %s | %s | %s | %s | %s | %s | %s |\n",
			metric.Engine, metric.Kind, agg.Count, FormatMetric(metric.Kind, agg.Mean),
			FormatMetric(metric.Kind, agg.StdDev), FormatMetric(metric.Kind, agg.Min),
			FormatMetric(metric.Kind, agg.Max), FormatMetric(metric.Kind, agg.Median),
			FormatMetric(metric.Kind, agg.P95), FormatMetric(metric.Kind, agg.P99))
	}
	b = append(b, "\n## Methodology Notes\n\n"...)
	b = append(b, "Distributions use every validated measured sample, excluding declared priming runs. Workloads retain engine mode, locality, warmth and sample policy. Cold samples reset the Artemis engine and competitor process; warm workloads prime each engine once. Bootstrap and OS cache state are not measured as page latency. Competitor CPU, RSS and allocation metrics are unavailable and cannot form regression baselines. Comparisons require matching semantic extraction digests and at least five samples per engine and workload.\n"...)
	return string(b), nil
}

// WriteReport writes a report to the output directory alongside the scorecard.
func WriteReport(outputDir string) error {
	artifacts, err := readUnsealedArtifacts(outputDir)
	if err != nil {
		return err
	}
	report, err := GenerateReport(artifacts)
	if err != nil {
		return err
	}
	path := filepath.Join(outputDir, "report.md")
	return writeArtifact(path, []byte(report))
}
