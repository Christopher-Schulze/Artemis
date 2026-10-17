package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/Christopher-Schulze/Artemis/engine"
	"github.com/Christopher-Schulze/Artemis/network"
	"github.com/Christopher-Schulze/Artemis/scraper"
	"github.com/google/uuid"
)

func cmdSitemap(args []string) (exitCode int) {
	fs := newFlagSet("sitemap")
	timeout := fs.Duration("timeout", 60*time.Second, "discovery timeout")
	maxDepth := fs.Int("max-depth", 3, "maximum sitemap-index nesting")
	maxSitemaps := fs.Int("max-sitemaps", 50, "maximum sitemap documents fetched")
	maxURLs := fs.Int("max-urls", 50000, "maximum page URLs collected")
	allowPrivate := fs.Bool("allow-private-networks", false, "allow private/loopback targets")
	allowPort := fs.Int("allow-port", 0, "allow a specific destination port (0 = 80/443)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: artemis sitemap [flags] <origin-url>")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 || *timeout <= 0 || *maxDepth <= 0 || *maxSitemaps <= 0 || *maxURLs <= 0 || *allowPort < 0 || *allowPort > 65535 {
		fs.Usage()
		return 2
	}
	policyConfig := network.PolicyConfig{AllowPrivateNetworks: *allowPrivate}
	if *allowPort != 0 {
		policyConfig.AllowedPorts = []int{*allowPort}
	}
	diagnosticConfig, err := cliDiagnosticsConfig(false)
	if err != nil {
		errf("diagnostics config: %v", err)
		return 1
	}
	eng, err := engine.New(engine.Config{Timeout: *timeout, SessionID: "cli-" + uuid.NewString(), PolicyConfig: policyConfig, Diagnostics: diagnosticConfig})
	if err != nil {
		errf("sitemap init: %v", err)
		return 1
	}
	defer cleanupOnReturn(&exitCode, "sitemap engine close", eng.Close)()
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	discovery, err := scraper.DiscoverSitemapURLs(ctx, eng.HTTPClient(), fs.Arg(0), scraper.SitemapLimits{MaxDepth: *maxDepth, MaxSitemaps: *maxSitemaps, MaxURLs: *maxURLs})
	if err != nil {
		errf("sitemap: %v", err)
		return 1
	}
	if err := printLabeledJSON(os.Stdout, discovery, fs.Arg(0), "sitemap"); err != nil {
		errf("sitemap output: %v", err)
		return 1
	}
	return 0
}
