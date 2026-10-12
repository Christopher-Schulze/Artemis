// Artemis - headless browser engine for AI agents.
// Copyright (c) 2026 Christopher Schulze.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	artemis "github.com/Christopher-Schulze/Artemis"
)

const Version = artemis.Version

func main() {
	if len(os.Args) < 2 {
		if err := printUsage(os.Stderr); err != nil {
			errf("usage: %v", err)
		}
		os.Exit(2)
	}
	switch os.Args[1] {
	case "version", "-v", "--version":
		fmt.Println(Version)
	case "help", "-h", "--help":
		if err := printUsage(os.Stdout); err != nil {
			errf("usage: %v", err)
			os.Exit(1)
		}
	case "capabilities":
		if err := printCapabilities(os.Stdout); err != nil {
			errf("capabilities: %v", err)
			os.Exit(1)
		}
	case "fetch":
		os.Exit(cmdFetch(os.Args[2:]))
	case "download":
		os.Exit(cmdDownload(os.Args[2:]))
	case "run":
		os.Exit(cmdRun(os.Args[2:]))
	case "serve":
		os.Exit(cmdServe(os.Args[2:]))
	case "observe":
		os.Exit(cmdObserve(os.Args[2:]))
	case "act":
		os.Exit(cmdAct(os.Args[2:]))
	case "session":
		os.Exit(cmdSession(os.Args[2:]))
	case "profile":
		os.Exit(cmdProfile(os.Args[2:]))
	case "doctor":
		os.Exit(cmdDoctor(os.Args[2:]))
	case "trace":
		os.Exit(cmdTrace(os.Args[2:]))
	case "diagnostics":
		os.Exit(cmdDiagnostics(os.Args[2:]))
	case "benchmark":
		os.Exit(cmdBenchmark(os.Args[2:]))
	default:
		errf("unknown command %q", os.Args[1])
		if err := printUsage(os.Stderr); err != nil {
			errf("usage: %v", err)
		}
		os.Exit(2)
	}
}

func printUsage(w io.Writer) error {
	if _, err := fmt.Fprintf(w, `artemis %s - headless browser engine

Usage:
  artemis <command> [flags] [args]

Commands:
  fetch      fetch a URL and dump html / markdown / text / title / links / structured / semantic
  download   fetch into the session-owned, policy-checked download store
  run        load a JavaScript file and execute it in the page context (--script FILE <url>)
  serve      run the JSON-over-WebSocket steering server
  observe    capture a bounded Chromium DOM/accessibility snapshot as JSON
  act        execute one typed Chromium action and emit evidence as JSON
  session    manage durable profile sessions
  profile    manage browser profiles
  doctor     diagnose the environment and runtime readiness
  trace      fetch a URL and emit a trace of lifecycle events
  diagnostics  read redacted policy and resource audit records
  benchmark  run the benchmark harness and emit a scorecard
  version    print version
  capabilities  print the machine-readable capability contract
  help       print this help

Run 'artemis <command> --help' for subcommand-specific flags.
`, Version); err != nil {
		return fmt.Errorf("write usage header: %w", err)
	}
	if _, err := fmt.Fprintln(w, "Release capability states:"); err != nil {
		return fmt.Errorf("write capability heading: %w", err)
	}
	for _, capability := range artemis.Capabilities() {
		if _, err := fmt.Fprintf(w, "  %-24s %-11s %s\n", capability.ID, capability.State, capability.Description); err != nil {
			return fmt.Errorf("write capability %q: %w", capability.ID, err)
		}
	}
	return nil
}

func printCapabilities(w io.Writer) error {
	if err := artemis.ValidateCapabilityRegistry(); err != nil {
		return err
	}
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(struct {
		Version      string               `json:"version"`
		Capabilities []artemis.Capability `json:"capabilities"`
	}{Version: Version, Capabilities: artemis.Capabilities()})
}
