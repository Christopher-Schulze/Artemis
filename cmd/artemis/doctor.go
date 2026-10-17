package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	artemis "github.com/Christopher-Schulze/Artemis"
	"github.com/Christopher-Schulze/Artemis/bridge"
	browserprocess "github.com/Christopher-Schulze/Artemis/process"
)

func cmdDoctor(args []string) int {
	fs := newFlagSet("doctor")
	format := fs.String("format", "json", "output format: json|text")
	timeout := fs.Duration("timeout", 10*time.Second, "per-check timeout")
	binaryPath := fs.String("binary", "", "Chromium executable to probe (auto-discovered when empty)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `usage: artemis doctor [flags]

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *format != "json" && *format != "text" {
		errf("doctor format %q invalid", *format)
		return 2
	}
	if *timeout <= 0 || fs.NArg() != 0 {
		errf("doctor requires a positive timeout and no positional arguments")
		return 2
	}

	result := DoctorResult{
		Version: Version,
		Go:      runtime.Version(),
		OS:      runtime.GOOS,
		Arch:    runtime.GOARCH,
		Checks:  make([]CheckResult, 0, 4),
	}

	binary, discoveryErr := browserprocess.DiscoverBinary(*binaryPath)
	if discoveryErr != nil {
		result.Checks = append(result.Checks, CheckResult{Name: "chromium_binary", OK: false, Message: discoveryErr.Error()})
		result.Checks = append(result.Checks, CheckResult{Name: "chromium", OK: false, Message: "Chromium executable unavailable; no runtime readiness proof"})
	} else {
		result.Checks = append(result.Checks, CheckResult{Name: "chromium_binary", OK: true, Message: binary.Path})
		ctx, cancel := context.WithTimeout(context.Background(), *timeout)
		message, err := doctorChromium(ctx, binary.Path, *timeout)
		cancel()
		if err != nil {
			result.Checks = append(result.Checks, CheckResult{Name: "chromium", OK: false, Message: err.Error()})
		} else {
			result.Checks = append(result.Checks, CheckResult{Name: "chromium", OK: true, Message: message})
		}
	}

	// Agent start/stop check.
	start := time.Now()
	agent, err := artemis.NewAgent(artemis.AgentConfig{})
	if err != nil {
		result.Checks = append(result.Checks, CheckResult{Name: "agent_start", OK: false, Message: err.Error()})
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), *timeout)
		startErr := agent.Start(ctx)
		stopErr := agent.Stop()
		err := errors.Join(startErr, stopErr, ctx.Err())
		cancel()
		if err != nil {
			result.Checks = append(result.Checks, CheckResult{Name: "agent_start", OK: false, Message: err.Error()})
		} else {
			result.Checks = append(result.Checks, CheckResult{Name: "agent_start", OK: true, Message: fmt.Sprintf("started and stopped in %s", time.Since(start).Round(time.Millisecond))})
		}
	}

	// Capability registry check.
	if err := artemis.ValidateCapabilityRegistry(); err != nil {
		result.Checks = append(result.Checks, CheckResult{Name: "capabilities", OK: false, Message: err.Error()})
	} else {
		result.Checks = append(result.Checks, CheckResult{Name: "capabilities", OK: true, Message: "registry valid"})
	}

	result.OK = true
	for _, c := range result.Checks {
		result.OK = result.OK && c.OK
	}

	if *format == "text" {
		lines := make([]string, 0, len(result.Checks))
		for _, c := range result.Checks {
			status := "PASS"
			if !c.OK {
				status = "FAIL"
			}
			lines = append(lines, fmt.Sprintf("[%s] %s: %s", status, c.Name, c.Message))
		}
		if err := printText(os.Stdout, strings.Join(lines, "\n")); err != nil {
			errf("doctor output: %v", err)
			return 1
		}
		if result.OK {
			return 0
		}
		return 1
	}

	if err := printJSON(os.Stdout, result); err != nil {
		errf("doctor: %v", err)
		return 1
	}
	if result.OK {
		return 0
	}
	return 1
}

func doctorChromium(ctx context.Context, binary string, timeout time.Duration) (message string, probeErr error) {
	browser, err := bridge.LaunchChromium(ctx, browserprocess.LaunchConfig{BinaryPath: binary, Headless: true, StartupTimeout: timeout, ShutdownTimeout: timeout})
	if err != nil {
		return "", err
	}
	defer func() {
		probeErr = errors.Join(probeErr, browser.Close())
		if probeErr != nil {
			message = ""
		}
	}()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	version := browser.Version()
	return fmt.Sprintf("%s; CDP %s handshake and owned shutdown verified", version.Product, version.ProtocolVersion), nil
}

// DoctorResult is the stable JSON output of the doctor command.
type DoctorResult struct {
	Version string        `json:"version"`
	Go      string        `json:"go"`
	OS      string        `json:"os"`
	Arch    string        `json:"arch"`
	OK      bool          `json:"ok"`
	Checks  []CheckResult `json:"checks"`
}

// CheckResult is a single diagnostic check.
type CheckResult struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}
