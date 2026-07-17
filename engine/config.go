// Package engine ties the network, parser, webapi, and agent layers
// together into a single embeddable type. The public surface (Engine,
// Page, Config, FetchOpts) is the entry point for both the CLI and any
// Go program embedding Artemis.
package engine

import (
	"time"

	"github.com/Christopher-Schulze/Artemis/network"
)

// Default values used when a Config field is left at its zero value.
const (
	DefaultUserAgent    = "Artemis/0.1.0-alpha.1 (+https://github.com/Christopher-Schulze/Artemis) AppleWebKit/537.36"
	DefaultTimeout      = 30 * time.Second
	DefaultMaxBodyBytes = int64(50 * 1024 * 1024)
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
}
