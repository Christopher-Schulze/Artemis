package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/Christopher-Schulze/Artemis/bridge"
	bridgeobserve "github.com/Christopher-Schulze/Artemis/bridge/observe"
	artemisobserve "github.com/Christopher-Schulze/Artemis/observe"
	browserprocess "github.com/Christopher-Schulze/Artemis/process"
	"github.com/Christopher-Schulze/Artemis/security"
	"github.com/Christopher-Schulze/Artemis/serve"
)

func cmdObserve(args []string) (exitCode int) {
	fs := newFlagSet("observe")
	adBlock := addAdBlockFlag(fs)
	control := addControlFlags(fs)
	policyFlags := addBrowserPolicyFlags(fs)
	binary := fs.String("binary", "", "Chromium binary path (auto-discovered when empty)")
	timeout := fs.Duration("timeout", 20*time.Second, "navigation and capture timeout")
	interactive := fs.Bool("interactive", false, "emit only interactive nodes")
	maxNodes := fs.Int("max-nodes", 2000, "maximum emitted nodes")
	sandbox := fs.String("sandbox", string(browserprocess.SandboxRequired), "Chromium sandbox policy: required or disabled")
	maxCPU := fs.Float64("max-cpu-percent", browserprocess.DefaultMaxCPUPercent, "maximum Chromium process-group CPU percent")
	maxMemory := fs.Int64("max-memory-bytes", browserprocess.DefaultMaxMemoryBytes, "maximum Chromium process-group RSS bytes")
	maxProfile := fs.Int64("max-profile-bytes", browserprocess.DefaultMaxProfileDiskBytes, "maximum Chromium profile bytes")
	sessionTimeout := fs.Duration("session-timeout", browserprocess.DefaultSessionTimeout, "maximum Chromium session lifetime")
	format := fs.String("format", string(formatEvidence), "output format: json, har or ndjson")
	fs.Usage = func() { fmt.Fprintln(os.Stderr, "usage: artemis observe [flags] <url>"); fs.PrintDefaults() }
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *control.server != "" {
		if fs.NArg() != 0 || *control.pageID == "" {
			errf("persistent observe requires --page and no URL (use chromium.open to create a page)")
			return 2
		}
		if err := rejectLocalControlFlags(fs, controlAllowed("format", "interactive")); err != nil {
			errf("observe: %v", err)
			return 2
		}
		if *timeout <= 0 {
			errf("observe timeout must be positive")
			return 2
		}
		outputFormat, err := parseObserveFormat(*format)
		if err != nil {
			errf("observe: %v", err)
			return 2
		}
		ctx, cancel := context.WithTimeout(context.Background(), *timeout)
		defer cancel()
		client, err := control.connect(ctx)
		if err != nil {
			errf("observe: %v", err)
			return 1
		}
		defer cleanupOnReturn(&exitCode, "observe control close", client.Close)()
		mode := bridgeobserve.ModeFull
		if *interactive {
			mode = bridgeobserve.ModeInteractive
		}
		var evidence artemisobserve.ObservationEvidence
		trust, err := client.CallLabeled(ctx, serve.CmdChromiumObserve, serve.ChromiumObserveParams{SessionID: *control.sessionID, PageID: *control.pageID, Mode: string(mode)}, &evidence)
		if err != nil {
			errf("observe: %v", err)
			return 1
		}
		if trust == nil {
			errf("observe: response has no trust label")
			return 1
		}
		if err := writeObserveOutput(os.Stdout, outputFormat, evidence, trust.Origin); err != nil {
			errf("observe output: %v", err)
			return 1
		}
		return 0
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}
	if *timeout <= 0 || *maxNodes <= 0 {
		errf("observe timeout and max-nodes must be positive")
		return 2
	}
	// Reject an unknown format before launching Chromium so a typo costs
	// nothing instead of a full browser session.
	outputFormat, err := parseObserveFormat(*format)
	if err != nil {
		errf("observe format: %v", err)
		return 2
	}
	sandboxPolicy, err := parseSandboxPolicy(*sandbox)
	if err != nil {
		errf("observe sandbox: %v", err)
		return 2
	}
	budget := browserprocess.ResourceBudget{MaxCPUPercent: *maxCPU, MaxMemoryBytes: *maxMemory, MaxProfileDiskBytes: *maxProfile, SessionTimeout: *sessionTimeout}
	if err := validateCLIResourceBudget(budget); err != nil {
		errf("observe: %v", err)
		return 2
	}
	_, policySink, resourceSink, err := newProcessDiagnostics("chromium", fmt.Sprintf("observe-%d", os.Getpid()))
	if err != nil {
		errf("observe diagnostics: %v", err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	actionPolicy, err := policyFlags.start(ctx)
	if err != nil {
		errf("observe policy: %v", err)
		return 1
	}
	defer actionPolicy.Stop()
	for _, action := range []string{"navigate", "observe"} {
		if err := checkCLIActionPolicy(ctx, actionPolicy, action, fs.Arg(0)); err != nil {
			errf("observe policy: %v", err)
			return 1
		}
	}
	browser, err := bridge.LaunchChromium(ctx, browserprocess.LaunchConfig{
		BinaryPath: *binary, Headless: true, Sandbox: sandboxPolicy,
		ResourceBudget:     budget,
		PolicyDecisionSink: policySink,
		ResourceSink:       resourceSink,
		DisableAdBlock:     !*adBlock,
	})
	if err != nil {
		errf("observe launch: %v", err)
		return 1
	}
	emitProcessWarnings(browser)
	defer cleanupOnReturn(&exitCode, "observe browser close", browser.Close)()
	browserContext, err := browser.NewContext(ctx)
	if err != nil {
		errf("observe context: %v", err)
		return 1
	}
	defer cleanupOnReturn(&exitCode, "observe context close", browserContext.Close)()
	scripts, err := envTargetScripts(ctx, browser, browserContext, fs.Arg(0))
	if err != nil {
		errf("observe stealth scripts: %v", err)
		return 1
	}
	page, err := browserContext.NewPageWithScripts(ctx, fs.Arg(0), scripts)
	if err != nil {
		errf("observe page: %v", err)
		return 1
	}
	defer cleanupOnReturn(&exitCode, "observe page close", page.Close)()
	config := bridgeobserve.DefaultConfig()
	config.MaxNodes = *maxNodes
	liveConfig := artemisobserve.DefaultLiveConfig()
	liveConfig.Observation = config
	collector, err := artemisobserve.NewLiveCollector(page, page, liveConfig)
	if err != nil {
		errf("observe collector: %v", err)
		return 1
	}
	defer cleanupOnReturn(&exitCode, "observe collector close", collector.Close)()
	if err = waitDocumentReady(ctx, page, false); err != nil {
		errf("observe readiness: %v", err)
		return 1
	}
	mode := bridgeobserve.ModeFull
	if *interactive {
		mode = bridgeobserve.ModeInteractive
	}
	target, err := page.FramePolicyURL(ctx, "")
	if err != nil {
		errf("observe policy context: %v", err)
		return 1
	}
	if err := checkCLIActionPolicy(ctx, actionPolicy, "observe", target); err != nil {
		errf("observe policy: %v", err)
		return 1
	}
	evidence, err := collector.Capture(ctx, mode, "")
	if err != nil {
		errf("observe capture: %v", err)
		return 1
	}
	pageURL, err := page.CurrentURL(ctx)
	if err != nil {
		errf("observe output origin: %v", err)
		return 1
	}
	if err = writeObserveOutput(os.Stdout, outputFormat, evidence, pageURL); err != nil {
		errf("observe output: %v", err)
		return 1
	}
	return 0
}

// observeFormat selects how a capture is written to stdout.
type observeFormat string

const (
	// formatEvidence emits the full ObservationEvidence document.
	formatEvidence observeFormat = "json"
	// formatHAR emits the captured network events as an HTTP Archive.
	formatHAR observeFormat = "har"
	// formatNDJSON emits one captured network event per line.
	formatNDJSON observeFormat = "ndjson"
)

// parseObserveFormat validates the --format value.
func parseObserveFormat(value string) (observeFormat, error) {
	switch observeFormat(strings.ToLower(strings.TrimSpace(value))) {
	case formatEvidence:
		return formatEvidence, nil
	case formatHAR:
		return formatHAR, nil
	case formatNDJSON:
		return formatNDJSON, nil
	default:
		return "", fmt.Errorf("unsupported format %q: want json, har or ndjson", value)
	}
}

// writeObserveOutput renders one capture in the selected format. The HAR and
// NDJSON paths route the captured network events through the spec-mandated
// formatters in observe/format.go (spec L4024).
func writeObserveOutput(out io.Writer, format observeFormat, evidence artemisobserve.ObservationEvidence, pageURL string) error {
	switch format {
	case formatHAR, formatNDJSON:
		target := artemisobserve.OutputFormatHAR
		if format == formatNDJSON {
			target = artemisobserve.OutputFormatNDJSON
		}
		encoded, err := artemisobserve.FormatOutput(target, evidence.Network)
		if err != nil {
			return err
		}
		trust, err := newCLIIDPI().Label(pageURL, "observe", encoded)
		if err != nil {
			return err
		}
		if format == formatHAR {
			if encoded, err = security.WithHARTrustMember(encoded, trust); err != nil {
				return err
			}
			return writeCLIOutput(out, append(encoded, '\n'))
		}
		return writeLabeledNDJSON(out, encoded, trust)
	default:
		return printLabeledJSON(out, evidence, pageURL, "observe")
	}
}

// writeLabeledNDJSON adds the trust member to every NDJSON record.
func writeLabeledNDJSON(out io.Writer, encoded []byte, trust security.ContentTrust) error {
	var labeled bytes.Buffer
	for _, line := range bytes.Split(encoded, []byte{'\n'}) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		record, err := security.WithTrustMember(line, trust)
		if err != nil {
			return err
		}
		labeled.Write(record)
		labeled.WriteByte('\n')
	}
	return writeCLIOutput(out, labeled.Bytes())
}

func waitDocumentReady(ctx context.Context, page *bridge.Page, requireComplete bool) error {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		var result struct {
			Result struct {
				Value any `json:"value"`
			} `json:"result"`
		}
		err := page.Call(ctx, "Runtime.evaluate", map[string]any{"expression": "document.readyState", "returnByValue": true}, &result)
		if err == nil && (result.Result.Value == "complete" || (!requireComplete && result.Result.Value == "interactive")) {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("document readiness: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}
