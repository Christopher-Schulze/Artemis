package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/Christopher-Schulze/Artemis/benchmark"
)

func cmdBenchmark(args []string) int {
	fs := newFlagSet("benchmark")
	skipCompetitor := fs.Bool("skip-competitor", true, "skip competitor side (Artemis-only)")
	downloadURL := fs.String("download-url", "", "competitor binary download URL")
	iterations := fs.Int("iterations", 5, "iterations per scenario")
	outputDir := fs.String("output", "benchmark/results", "output directory for scorecard")
	format := fs.String("format", "json", "output format: json|summary")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `usage: artemis benchmark [flags]

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *format != "json" && *format != "summary" {
		errf("benchmark format %q invalid", *format)
		return 2
	}

	cfg := benchmark.HarnessConfig{
		OutputDir:      *outputDir,
		Iterations:     *iterations,
		SkipCompetitor: *skipCompetitor,
		Competitor: benchmark.CompetitorConfig{
			DownloadURL: *downloadURL,
		},
	}

	h := benchmark.NewHarness(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	sc, err := h.Run(ctx)
	if err != nil {
		errf("benchmark: %v", err)
		return 1
	}

	if *format == "summary" {
		benchmark.PrintSummary(sc)
		return 0
	}

	if err := printJSON(os.Stdout, sc); err != nil {
		errf("benchmark: %v", err)
		return 1
	}
	return 0
}
