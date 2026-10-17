package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Christopher-Schulze/Artemis/playwright"
)

const playwrightInstallTimeout = 30 * time.Minute

// cmdPlaywright reports or provisions the Playwright peer lane's signed
// artifact cache. Execution of the artifact belongs to an embedding host's
// sandboxed runtime, so status stays unavailable until one is attached.
func cmdPlaywright(args []string) int {
	fs := newFlagSet("playwright")
	op := fs.String("op", "status", "status or install")
	cacheDir := fs.String("cache", "", "absolute Playwright artifact cache directory")
	keyFile := fs.String("trusted-key", "", "file holding the trusted Ed25519 artifact signer public key (base64 or 32 raw bytes)")
	artifact := fs.String("artifact", "", "signed artifact directory to install (install only)")
	minFree := fs.Int64("min-free", playwright.DefaultMinFreeBytes, "bytes that must stay free on the cache volume beyond the artifact")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `usage: artemis playwright --cache DIR --trusted-key FILE [--op status|install] [--artifact DIR]

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 || *cacheDir == "" || *keyFile == "" || (*op == "install") != (*artifact != "") || *op != "status" && *op != "install" {
		errf("playwright: --cache and --trusted-key are required; --artifact is required exactly for --op install")
		return 2
	}
	public, err := readSignerKey(*keyFile)
	if err != nil {
		errf("playwright: %v", err)
		return 2
	}
	cache, err := playwright.OpenCache(absolutePath(*cacheDir), playwright.TrustedKeys{playwright.KeyID(public): public}, *minFree)
	if err != nil {
		errf("playwright: %v", err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), playwrightInstallTimeout)
	defer cancel()
	if *op == "install" {
		if _, err := cache.Install(ctx, absolutePath(*artifact)); err != nil {
			errf("playwright: install: %v", err)
			return 1
		}
	}
	if err := printJSON(os.Stdout, playwright.Project(cache.Load(ctx), false, "")); err != nil {
		errf("playwright: %v", err)
		return 1
	}
	return 0
}

func readSignerKey(path string) (ed25519.PublicKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read trusted key: %w", err)
	}
	if len(raw) == ed25519.PublicKeySize {
		return ed25519.PublicKey(raw), nil
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(decoded) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("trusted key must be a base64 or raw %d-byte Ed25519 public key", ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(decoded), nil
}

func absolutePath(path string) string {
	if absolute, err := filepath.Abs(path); err == nil {
		return absolute
	}
	return path
}
