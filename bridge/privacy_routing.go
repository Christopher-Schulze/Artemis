package bridge

import (
	"strings"
	"sync"
)

// privacy_routing.go (spec L4250: privacy routing ss7.7 customer data
// LOCAL inference ONLY for CAPTCHA if URL contains customer data).
//
// Customer URLs go through privacy routing (ss7.7). If a URL contains
// customer data, CAPTCHA solving must use LOCAL inference ONLY (not
// remote/cloud inference) to prevent customer data leakage.

// PrivacyRoutingConfig configures the privacy routing hook (spec L4250).
type PrivacyRoutingConfig struct {
	// Enabled controls whether privacy routing is active.
	Enabled bool `json:"enabled"`
	// CustomerDataPatterns are patterns that indicate customer data in
	// URLs. Matched case-insensitively as substrings.
	CustomerDataPatterns []string `json:"customer_data_patterns"`
	// CustomerDomains are domains known to contain customer data.
	CustomerDomains []string `json:"customer_domains"`
}

// DefaultPrivacyRoutingConfig returns a config with safe defaults
// (spec L4250: default-on, customer data patterns).
func DefaultPrivacyRoutingConfig() PrivacyRoutingConfig {
	return PrivacyRoutingConfig{
		Enabled: true,
		CustomerDataPatterns: []string{
			"customer",
			"account",
			"invoice",
			"billing",
			"payment",
			"ssn",
			"tax",
			"salary",
			"contract",
			"personal",
		},
		CustomerDomains: []string{
			"portal.internal",
			"customer.internal",
		},
	}
}

// PrivacyDecision is the privacy routing decision for a URL.
type PrivacyDecision string

const (
	PrivacyDecisionNormal    PrivacyDecision = "normal"
	PrivacyDecisionLocalOnly PrivacyDecision = "local_only"
)

// PrivacyRoutingResult is the result of a privacy routing check.
type PrivacyRoutingResult struct {
	Decision        PrivacyDecision `json:"decision"`
	HasCustomerData bool            `json:"has_customer_data"`
	Reason          string          `json:"reason"`
}

// PrivacyRoutingHook checks URLs for customer data and routes CAPTCHA
// solving to LOCAL inference only (ss7.7) (spec L4250).
type PrivacyRoutingHook struct {
	mu     sync.RWMutex
	config PrivacyRoutingConfig
	stats  PrivacyRoutingStats
}

// PrivacyRoutingStats tracks privacy routing decisions.
type PrivacyRoutingStats struct {
	Total           int `json:"total"`
	NormalRouted    int `json:"normal_routed"`
	LocalOnlyRouted int `json:"local_only_routed"`
}

// NewPrivacyRoutingHook creates a new privacy routing hook.
func NewPrivacyRoutingHook(config PrivacyRoutingConfig) *PrivacyRoutingHook {
	return &PrivacyRoutingHook{config: config}
}

// Config returns the current config.
func (h *PrivacyRoutingHook) Config() PrivacyRoutingConfig {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.config
}

// SetConfig updates the config.
func (h *PrivacyRoutingHook) SetConfig(config PrivacyRoutingConfig) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.config = config
}

// Stats returns the current statistics.
func (h *PrivacyRoutingHook) Stats() PrivacyRoutingStats {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.stats
}

// ResetStats resets statistics (for testing).
func (h *PrivacyRoutingHook) ResetStats() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.stats = PrivacyRoutingStats{}
}

// CheckURL evaluates a URL for customer data and returns the privacy
// routing decision (spec L4250).
func (h *PrivacyRoutingHook) CheckURL(url string) PrivacyRoutingResult {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.stats.Total++

	if !h.config.Enabled {
		h.stats.NormalRouted++
		return PrivacyRoutingResult{
			Decision: PrivacyDecisionNormal,
			Reason:   "privacy routing disabled",
		}
	}

	lowerURL := strings.ToLower(url)

	// Check for customer data patterns in URL.
	for _, pattern := range h.config.CustomerDataPatterns {
		if strings.Contains(lowerURL, strings.ToLower(pattern)) {
			h.stats.LocalOnlyRouted++
			return PrivacyRoutingResult{
				Decision:        PrivacyDecisionLocalOnly,
				HasCustomerData: true,
				Reason:          "URL contains customer data pattern: " + pattern,
			}
		}
	}

	// Check for customer domains.
	for _, domain := range h.config.CustomerDomains {
		if strings.Contains(lowerURL, strings.ToLower(domain)) {
			h.stats.LocalOnlyRouted++
			return PrivacyRoutingResult{
				Decision:        PrivacyDecisionLocalOnly,
				HasCustomerData: true,
				Reason:          "URL is on customer domain: " + domain,
			}
		}
	}

	h.stats.NormalRouted++
	return PrivacyRoutingResult{
		Decision: PrivacyDecisionNormal,
		Reason:   "no customer data detected",
	}
}

// ShouldUseLocalOnly is a convenience method that returns true if the
// URL requires LOCAL inference only (spec L4250).
func (h *PrivacyRoutingHook) ShouldUseLocalOnly(url string) bool {
	result := h.CheckURL(url)
	return result.Decision == PrivacyDecisionLocalOnly
}
