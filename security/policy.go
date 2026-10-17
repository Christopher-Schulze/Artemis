package security

import (
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strings"
	"sync"

	browserpolicy "github.com/Christopher-Schulze/Artemis/policy"
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
	policy       *browserpolicy.NavigationPolicy
	lastError    error
}

// NewNavigationPolicy creates a new NavigationPolicy with default-deny
// (spec L4027: navigation policy domain allowlist).
func NewNavigationPolicy() *NavigationPolicy {
	policy := &NavigationPolicy{
		allowList:    make(map[string]bool),
		blockList:    make(map[string]bool),
		defaultAllow: false, // default-deny
	}
	policy.rebuild()
	return policy
}

// NewNavigationPolicyDefaultAllow creates a new NavigationPolicy with
// default-allow (spec L4027).
func NewNavigationPolicyDefaultAllow() *NavigationPolicy {
	policy := &NavigationPolicy{
		allowList:    make(map[string]bool),
		blockList:    make(map[string]bool),
		defaultAllow: true, // default-allow
	}
	policy.rebuild()
	return policy
}

// Allow adds a domain to the allowlist
// (spec L4027: navigation policy domain allowlist).
func (p *NavigationPolicy) Allow(domain string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.setDomain(domain, false)
}

// Block adds a domain to the blocklist
// (spec L4027: navigation policy).
func (p *NavigationPolicy) Block(domain string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.setDomain(domain, true)
}

// IsAllowed reports whether navigation to a domain is allowed
// (spec L4027: navigation policy domain allowlist).
func (p *NavigationPolicy) IsAllowed(domain string) bool {
	if p == nil {
		return false
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	host, err := policyDomainHost(domain)
	return err == nil && p.policy != nil && p.policy.ValidateURL("https://"+host+"/") == browserpolicy.PolicyDecisionAllow
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
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.policy == nil || p.policy.ValidateURL(rawURL) != browserpolicy.PolicyDecisionAllow {
		return fmt.Errorf("policy: URL denied by canonical navigation policy")
	}
	return nil
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
	p.rebuild()
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
	sort.Strings(domains)
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
	sort.Strings(domains)
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

// LastError exposes invalid configuration instead of silently permitting it.
func (p *NavigationPolicy) LastError() error {
	if p == nil {
		return fmt.Errorf("policy: nil policy")
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.lastError
}

func policyDomainHost(domain string) (string, error) {
	domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	host := domain
	if address := net.ParseIP(domain); address != nil && address.To4() == nil {
		host = "[" + domain + "]"
	}
	parsed, err := browserpolicy.ParsePolicyURL("https://" + host + "/")
	if err != nil || parsed != domain {
		return "", fmt.Errorf("policy: valid bare domain required")
	}
	return host, nil
}

// Caller holds the adapter lock; maps are configuration, never decision logic.
func (p *NavigationPolicy) setDomain(domain string, blocked bool) {
	host, err := policyDomainHost(domain)
	if err != nil {
		p.policy, p.lastError = nil, err
		return
	}
	domain = strings.Trim(host, "[]")
	if len(p.allowList)+len(p.blockList) >= 256 && !p.allowList[domain] && !p.blockList[domain] {
		p.policy, p.lastError = nil, fmt.Errorf("policy: domain rule budget exceeded")
		return
	}
	if p.allowList == nil {
		p.allowList = make(map[string]bool)
	}
	if p.blockList == nil {
		p.blockList = make(map[string]bool)
	}
	if blocked {
		p.blockList[domain] = true
	} else {
		p.allowList[domain] = true
	}
	p.rebuild()
}

func (p *NavigationPolicy) rebuild() {
	file := browserpolicy.PolicyFile{Version: browserpolicy.PolicyFileVersion, Default: browserpolicy.PolicyFileDeny, Rules: []browserpolicy.PolicyRule{}}
	if p.defaultAllow {
		file.Default = browserpolicy.PolicyFileAllow
	}
	for _, group := range []struct {
		values map[string]bool
		action browserpolicy.PolicyFileAction
	}{{p.blockList, browserpolicy.PolicyFileDeny}, {p.allowList, browserpolicy.PolicyFileAllow}} {
		domains := make([]string, 0, len(group.values))
		for domain := range group.values {
			domains = append(domains, domain)
		}
		sort.Strings(domains)
		for _, domain := range domains {
			host, err := policyDomainHost(domain)
			if err != nil {
				p.policy, p.lastError = nil, err
				return
			}
			pattern := `(?i)https?://` + regexp.QuoteMeta(host) + `\.?(?::[0-9]+)?(?:[/?#].*)?`
			file.Rules = append(file.Rules, browserpolicy.PolicyRule{Action: group.action, Pattern: pattern})
		}
	}
	data, err := json.Marshal(file)
	if err != nil {
		p.policy, p.lastError = nil, err
		return
	}
	p.policy, p.lastError = browserpolicy.NewStaticNavigationPolicy(data)
}
