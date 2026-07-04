package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	fs := flag.NewFlagSet("artemis-smoke", flag.ContinueOnError)
	scenariosPath := fs.String("scenarios", "", "path to scenarios YAML (required)")
	outDir := fs.String("out", "artemis-smoke-out", "evidence output directory")
	binPath := fs.String("bin", "", "pre-built artemis binary (skips build if set)")
	buildDir := fs.String("build-dir", "", "artemis module root for `go build ./cmd/artemis` (required if --bin empty)")
	host := fs.String("host", "127.0.0.1", "serve host")
	port := fs.Int("port", 9344, "serve port")
	stepTimeout := fs.Duration("step-timeout", 30*time.Second, "per-step timeout")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `artemis-smoke - live smoke harness for cmd/artemis

Usage:
  artemis-smoke [flags] --scenarios <path>

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}
	if *scenariosPath == "" {
		fs.Usage()
		os.Exit(2)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	ctx, cancel := signalContext()
	defer cancel()

	runner := NewRunner(RunnerConfig{
		ArtemisBinPath: *binPath,
		BuildDir:       *buildDir,
		OutDir:         *outDir,
		StepTimeout:    *stepTimeout,
		Host:           *host,
		Port:           *port,
		Logger:         logger,
	})

	if err := runner.BuildArtemis(ctx); err != nil {
		logger.Error("build artemis", "err", err)
		os.Exit(1)
	}

	sf, err := LoadScenarios(*scenariosPath)
	if err != nil {
		logger.Error("load scenarios", "err", err)
		os.Exit(1)
	}

	sc, err := runner.RunAll(ctx, sf)
	if err != nil {
		logger.Error("run", "err", err)
	}
	if sc == nil {
		os.Exit(1)
	}
	// Exit non-zero if any non-skipped scenario failed.
	if sc.Failed > 0 {
		logger.Info("smoke complete with failures", "passed", sc.Passed, "failed", sc.Failed, "skipped", sc.Skipped)
		os.Exit(1)
	}
	logger.Info("smoke complete", "passed", sc.Passed, "failed", sc.Failed, "skipped", sc.Skipped)
}

func signalContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-ch:
			cancel()
		case <-ctx.Done():
		}
		signal.Stop(ch)
	}()
	return ctx, cancel
}
