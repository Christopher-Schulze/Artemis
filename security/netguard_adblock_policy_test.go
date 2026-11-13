package security

import (
	"net"
	"testing"
)

// ==================== netguard.go tests ====================

// TestNewNetGuard verifies creation
// (SSRF prevention).
func TestNewNetGuard(t *testing.T) {
	n := NewNetGuard()
	if n == nil {
		t.Fatal("netguard should not be nil")
	}
}

// TestIsPrivateIP10 verifies 10.x.x.x is private
// (private IP blocking).
func TestIsPrivateIP10(t *testing.T) {
	if !IsPrivateIP(net.ParseIP("10.0.0.1")) {
		t.Error("10.0.0.1 should be private")
	}
	if !IsPrivateIP(net.ParseIP("10.255.255.255")) {
		t.Error("10.255.255.255 should be private")
	}
}

// TestIsPrivateIP172 verifies 172.16-31.x.x is private
// (private IP blocking).
func TestIsPrivateIP172(t *testing.T) {
	if !IsPrivateIP(net.ParseIP("172.16.0.1")) {
		t.Error("172.16.0.1 should be private")
	}
	if !IsPrivateIP(net.ParseIP("172.31.255.255")) {
		t.Error("172.31.255.255 should be private")
	}
	// 172.32.x.x is NOT private
	if IsPrivateIP(net.ParseIP("172.32.0.1")) {
		t.Error("172.32.0.1 should NOT be private")
	}
}

// TestIsPrivateIP192 verifies 192.168.x.x is private
// (private IP blocking).
func TestIsPrivateIP192(t *testing.T) {
	if !IsPrivateIP(net.ParseIP("192.168.0.1")) {
		t.Error("192.168.0.1 should be private")
	}
	if !IsPrivateIP(net.ParseIP("192.168.255.255")) {
		t.Error("192.168.255.255 should be private")
	}
}

// TestIsPrivateIPLoopback verifies 127.x.x.x is private
// (private IP blocking).
func TestIsPrivateIPLoopback(t *testing.T) {
	if !IsPrivateIP(net.ParseIP("127.0.0.1")) {
		t.Error("127.0.0.1 should be private")
	}
}

// TestIsPrivateIPLinkLocal verifies 169.254.x.x is private
// (private IP blocking).
func TestIsPrivateIPLinkLocal(t *testing.T) {
	if !IsPrivateIP(net.ParseIP("169.254.0.1")) {
		t.Error("169.254.0.1 should be private (link-local)")
	}
}

// TestIsPrivateIPPublic verifies public IPs are NOT private
// (private IP blocking).
func TestIsPrivateIPPublic(t *testing.T) {
	if IsPrivateIP(net.ParseIP("8.8.8.8")) {
		t.Error("8.8.8.8 should NOT be private")
	}
	if IsPrivateIP(net.ParseIP("1.1.1.1")) {
		t.Error("1.1.1.1 should NOT be private")
	}
}

// TestIsPrivateIPIPv6Loopback verifies ::1 is private
// (private IP blocking).
func TestIsPrivateIPIPv6Loopback(t *testing.T) {
	if !IsPrivateIP(net.ParseIP("::1")) {
		t.Error("::1 should be private (IPv6 loopback)")
	}
}

// TestIsPrivateIPIPv6ULA verifies fc00:: is private
// (private IP blocking).
func TestIsPrivateIPIPv6ULA(t *testing.T) {
	if !IsPrivateIP(net.ParseIP("fc00::1")) {
		t.Error("fc00::1 should be private (IPv6 ULA)")
	}
}

// TestIsPrivateIPNil verifies nil IP is safe.
func TestIsPrivateIPNil(t *testing.T) {
	if IsPrivateIP(nil) {
		t.Error("nil IP should not be private")
	}
}

// TestNetGuardCheckURLPrivate verifies private URL is blocked
// (SSRF prevention).
func TestNetGuardCheckURLPrivate(t *testing.T) {
	n := NewNetGuard()
	err := n.CheckURL("http://10.0.0.1/admin")
	if err == nil {
		t.Error("private IP URL should be blocked")
	}
}

// TestNetGuardCheckURLPublic verifies public URL is allowed
// (SSRF prevention).
func TestNetGuardCheckURLPublic(t *testing.T) {
	n := NewNetGuard()
	err := n.CheckURL("http://example.com/page")
	if err != nil {
		t.Errorf("public URL should be allowed: %v", err)
	}
}

// TestNetGuardCheckURLLoopback verifies loopback is blocked.
func TestNetGuardCheckURLLoopback(t *testing.T) {
	n := NewNetGuard()
	err := n.CheckURL("http://127.0.0.1:8080/api")
	if err == nil {
		t.Error("loopback URL should be blocked")
	}
}

// TestNetGuardAllow verifies allow list overrides private IP
// (SSRF prevention).
func TestNetGuardAllow(t *testing.T) {
	n := NewNetGuard()
	n.Allow("10.0.0.1")
	err := n.CheckURL("http://10.0.0.1/admin")
	if err != nil {
		t.Errorf("allowed private IP should pass: %v", err)
	}
}

// TestNetGuardBlock verifies explicit block.
func TestNetGuardBlock(t *testing.T) {
	n := NewNetGuard()
	n.Block("evil.com")
	err := n.CheckURL("http://evil.com/page")
	if err == nil {
		t.Error("blocked domain should fail")
	}
}

// TestNetGuardCheckURLInvalid verifies invalid URL errors.
func TestNetGuardCheckURLInvalid(t *testing.T) {
	n := NewNetGuard()
	err := n.CheckURL("://invalid")
	if err == nil {
		t.Error("invalid URL should error")
	}
}

// TestNetGuardIsBlockedIsAllowed verifies convenience methods.
func TestNetGuardIsBlockedIsAllowed(t *testing.T) {
	n := NewNetGuard()
	if !n.IsBlocked("http://10.0.0.1/") {
		t.Error("private IP should be blocked")
	}
	if !n.IsAllowed("http://example.com/") {
		t.Error("public domain should be allowed")
	}
}

// TestNetGuardNilSafe verifies nil guard is safe.
func TestNetGuardNilSafe(t *testing.T) {
	var n *NetGuard
	n.Allow("test")
	n.Block("test")
	if !n.IsBlocked("http://example.com/") {
		t.Error("nil should block everything")
	}
	if n.IsAllowed("http://example.com/") {
		t.Error("nil should not allow")
	}
}

// ==================== adblock.go tests ====================

// TestNewAdBlocker verifies creation
// (ad/tracker blocking 40+ patterns).
func TestNewAdBlocker(t *testing.T) {
	a := NewAdBlocker()
	if a == nil {
		t.Fatal("adblocker should not be nil")
	}
	if !a.IsEnabled() {
		t.Error("adblocker should be enabled by default")
	}
}

// TestAdBlockerPatternCount verifies 40+ patterns
// (40+ patterns).
func TestAdBlockerPatternCount(t *testing.T) {
	a := NewAdBlocker()
	if a.PatternCount() < 40 {
		t.Errorf("pattern count: got %d, want >= 40", a.PatternCount())
	}
}

// TestAdBlockerCheckAd verifies ad domain is blocked
// (ad/tracker blocking).
func TestAdBlockerCheckAd(t *testing.T) {
	a := NewAdBlocker()
	result := a.Check("ads.doubleclick.net")
	if !result.Blocked {
		t.Error("doubleclick.net should be blocked")
	}
	if result.Category != AdBlockCategoryAd {
		t.Errorf("category: got %s, want ad", result.Category)
	}
}

// TestAdBlockerCheckTracker verifies tracker domain is blocked
// (ad/tracker blocking).
func TestAdBlockerCheckTracker(t *testing.T) {
	a := NewAdBlocker()
	result := a.Check("www.hotjar.com")
	if !result.Blocked {
		t.Error("hotjar should be blocked")
	}
	if result.Category != AdBlockCategoryTracker {
		t.Errorf("category: got %s, want tracker", result.Category)
	}
}

// TestAdBlockerCheckClean verifies clean domain is not blocked
// (ad/tracker blocking).
func TestAdBlockerCheckClean(t *testing.T) {
	a := NewAdBlocker()
	result := a.Check("example.com")
	if result.Blocked {
		t.Error("example.com should NOT be blocked")
	}
}

// TestAdBlockerDisable verifies disable.
func TestAdBlockerDisable(t *testing.T) {
	a := NewAdBlocker()
	a.Disable()
	if a.IsEnabled() {
		t.Error("should be disabled")
	}
	// Disabled blocker should not block anything
	result := a.Check("ads.doubleclick.net")
	if result.Blocked {
		t.Error("disabled blocker should not block")
	}
}

// TestAdBlockerEnable verifies enable.
func TestAdBlockerEnable(t *testing.T) {
	a := NewAdBlocker()
	a.Disable()
	a.Enable()
	if !a.IsEnabled() {
		t.Error("should be enabled")
	}
}

// TestAdBlockerAddPattern verifies custom pattern
// (ad/tracker blocking).
func TestAdBlockerAddPattern(t *testing.T) {
	a := NewAdBlocker()
	a.AddPattern("evil-ads.com")
	result := a.Check("tracker.evil-ads.com")
	if !result.Blocked {
		t.Error("custom pattern should block")
	}
}

// TestAdBlockerIsBlocked verifies convenience method.
func TestAdBlockerIsBlocked(t *testing.T) {
	a := NewAdBlocker()
	if !a.IsBlocked("ads.doubleclick.net") {
		t.Error("doubleclick should be blocked")
	}
	if a.IsBlocked("example.com") {
		t.Error("example.com should not be blocked")
	}
}

// TestAdBlockerNilSafe verifies nil blocker is safe.
func TestAdBlockerNilSafe(t *testing.T) {
	var a *AdBlocker
	a.Enable()
	a.Disable()
	a.AddPattern("test")
	if a.IsEnabled() {
		t.Error("nil should not be enabled")
	}
	if a.IsBlocked("example.com") {
		t.Error("nil should not block")
	}
	if a.PatternCount() != 0 {
		t.Error("nil should have 0 patterns")
	}
}

// ==================== policy.go tests ====================

// TestNewNavigationPolicy verifies creation with default-deny
// (navigation policy domain allowlist).
func TestNewNavigationPolicy(t *testing.T) {
	p := NewNavigationPolicy()
	if p == nil {
		t.Fatal("policy should not be nil")
	}
	// Default-deny: unlisted domain should be blocked
	if p.IsAllowed("example.com") {
		t.Error("default-deny should block unlisted domain")
	}
}

// TestNewNavigationPolicyDefaultAllow verifies default-allow.
func TestNewNavigationPolicyDefaultAllow(t *testing.T) {
	p := NewNavigationPolicyDefaultAllow()
	if !p.IsAllowed("example.com") {
		t.Error("default-allow should allow unlisted domain")
	}
}

// TestNavigationPolicyAllow verifies allow list
// (navigation policy domain allowlist).
func TestNavigationPolicyAllow(t *testing.T) {
	p := NewNavigationPolicy()
	p.Allow("example.com")
	if !p.IsAllowed("example.com") {
		t.Error("allowed domain should be allowed")
	}
	if p.IsAllowed("evil.com") {
		t.Error("non-allowed domain should be blocked")
	}
}

// TestNavigationPolicyBlock verifies block list
// (navigation policy).
func TestNavigationPolicyBlock(t *testing.T) {
	p := NewNavigationPolicyDefaultAllow()
	p.Block("evil.com")
	if p.IsAllowed("evil.com") {
		t.Error("blocked domain should not be allowed")
	}
	if !p.IsAllowed("example.com") {
		t.Error("non-blocked domain should be allowed")
	}
}

// TestNavigationPolicyBlockOverridesAllow verifies block
// priority over allow (navigation policy).
func TestNavigationPolicyBlockOverridesAllow(t *testing.T) {
	p := NewNavigationPolicy()
	p.Allow("example.com")
	p.Block("example.com")
	if p.IsAllowed("example.com") {
		t.Error("block should override allow")
	}
}

// TestNavigationPolicyCheckURL verifies URL check
// (navigation policy domain allowlist).
func TestNavigationPolicyCheckURL(t *testing.T) {
	p := NewNavigationPolicy()
	p.Allow("example.com")
	err := p.CheckURL("https://example.com/page?q=1")
	if err != nil {
		t.Errorf("allowed URL should pass: %v", err)
	}
	err = p.CheckURL("https://evil.com/page")
	if err == nil {
		t.Error("non-allowed URL should fail")
	}
}

// TestNavigationPolicyCheckURLCaseInsensitive verifies
// case-insensitive domain matching.
func TestNavigationPolicyCheckURLCaseInsensitive(t *testing.T) {
	p := NewNavigationPolicy()
	p.Allow("Example.COM")
	if !p.IsAllowed("example.com") {
		t.Error("case-insensitive: example.com should be allowed")
	}
}

// TestNavigationPolicySetDefaultAllow verifies SetDefaultAllow.
func TestNavigationPolicySetDefaultAllow(t *testing.T) {
	p := NewNavigationPolicy()
	p.SetDefaultAllow(true)
	if !p.IsAllowed("unlisted.com") {
		t.Error("after SetDefaultAllow(true), unlisted should be allowed")
	}
}

// TestNavigationPolicyAllowList verifies AllowList.
func TestNavigationPolicyAllowList(t *testing.T) {
	p := NewNavigationPolicy()
	p.Allow("a.com")
	p.Allow("b.com")
	list := p.AllowList()
	if len(list) != 2 {
		t.Errorf("allow list: got %d, want 2", len(list))
	}
}

// TestNavigationPolicyBlockList verifies BlockList.
func TestNavigationPolicyBlockList(t *testing.T) {
	p := NewNavigationPolicy()
	p.Block("a.com")
	p.Block("b.com")
	list := p.BlockList()
	if len(list) != 2 {
		t.Errorf("block list: got %d, want 2", len(list))
	}
}

// TestNavigationPolicyAllowCount verifies AllowCount.
func TestNavigationPolicyAllowCount(t *testing.T) {
	p := NewNavigationPolicy()
	p.Allow("a.com")
	p.Allow("b.com")
	if p.AllowCount() != 2 {
		t.Errorf("allow count: got %d, want 2", p.AllowCount())
	}
}

// TestNavigationPolicyBlockCount verifies BlockCount.
func TestNavigationPolicyBlockCount(t *testing.T) {
	p := NewNavigationPolicy()
	p.Block("a.com")
	if p.BlockCount() != 1 {
		t.Errorf("block count: got %d, want 1", p.BlockCount())
	}
}

// TestNavigationPolicyNilSafe verifies nil policy is safe.
func TestNavigationPolicyNilSafe(t *testing.T) {
	var p *NavigationPolicy
	p.Allow("test")
	p.Block("test")
	p.SetDefaultAllow(true)
	if p.IsAllowed("example.com") {
		t.Error("nil should not allow")
	}
	if p.AllowCount() != 0 {
		t.Error("nil should have 0 allow count")
	}
}

// ==================== parity test ====================

// TestNetguardAdblockPolicyParity verifies full parity.
// (SSRF prevention, ad/tracker blocking, navigation policy).
func TestNetguardAdblockPolicyParity(t *testing.T) {
	// 1. SSRF prevention (netguard)
	n := NewNetGuard()
	if !n.IsBlocked("http://10.0.0.1/") {
		t.Error("netguard: private IP should be blocked")
	}
	if !n.IsAllowed("http://example.com/") {
		t.Error("netguard: public domain should be allowed")
	}

	// 2. Ad/tracker blocking (adblock)
	a := NewAdBlocker()
	if a.PatternCount() < 40 {
		t.Error("adblock: should have 40+ patterns")
	}
	if !a.IsBlocked("ads.doubleclick.net") {
		t.Error("adblock: ad domain should be blocked")
	}

	// 3. Navigation policy (policy)
	p := NewNavigationPolicy()
	p.Allow("trusted.com")
	if !p.IsAllowed("trusted.com") {
		t.Error("policy: trusted domain should be allowed")
	}
	if p.IsAllowed("untrusted.com") {
		t.Error("policy: untrusted domain should be blocked")
	}
}
