package network

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
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
	// AllowLocalhost allows explicit loopback IPs for development/testing.
	// Canonical Policy hostname rules still deny localhost aliases; use
	// an explicit loopback IP and AllowedPorts.
	AllowLocalhost bool `json:"allow_localhost"`
	// AllowedPorts are additional WebSocket target ports. The proxy
	// port and canonical 80/443 defaults are included automatically.
	AllowedPorts []int `json:"allowed_ports,omitempty"`
	// SessionID correlates redacted canonical policy decisions.
	SessionID string `json:"session_id,omitempty"`
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
	mu             sync.RWMutex
	config         WebSocketGuardConfig
	policy         *Policy
	policyErr      error
	externalPolicy bool
	stats          WebSocketGuardStats
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
	return NewWebSocketGuardWithPolicy(config, nil)
}

// NewWebSocketGuardWithPolicy creates a CDP compatibility guard that
// delegates URL, scheme, port, DNS, and private-address decisions to
// the canonical Policy owner. The guard retains only the stricter
// proxy-route check and decision counters.
func NewWebSocketGuardWithPolicy(config WebSocketGuardConfig, policy *Policy) *WebSocketGuard {
	guard := &WebSocketGuard{config: cloneWebSocketGuardConfig(config)}
	if policy != nil {
		guard.policy = policy
		guard.externalPolicy = true
		return guard
	}
	guard.policy, guard.policyErr = buildWebSocketGuardPolicy(config)
	return guard
}

// Config returns the current guard configuration.
func (g *WebSocketGuard) Config() WebSocketGuardConfig {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return cloneWebSocketGuardConfig(g.config)
}

// SetConfig updates the guard configuration.
func (g *WebSocketGuard) SetConfig(config WebSocketGuardConfig) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.config = cloneWebSocketGuardConfig(config)
	if !g.externalPolicy {
		g.policy, g.policyErr = buildWebSocketGuardPolicy(config)
	}
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

	if g.policyErr != nil {
		g.stats.Blocked++
		return WebSocketDecision{
			Verdict:  WebSocketVerdictBlock,
			Reason:   fmt.Sprintf("invalid policy config: %v", g.policyErr),
			Original: event.URL,
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), g.policy.Config().DialTimeout)
	defer cancel()
	if err := g.policy.ValidateRequest(
		ctx,
		event.URL,
		http.MethodGet,
		"",
		0,
		TargetWebSocket,
		g.config.SessionID,
	); err != nil {
		g.stats.Blocked++
		return WebSocketDecision{
			Verdict:  WebSocketVerdictBlock,
			Reason:   err.Error(),
			Original: event.URL,
		}
	}

	if g.config.BlockDirectConnections && g.config.ProxyHost != "" {
		if !matchesProxyTarget(event.URL, g.config.ProxyHost) {
			g.stats.Blocked++
			return WebSocketDecision{
				Verdict:  WebSocketVerdictBlock,
				Reason:   fmt.Sprintf("WebSocket target bypasses proxy %s", g.config.ProxyHost),
				Original: event.URL,
			}
		}
	}

	g.stats.Allowed++
	return WebSocketDecision{
		Verdict:  WebSocketVerdictAllow,
		Reason:   "canonical policy and proxy validation passed",
		Original: event.URL,
	}
}

func cloneWebSocketGuardConfig(config WebSocketGuardConfig) WebSocketGuardConfig {
	config.AllowedSchemes = append([]string(nil), config.AllowedSchemes...)
	config.AllowedPorts = append([]int(nil), config.AllowedPorts...)
	return config
}

func buildWebSocketGuardPolicy(config WebSocketGuardConfig) (*Policy, error) {
	schemes := append([]string(nil), config.AllowedSchemes...)
	if len(schemes) == 0 {
		schemes = []string{"ws", "wss"}
	}
	ports := append([]int{80, 443}, config.AllowedPorts...)
	var domains []string
	allowPrivate := config.AllowLocalhost
	if config.BlockDirectConnections && config.ProxyHost != "" {
		host, port, err := splitProxyTarget(config.ProxyHost)
		if err != nil {
			return nil, err
		}
		domains = []string{host}
		ports = append(ports, port)
		if address, ok := parsePolicyAddress(host); ok && blockedAddress(address) {
			allowPrivate = true
		}
	}
	return NewPolicy(PolicyConfig{
		AllowedSchemes:       schemes,
		AllowedDomains:       domains,
		AllowedPorts:         ports,
		AllowedMethods:       []string{http.MethodGet},
		AllowPrivateNetworks: allowPrivate,
	}, nil, nil)
}

func matchesProxyTarget(rawURL, proxyHost string) bool {
	parsed, err := parsePolicyURL(rawURL)
	if err != nil {
		return false
	}
	actualHost, err := normalizePolicyHost(parsed.Hostname())
	if err != nil {
		return false
	}
	actualPort, err := policyPort(parsed)
	if err != nil {
		return false
	}
	expectedHost, expectedPort, err := splitProxyTarget(proxyHost)
	return err == nil && actualHost == expectedHost && actualPort == expectedPort
}

func splitProxyTarget(proxyHost string) (string, int, error) {
	host, portText, err := net.SplitHostPort(strings.TrimSpace(proxyHost))
	if err != nil {
		return "", 0, fmt.Errorf("invalid proxy host %q: %w", proxyHost, err)
	}
	host, err = normalizePolicyHost(host)
	if err != nil {
		return "", 0, fmt.Errorf("invalid proxy host %q: %w", proxyHost, err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return "", 0, fmt.Errorf("invalid proxy port %q", portText)
	}
	return host, port, nil
}

// ResetStats resets the guard statistics (for testing).
func (g *WebSocketGuard) ResetStats() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.stats = WebSocketGuardStats{}
}
