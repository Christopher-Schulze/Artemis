package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/Christopher-Schulze/Artemis/engine"
	"github.com/Christopher-Schulze/Artemis/network"
	"github.com/google/uuid"
)

func cmdDownload(args []string) int {
	fs := newFlagSet("download")
	filename := fs.String("filename", "", "owned output filename (paths are rejected)")
	sessionID := fs.String("session-id", "", "download session ID (default: generated)")
	timeout := fs.Duration("timeout", 30*time.Second, "download timeout")
	maxFile := fs.Int64("max-file-bytes", 50<<20, "maximum bytes per download")
	maxSession := fs.Int64("max-session-bytes", 1<<30, "maximum committed download bytes per session")
	allowPrivate := fs.Bool("allow-private-networks", false, "allow private/loopback targets")
	allowPort := fs.Int("allow-port", 0, "allow a specific destination port (0 = 80/443)")
	var allowedTypes stringSliceFlag
	fs.Var(&allowedTypes, "allow-content-type", "allowed sniffed MIME type or wildcard; repeatable (default */*)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: artemis download [flags] <url>")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 || *maxFile <= 0 || *maxSession <= 0 {
		fs.Usage()
		return 2
	}
	if *sessionID == "" {
		*sessionID = "cli-" + uuid.NewString()
	}
	policyConfig := network.PolicyConfig{
		AllowPrivateNetworks: *allowPrivate,
		MaxDownloadBytes:     *maxFile,
		AllowedDownloadTypes: allowedTypes,
	}
	if *allowPort != 0 {
		policyConfig.AllowedPorts = []int{*allowPort}
	}
	diagnosticConfig, err := cliDiagnosticsConfig(false)
	if err != nil {
		errf("diagnostics config: %v", err)
		return 1
	}
	eng, err := engine.New(engine.Config{
		Timeout:              *timeout,
		SessionID:            *sessionID,
		MaxDownloadDiskBytes: *maxSession,
		PolicyConfig:         policyConfig,
		Diagnostics:          diagnosticConfig,
	})
	if err != nil {
		errf("download init: %v", err)
		return 1
	}
	defer eng.Close()
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	download, err := eng.Download(ctx, fs.Arg(0), *filename)
	if err != nil {
		errf("download: %v", err)
		return 1
	}
	if err := printJSON(os.Stdout, download); err != nil {
		errf("download output: %v", err)
		return 1
	}
	return 0
}
