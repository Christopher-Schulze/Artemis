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

func cmdServe(args []string) int {
	fs := newFlagSet("serve")
	host := fs.String("host", "127.0.0.1", "bind host")
	port := fs.Int("port", 9333, "bind port")
	token := fs.String("token", os.Getenv("ARTEMIS_SERVE_TOKEN"), "bearer token for connections; if empty, a token is generated")
	origin := fs.String("origin", "localhost,127.0.0.1,::1", "allowed WebSocket origins")
	insecureOrigin := fs.Bool("insecure-origin", false, "skip WebSocket origin verification")
	rate := fs.Int("rate", 0, "per-connection request rate limit (requests per second, 0=unlimited)")
	burst := fs.Int("burst", 0, "rate limit burst (0=rate)")
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
		slog.Info("generated auth token", "token", authToken)
	}

	var origins []string
	if *origin != "" {
		origins = strings.Split(*origin, ",")
	}

	opts := serve.Opts{
		Logger:             slog.Default(),
		AuthToken:          authToken,
		OriginPatterns:     origins,
		InsecureSkipOrigin: *insecureOrigin,
	}
	if *rate > 0 {
		opts.RateLimit = serve.RateLimit{RequestsPerSecond: *rate, Burst: *burst}
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
