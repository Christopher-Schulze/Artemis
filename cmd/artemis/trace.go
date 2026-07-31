package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/Christopher-Schulze/Artemis/bridge"
	"github.com/Christopher-Schulze/Artemis/network"
	"github.com/Christopher-Schulze/Artemis/process"
	"github.com/Christopher-Schulze/Artemis/telemetry"
)

func cmdTrace(args []string) int {
	fs := newFlagSet("trace")
	url := fs.String("url", "", "URL to fetch (required)")
	format := fs.String("format", "json", "output format: json")
	binary := fs.String("binary", "", "Chromium binary path (auto-discovered when empty)")
	sandbox := fs.String("sandbox", string(process.SandboxRequired), "Chromium sandbox policy: required or disabled")
	traceDir := fs.String("trace-dir", "", "directory for the atomic trace archive")
	screenshots := fs.Bool("screenshots", true, "capture screenshots in the trace archive")
	snapshots := fs.Bool("snapshots", true, "capture DOM snapshots in the trace archive")
	sources := fs.Bool("sources", false, "capture loaded resource sources in the trace archive")
	maxCPU := fs.Float64("max-cpu-percent", process.DefaultMaxCPUPercent, "maximum Chromium process-group CPU percent")
	maxMemory := fs.Int64("max-memory-bytes", process.DefaultMaxMemoryBytes, "maximum Chromium process-group RSS bytes")
	maxProfile := fs.Int64("max-profile-bytes", process.DefaultMaxProfileDiskBytes, "maximum Chromium profile bytes")
	sessionTimeout := fs.Duration("session-timeout", process.DefaultSessionTimeout, "maximum Chromium session lifetime")
	allowPrivate := fs.Bool("allow-private-networks", false, "allow requests to private/loopback IP targets")
	allowPort := fs.Int("allow-port", 0, "allow a specific destination port (0 = default 80/443 only)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `usage: artemis trace --url <url> [flags]

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *url == "" {
		errf("trace: --url required")
		return 2
	}
	sandboxPolicy, err := parseSandboxPolicy(*sandbox)
	if err != nil {
		errf("trace sandbox: %v", err)
		return 2
	}

	policy := network.PolicyConfig{AllowPrivateNetworks: *allowPrivate}
	if *allowPort != 0 {
		policy.AllowedPorts = []int{*allowPort}
	}
	_, policySink, resourceSink, err := newProcessDiagnostics("chromium", fmt.Sprintf("trace-%d", os.Getpid()))
	if err != nil {
		errf("trace diagnostics: %v", err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	browser, err := bridge.LaunchChromium(ctx, process.LaunchConfig{
		BinaryPath: *binary, Headless: true, Sandbox: sandboxPolicy,
		AllowPrivateNetworks: policy.AllowPrivateNetworks, AllowedPorts: policy.AllowedPorts,
		ResourceBudget: process.ResourceBudget{
			MaxCPUPercent: *maxCPU, MaxMemoryBytes: *maxMemory, MaxProfileDiskBytes: *maxProfile,
			SessionTimeout: *sessionTimeout,
		},
		PolicyDecisionSink: policySink, ResourceSink: resourceSink,
	})
	if err != nil {
		errf("trace launch: %v", err)
		return 1
	}
	emitProcessWarnings(browser)
	defer browser.Close()
	owner, err := browser.NewContext(ctx)
	if err != nil {
		errf("trace context: %v", err)
		return 1
	}
	defer owner.Close()
	page, err := owner.NewPage(ctx, "about:blank")
	if err != nil {
		errf("trace page: %v", err)
		return 1
	}
	trace, err := telemetry.NewBrowserTraceRecorder(page, telemetry.TraceRecordConfig{
		Screenshots: *screenshots, Snapshots: *snapshots, Sources: *sources, TraceDir: *traceDir,
	})
	if err != nil {
		errf("trace recorder: %v", err)
		return 1
	}
	if err := trace.Start(ctx); err != nil {
		errf("trace start: %v", err)
		return 1
	}
	defer func() {
		if trace.IsActive() {
			if _, cleanupErr := trace.Stop(context.Background()); cleanupErr != nil {
				errf("trace cleanup: %v", cleanupErr)
			}
		}
	}()

	t0 := time.Now()
	result := TraceResult{
		URL: *url, TargetID: page.TargetID(), BrowserContextID: page.BrowserContextID(),
	}
	result.Events = append(result.Events, TraceEvent{At: time.Since(t0).Milliseconds(), Stage: "trace_started"})
	if _, _, err := page.Navigate(ctx, *url); err != nil {
		errf("trace navigation: %v", err)
		return 1
	}
	if err := waitDocumentReady(ctx, page); err != nil {
		errf("trace readiness: %v", err)
		return 1
	}
	result.Events = append(result.Events, TraceEvent{At: time.Since(t0).Milliseconds(), Stage: "page_loaded", URL: *url, Status: 200})
	if title, titleErr := tracePageTitle(ctx, page); titleErr == nil && title != "" {
		result.Events = append(result.Events, TraceEvent{At: time.Since(t0).Milliseconds(), Stage: "title", Value: title})
	}
	tracePath, stopErr := trace.Stop(ctx)
	if stopErr != nil {
		errf("trace stop: %v", stopErr)
		return 1
	}
	result.TracePath = tracePath
	result.Events = append(result.Events, TraceEvent{At: time.Since(t0).Milliseconds(), Stage: "trace_stopped"})

	result.TotalMS = time.Since(t0).Milliseconds()

	if *format != "json" {
		for _, e := range result.Events {
			fmt.Printf("%+v\n", e)
		}
		return 0
	}

	if err := printJSON(os.Stdout, result); err != nil {
		errf("trace: %v", err)
		return 1
	}
	return 0
}

// TraceResult is the stable JSON output of the trace command.
type TraceResult struct {
	URL              string       `json:"url"`
	TargetID         string       `json:"targetId"`
	BrowserContextID string       `json:"browserContextId"`
	TracePath        string       `json:"tracePath"`
	TotalMS          int64        `json:"totalMs"`
	Events           []TraceEvent `json:"events"`
}

// TraceEvent is one trace stage.
type TraceEvent struct {
	At     int64  `json:"atMs"`
	Stage  string `json:"stage"`
	URL    string `json:"url,omitempty"`
	Status int    `json:"status,omitempty"`
	Value  string `json:"value,omitempty"`
}

func tracePageTitle(ctx context.Context, page *bridge.Page) (string, error) {
	var response struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	if err := page.Call(ctx, "Runtime.evaluate", traceTitleParams{Expression: "document.title", ReturnByValue: true}, &response); err != nil {
		return "", err
	}
	return response.Result.Value, nil
}

type traceTitleParams struct {
	Expression    string `json:"expression"`
	ReturnByValue bool   `json:"returnByValue"`
}
