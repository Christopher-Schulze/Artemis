package bridge

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func writePolicyFile(t *testing.T, path, content string) {
	t.Helper()
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write policy: %v", err)
	}
}

func TestNavigationPolicyAllowByDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.json")
	writePolicyFile(t, path, `{
		"version": 1,
		"default": "allow",
		"rules": []
	}`)
	p, err := NewNavigationPolicy(NavigationPolicyConfig{Path: path})
	if err != nil {
		t.Fatalf("NewNavigationPolicy: %v", err)
	}
	defer p.Stop()
	resp, err := p.Evaluate(context.Background(), PolicyRequest{URL: "https://example.com"})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if resp.Decision != PolicyDecisionAllow {
		t.Errorf("Decision = %s, want allow", resp.Decision)
	}
}

func TestNavigationPolicyDenyRule(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.json")
	writePolicyFile(t, path, `{
		"version": 1,
		"default": "allow",
		"rules": [
			{"action": "deny", "pattern": "https://internal.corp.local/.*"}
		]
	}`)
	p, err := NewNavigationPolicy(NavigationPolicyConfig{Path: path})
	if err != nil {
		t.Fatalf("NewNavigationPolicy: %v", err)
	}
	defer p.Stop()
	resp, err := p.Evaluate(context.Background(), PolicyRequest{URL: "https://internal.corp.local/admin"})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if resp.Decision != PolicyDecisionDeny {
		t.Errorf("Decision = %s, want deny", resp.Decision)
	}
}

func TestNavigationPolicyLiteralPrefixMatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.json")
	writePolicyFile(t, path, `{
		"version": 1,
		"default": "allow",
		"rules": [
			{"action": "deny", "pattern": "https://localhost"}
		]
	}`)
	p, err := NewNavigationPolicy(NavigationPolicyConfig{Path: path})
	if err != nil {
		t.Fatalf("NewNavigationPolicy: %v", err)
	}
	defer p.Stop()
	// Literal prefix: matches any URL starting with https://localhost.
	resp, _ := p.Evaluate(context.Background(), PolicyRequest{URL: "https://localhost:8080/admin"})
	if resp.Decision != PolicyDecisionDeny {
		t.Errorf("localhost:8080 Decision = %s, want deny", resp.Decision)
	}
	// Non-matching URL should be allowed by default.
	resp2, _ := p.Evaluate(context.Background(), PolicyRequest{URL: "https://example.com"})
	if resp2.Decision != PolicyDecisionAllow {
		t.Errorf("example.com Decision = %s, want allow", resp2.Decision)
	}
}

func TestNavigationPolicyRegexMatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.json")
	writePolicyFile(t, path, `{
		"version": 1,
		"default": "allow",
		"rules": [
			{"action": "challenge", "pattern": "https://admin\\.example\\.com/.*"}
		]
	}`)
	p, err := NewNavigationPolicy(NavigationPolicyConfig{Path: path})
	if err != nil {
		t.Fatalf("NewNavigationPolicy: %v", err)
	}
	defer p.Stop()
	resp, _ := p.Evaluate(context.Background(), PolicyRequest{URL: "https://admin.example.com/dashboard"})
	if resp.Decision != PolicyDecisionChallenge {
		t.Errorf("admin Decision = %s, want challenge", resp.Decision)
	}
	resp2, _ := p.Evaluate(context.Background(), PolicyRequest{URL: "https://example.com/dashboard"})
	if resp2.Decision != PolicyDecisionAllow {
		t.Errorf("non-admin Decision = %s, want allow", resp2.Decision)
	}
}

func TestNavigationPolicyFirstMatchWins(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.json")
	writePolicyFile(t, path, `{
		"version": 1,
		"default": "deny",
		"rules": [
			{"action": "allow", "pattern": "https://safe.example.com/.*"},
			{"action": "deny", "pattern": "https://.*\\.example\\.com/.*"}
		]
	}`)
	p, err := NewNavigationPolicy(NavigationPolicyConfig{Path: path})
	if err != nil {
		t.Fatalf("NewNavigationPolicy: %v", err)
	}
	defer p.Stop()
	// First rule matches -> allow (even though second rule would deny).
	resp, _ := p.Evaluate(context.Background(), PolicyRequest{URL: "https://safe.example.com/page"})
	if resp.Decision != PolicyDecisionAllow {
		t.Errorf("safe.example.com Decision = %s, want allow (first match)", resp.Decision)
	}
	// Second rule matches -> deny.
	resp2, _ := p.Evaluate(context.Background(), PolicyRequest{URL: "https://other.example.com/page"})
	if resp2.Decision != PolicyDecisionDeny {
		t.Errorf("other.example.com Decision = %s, want deny", resp2.Decision)
	}
	// No rule matches -> default deny.
	resp3, _ := p.Evaluate(context.Background(), PolicyRequest{URL: "https://other.org/page"})
	if resp3.Decision != PolicyDecisionDeny {
		t.Errorf("other.org Decision = %s, want deny (default)", resp3.Decision)
	}
}

func TestNavigationPolicyMissingFileFailClosed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent.json")
	p, err := NewNavigationPolicy(NavigationPolicyConfig{Path: path})
	if err != nil {
		t.Fatalf("NewNavigationPolicy: %v", err)
	}
	defer p.Stop()
	// Missing file -> degraded mode -> DisabledDefault (deny by default).
	resp, _ := p.Evaluate(context.Background(), PolicyRequest{URL: "https://example.com"})
	if resp.Decision != PolicyDecisionDeny {
		t.Errorf("missing file Decision = %s, want deny (fail-closed)", resp.Decision)
	}
}

func TestNavigationPolicyMissingFileCustomDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent.json")
	p, err := NewNavigationPolicy(NavigationPolicyConfig{
		Path:            path,
		DisabledDefault: PolicyFileAllow,
	})
	if err != nil {
		t.Fatalf("NewNavigationPolicy: %v", err)
	}
	defer p.Stop()
	resp, _ := p.Evaluate(context.Background(), PolicyRequest{URL: "https://example.com"})
	if resp.Decision != PolicyDecisionAllow {
		t.Errorf("missing file with allow default Decision = %s, want allow", resp.Decision)
	}
}

func TestNavigationPolicyEmptyURL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.json")
	writePolicyFile(t, path, `{"version":1,"default":"allow","rules":[]}`)
	p, err := NewNavigationPolicy(NavigationPolicyConfig{Path: path})
	if err != nil {
		t.Fatalf("NewNavigationPolicy: %v", err)
	}
	defer p.Stop()
	resp, _ := p.Evaluate(context.Background(), PolicyRequest{URL: ""})
	if resp.Decision != PolicyDecisionDeny {
		t.Errorf("empty URL Decision = %s, want deny", resp.Decision)
	}
}

func TestNavigationPolicyUnsupportedVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.json")
	writePolicyFile(t, path, `{"version":99,"default":"allow","rules":[]}`)
	p, err := NewNavigationPolicy(NavigationPolicyConfig{Path: path})
	if err != nil {
		t.Fatalf("NewNavigationPolicy: %v", err)
	}
	defer p.Stop()
	// Failed load -> degraded mode -> deny.
	resp, _ := p.Evaluate(context.Background(), PolicyRequest{URL: "https://example.com"})
	if resp.Decision != PolicyDecisionDeny {
		t.Errorf("unsupported version Decision = %s, want deny (degraded)", resp.Decision)
	}
}

func TestNavigationPolicyHotReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.json")
	writePolicyFile(t, path, `{"version":1,"default":"allow","rules":[]}`)
	p, err := NewNavigationPolicy(NavigationPolicyConfig{
		Path:           path,
		ReloadInterval: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewNavigationPolicy: %v", err)
	}
	defer p.Stop()

	// Initial: allow.
	resp, _ := p.Evaluate(context.Background(), PolicyRequest{URL: "https://example.com"})
	if resp.Decision != PolicyDecisionAllow {
		t.Fatalf("initial Decision = %s, want allow", resp.Decision)
	}

	// Rewrite policy to deny-all and bump mtime.
	writePolicyFile(t, path, `{"version":1,"default":"deny","rules":[]}`)
	// Force mtime to advance (some filesystems have coarse mtime resolution).
	now := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, now, now); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	p.StartHotReload()
	// Wait for hot-reload to pick up the change.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		resp, _ = p.Evaluate(context.Background(), PolicyRequest{URL: "https://example.com"})
		if resp.Decision == PolicyDecisionDeny {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if resp.Decision != PolicyDecisionDeny {
		t.Errorf("after hot-reload Decision = %s, want deny", resp.Decision)
	}

	_, _, _, _, reloads, _ := p.Stats()
	if reloads < 1 {
		t.Errorf("reloads = %d, want >= 1", reloads)
	}
}

func TestNavigationPolicyReloadManual(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.json")
	writePolicyFile(t, path, `{"version":1,"default":"allow","rules":[]}`)
	p, err := NewNavigationPolicy(NavigationPolicyConfig{Path: path})
	if err != nil {
		t.Fatalf("NewNavigationPolicy: %v", err)
	}
	defer p.Stop()
	// Rewrite and manual reload.
	writePolicyFile(t, path, `{"version":1,"default":"deny","rules":[]}`)
	if err := p.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	resp, _ := p.Evaluate(context.Background(), PolicyRequest{URL: "https://example.com"})
	if resp.Decision != PolicyDecisionDeny {
		t.Errorf("after manual reload Decision = %s, want deny", resp.Decision)
	}
}

func TestNavigationPolicyStats(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.json")
	writePolicyFile(t, path, `{"version":1,"default":"allow","rules":[]}`)
	p, err := NewNavigationPolicy(NavigationPolicyConfig{Path: path})
	if err != nil {
		t.Fatalf("NewNavigationPolicy: %v", err)
	}
	defer p.Stop()
	for i := 0; i < 5; i++ {
		p.Evaluate(context.Background(), PolicyRequest{URL: "https://example.com"})
	}
	total, allowed, denied, _, _, _ := p.Stats()
	if total != 5 {
		t.Errorf("total = %d, want 5", total)
	}
	if allowed != 5 {
		t.Errorf("allowed = %d, want 5", allowed)
	}
	if denied != 0 {
		t.Errorf("denied = %d, want 0", denied)
	}
}

func TestNavigationPolicyValidateURL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.json")
	writePolicyFile(t, path, `{
		"version": 1,
		"default": "allow",
		"rules": [
			{"action": "deny", "pattern": "https://blocked.example.com/.*"}
		]
	}`)
	p, err := NewNavigationPolicy(NavigationPolicyConfig{Path: path})
	if err != nil {
		t.Fatalf("NewNavigationPolicy: %v", err)
	}
	defer p.Stop()
	if p.ValidateURL("https://blocked.example.com/secret") != PolicyDecisionDeny {
		t.Error("ValidateURL should return deny for blocked URL")
	}
	if p.ValidateURL("https://safe.example.com/page") != PolicyDecisionAllow {
		t.Error("ValidateURL should return allow for safe URL")
	}
}

func TestNavigationPolicyCurrentPolicy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.json")
	writePolicyFile(t, path, `{"version":1,"default":"allow","rules":[]}`)
	p, err := NewNavigationPolicy(NavigationPolicyConfig{Path: path})
	if err != nil {
		t.Fatalf("NewNavigationPolicy: %v", err)
	}
	defer p.Stop()
	cp := p.CurrentPolicy()
	if cp == nil {
		t.Fatal("CurrentPolicy = nil")
	}
	if cp.Default != PolicyFileAllow {
		t.Errorf("CurrentPolicy.Default = %s, want allow", cp.Default)
	}
}

func TestNavigationPolicyCurrentPolicyDegraded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent.json")
	p, err := NewNavigationPolicy(NavigationPolicyConfig{Path: path})
	if err != nil {
		t.Fatalf("NewNavigationPolicy: %v", err)
	}
	defer p.Stop()
	if p.CurrentPolicy() != nil {
		t.Error("CurrentPolicy should be nil in degraded mode")
	}
}

func TestNavigationPolicyStopIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.json")
	writePolicyFile(t, path, `{"version":1,"default":"allow","rules":[]}`)
	p, err := NewNavigationPolicy(NavigationPolicyConfig{Path: path})
	if err != nil {
		t.Fatalf("NewNavigationPolicy: %v", err)
	}
	p.Stop()
	p.Stop() // must not panic
}

func TestNavigationPolicyContextCancel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.json")
	writePolicyFile(t, path, `{"version":1,"default":"allow","rules":[]}`)
	p, err := NewNavigationPolicy(NavigationPolicyConfig{Path: path})
	if err != nil {
		t.Fatalf("NewNavigationPolicy: %v", err)
	}
	defer p.Stop()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	resp, err := p.Evaluate(ctx, PolicyRequest{URL: "https://example.com"})
	if err == nil {
		t.Error("expected error from cancelled context")
	}
	if resp.Decision != PolicyDecisionDeny {
		t.Errorf("cancelled ctx Decision = %s, want deny", resp.Decision)
	}
}

func TestWriteDefaultPolicyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "policy.json")
	if err := WriteDefaultPolicyFile(path); err != nil {
		t.Fatalf("WriteDefaultPolicyFile: %v", err)
	}
	p, err := NewNavigationPolicy(NavigationPolicyConfig{Path: path})
	if err != nil {
		t.Fatalf("NewNavigationPolicy: %v", err)
	}
	defer p.Stop()
	// Default policy denies localhost.
	resp, _ := p.Evaluate(context.Background(), PolicyRequest{URL: "https://localhost:8080"})
	if resp.Decision != PolicyDecisionDeny {
		t.Errorf("localhost Decision = %s, want deny", resp.Decision)
	}
	// Default policy allows other URLs.
	resp2, _ := p.Evaluate(context.Background(), PolicyRequest{URL: "https://example.com"})
	if resp2.Decision != PolicyDecisionAllow {
		t.Errorf("example.com Decision = %s, want allow", resp2.Decision)
	}
}

func TestParsePolicyURL(t *testing.T) {
	host, err := ParsePolicyURL("https://example.com/path?q=1")
	if err != nil {
		t.Fatalf("ParsePolicyURL: %v", err)
	}
	if host != "example.com" {
		t.Errorf("host = %s, want example.com", host)
	}
}

func TestNavigationPolicyImplementsPolicyEngine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.json")
	writePolicyFile(t, path, `{"version":1,"default":"allow","rules":[]}`)
	p, err := NewNavigationPolicy(NavigationPolicyConfig{Path: path})
	if err != nil {
		t.Fatalf("NewNavigationPolicy: %v", err)
	}
	defer p.Stop()
	// Verify NavigationPolicy satisfies the PolicyEngine interface.
	var _ PolicyEngine = p
}

func TestNavigationPolicyWiredToPolicyHook(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.json")
	writePolicyFile(t, path, `{
		"version": 1,
		"default": "allow",
		"rules": [
			{"action": "deny", "pattern": "https://blocked.example.com/.*"}
		]
	}`)
	p, err := NewNavigationPolicy(NavigationPolicyConfig{Path: path})
	if err != nil {
		t.Fatalf("NewNavigationPolicy: %v", err)
	}
	defer p.Stop()
	hook := NewPolicyHook(p)
	resp, err := hook.Check(context.Background(), PolicyRequest{URL: "https://blocked.example.com/secret"})
	if err != nil {
		t.Fatalf("hook.Check: %v", err)
	}
	if resp.Decision != PolicyDecisionDeny {
		t.Errorf("hook.Check blocked Decision = %s, want deny", resp.Decision)
	}
	resp2, _ := hook.Check(context.Background(), PolicyRequest{URL: "https://safe.example.com/page"})
	if resp2.Decision != PolicyDecisionAllow {
		t.Errorf("hook.Check safe Decision = %s, want allow", resp2.Decision)
	}
	// Verify hook stats were updated.
	stats := hook.Stats()
	if stats.Total != 2 {
		t.Errorf("hook stats Total = %d, want 2", stats.Total)
	}
	if stats.Denied != 1 {
		t.Errorf("hook stats Denied = %d, want 1", stats.Denied)
	}
	if stats.Allowed != 1 {
		t.Errorf("hook stats Allowed = %d, want 1", stats.Allowed)
	}
}

// TestNavigationPolicyConcurrentEvaluate verifies thread-safety under
// concurrent Evaluate calls.
func TestNavigationPolicyConcurrentEvaluate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.json")
	writePolicyFile(t, path, `{"version":1,"default":"allow","rules":[]}`)
	p, err := NewNavigationPolicy(NavigationPolicyConfig{Path: path})
	if err != nil {
		t.Fatalf("NewNavigationPolicy: %v", err)
	}
	defer p.Stop()
	var done atomic.Int64
	for i := 0; i < 50; i++ {
		go func() {
			p.Evaluate(context.Background(), PolicyRequest{URL: "https://example.com"})
			done.Add(1)
		}()
	}
	deadline := time.Now().Add(2 * time.Second)
	for done.Load() < 50 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if done.Load() != 50 {
		t.Fatalf("only %d/50 goroutines completed", done.Load())
	}
	total, _, _, _, _, _ := p.Stats()
	if total != 50 {
		t.Errorf("total = %d, want 50", total)
	}
}
