// Package benchmark cmd runs the head-to-head harness and writes the
// scorecard to benchmark/results/. Run with:
//
//	go run ./cmd/benchmark [--skip-competitor] [--download-url URL]
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/Christopher-Schulze/Artemis/benchmark"
)

func main() {
	options, err := benchmark.ParseOptions(os.Args[1:], os.Stderr)
	if errors.Is(err, flag.ErrHelp) {
		return
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "benchmark: %v\n", err)
		os.Exit(2)
	}

	h := benchmark.NewHarness(options.Config)
	ctx, cancel := context.WithTimeout(context.Background(), options.Config.Timeout)
	defer cancel()

	sc, err := h.Run(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "benchmark: %v\n", err)
		os.Exit(1)
	}

	if options.Format == "summary" {
		err = benchmark.PrintSummary(sc)
	} else {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		err = encoder.Encode(sc)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "benchmark: %v\n", err)
		os.Exit(1)
	}
}

func validateOptions(skipCompetitor, requireHeadToHead bool, iterations int, outputDir string) error {
	cfg := benchmark.DefaultHarnessConfig()
	cfg.SkipCompetitor = skipCompetitor
	cfg.RequireHeadToHead = requireHeadToHead
	cfg.Iterations = iterations
	cfg.OutputDir = outputDir
	return benchmark.ValidateConfig(cfg, benchmark.DefaultScenarios())
}
