package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Christopher-Schulze/Artemis/agent"
	"github.com/Christopher-Schulze/Artemis/engine"
	"github.com/Christopher-Schulze/Artemis/js"
	"github.com/Christopher-Schulze/Artemis/network"
	artemisrouter "github.com/Christopher-Schulze/Artemis/router"
)

func cmdFetch(args []string) int {
	fs := newFlagSet("fetch")
	dump := fs.String("dump", "markdown", "what to print: html, markdown, text, title")
	userAgent := fs.String("user-agent", "", "override User-Agent")
	proxyURL := fs.String("proxy", "", "proxy URL")
	timeoutS := fs.String("timeout", "30s", "request timeout")
	maxBody := fs.Int64("max-body-bytes", 0, "max response body bytes (0 = engine default)")
	runScripts := fs.Bool("run-scripts", false, "execute inline <script> tags after parse")
	evalExpr := fs.String("eval", "", "JS expression to evaluate after fetch (printed to stdout, replacing --dump)")
	consoleOn := fs.Bool("console", false, "forward JS console.* to stderr (slog)")
	allowPrivate := fs.Bool("allow-private-networks", false, "allow requests to private/loopback IP targets (default false)")
	allowPort := fs.Int("allow-port", 0, "allow a specific destination port (0 = default 80/443 only)")
	var headers stringSliceFlag
	fs.Var(&headers, "header", "extra header (k=v or k:v); repeatable")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `usage: artemis fetch [flags] <url>

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}

	url := fs.Arg(0)
	timeout, err := parseDuration(*timeoutS, 30*time.Second)
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

	cfg := engine.Config{
		UserAgent:    *userAgent,
		ProxyURL:     *proxyURL,
		Timeout:      timeout,
		MaxBodyBytes: *maxBody,
		SessionID:    fmt.Sprintf("cli-fetch-%d", os.Getpid()),
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
	defer eng.Close()

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

	routeResult, err := routePage(ctx, eng, url, *runScripts, hdrs, console, artemisrouter.ActionFetch)
	if err != nil {
		errf("fetch %s: %v", url, err)
		return 1
	}
	defer routeResult.Close()
	page, ok := routeResult.Resource.(*engine.Page)
	if !ok || page == nil {
		errf("fetch %s: router returned no page", url)
		return 1
	}

	if *evalExpr != "" {
		v, err := page.Eval(ctx, *evalExpr)
		if err != nil {
			errf("eval: %v", err)
			return 1
		}
		fmt.Println(v.String())
		return 0
	}

	switch *dump {
	case "html":
		fmt.Println(page.HTML())
	case "markdown", "md":
		fmt.Println(page.Markdown())
	case "text":
		fmt.Println(page.Text())
	case "title":
		fmt.Println(page.Title())
	case "links":
		for _, l := range page.Links() {
			fmt.Printf("%s\t%s\n", l.Href, l.Text)
		}
	case "structured":
		out, _ := json.MarshalIndent(page.StructuredData(), "", "  ")
		fmt.Println(string(out))
	case "semantic":
		fmt.Println(agent.SemanticString(page.SemanticTree()))
	default:
		errf("unknown --dump %q (allowed: html, markdown, text, title, links, structured, semantic)", *dump)
		return 2
	}
	return 0
}

func signalContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-ch:
			cancel()
		case <-ctx.Done():
		}
		signal.Stop(ch)
	}()
	return ctx, cancel
}
