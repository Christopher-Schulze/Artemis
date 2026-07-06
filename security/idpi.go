package security

import (
	"fmt"
	"strings"
	"sync"
)

// idpi.go (spec L4202: IDPI - Indirect Prompt Injection Defense).
//
// 3-layer defense against indirect prompt injection from untrusted web
// content:
//   Layer 1: domain whitelisting (blocklist + allowlist)
//   Layer 2: content scanning for injection phrases
//   Layer 3: content wrapping with <untrusted_web_content> delimiter
//
// CheckResult {Threat, Blocked, TaintRefs, Reason} feeds
// AttachmentTrust ss49.2a, ToolDispatchPlan, DecisionTrace and
// browser idpi metrics {scanned_total,tainted_total,blocked_total,
// policy_downgraded_total} metrics.
//
// Default action is taint+fence (not block) unless policy/tool
// authority would consume untrusted text as instructions.

// IDPIConfig configures the 3-layer IDPI defense (spec L4202).
type IDPIConfig struct {
	// Enabled controls whether IDPI checking is active.
	Enabled bool `json:"enabled"`
	// BlocklistDomains are domains that are always blocked (Layer 1).
	BlocklistDomains []string `json:"blocklist_domains"`
	// AllowlistDomains are domains that are trusted (Layer 1).
	// Authenticated private/LAN connector domains may policy-downgrade
	// to trust-label-only after audited allowlist.
	AllowlistDomains []string `json:"allowlist_domains"`
	// InjectionPhrases are patterns that indicate prompt injection
	// (Layer 2). Matched case-insensitively as substrings.
	InjectionPhrases []string `json:"injection_phrases"`
	// WrapContent controls whether content is wrapped with
	// <untrusted_web_content> delimiter (Layer 3).
	WrapContent bool `json:"wrap_content"`
	// BlockOnInjection controls whether injection detection blocks
	// (true) or taints+fences (false). Default is false (taint+fence)
	// per spec L4202.
	BlockOnInjection bool `json:"block_on_injection"`
	// PolicyDowngradeAllowlist enables policy-downgrade to trust-label
	// for allowlisted domains (instead of full trust).
	PolicyDowngradeAllowlist bool `json:"policy_downgrade_allowlist"`
}

// DefaultIDPIConfig returns a config with safe defaults (spec L4202:
// default-on, taint+fence not block, content wrapping enabled).
func DefaultIDPIConfig() IDPIConfig {
	return IDPIConfig{
		Enabled: true,
		BlocklistDomains: []string{
			"evil.example.com",
			"malicious.test",
		},
		AllowlistDomains: []string{
			"trusted.internal",
			"lan.local",
		},
		InjectionPhrases: []string{
			"ignore previous instructions",
			"disregard the above",
			"you are now",
			"new instructions:",
			"system prompt:",
			"forget your rules",
			"override your directives",
			"act as",
			"pretend you are",
			"jailbreak",
			"</untrusted_web_content>",
			"<untrusted_web_content>",
		},
		WrapContent:              true,
		BlockOnInjection:         false,
		PolicyDowngradeAllowlist: true,
	}
}

// IDPIThreat describes the type of threat detected.
type IDPIThreat string

const (
	IDPIThreatNone              IDPIThreat = "none"
	IDPIThreatBlocklistedDomain IDPIThreat = "blocklisted_domain"
	IDPIThreatInjectionPhrase   IDPIThreat = "injection_phrase"
	IDPIThreatUntrustedContent  IDPIThreat = "untrusted_content"
)

// CheckResult is the result of an IDPI check (spec L4202:
// {Threat, Blocked, TaintRefs, Reason}).
type CheckResult struct {
	Threat    IDPIThreat `json:"threat"`
	Blocked   bool       `json:"blocked"`
	TaintRefs []string   `json:"taint_refs"`
	Reason    string     `json:"reason"`
	// WrappedContent is the content wrapped with
	// <untrusted_web_content> delimiter (Layer 3, if WrapContent is
	// enabled).
	WrappedContent string `json:"wrapped_content,omitempty"`
	// TrustLabel is set when policy-downgrade is applied to an
	// allowlisted domain.
	TrustLabel string `json:"trust_label,omitempty"`
}

// IDPIMetrics tracks the 4 spec-mandated metrics (spec L4202:
// browser idpi metrics {scanned_total,tainted_total,blocked_total,
// policy_downgraded_total}).
type IDPIMetrics struct {
	ScannedTotal          int `json:"scanned_total"`
	TaintedTotal          int `json:"tainted_total"`
	BlockedTotal          int `json:"blocked_total"`
	PolicyDowngradedTotal int `json:"policy_downgraded_total"`
}

// IDPIChecker implements the 3-layer IDPI defense (spec L4202).
type IDPIChecker struct {
	mu      sync.RWMutex
	config  IDPIConfig
	metrics IDPIMetrics
}

// NewIDPIChecker creates a new checker with the given config.
func NewIDPIChecker(config IDPIConfig) *IDPIChecker {
	return &IDPIChecker{config: config}
}

// Config returns the current config.
func (c *IDPIChecker) Config() IDPIConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.config
}

// SetConfig updates the config.
func (c *IDPIChecker) SetConfig(config IDPIConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config = config
}

// Metrics returns the current metrics counters.
func (c *IDPIChecker) Metrics() IDPIMetrics {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.metrics
}

// ResetMetrics resets all metrics counters (for testing).
func (c *IDPIChecker) ResetMetrics() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.metrics = IDPIMetrics{}
}

// Check evaluates content from a domain through the 3-layer IDPI
// defense (spec L4202).
//
// Layer 1: domain whitelisting (blocklist -> block, allowlist -> trust)
// Layer 2: content scanning for injection phrases
// Layer 3: content wrapping with <untrusted_web_content> delimiter
func (c *IDPIChecker) Check(domain, content string) CheckResult {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.metrics.ScannedTotal++

	if !c.config.Enabled {
		return CheckResult{
			Threat:  IDPIThreatNone,
			Blocked: false,
			Reason:  "IDPI disabled",
		}
	}

	domain = strings.ToLower(strings.TrimSpace(domain))

	// Layer 1: domain whitelisting.
	if isDomainInList(domain, c.config.BlocklistDomains) {
		c.metrics.BlockedTotal++
		return CheckResult{
			Threat:  IDPIThreatBlocklistedDomain,
			Blocked: true,
			Reason:  fmt.Sprintf("domain %s is blocklisted", domain),
		}
	}

	result := CheckResult{
		Threat:  IDPIThreatNone,
		Blocked: false,
	}

	// Layer 1: allowlist with optional policy-downgrade.
	if isDomainInList(domain, c.config.AllowlistDomains) {
		if c.config.PolicyDowngradeAllowlist {
			c.metrics.PolicyDowngradedTotal++
			result.TrustLabel = "policy_downgraded"
			result.Reason = fmt.Sprintf("domain %s allowlisted with policy-downgrade to trust-label", domain)
		} else {
			result.Reason = fmt.Sprintf("domain %s fully trusted", domain)
		}
		// Still scan content even for allowlisted domains (defense in depth).
	}

	// Layer 2: content scanning for injection phrases.
	taintRefs := scanForInjection(content, c.config.InjectionPhrases)
	if len(taintRefs) > 0 {
		c.metrics.TaintedTotal++
		result.Threat = IDPIThreatInjectionPhrase
		result.TaintRefs = taintRefs
		if result.Reason == "" {
			result.Reason = fmt.Sprintf("injection phrases detected: %s", strings.Join(taintRefs, ", "))
		} else {
			result.Reason += fmt.Sprintf("; injection phrases detected: %s", strings.Join(taintRefs, ", "))
		}
		if c.config.BlockOnInjection {
			c.metrics.BlockedTotal++
			result.Blocked = true
		}
	}

	// Layer 3: content wrapping with <untrusted_web_content> delimiter.
	if c.config.WrapContent {
		result.WrappedContent = wrapUntrusted(content)
		if result.Threat == IDPIThreatNone {
			result.Threat = IDPIThreatUntrustedContent
		}
	}

	return result
}

// isDomainInList checks if a domain is in a list (case-insensitive).
func isDomainInList(domain string, list []string) bool {
	for _, d := range list {
		if strings.EqualFold(domain, strings.TrimSpace(d)) {
			return true
		}
	}
	return false
}

// scanForInjection scans content for injection phrases and returns
// the matched phrases as taint references (spec L4202: Layer 2).
func scanForInjection(content string, phrases []string) []string {
	lower := strings.ToLower(content)
	var refs []string
	for _, phrase := range phrases {
		if strings.Contains(lower, strings.ToLower(phrase)) {
			refs = append(refs, phrase)
		}
	}
	return refs
}

// wrapUntrusted wraps content with <untrusted_web_content> delimiter
// (spec L4202: Layer 3 content wrapping for LLM).
func wrapUntrusted(content string) string {
	return "<untrusted_web_content>\n" + content + "\n</untrusted_web_content>"
}
