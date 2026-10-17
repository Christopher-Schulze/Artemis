package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/Christopher-Schulze/Artemis"

	"github.com/Christopher-Schulze/Artemis/bridge"
	"github.com/Christopher-Schulze/Artemis/bridge/actions"
	bridgeobserve "github.com/Christopher-Schulze/Artemis/bridge/observe"
	"github.com/Christopher-Schulze/Artemis/engine"
	browserprocess "github.com/Christopher-Schulze/Artemis/process"
	"github.com/Christopher-Schulze/Artemis/serve"
)

func cmdAct(args []string) (exitCode int) {
	fs := newFlagSet("act")
	adBlock := addAdBlockFlag(fs)
	control := addControlFlags(fs)
	actionPolicyFlags := addBrowserPolicyFlags(fs)
	binary := fs.String("binary", "", "Chromium binary path")
	timeout := fs.Duration("timeout", 30*time.Second, "total action timeout")
	requestJSON := fs.String("request", "", "typed action request JSON")
	sandbox := fs.String("sandbox", string(browserprocess.SandboxRequired), "Chromium sandbox policy: required or disabled")
	maxCPU := fs.Float64("max-cpu-percent", browserprocess.DefaultMaxCPUPercent, "maximum Chromium process-group CPU percent")
	maxMemory := fs.Int64("max-memory-bytes", browserprocess.DefaultMaxMemoryBytes, "maximum Chromium process-group RSS bytes")
	maxProfile := fs.Int64("max-profile-bytes", browserprocess.DefaultMaxProfileDiskBytes, "maximum Chromium profile bytes")
	sessionTimeout := fs.Duration("session-timeout", browserprocess.DefaultSessionTimeout, "maximum Chromium session lifetime")
	fs.Usage = func() { fmt.Fprintln(os.Stderr, "usage: artemis act --request JSON [flags] <url>"); fs.PrintDefaults() }
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if (*control.server == "" && fs.NArg() != 1) || (*control.server != "" && (fs.NArg() != 0 || *control.pageID == "")) || *requestJSON == "" {
		fs.Usage()
		return 2
	}
	if *timeout <= 0 {
		errf("act timeout must be positive")
		return 2
	}
	if len(*requestJSON) > maxCLIRequestBytes {
		errf("act request exceeds %d bytes", maxCLIRequestBytes)
		return 2
	}
	var request actions.Request
	if err := json.Unmarshal([]byte(*requestJSON), &request); err != nil {
		errf("act request: %v", err)
		return 2
	}
	if err := actions.ValidateRequest(request); err != nil {
		errf("act request: %v", err)
		return 2
	}
	if *control.server != "" {
		if err := rejectLocalControlFlags(fs, controlAllowed("request")); err != nil {
			errf("act: %v", err)
			return 2
		}
		ctx, cancel := context.WithTimeout(context.Background(), *timeout)
		defer cancel()
		client, err := control.connect(ctx)
		if err != nil {
			errf("act: %v", err)
			return 1
		}
		defer cleanupOnReturn(&exitCode, "act control close", client.Close)()
		var result serve.ChromiumActResult
		trust, err := client.CallLabeled(ctx, serve.CmdChromiumAct, serve.ChromiumActParams{SessionID: *control.sessionID, PageID: *control.pageID, SnapshotEpoch: *control.epoch, Request: request}, &result)
		if err != nil {
			errf("act: %v", err)
			return 1
		}
		if trust == nil {
			errf("act: response has no trust label")
			return 1
		}
		trust.Source = "act"
		raw, err := json.Marshal(result.Outcome)
		if err == nil {
			err = writeLabeledJSON(os.Stdout, raw, *trust)
		}
		if err != nil {
			errf("act output: %v", err)
			return 1
		}
		if !result.Outcome.Success {
			return 1
		}
		return 0
	}
	sandboxPolicy, err := parseSandboxPolicy(*sandbox)
	if err != nil {
		errf("act sandbox: %v", err)
		return 2
	}
	budget := browserprocess.ResourceBudget{MaxCPUPercent: *maxCPU, MaxMemoryBytes: *maxMemory, MaxProfileDiskBytes: *maxProfile, SessionTimeout: *sessionTimeout}
	if err := validateCLIResourceBudget(budget); err != nil {
		errf("act: %v", err)
		return 2
	}
	_, policySink, resourceSink, err := newProcessDiagnostics("chromium", fmt.Sprintf("act-%d", os.Getpid()))
	if err != nil {
		errf("act diagnostics: %v", err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	policyConfig, err := actionPolicyFlags.config()
	if err != nil {
		errf("act action policy: %v", err)
		return 2
	}
	actionPolicy, err := artemis.NewBrowserPolicy(ctx, policyConfig)
	if err != nil {
		errf("act action policy: %v", err)
		return 1
	}
	defer actionPolicy.Stop()
	actionPolicy.StartHotReload()
	if err := checkCLIActionPolicy(ctx, actionPolicy, "navigate", fs.Arg(0)); err != nil {
		errf("act initial navigation policy: %v", err)
		return 1
	}
	requestedURL := fs.Arg(0)
	if request.Kind == actions.KindNavigate || request.Kind == actions.KindTabOpen {
		requestedURL = request.URL
	}
	if err := checkCLIActionPolicy(ctx, actionPolicy, string(request.Kind), requestedURL); err != nil {
		errf("act action policy: %v", err)
		return 1
	}
	browser, err := bridge.LaunchChromium(ctx, browserprocess.LaunchConfig{
		BinaryPath: *binary, Headless: true, Sandbox: sandboxPolicy,
		ResourceBudget:     budget,
		PolicyDecisionSink: policySink,
		ResourceSink:       resourceSink,
		DisableAdBlock:     !*adBlock,
	})
	if err != nil {
		errf("act launch: %v", err)
		return 1
	}
	emitProcessWarnings(browser)
	defer cleanupOnReturn(&exitCode, "act browser close", browser.Close)()
	owner, err := browser.NewContext(ctx)
	if err != nil {
		errf("act context: %v", err)
		return 1
	}
	defer cleanupOnReturn(&exitCode, "act context close", owner.Close)()
	scripts, err := envTargetScripts(ctx, browser, owner, fs.Arg(0))
	if err != nil {
		errf("act stealth scripts: %v", err)
		return 1
	}
	page, err := owner.NewPageWithScripts(ctx, fs.Arg(0), scripts)
	if err != nil {
		errf("act page: %v", err)
		return 1
	}
	defer cleanupOnReturn(&exitCode, "act page close", page.Close)()
	if err = waitDocumentReady(ctx, page, false); err != nil {
		errf("act readiness: %v", err)
		return 1
	}
	observer, err := bridgeobserve.NewCollector(page, bridgeobserve.DefaultConfig())
	if err != nil {
		errf("act observer: %v", err)
		return 1
	}
	if _, err = observer.Capture(ctx, bridgeobserve.ModeFull, ""); err != nil {
		errf("act initial observation: %v", err)
		return 1
	}
	inputMemory, closeMemory, err := engine.NewMemoryAdmission(engine.SessionBudget{})
	if err != nil {
		errf("act memory admission: %v", err)
		return 1
	}
	defer cleanupOnReturn(&exitCode, "act memory admission close", closeMemory)()
	runtime, err := actions.NewRuntimeWithConfig(page, observer, nil, actions.RuntimeConfig{PolicyEngine: actionPolicy, InputMemory: inputMemory})
	if err != nil {
		errf("act runtime: %v", err)
		return 1
	}
	defer cleanupOnReturn(&exitCode, "act runtime close", runtime.Close)()
	outcome := runtime.Execute(ctx, request)
	pageURL, err := page.CurrentURL(ctx)
	if err != nil {
		errf("act output origin: %v", err)
		return 1
	}
	raw, err := json.Marshal(outcome)
	if err == nil {
		raw, err = labelJSONObject(raw, pageURL, "act")
	}
	if err != nil {
		errf("act output: %v", err)
		return 1
	}
	if err = writeCLIOutput(os.Stdout, append(raw, '\n')); err != nil {
		errf("act output: %v", err)
		return 1
	}
	if !outcome.Success {
		return 1
	}
	return 0
}
