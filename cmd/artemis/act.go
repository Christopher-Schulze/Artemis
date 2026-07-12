package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/Christopher-Schulze/Artemis/bridge"
	"github.com/Christopher-Schulze/Artemis/bridge/actions"
	bridgeobserve "github.com/Christopher-Schulze/Artemis/bridge/observe"
	browserprocess "github.com/Christopher-Schulze/Artemis/process"
)

func cmdAct(args []string) int {
	fs := newFlagSet("act")
	binary := fs.String("binary", "", "Chromium binary path")
	timeout := fs.Duration("timeout", 30*time.Second, "total action timeout")
	requestJSON := fs.String("request", "", "typed action request JSON")
	fs.Usage = func() { fmt.Fprintln(os.Stderr, "usage: artemis act --request JSON [flags] <url>"); fs.PrintDefaults() }
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 || *requestJSON == "" {
		fs.Usage()
		return 2
	}
	var request actions.Request
	if err := json.Unmarshal([]byte(*requestJSON), &request); err != nil {
		errf("act request: %v", err)
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	browser, err := bridge.LaunchChromium(ctx, browserprocess.LaunchConfig{BinaryPath: *binary, Headless: true})
	if err != nil {
		errf("act launch: %v", err)
		return 1
	}
	defer browser.Close()
	owner, err := browser.NewContext(ctx)
	if err != nil {
		errf("act context: %v", err)
		return 1
	}
	defer owner.Close()
	page, err := owner.NewPage(ctx, fs.Arg(0))
	if err != nil {
		errf("act page: %v", err)
		return 1
	}
	if err = waitDocumentReady(ctx, page); err != nil {
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
	runtime, err := actions.NewRuntime(page, observer, nil)
	if err != nil {
		errf("act runtime: %v", err)
		return 1
	}
	outcome := runtime.Execute(ctx, request)
	if err = json.NewEncoder(os.Stdout).Encode(outcome); err != nil {
		errf("act output: %v", err)
		return 1
	}
	if !outcome.Success {
		return 1
	}
	return 0
}
