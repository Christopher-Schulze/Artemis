package benchmark

import (
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Options is the shared command contract for both benchmark entry points.
type Options struct {
	Config HarnessConfig
	Format string
}

// ParseOptions validates CLI options without performing runtime side effects.
func ParseOptions(args []string, diagnostics io.Writer) (Options, error) {
	options := Options{Config: DefaultHarnessConfig(), Format: "json"}
	options.Config.SkipCompetitor = true
	fs := flag.NewFlagSet("benchmark", flag.ContinueOnError)
	fs.SetOutput(diagnostics)
	fs.BoolVar(&options.Config.SkipCompetitor, "skip-competitor", true, "run Artemis only")
	fs.BoolVar(&options.Config.RequireHeadToHead, "require-head-to-head", false, "fail unless every comparison is eligible")
	fs.StringVar(&options.Config.Competitor.DownloadURL, "download-url", "", "pinned competitor binary URL")
	fs.StringVar(&options.Config.Competitor.ExpectedSHA256, "competitor-sha256", "", "required SHA256 of executed competitor")
	fs.StringVar(&options.Config.Competitor.BinaryDir, "competitor-dir", options.Config.Competitor.BinaryDir, "competitor binary cache directory")
	fs.IntVar(&options.Config.Iterations, "iterations", 5, "measured samples per workload, 1..1000")
	fs.StringVar(&options.Config.OutputDir, "output", options.Config.OutputDir, "fresh output directory")
	fs.StringVar(&options.Config.BenchmarkTag, "benchmark-tag", "", "environment discriminator")
	fs.StringVar(&options.Config.Profile.CPUProfilePath, "cpu-profile", "", "CPU profile path inside output")
	fs.StringVar(&options.Config.Profile.MemProfilePath, "mem-profile", "", "heap profile path inside output")
	fs.DurationVar(&options.Config.Timeout, "timeout", DefaultTimeout, "whole-run deadline")
	fs.StringVar(&options.Format, "format", "json", "stdout format: json|summary")
	fs.Func("regression-budget", "repeatable metric:absolute_delta:relative_ratio", func(value string) error {
		budget, err := parseBudget(value)
		if err == nil {
			options.Config.RegressionBudgets = append(options.Config.RegressionBudgets, budget)
		}
		return err
	})
	if err := fs.Parse(args); err != nil {
		return options, err
	}
	if fs.NArg() != 0 {
		return options, fmt.Errorf("unexpected benchmark arguments: %v", fs.Args())
	}
	if options.Format != "json" && options.Format != "summary" {
		return options, fmt.Errorf("format must be json or summary")
	}
	options.Config.OutputFormat = options.Format
	return options, ValidateConfig(options.Config, DefaultScenarios())
}

func parseBudget(value string) (RegressionBudget, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 3 {
		return RegressionBudget{}, fmt.Errorf("budget must be metric:absolute_delta:relative_ratio")
	}
	kind, err := MetricKindFromString(parts[0])
	if err != nil {
		return RegressionBudget{}, err
	}
	delta, err := strconv.ParseFloat(parts[1], 64)
	if err != nil {
		return RegressionBudget{}, fmt.Errorf("budget absolute delta: %w", err)
	}
	percent, err := strconv.ParseFloat(parts[2], 64)
	if err != nil {
		return RegressionBudget{}, fmt.Errorf("budget relative ratio: %w", err)
	}
	return RegressionBudget{MetricKind: kind, MaxDelta: delta, MaxPercent: percent}, nil
}
