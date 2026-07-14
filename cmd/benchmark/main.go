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
	requireHeadToHead := flag.Bool("require-head-to-head", false, "fail if head-to-head comparison cannot be produced honestly")
	downloadURL := flag.String("download-url", "", "competitor binary download URL")
	iterations := flag.Int("iterations", 5, "iterations per scenario")
	outputDir := flag.String("output", "benchmark/results", "output directory for scorecard")
	benchmarkTag := flag.String("benchmark-tag", "", "environment tag (e.g. cold, warm, renderless)")
	cpuProfile := flag.String("cpu-profile", "", "write CPU profile to file")
	memProfile := flag.String("mem-profile", "", "write memory profile to file")
	flag.Parse()

	cfg := benchmark.HarnessConfig{
		OutputDir:         *outputDir,
		Iterations:        *iterations,
		SkipCompetitor:    *skipCompetitor,
		RequireHeadToHead: *requireHeadToHead,
		BenchmarkTag:      *benchmarkTag,
		Profile: benchmark.ProfileConfig{
			CPUProfilePath: *cpuProfile,
			MemProfilePath: *memProfile,
		},
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

	if *requireHeadToHead && !sc.Honest {
		fmt.Fprintf(os.Stderr, "benchmark: head-to-head required but scorecard is not honest: %s\n", sc.HonestReason)
		os.Exit(1)
	}

	benchmark.PrintSummary(sc)
}
