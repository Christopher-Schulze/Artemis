package security

import (
	"net"
	"testing"
)

// ==================== netguard.go tests ====================

// TestTASK2249_NewNetGuard verifies creation
// (spec L4027: SSRF prevention).
func TestTASK2249_NewNetGuard(t *testing.T) {
	n := NewNetGuard()
	if n == nil {
		t.Fatal("netguard should not be nil")
	}
}

// TestTASK2249_IsPrivateIP10 verifies 10.x.x.x is private
// (spec L4027: private IP blocking).
func TestTASK2249_IsPrivateIP10(t *testing.T) {
	if !IsPrivateIP(net.ParseIP("10.0.0.1")) {
		t.Error("10.0.0.1 should be private")
	}
	if !IsPrivateIP(net.ParseIP("10.255.255.255")) {
		t.Error("10.255.255.255 should be private")
	}
}

// TestTASK2249_IsPrivateIP172 verifies 172.16-31.x.x is private
// (spec L4027: private IP blocking).
func TestTASK2249_IsPrivateIP172(t *testing.T) {
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

// TestTASK2249_IsPrivateIP192 verifies 192.168.x.x is private
// (spec L4027: private IP blocking).
func TestTASK2249_IsPrivateIP192(t *testing.T) {
	if !IsPrivateIP(net.ParseIP("192.168.0.1")) {
		t.Error("192.168.0.1 should be private")
	}
	if !IsPrivateIP(net.ParseIP("192.168.255.255")) {
		t.Error("192.168.255.255 should be private")
	}
}

// TestTASK2249_IsPrivateIPLoopback verifies 127.x.x.x is private
// (spec L4027: private IP blocking).
func TestTASK2249_IsPrivateIPLoopback(t *testing.T) {
	if !IsPrivateIP(net.ParseIP("127.0.0.1")) {
		t.Error("127.0.0.1 should be private")
	}
}

// TestTASK2249_IsPrivateIPLinkLocal verifies 169.254.x.x is private
// (spec L4027: private IP blocking).
func TestTASK2249_IsPrivateIPLinkLocal(t *testing.T) {
	if !IsPrivateIP(net.ParseIP("169.254.0.1")) {
		t.Error("169.254.0.1 should be private (link-local)")
	}
}

// TestTASK2249_IsPrivateIPPublic verifies public IPs are NOT private
// (spec L4027: private IP blocking).
func TestTASK2249_IsPrivateIPPublic(t *testing.T) {
	if IsPrivateIP(net.ParseIP("8.8.8.8")) {
		t.Error("8.8.8.8 should NOT be private")
	}
	if IsPrivateIP(net.ParseIP("1.1.1.1")) {
		t.Error("1.1.1.1 should NOT be private")
	}
}

// TestTASK2249_IsPrivateIPIPv6Loopback verifies ::1 is private
// (spec L4027: private IP blocking).
func TestTASK2249_IsPrivateIPIPv6Loopback(t *testing.T) {
	if !IsPrivateIP(net.ParseIP("::1")) {
		t.Error("::1 should be private (IPv6 loopback)")
	}
}

// TestTASK2249_IsPrivateIPIPv6ULA verifies fc00:: is private
// (spec L4027: private IP blocking).
func TestTASK2249_IsPrivateIPIPv6ULA(t *testing.T) {
	if !IsPrivateIP(net.ParseIP("fc00::1")) {
		t.Error("fc00::1 should be private (IPv6 ULA)")
	}
}

// TestTASK2249_IsPrivateIPNil verifies nil IP is safe.
func TestTASK2249_IsPrivateIPNil(t *testing.T) {
	if IsPrivateIP(nil) {
		t.Error("nil IP should not be private")
	}
}

// TestTASK2249_NetGuardCheckURLPrivate verifies private URL is blocked
// (spec L4027: SSRF prevention).
func TestTASK2249_NetGuardCheckURLPrivate(t *testing.T) {
	n := NewNetGuard()
	err := n.CheckURL("http://10.0.0.1/admin")
	if err == nil {
		t.Error("private IP URL should be blocked")
	}
}

// TestTASK2249_NetGuardCheckURLPublic verifies public URL is allowed
// (spec L4027: SSRF prevention).
func TestTASK2249_NetGuardCheckURLPublic(t *testing.T) {
	n := NewNetGuard()
	err := n.CheckURL("http://example.com/page")
	if err != nil {
		t.Errorf("public URL should be allowed: %v", err)
	}
}

// TestTASK2249_NetGuardCheckURLLoopback verifies loopback is blocked.
func TestTASK2249_NetGuardCheckURLLoopback(t *testing.T) {
	n := NewNetGuard()
	err := n.CheckURL("http://127.0.0.1:8080/api")
	if err == nil {
		t.Error("loopback URL should be blocked")
	}
}

// TestTASK2249_NetGuardAllow verifies allow list overrides private IP
// (spec L4027: SSRF prevention).
func TestTASK2249_NetGuardAllow(t *testing.T) {
	n := NewNetGuard()
	n.Allow("10.0.0.1")
	err := n.CheckURL("http://10.0.0.1/admin")
	if err != nil {
		t.Errorf("allowed private IP should pass: %v", err)
	}
}

// TestTASK2249_NetGuardBlock verifies explicit block.
func TestTASK2249_NetGuardBlock(t *testing.T) {
	n := NewNetGuard()
	n.Block("evil.com")
	err := n.CheckURL("http://evil.com/page")
	if err == nil {
		t.Error("blocked domain should fail")
	}
}

// TestTASK2249_NetGuardCheckURLInvalid verifies invalid URL errors.
func TestTASK2249_NetGuardCheckURLInvalid(t *testing.T) {
	n := NewNetGuard()
	err := n.CheckURL("://invalid")
	if err == nil {
		t.Error("invalid URL should error")
	}
}

// TestTASK2249_NetGuardIsBlockedIsAllowed verifies convenience methods.
func TestTASK2249_NetGuardIsBlockedIsAllowed(t *testing.T) {
	n := NewNetGuard()
	if !n.IsBlocked("http://10.0.0.1/") {
		t.Error("private IP should be blocked")
	}
	if !n.IsAllowed("http://example.com/") {
		t.Error("public domain should be allowed")
	}
}

// TestTASK2249_NetGuardNilSafe verifies nil guard is safe.
func TestTASK2249_NetGuardNilSafe(t *testing.T) {
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

// TestTASK2249_NewAdBlocker verifies creation
// (spec L4027: ad/tracker blocking 40+ patterns).
func TestTASK2249_NewAdBlocker(t *testing.T) {
	a := NewAdBlocker()
	if a == nil {
		t.Fatal("adblocker should not be nil")
	}
	if !a.IsEnabled() {
		t.Error("adblocker should be enabled by default")
	}
}

// TestTASK2249_AdBlockerPatternCount verifies 40+ patterns
// (spec L4027: 40+ patterns).
func TestTASK2249_AdBlockerPatternCount(t *testing.T) {
	a := NewAdBlocker()
	if a.PatternCount() < 40 {
		t.Errorf("pattern count: got %d, want >= 40", a.PatternCount())
	}
}

// TestTASK2249_AdBlockerCheckAd verifies ad domain is blocked
// (spec L4027: ad/tracker blocking).
func TestTASK2249_AdBlockerCheckAd(t *testing.T) {
	a := NewAdBlocker()
	result := a.Check("ads.doubleclick.net")
	if !result.Blocked {
		t.Error("doubleclick.net should be blocked")
	}
	if result.Category != AdBlockCategoryAd {
		t.Errorf("category: got %s, want ad", result.Category)
	}
}

// TestTASK2249_AdBlockerCheckTracker verifies tracker domain is blocked
// (spec L4027: ad/tracker blocking).
func TestTASK2249_AdBlockerCheckTracker(t *testing.T) {
	a := NewAdBlocker()
	result := a.Check("www.hotjar.com")
	if !result.Blocked {
		t.Error("hotjar should be blocked")
	}
	if result.Category != AdBlockCategoryTracker {
		t.Errorf("category: got %s, want tracker", result.Category)
	}
}

// TestTASK2249_AdBlockerCheckClean verifies clean domain is not blocked
// (spec L4027: ad/tracker blocking).
func TestTASK2249_AdBlockerCheckClean(t *testing.T) {
	a := NewAdBlocker()
	result := a.Check("example.com")
	if result.Blocked {
		t.Error("example.com should NOT be blocked")
	}
}

// TestTASK2249_AdBlockerDisable verifies disable
// (spec L4027).
func TestTASK2249_AdBlockerDisable(t *testing.T) {
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

// TestTASK2249_AdBlockerEnable verifies enable.
func TestTASK2249_AdBlockerEnable(t *testing.T) {
	a := NewAdBlocker()
	a.Disable()
	a.Enable()
	if !a.IsEnabled() {
		t.Error("should be enabled")
	}
}

// TestTASK2249_AdBlockerAddPattern verifies custom pattern
// (spec L4027: ad/tracker blocking).
func TestTASK2249_AdBlockerAddPattern(t *testing.T) {
	a := NewAdBlocker()
	a.AddPattern("evil-ads.com")
	result := a.Check("tracker.evil-ads.com")
	if !result.Blocked {
		t.Error("custom pattern should block")
	}
}

// TestTASK2249_AdBlockerIsBlocked verifies convenience method.
func TestTASK2249_AdBlockerIsBlocked(t *testing.T) {
	a := NewAdBlocker()
	if !a.IsBlocked("ads.doubleclick.net") {
		t.Error("doubleclick should be blocked")
	}
	if a.IsBlocked("example.com") {
		t.Error("example.com should not be blocked")
	}
}

// TestTASK2249_AdBlockerNilSafe verifies nil blocker is safe.
func TestTASK2249_AdBlockerNilSafe(t *testing.T) {
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

// TestTASK2249_NewNavigationPolicy verifies creation with default-deny
// (spec L4027: navigation policy domain allowlist).
func TestTASK2249_NewNavigationPolicy(t *testing.T) {
	p := NewNavigationPolicy()
	if p == nil {
		t.Fatal("policy should not be nil")
	}
	// Default-deny: unlisted domain should be blocked
	if p.IsAllowed("example.com") {
		t.Error("default-deny should block unlisted domain")
	}
}

// TestTASK2249_NewNavigationPolicyDefaultAllow verifies default-allow.
func TestTASK2249_NewNavigationPolicyDefaultAllow(t *testing.T) {
	p := NewNavigationPolicyDefaultAllow()
	if !p.IsAllowed("example.com") {
		t.Error("default-allow should allow unlisted domain")
	}
}

// TestTASK2249_NavigationPolicyAllow verifies allow list
// (spec L4027: navigation policy domain allowlist).
func TestTASK2249_NavigationPolicyAllow(t *testing.T) {
	p := NewNavigationPolicy()
	p.Allow("example.com")
	if !p.IsAllowed("example.com") {
		t.Error("allowed domain should be allowed")
	}
	if p.IsAllowed("evil.com") {
		t.Error("non-allowed domain should be blocked")
	}
}

// TestTASK2249_NavigationPolicyBlock verifies block list
// (spec L4027: navigation policy).
func TestTASK2249_NavigationPolicyBlock(t *testing.T) {
	p := NewNavigationPolicyDefaultAllow()
	p.Block("evil.com")
	if p.IsAllowed("evil.com") {
		t.Error("blocked domain should not be allowed")
	}
	if !p.IsAllowed("example.com") {
		t.Error("non-blocked domain should be allowed")
	}
}

// TestTASK2249_NavigationPolicyBlockOverridesAllow verifies block
// priority over allow (spec L4027: navigation policy).
func TestTASK2249_NavigationPolicyBlockOverridesAllow(t *testing.T) {
	p := NewNavigationPolicy()
	p.Allow("example.com")
	p.Block("example.com")
	if p.IsAllowed("example.com") {
		t.Error("block should override allow")
	}
}

// TestTASK2249_NavigationPolicyCheckURL verifies URL check
// (spec L4027: navigation policy domain allowlist).
func TestTASK2249_NavigationPolicyCheckURL(t *testing.T) {
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

// TestTASK2249_NavigationPolicyCheckURLCaseInsensitive verifies
// case-insensitive domain matching (spec L4027).
func TestTASK2249_NavigationPolicyCheckURLCaseInsensitive(t *testing.T) {
	p := NewNavigationPolicy()
	p.Allow("Example.COM")
	if !p.IsAllowed("example.com") {
		t.Error("case-insensitive: example.com should be allowed")
	}
}

// TestTASK2249_NavigationPolicySetDefaultAllow verifies SetDefaultAllow.
func TestTASK2249_NavigationPolicySetDefaultAllow(t *testing.T) {
	p := NewNavigationPolicy()
	p.SetDefaultAllow(true)
	if !p.IsAllowed("unlisted.com") {
		t.Error("after SetDefaultAllow(true), unlisted should be allowed")
	}
}

// TestTASK2249_NavigationPolicyAllowList verifies AllowList.
func TestTASK2249_NavigationPolicyAllowList(t *testing.T) {
	p := NewNavigationPolicy()
	p.Allow("a.com")
	p.Allow("b.com")
	list := p.AllowList()
	if len(list) != 2 {
		t.Errorf("allow list: got %d, want 2", len(list))
	}
}

// TestTASK2249_NavigationPolicyBlockList verifies BlockList.
func TestTASK2249_NavigationPolicyBlockList(t *testing.T) {
	p := NewNavigationPolicy()
	p.Block("a.com")
	p.Block("b.com")
	list := p.BlockList()
	if len(list) != 2 {
		t.Errorf("block list: got %d, want 2", len(list))
	}
}

// TestTASK2249_NavigationPolicyAllowCount verifies AllowCount.
func TestTASK2249_NavigationPolicyAllowCount(t *testing.T) {
	p := NewNavigationPolicy()
	p.Allow("a.com")
	p.Allow("b.com")
	if p.AllowCount() != 2 {
		t.Errorf("allow count: got %d, want 2", p.AllowCount())
	}
}

// TestTASK2249_NavigationPolicyBlockCount verifies BlockCount.
func TestTASK2249_NavigationPolicyBlockCount(t *testing.T) {
	p := NewNavigationPolicy()
	p.Block("a.com")
	if p.BlockCount() != 1 {
		t.Errorf("block count: got %d, want 1", p.BlockCount())
	}
}

// TestTASK2249_NavigationPolicyNilSafe verifies nil policy is safe.
func TestTASK2249_NavigationPolicyNilSafe(t *testing.T) {
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

// ==================== full spec parity test ====================

// TestTASK2249_FullSpecParity verifies full spec parity for L4027
// (spec L4027: SSRF prevention, ad/tracker blocking, navigation policy).
func TestTASK2249_FullSpecParity(t *testing.T) {
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
