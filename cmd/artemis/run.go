package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/Christopher-Schulze/Artemis/engine"
	"github.com/Christopher-Schulze/Artemis/js"
	"github.com/Christopher-Schulze/Artemis/network"
	artemisrouter "github.com/Christopher-Schulze/Artemis/router"
)

// run.go (TASK-2343: `run --script FILE <url>` CLI subcommand).
//
// The `run` subcommand loads a user-provided JavaScript file and executes
// it in the page context after fetching the target URL. This is distinct
// from `fetch --run-scripts` (which runs page-owned inline <script> tags)
// and from `fetch --eval` (which evaluates a single inline expression):
// `run --script` loads a file from disk and executes its full contents as
// a JavaScript program in the page's V8 context.
//
// Usage:
//
//	artemis run --script FILE [flags] <url>
//
// The script executes with the same JS context as `fetch --eval`, with
// access to the DOM via the `document` global and to `fetch()` for
// sub-requests. The script's return value (the last evaluated expression)
// is printed to stdout. If --json is set, the result is printed as JSON.
//
// This subcommand is designed for external agents that need to run
// ergonomic standalone scripts against a page without the full steering
// protocol.

func cmdRun(args []string) (exitCode int) {
	fs := newFlagSet("run")
	scriptFile := fs.String("script", "", "path to a JavaScript file to execute in the page context (required)")
	userAgent := fs.String("user-agent", "", "override User-Agent")
	proxyURL := fs.String("proxy", "", "proxy URL")
	timeoutS := fs.String("timeout", "60s", "request + script execution timeout")
	maxBody := fs.Int64("max-body-bytes", 0, "max response body bytes (0 = engine default)")
	runInlineScripts := fs.Bool("run-inline-scripts", true, "execute page inline <script> tags before the user script")
	jsonOut := fs.Bool("json", false, "print the script result as JSON")
	consoleOn := fs.Bool("console", false, "forward JS console.* to stderr (slog)")
	waitIdle := fs.Bool("wait-idle", true, "wait for in-flight async fetches to settle before printing the result")
	allowPrivate := fs.Bool("allow-private-networks", false, "allow requests to private/loopback IP targets (default false)")
	allowPort := fs.Int("allow-port", 0, "allow a specific destination port (0 = default 80/443 only)")
	var headers stringSliceFlag
	fs.Var(&headers, "header", "extra header (k=v or k:v); repeatable")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `usage: artemis run --script FILE [flags] <url>

Loads a JavaScript file and executes it in the page context after fetching
the target URL. The script's return value is printed to stdout.

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *scriptFile == "" {
		errf("--script is required")
		fs.Usage()
		return 2
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}

	targetURL := fs.Arg(0)
	timeout, err := parseDuration(*timeoutS, 60*time.Second)
	if err != nil {
		errf("%v", err)
		return 2
	}
	hdrs, err := parseHeaderFlags(headers)
	if err != nil {
		errf("%v", err)
		return 2
	}
	diagnosticConfig, err := cliDiagnosticsConfig(false)
	if err != nil {
		errf("diagnostics config: %v", err)
		return 1
	}

	// Read the script file.
	scriptBytes, err := os.ReadFile(*scriptFile)
	if err != nil {
		errf("read script %s: %v", *scriptFile, err)
		return 1
	}
	scriptSrc := string(scriptBytes)
	if strings.TrimSpace(scriptSrc) == "" {
		errf("script file %s is empty", *scriptFile)
		return 1
	}

	cfg := engine.Config{
		UserAgent:    *userAgent,
		ProxyURL:     *proxyURL,
		Timeout:      timeout,
		MaxBodyBytes: *maxBody,
		SessionID:    fmt.Sprintf("cli-run-%d", os.Getpid()),
		Diagnostics:  diagnosticConfig,
		PolicyConfig: network.PolicyConfig{
			AllowPrivateNetworks: *allowPrivate,
		},
	}
	if *allowPort != 0 {
		cfg.PolicyConfig.AllowedPorts = []int{*allowPort}
	}
	eng, err := engine.New(cfg)
	if err != nil {
		errf("init engine: %v", err)
		return 1
	}
	defer cleanupOnReturn(&exitCode, "run engine close", eng.Close)()

	ctx, cancel := signalContext()
	defer cancel()

	var console js.Console
	if *consoleOn {
		console = js.FuncConsole(func(level, msg string) {
			switch level {
			case "error":
				slog.Error(msg, "src", "console")
			case "warn":
				slog.Warn(msg, "src", "console")
			case "debug":
				slog.Debug(msg, "src", "console")
			default:
				slog.Info(msg, "src", "console")
			}
		})
	}

	routeResult, err := routePage(ctx, eng, targetURL, *runInlineScripts, hdrs, console, artemisrouter.ActionFetch)
	if err != nil {
		errf("fetch %s: %v", targetURL, err)
		return 1
	}
	defer cleanupOnReturn(&exitCode, "run route resource close", routeResult.Close)()
	page, ok := routeResult.Resource.(*engine.Page)
	if !ok || page == nil {
		errf("fetch %s: router returned no page", targetURL)
		return 1
	}

	// Execute the user script.
	result, err := page.Eval(ctx, scriptSrc)
	if err != nil {
		errf("script execution failed: %v", err)
		return 1
	}

	// Wait for async operations to settle.
	if *waitIdle {
		if err := page.WaitIdle(ctx); err != nil {
			errf("wait-idle: %v", err)
			return 1
		}
	}

	// Print the result.
	if *jsonOut {
		printResultJSON(result)
	} else {
		printResultText(result)
	}
	return 0
}

// printResultText prints the script result as a string.
func printResultText(v *js.Value) {
	if v == nil {
		fmt.Println("")
		return
	}
	s := v.String()
	if s == "" || s == "undefined" {
		fmt.Println("")
		return
	}
	fmt.Println(s)
}

// printResultJSON prints the script result as JSON.
func printResultJSON(v *js.Value) {
	if v == nil {
		fmt.Println("null")
		return
	}
	s := v.String()
	// Try to parse as JSON first; if it's already JSON, print as-is.
	var raw json.RawMessage
	if err := json.Unmarshal([]byte(s), &raw); err == nil {
		fmt.Println(s)
		return
	}
	// Otherwise, wrap as a JSON string.
	out, marshalErr := json.Marshal(s)
	if marshalErr != nil {
		errf("result JSON: %v", marshalErr)
		return
	}
	fmt.Println(string(out))
}

// runScriptInPage is the testable core of the `run` subcommand. It fetches
// a URL, executes a script, and returns the result. This is extracted from
// cmdRun so tests can verify the logic without spawning the CLI.
func runScriptInPage(ctx context.Context, eng *engine.Engine, targetURL, scriptSrc string, opts engine.FetchOpts) (result string, resultErr error) {
	page, err := eng.Fetch(ctx, targetURL, opts)
	if err != nil {
		return "", fmt.Errorf("fetch %s: %w", targetURL, err)
	}
	defer func() {
		if closeErr := page.Close(); closeErr != nil {
			result = ""
			resultErr = errors.Join(resultErr, fmt.Errorf("close fetched page: %w", closeErr))
		}
	}()

	evalResult, err := page.Eval(ctx, scriptSrc)
	if err != nil {
		return "", fmt.Errorf("script execution: %w", err)
	}

	if err := page.WaitIdle(ctx); err != nil {
		return "", fmt.Errorf("wait-idle: %w", err)
	}

	if evalResult == nil {
		return "", nil
	}
	return evalResult.String(), nil
}
