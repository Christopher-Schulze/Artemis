package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Christopher-Schulze/Artemis/bridge"
	"github.com/Christopher-Schulze/Artemis/diagnostics"
	"github.com/Christopher-Schulze/Artemis/network"
	browserprocess "github.com/Christopher-Schulze/Artemis/process"
)

type stringSliceFlag []string

func (s *stringSliceFlag) String() string { return strings.Join(*s, ",") }
func (s *stringSliceFlag) Set(v string) error {
	*s = append(*s, v)
	return nil
}

func parseHeaderFlags(values []string) (http.Header, error) {
	if len(values) == 0 {
		return nil, nil
	}
	h := make(http.Header)
	for _, raw := range values {
		k, v, ok := strings.Cut(raw, "=")
		if !ok {
			k, v, ok = strings.Cut(raw, ":")
		}
		if !ok || strings.TrimSpace(k) == "" {
			return nil, fmt.Errorf("invalid --header %q (expected k=v or k:v)", raw)
		}
		h.Add(strings.TrimSpace(k), strings.TrimSpace(v))
	}
	return h, nil
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

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	return fs
}

func printJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func parseSandboxPolicy(raw string) (browserprocess.SandboxPolicy, error) {
	policy := browserprocess.SandboxPolicy(raw)
	if policy != browserprocess.SandboxRequired && policy != browserprocess.SandboxDisabled {
		return "", fmt.Errorf("invalid sandbox policy %q (expected required or disabled)", raw)
	}
	return policy, nil
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
			SessionRef: diagnostics.HashSession(sessionID),
		})
	}
	resourceSink := func(usage browserprocess.ResourceUsage) error {
		return store.AppendResource(diagnostics.ResourceUsage{
			Scope: scope, SessionRef: diagnostics.HashSession(sessionID), CPUPercent: usage.CPUPercent,
			MemoryBytes: usage.MemoryBytes, DiskBytes: usage.ProfileDiskBytes,
		})
	}
	return store, policySink, resourceSink, nil
}
