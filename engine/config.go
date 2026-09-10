// Package engine ties the network, parser, webapi, and agent layers
// together into a single embeddable type. The public surface (Engine,
// Page, Config, FetchOpts) is the entry point for both the CLI and any
// Go program embedding Artemis.
package engine

import (
	"context"
	"errors"
	"time"

	"github.com/Christopher-Schulze/Artemis/diagnostics"
	"github.com/Christopher-Schulze/Artemis/download"
	"github.com/Christopher-Schulze/Artemis/network"
)

// Default values used when a Config field is left at its zero value.
const (
	DefaultUserAgent         = "Artemis/0.1.0-alpha.1 (+https://github.com/Christopher-Schulze/Artemis) AppleWebKit/537.36"
	DefaultTimeout           = 30 * time.Second
	DefaultMaxBodyBytes      = int64(50 * 1024 * 1024)
	DefaultDownloadDiskBytes = int64(1024 * 1024 * 1024)
	DefaultDownloadFreeBytes = int64(512 * 1024 * 1024)
)

// Config configures engine behavior. The zero value is usable; defaults
// are filled in by New. The zero value is secure by default: it denies
// private/loopback/link-local/multicast/metadata/CGNAT, Unix, file, and
// data schemes, and only allows ports 80 and 443. Callers can relax
// these rules by setting PolicyConfig explicitly.
type Config struct {
	// UserAgent is the User-Agent header sent with every request.
	UserAgent string
	// ProxyURL routes outbound traffic through the given proxy URL.
	ProxyURL string
	// Timeout is the per-request deadline.
	Timeout time.Duration
	// MaxBodyBytes caps response body size; 0 means unlimited.
	MaxBodyBytes int64
	// ObeyRobots fetches /robots.txt for each new host and refuses to
	// fetch URLs that the configured UserAgent is not allowed to crawl.
	ObeyRobots bool
	// PolicyConfig controls the outbound network security policy. The
	// zero value denies private/local/metadata/file/data/unsupported
	// targets and restricts ports to 80/443.
	PolicyConfig network.PolicyConfig
	// SessionID correlates redacted network-policy decisions across
	// HTTP, JavaScript fetch, iframe, stylesheet, and WebSocket paths.
	SessionID string
	// Diagnostics configures the redacted retention-bounded audit ledger.
	// An empty path keeps the bounded ledger in memory only.
	Diagnostics diagnostics.Config
	// DownloadRoot owns per-session download directories. Empty resolves to
	// ~/.artemis/tmp/browser. Callers may override it for isolated runtimes.
	DownloadRoot string
	// MaxDownloadDiskBytes caps all committed downloads in one session.
	MaxDownloadDiskBytes int64
	// MinDownloadFreeBytes is the free-space headroom preserved after a write.
	MinDownloadFreeBytes int64
	// DownloadReservation is the shared durable admission authority for
	// committed browser downloads.
	DownloadReservation download.StorageReservationAuthority
	// RequireDownloadReservation fails closed when the shared authority is not
	// configured for a production engine.
	RequireDownloadReservation bool
	// DownloadIngress publishes every committed browser download into the
	// embedding application's governed attachment lifecycle.
	DownloadIngress DownloadIngress
	// RequireDownloadIngress fails closed when a production engine has no
	// governed download publication boundary.
	RequireDownloadIngress bool
	// SessionBudget contains hard limits shared by all renderless work owned
	// by this engine unit.
	SessionBudget SessionBudget
	// JSContextPoolSize enables the v8.Context pool for JS execution.
	// Pooled Contexts skip ~30% of NewContext CPU cost (install* and
	// flushBootstraps) by reusing a previously-built v8.Context after
	// a JS-side __artemis_reset() clears mutated globals. Set to 0 to
	// disable (default; full isolation between pages). Recommended
	// values: 4-16 for serial/low-concurrency agent workflows.
	JSContextPoolSize int
	// JSContextPoolWarm pre-builds all JSContextPoolSize Contexts
	// during engine.New so the first Fetch hits the pool fast path.
	// Adds ~JSContextPoolSize ms to startup but eliminates the cold
	// build cost on the first page. Ignored when JSContextPoolSize == 0.
	JSContextPoolWarm bool
}

// DownloadIngress is the dependency-free publication boundary for browser
// downloads. The embedding application owns classification, quarantine,
// persistence and activation policy; Artemis only supplies verified bytes.
type DownloadIngress interface {
	PublishDownload(ctx context.Context, sessionID, filename, contentType string, content []byte) error
}

func (c *Config) applyDefaults() {
	if c.UserAgent == "" {
		c.UserAgent = DefaultUserAgent
	}
	if c.Timeout == 0 {
		c.Timeout = DefaultTimeout
	}
	if c.MaxBodyBytes == 0 {
		c.MaxBodyBytes = DefaultMaxBodyBytes
	}
	if c.MaxDownloadDiskBytes == 0 {
		c.MaxDownloadDiskBytes = DefaultDownloadDiskBytes
	}
	if c.MinDownloadFreeBytes == 0 {
		c.MinDownloadFreeBytes = DefaultDownloadFreeBytes
	}
	c.SessionBudget.applyDefaults(c.MaxDownloadDiskBytes)
	if c.SessionBudget.MaxDiskBytes < c.MaxDownloadDiskBytes {
		c.MaxDownloadDiskBytes = c.SessionBudget.MaxDiskBytes
	}
}

func (c Config) validate() error {
	if c.Timeout < time.Millisecond {
		return errors.New("engine: request timeout must be at least 1ms")
	}
	if c.MaxBodyBytes < 1 {
		return errors.New("engine: maximum body bytes must be positive")
	}
	if c.MaxDownloadDiskBytes < 1 {
		return errors.New("engine: maximum download disk bytes must be positive")
	}
	if c.MinDownloadFreeBytes < 0 {
		return errors.New("engine: minimum download free bytes must not be negative")
	}
	if c.RequireDownloadReservation && c.DownloadReservation == nil {
		return errors.New("engine: download reservation required")
	}
	if c.JSContextPoolSize < 0 {
		return errors.New("engine: JavaScript context pool size must not be negative")
	}
	if c.RequireDownloadIngress && c.DownloadIngress == nil {
		return errors.New("engine: download ingress required")
	}
	return nil
}
