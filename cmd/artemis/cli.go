package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Christopher-Schulze/Artemis/bridge"
	"github.com/Christopher-Schulze/Artemis/diagnostics"
	"github.com/Christopher-Schulze/Artemis/network"
	browserprocess "github.com/Christopher-Schulze/Artemis/process"
	"github.com/Christopher-Schulze/Artemis/profile"
	"github.com/Christopher-Schulze/Artemis/stealth"
)

// envTargetScripts derives the measured stealth pre-script contract when the
// operator opted in via ARTEMIS_STEALTH (+ PURPOSE/LEGAL_BASIS ack). An
// incomplete env policy is a hard error — silently browsing unstealthed is
// how operators get CAPTCHA walls.
func envTargetScripts(ctx context.Context, browser *bridge.ChromiumBrowser, bc *bridge.BrowserContext, url string) (bridge.TargetScriptConfig, error) {
	policy, requested, err := stealth.PolicyFromEnv()
	if err != nil {
		return bridge.TargetScriptConfig{}, err
	}
	if !requested {
		return bridge.TargetScriptConfig{}, nil
	}
	return profile.PrepareTargetScripts(ctx, browser, bc, profile.SessionID("cli"), url, policy)
}

type stringSliceFlag []string

const (
	maxCLIFlagValues   = 128
	maxCLIFlagBytes    = 32 << 10
	maxCLIOutputBytes  = 16 << 20
	maxCLIRequestBytes = 1 << 20
)

func (s *stringSliceFlag) String() string { return strings.Join(*s, ",") }
func (s *stringSliceFlag) Set(v string) error {
	if len(*s) >= maxCLIFlagValues || len(v) > maxCLIFlagBytes {
		return fmt.Errorf("repeatable flag exceeds %d values or %d bytes", maxCLIFlagValues, maxCLIFlagBytes)
	}
	bytes := len(v)
	for _, value := range *s {
		bytes += len(value)
	}
	if bytes > maxCLIFlagBytes {
		return fmt.Errorf("repeatable flag exceeds %d total bytes", maxCLIFlagBytes)
	}
	*s = append(*s, v)
	return nil
}

func parseHeaderFlags(values []string) (http.Header, error) {
	if len(values) == 0 {
		return nil, nil
	}
	if len(values) > maxCLIFlagValues {
		return nil, fmt.Errorf("header flags exceed %d entries", maxCLIFlagValues)
	}
	h := make(http.Header)
	bytes := 0
	for _, raw := range values {
		if len(raw) > maxCLIFlagBytes-bytes {
			return nil, fmt.Errorf("header flags exceed %d bytes", maxCLIFlagBytes)
		}
		bytes += len(raw)
		separator := strings.IndexAny(raw, "=:")
		if separator < 1 {
			return nil, fmt.Errorf("invalid --header (expected k=v or k:v)")
		}
		k, v := raw[:separator], raw[separator+1:]
		h.Add(strings.Trim(k, " \t"), strings.Trim(v, " \t"))
	}
	return network.ValidatedHeaders(h)
}

func parseDuration(s string, def time.Duration) (time.Duration, error) {
	if s == "" {
		return def, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: %w", s, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("invalid duration %q: must be positive", s)
	}
	return d, nil
}

func errf(format string, args ...any) {
	fmt.Fprintln(os.Stderr, "artemis: "+fmt.Sprintf(format, args...))
}

func cleanupOnReturn(exitCode *int, label string, close func() error) func() {
	return func() {
		if closeErr := close(); closeErr != nil {
			errf("%s: %v", label, closeErr)
			if *exitCode == 0 {
				*exitCode = 1
			}
		}
	}
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	return fs
}

func printJSON(w io.Writer, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if len(raw) >= maxCLIOutputBytes {
		return fmt.Errorf("JSON output exceeds %d bytes including newline", maxCLIOutputBytes)
	}
	return writeCLIOutput(w, append(raw, '\n'))
}

func writeCLIOutput(w io.Writer, data []byte) error {
	if len(data) > maxCLIOutputBytes {
		return fmt.Errorf("output exceeds %d bytes", maxCLIOutputBytes)
	}
	n, err := w.Write(data)
	if err == nil && n != len(data) {
		return io.ErrShortWrite
	}
	return err
}

func printText(w io.Writer, text string) error {
	if len(text) >= maxCLIOutputBytes {
		return fmt.Errorf("text output exceeds %d bytes including newline", maxCLIOutputBytes)
	}
	n, err := fmt.Fprintln(w, text)
	if err == nil && n != len(text)+1 {
		return io.ErrShortWrite
	}
	return err
}

func parseSandboxPolicy(raw string) (browserprocess.SandboxPolicy, error) {
	policy := browserprocess.SandboxPolicy(raw)
	if policy != browserprocess.SandboxRequired && policy != browserprocess.SandboxDisabled {
		return "", fmt.Errorf("invalid sandbox policy %q (expected required or disabled)", raw)
	}
	return policy, nil
}

func validateCLIResourceBudget(budget browserprocess.ResourceBudget) error {
	if math.IsNaN(budget.MaxCPUPercent) || math.IsInf(budget.MaxCPUPercent, 0) || budget.MaxCPUPercent <= 0 || budget.MaxMemoryBytes <= 0 || budget.MaxProfileDiskBytes <= 0 || budget.SessionTimeout < time.Millisecond {
		return fmt.Errorf("Chromium CPU/memory/profile limits must be finite and positive; session-timeout must be at least 1ms")
	}
	return nil
}

func emitProcessWarnings(browser *bridge.ChromiumBrowser) {
	for _, warning := range browser.ProcessWarnings() {
		fmt.Fprintln(os.Stderr, "artemis: WARNING: "+warning)
	}
}

func cliDiagnosticsConfig(persistent bool) (diagnostics.Config, error) {
	if path := strings.TrimSpace(os.Getenv("ARTEMIS_DIAGNOSTICS_FILE")); path != "" {
		return diagnostics.Config{Path: path}, nil
	}
	if !persistent {
		return diagnostics.Config{}, nil
	}
	return diagnostics.DefaultPersistentConfig()
}

func newProcessDiagnostics(scope, sessionID string) (*diagnostics.Store, network.DecisionSink, browserprocess.ResourceSink, error) {
	config, err := cliDiagnosticsConfig(true)
	if err != nil {
		return nil, nil, nil, err
	}
	store, err := diagnostics.NewStore(config)
	if err != nil {
		return nil, nil, nil, err
	}
	policySink := func(decision network.Decision) error {
		return store.AppendPolicy(diagnostics.PolicyDecision{
			Operation: string(decision.Kind), Transport: decision.Scheme, Host: decision.Host,
			Port: decision.Port, Result: string(decision.Action), ReasonCode: decision.Reason,
			SessionRef: store.SessionRef(sessionID),
		})
	}
	resourceSink := func(usage browserprocess.ResourceUsage) error {
		return store.AppendResource(diagnostics.ResourceUsage{
			Scope: scope, SessionRef: store.SessionRef(sessionID), CPUPercent: usage.CPUPercent,
			MemoryBytes: usage.MemoryBytes, DiskBytes: usage.ProfileDiskBytes,
		})
	}
	return store, policySink, resourceSink, nil
}
