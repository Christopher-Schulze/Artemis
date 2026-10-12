package network

import (
	"testing"
)

func TestDefaultWebSocketGuardConfig(t *testing.T) {
	config := DefaultWebSocketGuardConfig()
	if !config.Enabled {
		t.Error("expected enabled=true")
	}
	if config.ProxyHost == "" {
		t.Error("expected non-empty proxy_host")
	}
	if !config.BlockDirectConnections {
		t.Error("expected block_direct_connections=true")
	}
	if config.AllowLocalhost {
		t.Error("expected allow_localhost=false by default")
	}
	if len(config.AllowedSchemes) != 2 {
		t.Fatalf("expected 2 allowed schemes, got %d", len(config.AllowedSchemes))
	}
}

func TestWebSocketGuard_Evaluate_Disabled(t *testing.T) {
	g := NewWebSocketGuard(WebSocketGuardConfig{Enabled: false})
	decision := g.Evaluate(WebSocketEvent{URL: "ws://example.com/socket"})
	if decision.Verdict != WebSocketVerdictAllow {
		t.Fatalf("expected allow when disabled, got %s", decision.Verdict)
	}
	if decision.Reason != "guard disabled" {
		t.Errorf("expected 'guard disabled', got %s", decision.Reason)
	}
}

func TestWebSocketGuard_Evaluate_AllowThroughProxy(t *testing.T) {
	config := DefaultWebSocketGuardConfig()
	g := NewWebSocketGuard(config)
	decision := g.Evaluate(WebSocketEvent{
		URL: "ws://" + config.ProxyHost + "/socket",
	})
	if decision.Verdict != WebSocketVerdictAllow {
		t.Fatalf("expected allow for proxy-routed WebSocket, got %s: %s", decision.Verdict, decision.Reason)
	}
}

func TestWebSocketGuard_Evaluate_BlockBypass(t *testing.T) {
	config := DefaultWebSocketGuardConfig()
	g := NewWebSocketGuard(config)
	decision := g.Evaluate(WebSocketEvent{
		URL: "ws://evil.example.com/socket",
	})
	if decision.Verdict != WebSocketVerdictBlock {
		t.Fatalf("expected block for direct WebSocket, got %s: %s", decision.Verdict, decision.Reason)
	}
	if decision.Reason == "" {
		t.Error("expected non-empty block reason")
	}
}

func TestWebSocketGuard_Evaluate_BlockInvalidURL(t *testing.T) {
	config := DefaultWebSocketGuardConfig()
	g := NewWebSocketGuard(config)
	decision := g.Evaluate(WebSocketEvent{
		URL: "://invalid",
	})
	if decision.Verdict != WebSocketVerdictBlock {
		t.Fatalf("expected block for invalid URL, got %s", decision.Verdict)
	}
}

func TestWebSocketGuard_Evaluate_BlockBadScheme(t *testing.T) {
	config := DefaultWebSocketGuardConfig()
	g := NewWebSocketGuard(config)
	decision := g.Evaluate(WebSocketEvent{
		URL: "http://example.com/socket",
	})
	if decision.Verdict != WebSocketVerdictBlock {
		t.Fatalf("expected block for non-ws scheme, got %s", decision.Verdict)
	}
}

func TestWebSocketGuard_Evaluate_BlockLocalhost(t *testing.T) {
	config := DefaultWebSocketGuardConfig()
	g := NewWebSocketGuard(config)
	decision := g.Evaluate(WebSocketEvent{
		URL: "ws://localhost:3000/socket",
	})
	if decision.Verdict != WebSocketVerdictBlock {
		t.Fatalf("expected block for localhost, got %s", decision.Verdict)
	}
}

func TestWebSocketGuard_Evaluate_LocalhostAliasRemainsBlocked(t *testing.T) {
	config := DefaultWebSocketGuardConfig()
	config.AllowLocalhost = true
	config.BlockDirectConnections = false
	g := NewWebSocketGuard(config)
	decision := g.Evaluate(WebSocketEvent{
		URL: "ws://localhost:3000/socket",
	})
	if decision.Verdict != WebSocketVerdictBlock {
		t.Fatalf("expected canonical policy to block localhost alias, got %s: %s", decision.Verdict, decision.Reason)
	}
}

func TestWebSocketGuard_Evaluate_Allow127Localhost(t *testing.T) {
	config := DefaultWebSocketGuardConfig()
	config.AllowLocalhost = true
	config.BlockDirectConnections = false
	config.AllowedPorts = []int{3000}
	g := NewWebSocketGuard(config)
	decision := g.Evaluate(WebSocketEvent{
		URL: "ws://127.0.0.1:3000/socket",
	})
	if decision.Verdict != WebSocketVerdictAllow {
		t.Fatalf("expected allow for 127.0.0.1 when enabled, got %s", decision.Verdict)
	}
}

func TestWebSocketGuard_Evaluate_AllowIPv6Localhost(t *testing.T) {
	config := DefaultWebSocketGuardConfig()
	config.AllowLocalhost = true
	config.BlockDirectConnections = false
	config.AllowedPorts = []int{3000}
	g := NewWebSocketGuard(config)
	decision := g.Evaluate(WebSocketEvent{
		URL: "ws://[::1]:3000/socket",
	})
	if decision.Verdict != WebSocketVerdictAllow {
		t.Fatalf("expected allow for ::1 when enabled, got %s", decision.Verdict)
	}
}

func TestWebSocketGuard_Evaluate_WSSScheme(t *testing.T) {
	config := DefaultWebSocketGuardConfig()
	g := NewWebSocketGuard(config)
	decision := g.Evaluate(WebSocketEvent{
		URL: "wss://" + config.ProxyHost + "/socket",
	})
	if decision.Verdict != WebSocketVerdictAllow {
		t.Fatalf("expected allow for wss through proxy, got %s: %s", decision.Verdict, decision.Reason)
	}
}

func TestWebSocketGuard_Evaluate_NoBlockDirectConnections(t *testing.T) {
	config := DefaultWebSocketGuardConfig()
	config.BlockDirectConnections = false
	g := NewWebSocketGuard(config)
	decision := g.Evaluate(WebSocketEvent{
		URL: "ws://1.1.1.1/socket",
	})
	if decision.Verdict != WebSocketVerdictAllow {
		t.Fatalf("expected allow when block_direct_connections=false, got %s", decision.Verdict)
	}
}

func TestWebSocketGuard_Evaluate_NoProxyHost(t *testing.T) {
	config := DefaultWebSocketGuardConfig()
	config.ProxyHost = ""
	g := NewWebSocketGuard(config)
	decision := g.Evaluate(WebSocketEvent{
		URL: "ws://1.1.1.1/socket",
	})
	if decision.Verdict != WebSocketVerdictAllow {
		t.Fatalf("expected allow when no proxy_host set, got %s", decision.Verdict)
	}
}

func TestWebSocketGuard_Stats(t *testing.T) {
	config := DefaultWebSocketGuardConfig()
	g := NewWebSocketGuard(config)

	g.Evaluate(WebSocketEvent{URL: "ws://" + config.ProxyHost + "/ok"})
	g.Evaluate(WebSocketEvent{URL: "ws://evil.com/blocked"})
	g.Evaluate(WebSocketEvent{URL: "ws://" + config.ProxyHost + "/ok2"})

	stats := g.Stats()
	if stats.Total != 3 {
		t.Fatalf("expected total=3, got %d", stats.Total)
	}
	if stats.Allowed != 2 {
		t.Fatalf("expected allowed=2, got %d", stats.Allowed)
	}
	if stats.Blocked != 1 {
		t.Fatalf("expected blocked=1, got %d", stats.Blocked)
	}
	if stats.Redirect != 0 {
		t.Fatalf("expected redirect=0, got %d", stats.Redirect)
	}
}

func TestWebSocketGuard_ResetStats(t *testing.T) {
	g := NewWebSocketGuard(DefaultWebSocketGuardConfig())
	g.Evaluate(WebSocketEvent{URL: "ws://example.com/socket"})
	if g.Stats().Total != 1 {
		t.Fatal("expected total=1 before reset")
	}
	g.ResetStats()
	if g.Stats().Total != 0 {
		t.Fatal("expected total=0 after reset")
	}
}

func TestWebSocketGuard_Config(t *testing.T) {
	config := DefaultWebSocketGuardConfig()
	g := NewWebSocketGuard(config)
	if g.Config().ProxyHost != config.ProxyHost {
		t.Error("config mismatch")
	}
}

func TestWebSocketGuard_SetConfig(t *testing.T) {
	g := NewWebSocketGuard(DefaultWebSocketGuardConfig())
	newConfig := WebSocketGuardConfig{
		Enabled:        true,
		ProxyHost:      "10.0.0.1:9090",
		AllowedSchemes: []string{"ws", "wss"},
	}
	g.SetConfig(newConfig)
	if g.Config().ProxyHost != "10.0.0.1:9090" {
		t.Fatalf("expected proxy_host=10.0.0.1:9090, got %s", g.Config().ProxyHost)
	}
}

func TestWebSocketGuard_Evaluate_OriginalURLPreserved(t *testing.T) {
	g := NewWebSocketGuard(DefaultWebSocketGuardConfig())
	decision := g.Evaluate(WebSocketEvent{URL: "ws://evil.com/socket"})
	if decision.Original != "ws://evil.com/socket" {
		t.Fatalf("expected original URL preserved, got %s", decision.Original)
	}
}

func TestWebSocketGuard_Evaluate_RequestIDNotUsed(t *testing.T) {
	// RequestID is for tracking only, not for validation.
	g := NewWebSocketGuard(DefaultWebSocketGuardConfig())
	decision := g.Evaluate(WebSocketEvent{
		RequestID: "req-123",
		URL:       "ws://evil.com/socket",
	})
	if decision.Verdict != WebSocketVerdictBlock {
		t.Fatalf("expected block regardless of request_id, got %s", decision.Verdict)
	}
}

func TestWebSocketGuard_ConcurrentEvaluate(t *testing.T) {
	// Verify the guard is thread-safe under concurrent access.
	g := NewWebSocketGuard(DefaultWebSocketGuardConfig())
	done := make(chan struct{}, 10)
	for i := 0; i < 10; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 100; j++ {
				g.Evaluate(WebSocketEvent{URL: "ws://example.com/socket"})
			}
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
	stats := g.Stats()
	if stats.Total != 1000 {
		t.Fatalf("expected total=1000, got %d", stats.Total)
	}
}

func TestWebSocketGuard_Evaluate_BlockUnspecifiedAddress(t *testing.T) {
	config := DefaultWebSocketGuardConfig()
	config.AllowLocalhost = true
	config.BlockDirectConnections = false
	config.AllowedPorts = []int{3000}
	g := NewWebSocketGuard(config)
	decision := g.Evaluate(WebSocketEvent{
		URL: "ws://0.0.0.0:3000/socket",
	})
	if decision.Verdict != WebSocketVerdictBlock {
		t.Fatalf("expected canonical policy to block 0.0.0.0, got %s", decision.Verdict)
	}
}

func TestWebSocketGuard_Evaluate_BlockNonWSPort(t *testing.T) {
	// Even if the host matches the proxy, a non-ws scheme should be blocked.
	config := DefaultWebSocketGuardConfig()
	g := NewWebSocketGuard(config)
	decision := g.Evaluate(WebSocketEvent{
		URL: "ftp://" + config.ProxyHost + "/socket",
	})
	if decision.Verdict != WebSocketVerdictBlock {
		t.Fatalf("expected block for ftp scheme, got %s", decision.Verdict)
	}
}

func TestWebSocketGuardDelegatesToCanonicalPolicy(t *testing.T) {
	decisions := make(chan Decision, 1)
	policy, err := NewPolicy(DefaultPolicyConfig(), nil, func(decision Decision) error {
		decisions <- decision
		return nil
	})
	if err != nil {
		t.Fatalf("NewPolicy: %v", err)
	}
	config := DefaultWebSocketGuardConfig()
	config.BlockDirectConnections = false
	config.SessionID = "cdp-session"
	guard := NewWebSocketGuardWithPolicy(config, policy)
	decision := guard.Evaluate(WebSocketEvent{URL: "ws://127.0.0.1/socket"})
	if decision.Verdict != WebSocketVerdictBlock {
		t.Fatalf("expected canonical private-address denial, got %s: %s", decision.Verdict, decision.Reason)
	}
	select {
	case policyDecision := <-decisions:
		if policyDecision.Kind != TargetWebSocket || policyDecision.Action != DecisionDeny {
			t.Fatalf("policy decision=%+v", policyDecision)
		}
		if policyDecision.SessionID != "cdp-session" {
			t.Fatalf("session ID=%q", policyDecision.SessionID)
		}
	default:
		t.Fatal("canonical policy was not invoked")
	}
}
