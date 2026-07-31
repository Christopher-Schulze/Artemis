package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/Christopher-Schulze/Artemis/bridge"
	bridgeobserve "github.com/Christopher-Schulze/Artemis/bridge/observe"
	artemisobserve "github.com/Christopher-Schulze/Artemis/observe"
	browserprocess "github.com/Christopher-Schulze/Artemis/process"
)

func cmdObserve(args []string) int {
	fs := newFlagSet("observe")
	binary := fs.String("binary", "", "Chromium binary path (auto-discovered when empty)")
	timeout := fs.Duration("timeout", 20*time.Second, "navigation and capture timeout")
	interactive := fs.Bool("interactive", false, "emit only interactive nodes")
	maxNodes := fs.Int("max-nodes", 2000, "maximum emitted nodes")
	sandbox := fs.String("sandbox", string(browserprocess.SandboxRequired), "Chromium sandbox policy: required or disabled")
	maxCPU := fs.Float64("max-cpu-percent", browserprocess.DefaultMaxCPUPercent, "maximum Chromium process-group CPU percent")
	maxMemory := fs.Int64("max-memory-bytes", browserprocess.DefaultMaxMemoryBytes, "maximum Chromium process-group RSS bytes")
	maxProfile := fs.Int64("max-profile-bytes", browserprocess.DefaultMaxProfileDiskBytes, "maximum Chromium profile bytes")
	sessionTimeout := fs.Duration("session-timeout", browserprocess.DefaultSessionTimeout, "maximum Chromium session lifetime")
	fs.Usage = func() { fmt.Fprintln(os.Stderr, "usage: artemis observe [flags] <url>"); fs.PrintDefaults() }
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}
	sandboxPolicy, err := parseSandboxPolicy(*sandbox)
	if err != nil {
		errf("observe sandbox: %v", err)
		return 2
	}
	_, policySink, resourceSink, err := newProcessDiagnostics("chromium", fmt.Sprintf("observe-%d", os.Getpid()))
	if err != nil {
		errf("observe diagnostics: %v", err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	browser, err := bridge.LaunchChromium(ctx, browserprocess.LaunchConfig{
		BinaryPath: *binary, Headless: true, Sandbox: sandboxPolicy,
		ResourceBudget: browserprocess.ResourceBudget{
			MaxCPUPercent: *maxCPU, MaxMemoryBytes: *maxMemory, MaxProfileDiskBytes: *maxProfile,
			SessionTimeout: *sessionTimeout,
		},
		PolicyDecisionSink: policySink,
		ResourceSink:       resourceSink,
	})
	if err != nil {
		errf("observe launch: %v", err)
		return 1
	}
	emitProcessWarnings(browser)
	defer browser.Close()
	browserContext, err := browser.NewContext(ctx)
	if err != nil {
		errf("observe context: %v", err)
		return 1
	}
	defer browserContext.Close()
	page, err := browserContext.NewPage(ctx, "about:blank")
	if err != nil {
		errf("observe page: %v", err)
		return 1
	}
	config := bridgeobserve.DefaultConfig()
	config.MaxNodes = *maxNodes
	liveConfig := artemisobserve.DefaultLiveConfig()
	liveConfig.Observation = config
	collector, err := artemisobserve.NewLiveCollector(page, page, liveConfig)
	if err != nil {
		errf("observe collector: %v", err)
		return 1
	}
	defer collector.Close()
	if _, _, err = page.Navigate(ctx, fs.Arg(0)); err != nil {
		errf("observe navigation: %v", err)
		return 1
	}
	if err = waitDocumentReady(ctx, page); err != nil {
		errf("observe readiness: %v", err)
		return 1
	}
	mode := bridgeobserve.ModeFull
	if *interactive {
		mode = bridgeobserve.ModeInteractive
	}
	evidence, err := collector.Capture(ctx, mode, "")
	if err != nil {
		errf("observe capture: %v", err)
		return 1
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err = encoder.Encode(evidence); err != nil {
		errf("observe output: %v", err)
		return 1
	}
	return 0
}

func waitDocumentReady(ctx context.Context, page *bridge.Page) error {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		var result struct {
			Result struct {
				Value any `json:"value"`
			} `json:"result"`
		}
		err := page.Call(ctx, "Runtime.evaluate", map[string]any{"expression": "document.readyState", "returnByValue": true}, &result)
		if err == nil && (result.Result.Value == "interactive" || result.Result.Value == "complete") {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("document readiness: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}
