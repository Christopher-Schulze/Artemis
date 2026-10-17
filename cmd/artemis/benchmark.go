package main

import (
	"context"
	"errors"
	"flag"
	"os"

	"github.com/Christopher-Schulze/Artemis/benchmark"
)

func cmdBenchmark(args []string) int {
	options, err := benchmark.ParseOptions(args, os.Stderr)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		errf("benchmark: %v", err)
		return 2
	}

	h := benchmark.NewHarness(options.Config)
	ctx, cancel := context.WithTimeout(context.Background(), options.Config.Timeout)
	defer cancel()

	sc, err := h.Run(ctx)
	if err != nil {
		errf("benchmark: %v", err)
		return 1
	}

	if options.Format == "summary" {
		if err := benchmark.PrintSummary(sc); err != nil {
			errf("benchmark: %v", err)
			return 1
		}
		return 0
	}

	if err := printJSON(os.Stdout, sc); err != nil {
		errf("benchmark: %v", err)
		return 1
	}
	return 0
}
