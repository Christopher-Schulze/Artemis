package main

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"time"

	artemis "github.com/Christopher-Schulze/Artemis"
	browserprocess "github.com/Christopher-Schulze/Artemis/process"
)

func cmdDoctor(args []string) int {
	fs := newFlagSet("doctor")
	format := fs.String("format", "json", "output format: json|text")
	timeout := fs.Duration("timeout", 10*time.Second, "per-check timeout")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `usage: artemis doctor [flags]

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	result := DoctorResult{
		Version: Version,
		Go:      runtime.Version(),
		OS:      runtime.GOOS,
		Arch:    runtime.GOARCH,
		Checks:  make([]CheckResult, 0, 4),
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	// Chromium binary discovery check.
	if binary, err := browserprocess.DiscoverBinary(""); err != nil {
		result.Checks = append(result.Checks, CheckResult{Name: "chromium", OK: false, Message: err.Error()})
	} else {
		result.Checks = append(result.Checks, CheckResult{Name: "chromium", OK: true, Message: binary.Path})
	}

	// Agent start/stop check.
	start := time.Now()
	agent, err := artemis.NewAgent(artemis.AgentConfig{})
	if err != nil {
		result.Checks = append(result.Checks, CheckResult{Name: "agent_start", OK: false, Message: err.Error()})
	} else {
		if err := agent.Start(ctx); err != nil {
			result.Checks = append(result.Checks, CheckResult{Name: "agent_start", OK: false, Message: err.Error()})
			_ = agent.Stop()
		} else {
			_ = agent.Stop()
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
		for _, c := range result.Checks {
			status := "PASS"
			if !c.OK {
				status = "FAIL"
			}
			fmt.Printf("[%s] %s: %s\n", status, c.Name, c.Message)
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
