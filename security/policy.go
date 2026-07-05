package security

import (
	"fmt"
	"strings"
	"sync"
)

// policy.go (spec L4027: security/policy.go - navigation policy:
// domain allowlist).
//
// Browser security: navigation policy with domain allowlist to
// restrict which domains the browser can navigate to. This prevents
// the browser from navigating to unauthorized domains during
// automation.

// NavigationPolicy manages the domain allowlist for navigation
// (spec L4027: navigation policy domain allowlist).
type NavigationPolicy struct {
	mu           sync.RWMutex
	allowList    map[string]bool // explicitly allowed domains
	blockList    map[string]bool // explicitly blocked domains
	defaultAllow bool            // default policy when domain not in either list
}

// NewNavigationPolicy creates a new NavigationPolicy with default-deny
// (spec L4027: navigation policy domain allowlist).
func NewNavigationPolicy() *NavigationPolicy {
	return &NavigationPolicy{
		allowList:    make(map[string]bool),
		blockList:    make(map[string]bool),
		defaultAllow: false, // default-deny
	}
}

// NewNavigationPolicyDefaultAllow creates a new NavigationPolicy with
// default-allow (spec L4027).
func NewNavigationPolicyDefaultAllow() *NavigationPolicy {
	return &NavigationPolicy{
		allowList:    make(map[string]bool),
		blockList:    make(map[string]bool),
		defaultAllow: true, // default-allow
	}
}

// Allow adds a domain to the allowlist
// (spec L4027: navigation policy domain allowlist).
func (p *NavigationPolicy) Allow(domain string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.allowList[strings.ToLower(domain)] = true
}

// Block adds a domain to the blocklist
// (spec L4027: navigation policy).
func (p *NavigationPolicy) Block(domain string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.blockList[strings.ToLower(domain)] = true
}

// IsAllowed reports whether navigation to a domain is allowed
// (spec L4027: navigation policy domain allowlist).
func (p *NavigationPolicy) IsAllowed(domain string) bool {
	if p == nil {
		return false
	}
	domain = strings.ToLower(domain)
	p.mu.RLock()
	defer p.mu.RUnlock()
	// Blocklist takes priority
	if p.blockList[domain] {
		return false
	}
	// Allowlist
	if p.allowList[domain] {
		return true
	}
	// Default policy
	return p.defaultAllow
}

// IsBlocked reports whether navigation to a domain is blocked
// (spec L4027: navigation policy).
func (p *NavigationPolicy) IsBlocked(domain string) bool {
	return !p.IsAllowed(domain)
}

// CheckURL checks whether navigation to a URL is allowed
// (spec L4027: navigation policy domain allowlist).
func (p *NavigationPolicy) CheckURL(rawURL string) error {
	if p == nil {
		return fmt.Errorf("policy: nil policy")
	}
	domain := extractDomain(rawURL)
	if domain == "" {
		return fmt.Errorf("policy: cannot extract domain from %q", rawURL)
	}
	if !p.IsAllowed(domain) {
		return fmt.Errorf("policy: domain %q not in allowlist", domain)
	}
	return nil
}

// extractDomain extracts the domain from a URL string
// (spec L4027: navigation policy).
func extractDomain(rawURL string) string {
	// Remove protocol
	if idx := strings.Index(rawURL, "://"); idx >= 0 {
		rawURL = rawURL[idx+3:]
	}
	// Remove path
	if idx := strings.Index(rawURL, "/"); idx >= 0 {
		rawURL = rawURL[:idx]
	}
	// Remove port
	if idx := strings.Index(rawURL, ":"); idx >= 0 {
		rawURL = rawURL[:idx]
	}
	return strings.ToLower(rawURL)
}

// SetDefaultAllow sets the default policy for unlisted domains
// (spec L4027: navigation policy).
func (p *NavigationPolicy) SetDefaultAllow(allow bool) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.defaultAllow = allow
}

// AllowList returns the list of allowed domains
// (spec L4027).
func (p *NavigationPolicy) AllowList() []string {
	if p == nil {
		return nil
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	domains := make([]string, 0, len(p.allowList))
	for d := range p.allowList {
		domains = append(domains, d)
	}
	return domains
}

// BlockList returns the list of blocked domains
// (spec L4027).
func (p *NavigationPolicy) BlockList() []string {
	if p == nil {
		return nil
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	domains := make([]string, 0, len(p.blockList))
	for d := range p.blockList {
		domains = append(domains, d)
	}
	return domains
}

// AllowCount returns the number of allowed domains
// (spec L4027).
func (p *NavigationPolicy) AllowCount() int {
	if p == nil {
		return 0
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.allowList)
}

// BlockCount returns the number of blocked domains
// (spec L4027).
func (p *NavigationPolicy) BlockCount() int {
	if p == nil {
		return 0
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.blockList)
}

// String returns a diagnostic summary.
func (p *NavigationPolicy) String() string {
	if p == nil {
		return "NavigationPolicy(nil)"
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return fmt.Sprintf("NavigationPolicy{allow:%d block:%d defaultAllow:%v}",
		len(p.allowList), len(p.blockList), p.defaultAllow)
}
