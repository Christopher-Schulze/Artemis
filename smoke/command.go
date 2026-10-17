package smoke

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

// Command runs the existing CLI contract and returns its process exit status.
func Command(args []string) (exitCode int) {
	fs := flag.NewFlagSet("artemis-smoke", flag.ContinueOnError)
	scenariosPath := fs.String("scenarios", "", "path to scenarios YAML (required)")
	manifestPath := fs.String("fixture-manifest", "", "canonical fixture manifest bound to the scenario YAML")
	stdoutScorecard := fs.Bool("scorecard-stdout", false, "emit the exact completed scorecard JSON on stdout")
	outDir := fs.String("out", "artemis-smoke-out", "parent directory for unique completed run evidence")
	binPath := fs.String("bin", "", "pre-built artemis binary (skips build if set)")
	buildDir := fs.String("build-dir", "", "artemis module root for `go build ./cmd/artemis` (required if --bin empty)")
	host := fs.String("host", "127.0.0.1", "serve host")
	port := fs.Int("port", 0, "serve port; 0 assigns an isolated ephemeral port")
	stepTimeout := fs.Duration("step-timeout", 30*time.Second, "per-step timeout")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `artemis-smoke - live smoke harness for cmd/artemis

Usage:
  artemis-smoke [flags] --scenarios <path>

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	address, addressErr := netip.ParseAddr(*host)
	if *scenariosPath == "" || *outDir == "" || (*binPath == "" && *buildDir == "") || fs.NArg() != 0 || addressErr != nil || !address.IsLoopback() {
		fs.Usage()
		return 2
	}
	if err := validateCLIConfig(*port, *stepTimeout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
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
	defer func() {
		if err := runner.Close(); err != nil {
			logger.Error("smoke owner cleanup", "err", err)
			exitCode = 1
		}
	}()
	sf, err := LoadScenarios(*scenariosPath)
	if err != nil {
		logger.Error("load scenarios", "err", err)
		return 1
	}
	if *manifestPath != "" {
		if err := sf.bindFixtureManifest(*manifestPath); err != nil {
			logger.Error("admit fixture manifest", "err", err)
			return 1
		}
	}

	if err := runner.BuildArtemis(ctx); err != nil {
		logger.Error("build artemis", "err", err)
		return 1
	}

	sc, err := runner.RunAll(ctx, sf)
	if err != nil {
		logger.Error("run", "err", err)
		return 1
	}
	if sc == nil {
		return 1
	}
	if *stdoutScorecard {
		if err := writeScorecardOutput(sc, os.Stdout); err != nil {
			logger.Error("scorecard stdout", "err", err)
			return 1
		}
	}
	// Exit non-zero if any non-skipped scenario failed.
	if sc.Failed > 0 {
		logger.Info("smoke complete with failures", "passed", sc.Passed, "failed", sc.Failed, "skipped", sc.Skipped, "scorecard", sc.OutputDir+"/scorecard.json")
		return 1
	}
	logger.Info("smoke complete", "passed", sc.Passed, "failed", sc.Failed, "skipped", sc.Skipped, "scorecard", sc.OutputDir+"/scorecard.json")
	return 0
}

func writeScorecardOutput(scorecard *Scorecard, output io.Writer) error {
	data, err := readSmokeInput(filepath.Join(scorecard.OutputDir, "scorecard.json"), smokeArtifactBytesLimit)
	if err != nil {
		return err
	}
	n, err := output.Write(data)
	if err == nil && n != len(data) {
		return io.ErrShortWrite
	}
	return err
}

func validateCLIConfig(port int, stepTimeout time.Duration) error {
	if port < 0 || port > 65535 {
		return fmt.Errorf("port must be between 0 and 65535")
	}
	if stepTimeout <= 0 {
		return fmt.Errorf("step-timeout must be positive")
	}
	return nil
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
