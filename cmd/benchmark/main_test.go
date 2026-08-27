package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestValidateOptions(t *testing.T) {
	cases := []struct {
		name              string
		skipCompetitor    bool
		requireHeadToHead bool
		iterations        int
		outputDir         string
		wantErr           bool
	}{
		{name: "artemis only", skipCompetitor: true, iterations: 1, outputDir: "out"},
		{name: "head to head", requireHeadToHead: true, iterations: 5, outputDir: "out"},
		{name: "zero iterations", skipCompetitor: true, outputDir: "out", wantErr: true},
		{name: "negative iterations", skipCompetitor: true, iterations: -1, outputDir: "out", wantErr: true},
		{name: "empty output", skipCompetitor: true, iterations: 1, wantErr: true},
		{name: "contradictory modes", skipCompetitor: true, requireHeadToHead: true, iterations: 1, outputDir: "out", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotErr := validateOptions(tc.skipCompetitor, tc.requireHeadToHead, tc.iterations, tc.outputDir) != nil
			if gotErr != tc.wantErr {
				t.Fatalf("validateOptions() error = %v, want error %v", gotErr, tc.wantErr)
			}
		})
	}
}

// TestBenchmarkCmdRuns verifies the benchmark CLI tool compiles and
// runs successfully in Artemis-only mode, producing a scorecard. This
// is a smoke test that exercises the full harness via the CLI entry
// point.
func TestBenchmarkCmdRuns(t *testing.T) {
	// Build the binary
	binDir := t.TempDir()
	binPath := filepath.Join(binDir, "benchmark")
	cmd := exec.CommandContext(t.Context(), "go", "build", "-o", binPath, ".")
	cmd.Dir = "."
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	// Run it in Artemis-only mode with a temp output dir
	outDir := t.TempDir()
	runCmd := exec.CommandContext(t.Context(), binPath, "--skip-competitor", "--iterations", "2", "--output", outDir)
	runCmd.Dir = "."
	if out, err := runCmd.CombinedOutput(); err != nil {
		t.Fatalf("benchmark run: %v\n%s", err, out)
	}

	// Verify scorecard files exist
	jsonPath := filepath.Join(outDir, "scorecard.json")
	mdPath := filepath.Join(outDir, "scorecard.md")
	if _, err := os.Stat(jsonPath); err != nil {
		t.Errorf("scorecard.json not written: %v", err)
	}
	if _, err := os.Stat(mdPath); err != nil {
		t.Errorf("scorecard.md not written: %v", err)
	}
}
