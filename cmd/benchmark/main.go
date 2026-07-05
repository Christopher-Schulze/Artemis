// Package benchmark cmd runs the head-to-head harness and writes the
// scorecard to benchmark/results/. Run with:
//
//	go run ./cmd/benchmark [--skip-competitor] [--download-url URL]
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/Christopher-Schulze/Artemis/benchmark"
)

func main() {
	skipCompetitor := flag.Bool("skip-competitor", true, "skip competitor side (Artemis-only)")
	downloadURL := flag.String("download-url", "", "competitor binary download URL")
	iterations := flag.Int("iterations", 5, "iterations per scenario")
	outputDir := flag.String("output", "benchmark/results", "output directory for scorecard")
	flag.Parse()

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
		fmt.Fprintf(os.Stderr, "benchmark: %v\n", err)
		os.Exit(1)
	}

	benchmark.PrintSummary(sc)
}
