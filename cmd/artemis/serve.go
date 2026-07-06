package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"

	"github.com/Christopher-Schulze/Artemis/engine"
	"github.com/Christopher-Schulze/Artemis/serve"
)

func cmdServe(args []string) int {
	fs := newFlagSet("serve")
	host := fs.String("host", "127.0.0.1", "bind host")
	port := fs.Int("port", 9333, "bind port")
	obeyRobots := fs.Bool("obey-robots", false, "consult robots.txt before fetching")
	blockPriv := fs.Bool("block-private-ips", false, "refuse to fetch private/loopback IPs")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `usage: artemis serve [flags]

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	eng, err := engine.New(engine.Config{
		ObeyRobots:      *obeyRobots,
		BlockPrivateIPs: *blockPriv,
	})
	if err != nil {
		errf("init engine: %v", err)
		return 1
	}
	defer eng.Close()

	srv := serve.New(eng, serve.Opts{Logger: slog.Default()})
	addr := net.JoinHostPort(*host, fmt.Sprintf("%d", *port))
	slog.Info("artemis steering server", "addr", addr)

	ctx, cancel := signalContext()
	defer cancel()

	if err := srv.ListenAndServe(ctx, addr); err != nil && err != context.Canceled {
		errf("serve: %v", err)
		return 1
	}
	return 0
}
