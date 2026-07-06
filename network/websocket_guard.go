package network

import (
	"fmt"
	"net/url"
	"strings"
	"sync"
)

// websocket_guard.go (spec L4148: WebSocket Proxy Leak Prevention).
//
// WebSocket connections can bypass the HTTP proxy and leak the real IP.
// The fix requires intercepting CDP Network.webSocketCreated events and
// forcing all WebSocket URLs through proxy validation.
//
// Reference: research/webstack/pinchtab-main/internal/bridge/observe/network.go:1-527

// WebSocketGuardConfig configures the WebSocket proxy leak prevention
// guard (spec L4148: force all WebSocket URLs through proxy validation).
type WebSocketGuardConfig struct {
	// Enabled controls whether the guard is active.
	Enabled bool `json:"enabled"`
	// ProxyHost is the expected proxy host (e.g. "127.0.0.1:8080").
	// WebSocket URLs must route through this host.
	ProxyHost string `json:"proxy_host"`
	// AllowedSchemes are the WebSocket URL schemes that are allowed
	// (ws, wss).
	AllowedSchemes []string `json:"allowed_schemes"`
	// BlockDirectConnections blocks WebSocket URLs that would connect
	// directly to a target (bypassing the proxy).
	BlockDirectConnections bool `json:"block_direct_connections"`
	// AllowLocalhost allows ws://localhost and ws://127.0.0.1 for
	// development/testing.
	AllowLocalhost bool `json:"allow_localhost"`
}

// DefaultWebSocketGuardConfig returns a config with safe defaults
// (spec L4148: block any WebSocket that would bypass the proxy).
func DefaultWebSocketGuardConfig() WebSocketGuardConfig {
	return WebSocketGuardConfig{
		Enabled:                true,
		ProxyHost:              "127.0.0.1:8080",
		AllowedSchemes:         []string{"ws", "wss"},
		BlockDirectConnections: true,
		AllowLocalhost:         false,
	}
}

// WebSocketEvent represents a CDP Network.webSocketCreated event
// (spec L4148: intercepting CDP Network.webSocketCreated events).
type WebSocketEvent struct {
	RequestID  string `json:"request_id"`
	URL        string `json:"url"`
	Initiator  string `json:"initiator,omitempty"`
	StackTrace string `json:"stack_trace,omitempty"`
}

// WebSocketVerdict is the decision made by the guard for a WebSocket
// creation event.
type WebSocketVerdict string

const (
	// WebSocketVerdictAllow allows the WebSocket connection.
	WebSocketVerdictAllow WebSocketVerdict = "allow"
	// WebSocketVerdictBlock blocks the WebSocket connection.
	WebSocketVerdictBlock WebSocketVerdict = "block"
	// WebSocketVerdictRedirect redirects the WebSocket to the proxy.
	WebSocketVerdictRedirect WebSocketVerdict = "redirect"
)

// WebSocketDecision is the result of evaluating a WebSocket creation
// event against the guard config.
type WebSocketDecision struct {
	Verdict  WebSocketVerdict `json:"verdict"`
	Reason   string           `json:"reason"`
	Original string           `json:"original_url"`
	Redirect string           `json:"redirect_url,omitempty"`
}

// WebSocketGuard intercepts Network.webSocketCreated events and
// validates WebSocket URLs through the proxy (spec L4148).
type WebSocketGuard struct {
	mu     sync.RWMutex
	config WebSocketGuardConfig
	stats  WebSocketGuardStats
}

// WebSocketGuardStats tracks guard decisions for diagnostics.
type WebSocketGuardStats struct {
	Total    int `json:"total"`
	Allowed  int `json:"allowed"`
	Blocked  int `json:"blocked"`
	Redirect int `json:"redirect"`
}

// NewWebSocketGuard creates a new guard with the given config.
func NewWebSocketGuard(config WebSocketGuardConfig) *WebSocketGuard {
	return &WebSocketGuard{
		config: config,
	}
}

// Config returns the current guard configuration.
func (g *WebSocketGuard) Config() WebSocketGuardConfig {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.config
}

// SetConfig updates the guard configuration.
func (g *WebSocketGuard) SetConfig(config WebSocketGuardConfig) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.config = config
}

// Stats returns the current guard statistics.
func (g *WebSocketGuard) Stats() WebSocketGuardStats {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.stats
}

// Evaluate checks a WebSocket creation event against the guard config
// (spec L4148: validate WebSocket URLs through proxy, block bypass).
func (g *WebSocketGuard) Evaluate(event WebSocketEvent) WebSocketDecision {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.stats.Total++

	if !g.config.Enabled {
		g.stats.Allowed++
		return WebSocketDecision{
			Verdict:  WebSocketVerdictAllow,
			Reason:   "guard disabled",
			Original: event.URL,
		}
	}

	parsed, err := url.Parse(event.URL)
	if err != nil {
		g.stats.Blocked++
		return WebSocketDecision{
			Verdict:  WebSocketVerdictBlock,
			Reason:   fmt.Sprintf("invalid URL: %v", err),
			Original: event.URL,
		}
	}

	// Validate scheme.
	scheme := strings.ToLower(parsed.Scheme)
	if !isAllowedScheme(scheme, g.config.AllowedSchemes) {
		g.stats.Blocked++
		return WebSocketDecision{
			Verdict:  WebSocketVerdictBlock,
			Reason:   fmt.Sprintf("scheme %q not allowed (expected ws/wss)", scheme),
			Original: event.URL,
		}
	}

	// Check if the WebSocket routes through the proxy host.
	// This check takes priority over the localhost check, since the
	// proxy itself may be on localhost (e.g. 127.0.0.1:8080).
	if g.config.BlockDirectConnections && g.config.ProxyHost != "" {
		if parsed.Host == g.config.ProxyHost {
			g.stats.Allowed++
			return WebSocketDecision{
				Verdict:  WebSocketVerdictAllow,
				Reason:   "routes through proxy",
				Original: event.URL,
			}
		}
	}

	// Check localhost bypass (only for non-proxy hosts).
	host := parsed.Hostname()
	if isLocalhost(host) {
		if g.config.AllowLocalhost {
			g.stats.Allowed++
			return WebSocketDecision{
				Verdict:  WebSocketVerdictAllow,
				Reason:   "localhost allowed",
				Original: event.URL,
			}
		}
		g.stats.Blocked++
		return WebSocketDecision{
			Verdict:  WebSocketVerdictBlock,
			Reason:   "localhost WebSocket blocked (would bypass proxy)",
			Original: event.URL,
		}
	}

	// Non-localhost, non-proxy host: block if direct connections are blocked.
	if g.config.BlockDirectConnections && g.config.ProxyHost != "" {
		g.stats.Blocked++
		return WebSocketDecision{
			Verdict:  WebSocketVerdictBlock,
			Reason:   fmt.Sprintf("WebSocket to %s bypasses proxy %s", parsed.Host, g.config.ProxyHost),
			Original: event.URL,
		}
	}

	g.stats.Allowed++
	return WebSocketDecision{
		Verdict:  WebSocketVerdictAllow,
		Reason:   "passes proxy validation",
		Original: event.URL,
	}
}

// isAllowedScheme checks if a scheme is in the allowed list.
func isAllowedScheme(scheme string, allowed []string) bool {
	for _, a := range allowed {
		if strings.EqualFold(scheme, a) {
			return true
		}
	}
	return false
}

// isLocalhost checks if a host is a localhost variant.
func isLocalhost(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1" || host == "0.0.0.0"
}

// ResetStats resets the guard statistics (for testing).
func (g *WebSocketGuard) ResetStats() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.stats = WebSocketGuardStats{}
}
