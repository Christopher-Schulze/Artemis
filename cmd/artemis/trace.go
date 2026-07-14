package main

import (
	"context"
	"fmt"
	"os"
	"time"

	artemis "github.com/Christopher-Schulze/Artemis"
	"github.com/Christopher-Schulze/Artemis/network"
)

func cmdTrace(args []string) int {
	fs := newFlagSet("trace")
	url := fs.String("url", "", "URL to fetch (required)")
	runScripts := fs.Bool("run-scripts", false, "execute scripts during fetch")
	format := fs.String("format", "json", "output format: json")
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

	policy := network.PolicyConfig{AllowPrivateNetworks: *allowPrivate}
	if *allowPort != 0 {
		policy.AllowedPorts = []int{*allowPort}
	}
	cfg := artemis.AgentConfig{PolicyConfig: policy}
	agent, err := artemis.NewAgent(cfg)
	if err != nil {
		errf("trace: init agent: %v", err)
		return 1
	}
	defer agent.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := agent.Start(ctx); err != nil {
		errf("trace: start agent: %v", err)
		return 1
	}

	t0 := time.Now()
	result := TraceResult{
		URL: *url,
	}

	session, err := agent.CreateSession("trace")
	if err != nil {
		errf("trace: create session: %v", err)
		return 1
	}
	defer func() {
		_ = agent.CloseSession(session.SessionID())
	}()

	result.Events = append(result.Events, TraceEvent{At: time.Since(t0).Milliseconds(), Stage: "session_created"})

	pageID, page, openErr := session.OpenPage(ctx, *url, *runScripts)
	if openErr != nil {
		errf("trace: open page: %v", openErr)
		return 1
	}
	result.Events = append(result.Events, TraceEvent{At: time.Since(t0).Milliseconds(), Stage: "page_loaded", URL: page.URL(), Status: page.StatusCode()})

	if title := page.Title(); title != "" {
		result.Events = append(result.Events, TraceEvent{At: time.Since(t0).Milliseconds(), Stage: "title", Value: title})
	}

	if closeErr := session.ClosePage(pageID); closeErr != nil {
		errf("trace: close page: %v", closeErr)
		return 1
	}
	result.Events = append(result.Events, TraceEvent{At: time.Since(t0).Milliseconds(), Stage: "page_closed"})

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
	URL     string       `json:"url"`
	TotalMS int64        `json:"totalMs"`
	Events  []TraceEvent `json:"events"`
}

// TraceEvent is one trace stage.
type TraceEvent struct {
	At     int64  `json:"atMs"`
	Stage  string `json:"stage"`
	URL    string `json:"url,omitempty"`
	Status int    `json:"status,omitempty"`
	Value  string `json:"value,omitempty"`
}
