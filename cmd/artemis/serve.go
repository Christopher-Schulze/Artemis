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
	"github.com/Christopher-Schulze/Artemis/serve"
)

const defaultServeHost = "127.0.0.1"

func cmdServe(args []string) int {
	fs := newFlagSet("serve")
	host := fs.String("host", defaultServeHost, "bind host")
	port := fs.Int("port", 9333, "bind port")
	token := fs.String("token", os.Getenv("ARTEMIS_SERVE_TOKEN"), "bearer token for connections; if empty, a token is generated")
	origin := fs.String("origin", strings.Join(serve.DefaultOriginPatterns(), ","), "allowed WebSocket origin patterns")
	rate := fs.Int("rate", 30, "per-connection request rate limit (requests per second)")
	burst := fs.Int("burst", 10, "per-connection rate limit burst")
	clientRate := fs.Int("client-rate", 120, "normalized-client request rate limit (requests per minute)")
	clientBurst := fs.Int("client-burst", 10, "normalized-client rate limit burst")
	obeyRobots := fs.Bool("obey-robots", false, "consult robots.txt before fetching")
	blockPriv := fs.Bool("block-private-ips", true, "refuse to fetch private/loopback IPs")
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
	if *rate <= 0 || *burst <= 0 || *clientRate <= 0 || *clientBurst <= 0 {
		errf("serve rate limits and bursts must be positive")
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
			if perr != nil {
				errf("invalid allowed port %q", p)
				return 2
			}
			ports = append(ports, pi)
		}
		policy.AllowedPorts = ports
	}
	cfg := artemis.AgentConfig{
		ObeyRobots:   *obeyRobots,
		PolicyConfig: policy,
	}
	agent, err := artemis.NewAgent(cfg)
	if err != nil {
		errf("init agent: %v", err)
		return 1
	}
	defer agent.Stop()

	ctx, cancel := signalContext()
	defer cancel()

	if err := agent.Start(ctx); err != nil {
		errf("start agent: %v", err)
		return 1
	}

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

	srv := serve.New(agent, opts)
	addr := net.JoinHostPort(*host, fmt.Sprintf("%d", *port))
	slog.Info("artemis steering server", "addr", addr)

	if err := srv.ListenAndServe(ctx, addr); err != nil && err != context.Canceled {
		errf("serve: %v", err)
		return 1
	}
	return 0
}
