package main

import (
	"fmt"
	"os"
	"strconv"

	artemis "github.com/Christopher-Schulze/Artemis"
	"github.com/Christopher-Schulze/Artemis/mcpserve"
	"github.com/Christopher-Schulze/Artemis/network"
	browserprocess "github.com/Christopher-Schulze/Artemis/process"
)

// webMCPLaunchFlag exposes Chrome's experimental WebMCP page tools.
const webMCPLaunchFlag = "--enable-experimental-web-platform-features"

func cmdMCP(args []string) (exitCode int) {
	fs := newFlagSet("mcp")
	actionPolicyFlags := addBrowserPolicyFlags(fs)
	obeyRobots := fs.Bool("obey-robots", false, "consult robots.txt before fetching")
	blockPriv := fs.Bool("block-private-ips", true, "refuse to fetch private/loopback IPs")
	chromium := fs.Bool("chromium", true, "enable lazy session-owned Chromium control")
	webMCP := fs.Bool("webmcp", false, "launch Chromium with experimental web platform features so pages can register WebMCP tools")
	binary := fs.String("binary", "", "Chromium executable for owned control")
	sandbox := fs.String("sandbox", string(browserprocess.SandboxRequired), "Chromium sandbox policy: required or disabled")
	var allowedPorts stringSliceFlag
	fs.Var(&allowedPorts, "allowed-port", "allow a destination port (repeatable; 0=clear defaults 80/443)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `usage: artemis mcp [flags]

Serves one Model Context Protocol connection on stdin/stdout. Logs go to stderr.

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		errf("mcp accepts no positional arguments")
		return 2
	}
	if *webMCP && !*chromium {
		errf("mcp --webmcp requires --chromium")
		return 2
	}
	sandboxPolicy, err := parseSandboxPolicy(*sandbox)
	if err != nil {
		errf("mcp sandbox: %v", err)
		return 2
	}
	policy := network.PolicyConfig{AllowPrivateNetworks: !*blockPriv}
	for _, raw := range allowedPorts {
		port, err := strconv.Atoi(raw)
		if err != nil || port < 0 || port > 65535 {
			errf("invalid allowed port %q", raw)
			return 2
		}
		policy.AllowedPorts = append(policy.AllowedPorts, port)
	}
	cfg, err := actionPolicyFlags.config()
	if err != nil {
		errf("mcp action policy: %v", err)
		return 2
	}
	diagnosticConfig, err := cliDiagnosticsConfig(true)
	if err != nil {
		errf("mcp diagnostics: %v", err)
		return 1
	}
	cfg.ObeyRobots, cfg.PolicyConfig, cfg.Diagnostics = *obeyRobots, policy, diagnosticConfig
	if *chromium {
		_, policySink, resourceSink, err := newProcessDiagnostics("chromium", "mcp-owned-browser")
		if err != nil {
			errf("mcp Chromium diagnostics: %v", err)
			return 1
		}
		cfg.Chromium = &browserprocess.LaunchConfig{BinaryPath: *binary, Headless: true, Sandbox: sandboxPolicy, PolicyDecisionSink: policySink, ResourceSink: resourceSink}
		if *webMCP {
			cfg.Chromium.ExtraArgs = []string{webMCPLaunchFlag}
		}
	}
	agent, err := artemis.NewAgent(cfg)
	if err != nil {
		errf("init agent: %v", err)
		return 1
	}
	defer cleanupOnReturn(&exitCode, "mcp agent stop", agent.Stop)()
	ctx, cancel := signalContext()
	defer cancel()
	if err := agent.Start(ctx); err != nil {
		errf("start agent: %v", err)
		return 1
	}
	if err := mcpserve.New(agent, Version).Run(ctx, os.Stdin, os.Stdout); err != nil && ctx.Err() == nil {
		errf("mcp: %v", err)
		return 1
	}
	return 0
}
