package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Christopher-Schulze/Artemis/agent"
	"github.com/Christopher-Schulze/Artemis/engine"
	"github.com/Christopher-Schulze/Artemis/js"
	"github.com/Christopher-Schulze/Artemis/network"
	artemisrouter "github.com/Christopher-Schulze/Artemis/router"
	"github.com/Christopher-Schulze/Artemis/serve"
)

func cmdFetch(args []string) (exitCode int) {
	fs := newFlagSet("fetch")
	adBlock := addAdBlockFlag(fs)
	control := addControlFlags(fs)
	policyFlags := addBrowserPolicyFlags(fs)
	dump := fs.String("dump", "markdown", "what to print: markdown, text, html, title, links, structured, semantic")
	userAgent := fs.String("user-agent", "", "override User-Agent")
	proxyURL := fs.String("proxy", "", "proxy URL")
	timeoutS := fs.String("timeout", "30s", "request timeout")
	maxBody := fs.Int64("max-body-bytes", 0, "max response body bytes (0 = engine default)")
	runScripts := fs.Bool("run-scripts", false, "execute inline <script> tags after parse")
	crawl := fs.Bool("crawl", false, "crawler traffic: respect robots.txt, including every redirect hop")
	fence := fs.Bool("fence", false, "wrap page-derived text in the spoof-proof IDPI untrusted-content fence")
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
	switch *dump {
	case "html", "markdown", "text", "title", "links", "structured", "semantic":
	case "md":
		*dump = "markdown"
	default:
		errf("unknown --dump %q (allowed: html, markdown, text, title, links, structured, semantic)", *dump)
		return 2
	}
	if *maxBody < 0 || *allowPort < 0 || *allowPort > 65535 {
		errf("fetch requires non-negative body limit and destination port within 0..65535")
		return 2
	}
	if *control.server != "" {
		if fs.NArg() != 0 || *control.pageID == "" {
			errf("persistent fetch requires --page and no URL")
			return 2
		}
		if err := rejectLocalControlFlags(fs, controlAllowed("dump", "fence")); err != nil {
			errf("fetch: %v", err)
			return 2
		}
		timeout, err := parseDuration(*timeoutS, 30*time.Second)
		if err != nil {
			errf("fetch: %v", err)
			return 2
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		client, err := control.connect(ctx)
		if err != nil {
			errf("fetch: %v", err)
			return 1
		}
		defer cleanupOnReturn(&exitCode, "fetch control close", client.Close)()
		var result struct {
			Data json.RawMessage `json:"data"`
		}
		trust, err := client.CallLabeled(ctx, serve.CmdPageDump, serve.PageDumpParams{SessionID: *control.sessionID, PageID: *control.pageID, Format: *dump}, &result)
		if err != nil {
			errf("fetch: %v", err)
			return 1
		}
		if trust == nil {
			errf("fetch: response has no trust label")
			return 1
		}
		trust.Source = "fetch"
		var text string
		if json.Unmarshal(result.Data, &text) == nil {
			if err := printFetchText(*fence, text, trust.Origin); err != nil {
				errf("fetch output: %v", err)
				return 1
			}
			return 0
		}
		if err := writeLabeledJSON(os.Stdout, result.Data, *trust); err != nil {
			errf("fetch output: %v", err)
			return 1
		}
		return 0
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
		DisableAdBlock: !*adBlock,
		ObeyRobots:     *crawl,
		UserAgent:      *userAgent,
		ProxyURL:       *proxyURL,
		Timeout:        timeout,
		MaxBodyBytes:   *maxBody,
		SessionID:      fmt.Sprintf("cli-fetch-%d", os.Getpid()),
		Diagnostics:    diagnosticConfig,
		PolicyConfig: network.PolicyConfig{
			AllowPrivateNetworks: *allowPrivate,
		},
	}
	if *allowPort != 0 {
		cfg.PolicyConfig.AllowedPorts = []int{*allowPort}
	}
	ctx, cancel := signalContext()
	defer cancel()
	actionPolicy, err := policyFlags.start(ctx)
	if err != nil {
		errf("fetch policy: %v", err)
		return 1
	}
	defer actionPolicy.Stop()
	if err := checkCLIActionPolicy(ctx, actionPolicy, "navigate", url); err != nil {
		errf("fetch policy: %v", err)
		return 1
	}
	if *evalExpr != "" {
		if err := checkCLIActionPolicy(ctx, actionPolicy, "evaluate", url); err != nil {
			errf("fetch policy: %v", err)
			return 1
		}
	}
	eng, err := engine.New(cfg)
	if err != nil {
		errf("init engine: %v", err)
		return 1
	}
	defer cleanupOnReturn(&exitCode, "fetch engine close", eng.Close)()

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
	defer cleanupOnReturn(&exitCode, "fetch route resource close", routeResult.Close)()
	page, ok := routeResult.Resource.(*engine.Page)
	if !ok || page == nil {
		errf("fetch %s: router returned no page", url)
		return 1
	}

	if *evalExpr != "" {
		if err := checkCLIActionPolicy(ctx, actionPolicy, "evaluate", page.URL()); err != nil {
			errf("fetch policy: %v", err)
			return 1
		}
		v, err := page.Eval(ctx, *evalExpr)
		if err != nil {
			errf("eval: %v", err)
			return 1
		}
		defer v.Release()
		if err := printFetchText(*fence, v.String(), page.URL()); err != nil {
			errf("fetch output: %v", err)
			return 1
		}
		return 0
	}

	if err := checkCLIActionPolicy(ctx, actionPolicy, "extract", page.URL()); err != nil {
		errf("fetch policy: %v", err)
		return 1
	}
	var output string
	switch *dump {
	case "html":
		output = page.HTML()
	case "markdown":
		output = page.Markdown()
	case "text":
		output = page.Text()
	case "title":
		output = page.Title()
	case "links":
		var links strings.Builder
		for _, l := range page.Links() {
			if len(l.Href)+len(l.Text)+2 > maxCLIOutputBytes-links.Len() {
				errf("fetch links output exceeds %d bytes", maxCLIOutputBytes)
				return 1
			}
			links.WriteString(l.Href)
			links.WriteByte('\t')
			links.WriteString(l.Text)
			links.WriteByte('\n')
		}
		write := func() error { return writeCLIOutput(os.Stdout, []byte(links.String())) }
		if *fence {
			write = func() error { return printFenced(os.Stdout, links.String(), page.URL(), "fetch") }
		}
		if err := write(); err != nil {
			errf("fetch links output: %v", err)
			return 1
		}
		return 0
	case "structured":
		if err := printLabeledJSON(os.Stdout, page.StructuredData(), page.URL(), "fetch"); err != nil {
			errf("structured output: %v", err)
			return 1
		}
		return 0
	case "semantic":
		output = agent.SemanticString(page.SemanticTree())
	}
	if err := printFetchText(*fence, output, page.URL()); err != nil {
		errf("fetch output: %v", err)
		return 1
	}
	return 0
}

// printFetchText writes page-derived text raw, or fenced on request.
func printFetchText(fence bool, text, pageURL string) error {
	if fence {
		return printFenced(os.Stdout, text, pageURL, "fetch")
	}
	return printText(os.Stdout, text)
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
