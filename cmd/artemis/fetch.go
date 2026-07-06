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

	cfg := engine.Config{
		UserAgent:    *userAgent,
		ProxyURL:     *proxyURL,
		Timeout:      timeout,
		MaxBodyBytes: *maxBody,
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

	page, err := eng.Fetch(ctx, url, engine.FetchOpts{
		Headers:          hdrs,
		RunInlineScripts: *runScripts,
		Console:          console,
	})
	if err != nil {
		errf("fetch %s: %v", url, err)
		return 1
	}
	defer page.Close()

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
