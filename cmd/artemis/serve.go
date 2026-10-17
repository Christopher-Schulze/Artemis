package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/Christopher-Schulze/Artemis"
	"github.com/Christopher-Schulze/Artemis/network"
	browserprocess "github.com/Christopher-Schulze/Artemis/process"
	"github.com/Christopher-Schulze/Artemis/profile"
	"github.com/Christopher-Schulze/Artemis/serve"
)

const defaultServeHost = "127.0.0.1"

func cmdServe(args []string) (exitCode int) {
	fs := newFlagSet("serve")
	actionPolicyFlags := addBrowserPolicyFlags(fs)
	adBlock := addServeAdBlockFlags(fs)
	host := fs.String("host", defaultServeHost, "bind host")
	port := fs.Int("port", 9333, "bind port")
	readyJSON := fs.Bool("ready-json", false, "emit owned endpoint/process/instance readiness as JSON on stdout")
	token := fs.String("token", os.Getenv("ARTEMIS_SERVE_TOKEN"), "bearer token for connections; if empty, a token is generated")
	origin := fs.String("origin", strings.Join(serve.DefaultOriginPatterns(), ","), "allowed WebSocket origin patterns")
	rate := fs.Int("rate", 30, "per-connection request rate limit (requests per second)")
	burst := fs.Int("burst", 10, "per-connection rate limit burst")
	clientRate := fs.Int("client-rate", 120, "normalized-client request rate limit (requests per minute)")
	clientBurst := fs.Int("client-burst", 10, "normalized-client rate limit burst")
	profileRoot := fs.String("profile-root", "", "profile runtime root owned by this server; empty disables named profiles")
	obeyRobots := fs.Bool("obey-robots", false, "consult robots.txt before fetching")
	blockPriv := fs.Bool("block-private-ips", true, "refuse to fetch private/loopback IPs")
	chromium := fs.Bool("chromium", true, "enable lazy session-owned Chromium control")
	binary := fs.String("binary", "", "Chromium executable for owned control")
	sandbox := fs.String("sandbox", string(browserprocess.SandboxRequired), "Chromium sandbox policy: required or disabled")
	maxCPU := fs.Float64("max-cpu-percent", browserprocess.DefaultMaxCPUPercent, "maximum owned Chromium process-group CPU percent")
	maxMemory := fs.Int64("max-memory-bytes", browserprocess.DefaultMaxMemoryBytes, "maximum owned Chromium process-group RSS bytes")
	maxProfile := fs.Int64("max-profile-bytes", browserprocess.DefaultMaxProfileDiskBytes, "maximum owned Chromium profile bytes")
	sessionTimeout := fs.Duration("session-timeout", browserprocess.DefaultSessionTimeout, "maximum owned Chromium process lifetime")
	var allowedPorts stringSliceFlag
	fs.Var(&allowedPorts, "allowed-port", "allow a destination port (repeatable; 0=clear defaults 80/443)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `usage: artemis serve [flags]

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 || strings.TrimSpace(*host) == "" || *port < 0 || *port > 65535 {
		errf("serve requires a host, port within 0..65535 and no positional arguments")
		return 2
	}
	if *rate <= 0 || *burst <= 0 || *clientRate <= 0 || *clientBurst <= 0 {
		errf("serve rate limits and bursts must be positive")
		return 2
	}
	sandboxPolicy, err := parseSandboxPolicy(*sandbox)
	if err != nil {
		errf("serve sandbox: %v", err)
		return 2
	}
	budget := browserprocess.ResourceBudget{MaxCPUPercent: *maxCPU, MaxMemoryBytes: *maxMemory, MaxProfileDiskBytes: *maxProfile, SessionTimeout: *sessionTimeout}
	if err := validateCLIResourceBudget(budget); err != nil {
		errf("serve: %v", err)
		return 2
	}
	var origins []string
	for _, pattern := range strings.Split(*origin, ",") {
		if pattern = strings.TrimSpace(pattern); pattern != "" {
			origins = append(origins, pattern)
		}
	}
	if len(origins) == 0 {
		errf("serve requires at least one allowed origin pattern")
		return 2
	}
	if err := serve.ValidateOriginPatterns(origins); err != nil {
		errf("serve origin patterns: %v", err)
		return 2
	}

	policy := network.PolicyConfig{AllowPrivateNetworks: !*blockPriv}
	if len(allowedPorts) > 0 {
		ports := make([]int, 0, len(allowedPorts))
		for _, p := range allowedPorts {
			pi, perr := strconv.Atoi(p)
			if perr != nil || pi < 0 || pi > 65535 {
				errf("invalid allowed port %q", p)
				return 2
			}
			ports = append(ports, pi)
		}
		policy.AllowedPorts = ports
	}
	diagnosticConfig, err := cliDiagnosticsConfig(true)
	if err != nil {
		errf("serve diagnostics: %v", err)
		return 1
	}
	cfg, err := actionPolicyFlags.config()
	if err != nil {
		errf("serve action policy: %v", err)
		return 2
	}
	cfg.ObeyRobots, cfg.PolicyConfig, cfg.Diagnostics = *obeyRobots, policy, diagnosticConfig
	if cfg.AdBlock, err = adBlock.blocker(); err != nil {
		errf("serve adblock: %v", err)
		return 1
	}
	cfg.DisableAdBlock = cfg.AdBlock == nil
	if *chromium {
		_, policySink, resourceSink, err := newProcessDiagnostics("chromium", "serve-owned-browser")
		if err != nil {
			errf("serve Chromium diagnostics: %v", err)
			return 1
		}
		cfg.Chromium = &browserprocess.LaunchConfig{BinaryPath: *binary, Headless: true, Sandbox: sandboxPolicy, PolicyDecisionSink: policySink, ResourceSink: resourceSink, ResourceBudget: budget}
	}
	agent, err := artemis.NewAgent(cfg)
	if err != nil {
		errf("init agent: %v", err)
		return 1
	}
	defer cleanupOnReturn(&exitCode, "serve agent stop", agent.Stop)()
	if *profileRoot != "" {
		manager, err := profile.NewRuntimeManager(*profileRoot)
		if err != nil {
			errf("serve profile runtime: %v", err)
			return 1
		}
		agent.SetProfileRuntime(manager)
	}

	ctx, cancel := signalContext()
	defer cancel()

	if err := agent.Start(ctx); err != nil {
		errf("start agent: %v", err)
		return 1
	}
	waitUpdates, err := adBlock.startUpdates(ctx, cfg.AdBlock)
	if err != nil {
		errf("serve: %v", err)
		return 1
	}
	defer func() {
		cancel()
		waitUpdates()
	}()

	authToken := *token
	if authToken == "" {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			errf("generate token: %v", err)
			return 1
		}
		authToken = hex.EncodeToString(b)
		fmt.Fprintf(os.Stderr, "Artemis serve token: %s\n", authToken)
	}

	opts := serve.Opts{
		Logger:         slog.Default(),
		AuthToken:      authToken,
		OriginPatterns: origins,
		RateLimit: serve.RateLimit{
			RequestsPerSecond:       *rate,
			Burst:                   *burst,
			ClientRequestsPerMinute: *clientRate,
			ClientBurst:             *clientBurst,
		},
	}

	if *readyJSON {
		opts.Ready = func(info serve.Readiness) error { return printJSON(os.Stdout, info) }
	}
	srv := serve.New(agent, opts)
	defer cleanupOnReturn(&exitCode, "serve control close", srv.Close)()
	addr := net.JoinHostPort(*host, fmt.Sprintf("%d", *port))
	slog.Info("artemis steering server", "addr", addr)

	if err := srv.ListenAndServe(ctx, addr); err != nil && err != context.Canceled {
		errf("serve: %v", err)
		return 1
	}
	return 0
}
