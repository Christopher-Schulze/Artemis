package policy

import (
	"encoding/json"
	"errors"
	"strings"

	"golang.org/x/net/idna"
)

const maxPolicyCategoryBytes = 64

// NewStaticNavigationPolicy validates already admitted bytes without file IO.
// The embedding caller retains ownership of ingress and snapshot replacement.
func NewStaticNavigationPolicy(data []byte) (*NavigationPolicy, error) {
	policy, err := decodeNavigationPolicy(data)
	if err != nil {
		return nil, err
	}
	return &NavigationPolicy{cfg: NavigationPolicyConfig{DisabledDefault: PolicyFileDeny, ReloadInterval: DefaultReloadInterval},
		policy: policy, initialized: true, stopCh: make(chan struct{}), doneCh: make(chan struct{})}, nil
}

// MarshalJSON preserves omitted versus explicitly empty lists and URL rules.
// Their presence distinguishes URL-only rules from action-policy documents.
func (p PolicyFile) MarshalJSON() ([]byte, error) {
	var rules *[]PolicyRule
	var allow, deny, confirm, domains *[]string
	if p.Rules != nil {
		rules = &p.Rules
	}
	if p.Allow != nil {
		allow = &p.Allow
	}
	if p.Deny != nil {
		deny = &p.Deny
	}
	if p.Confirm != nil {
		confirm = &p.Confirm
	}
	if p.DomainAllowlist != nil {
		domains = &p.DomainAllowlist
	}
	return json.Marshal(struct {
		Version         int              `json:"version"`
		Default         PolicyFileAction `json:"default"`
		Rules           *[]PolicyRule    `json:"rules,omitempty"`
		Allow           *[]string        `json:"allow,omitempty"`
		Deny            *[]string        `json:"deny,omitempty"`
		Confirm         *[]string        `json:"confirm,omitempty"`
		DomainAllowlist *[]string        `json:"domain_allowlist,omitempty"`
	}{p.Version, p.Default, rules, allow, deny, confirm, domains})
}

func compilePolicyCategories(policy *PolicyFile) error {
	lists := []struct {
		values *[]string
		index  *map[string]struct{}
	}{
		{&policy.Allow, &policy.actionAllow}, {&policy.Deny, &policy.actionDeny}, {&policy.Confirm, &policy.actionConfirm},
	}
	for _, list := range lists {
		values, index, err := normalizePolicyCategories(*list.values)
		if err != nil {
			return err
		}
		*list.values, *list.index = values, index
	}
	if len(policy.DomainAllowlist) > maxNavigationPolicyRules {
		return errors.New("navigation policy: too many domains")
	}
	policy.domainAllow = make(map[string]struct{}, len(policy.DomainAllowlist))
	for i, domain := range policy.DomainAllowlist {
		domain, err := normalizePolicyDomain(domain)
		if err != nil {
			return err
		}
		policy.DomainAllowlist[i] = domain
		policy.domainAllow[domain] = struct{}{}
	}
	return nil
}

func normalizePolicyCategories(values []string) ([]string, map[string]struct{}, error) {
	if len(values) > maxNavigationPolicyRules {
		return nil, nil, errors.New("navigation policy: too many categories")
	}
	if values == nil {
		return nil, nil, nil
	}
	result := make([]string, 0, len(values))
	index := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if !validPolicyCategory(value) {
			return nil, nil, errors.New("navigation policy: invalid category")
		}
		if _, found := index[value]; !found {
			result = append(result, value)
		}
		index[value] = struct{}{}
	}
	return result, index, nil
}

func validPolicyCategory(value string) bool {
	if len(value) == 0 || len(value) > maxPolicyCategoryBytes || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '_' {
			return false
		}
	}
	return true
}

// ParseConfirmActions validates the reference comma-separated environment value.
func ParseConfirmActions(raw string) ([]string, error) {
	if len(raw) > maxNavigationPolicyRules*(maxPolicyCategoryBytes+1) {
		return nil, errors.New("navigation policy: confirm environment too large")
	}
	var values []string
	for _, value := range strings.Split(raw, ",") {
		if strings.TrimSpace(value) != "" {
			values = append(values, value)
		}
	}
	result, _, err := normalizePolicyCategories(values)
	return result, err
}

func normalizePolicyDomain(raw string) (string, error) {
	domain := strings.ToLower(strings.TrimSpace(raw))
	if domain == "" || len(domain) > 253 || strings.ContainsAny(domain, "/:@?#[]%\\ ") || strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return "", errors.New("navigation policy: invalid domain")
	}
	ascii, err := idna.Lookup.ToASCII(domain)
	if err != nil || len(ascii) > 253 {
		return "", errors.New("navigation policy: invalid domain")
	}
	for _, label := range strings.Split(ascii, ".") {
		if label == "" || len(label) > 63 {
			return "", errors.New("navigation policy: invalid domain label")
		}
	}
	return strings.ToLower(ascii), nil
}

// EvaluateAction reports category precedence only; it cannot authorize a target.
// Execution authorization uses Evaluate with the actual request URL.
func (p *NavigationPolicy) EvaluateAction(action string) PolicyResponse {
	if p == nil || p.stopped.Load() {
		return PolicyResponse{Decision: PolicyDecisionDeny, Reason: "policy unavailable"}
	}
	p.mu.RLock()
	policy := p.policy
	p.mu.RUnlock()
	if policy == nil {
		return PolicyResponse{Decision: PolicyDecisionDeny, Reason: "policy unavailable"}
	}
	decision, reason := evaluatePolicyCategory(policy, action)
	return PolicyResponse{Decision: toDecision(decision), Reason: reason}
}

func evaluatePolicyCategory(policy *PolicyFile, action string) (PolicyFileAction, string) {
	action = strings.ToLower(strings.TrimSpace(action))
	if !validPolicyCategory(action) {
		return PolicyFileDeny, "invalid action category"
	}
	if !policy.actionConfigured {
		if action == "navigate" {
			return policy.Default, "navigation category only"
		}
		return PolicyFileDeny, "action policy not configured"
	}
	if _, found := policy.actionDeny[action]; found {
		return PolicyFileDeny, "action denied"
	}
	if _, found := policy.actionConfirm[action]; found {
		return PolicyFileChallenge, "action requires confirmation"
	}
	if _, found := policy.actionAllow[action]; found {
		return PolicyFileAllow, "action allowed"
	}
	return policy.Default, "default action decision"
}

// EvaluateDomain reports the configured domain constraint, without action permission.
func (p *NavigationPolicy) EvaluateDomain(domain string) PolicyResponse {
	if p == nil || p.stopped.Load() {
		return PolicyResponse{Decision: PolicyDecisionDeny, Reason: "policy unavailable"}
	}
	p.mu.RLock()
	policy := p.policy
	p.mu.RUnlock()
	if policy == nil || !policyDomainAllowed(policy, domain) {
		return PolicyResponse{Decision: PolicyDecisionDeny, Reason: "domain denied"}
	}
	return PolicyResponse{Decision: PolicyDecisionAllow, Reason: "domain constraint satisfied"}
}

func policyDomainAllowed(policy *PolicyFile, domain string) bool {
	if len(policy.domainAllow) == 0 {
		return true
	}
	domain, err := normalizePolicyDomain(domain)
	if err != nil {
		return false
	}
	for {
		if _, found := policy.domainAllow[domain]; found {
			return true
		}
		dot := strings.IndexByte(domain, '.')
		if dot < 0 {
			return false
		}
		domain = domain[dot+1:]
	}
}

func evaluatePolicyRequest(policy *PolicyFile, request PolicyRequest) (PolicyFileAction, string) {
	if len(request.TargetURLs) > 3 {
		return PolicyFileDeny, "too many policy targets"
	}
	action := strings.ToLower(strings.TrimSpace(request.Action))
	if action == "" {
		action = "navigate"
	}
	category, reason := evaluateRequestCategory(policy, action)
	if category == PolicyFileDeny {
		return category, reason
	}
	for _, target := range append([]string{request.URL}, request.TargetURLs...) {
		decision, targetReason := evaluatePolicyURL(policy, target)
		if decision == PolicyFileDeny {
			return decision, targetReason
		}
		if decision == PolicyFileChallenge {
			category, reason = decision, targetReason
		}
	}
	return category, reason
}

func evaluateRequestCategory(policy *PolicyFile, action string) (PolicyFileAction, string) {
	if !policy.actionConfigured {
		if action != "navigate" {
			return PolicyFileDeny, "action policy not configured"
		}
		return PolicyFileAllow, "URL navigation"
	}
	category, reason := evaluatePolicyCategory(policy, action)
	if category == PolicyFileDeny {
		return category, reason
	}
	if action == "assert" || action == "frame_evaluate" {
		evaluation, evaluationReason := evaluatePolicyCategory(policy, "evaluate")
		if evaluation == PolicyFileDeny {
			return evaluation, evaluationReason
		}
		if evaluation == PolicyFileChallenge {
			category, reason = evaluation, evaluationReason
		}
	}
	return category, reason
}

func evaluatePolicyURL(policy *PolicyFile, target string) (PolicyFileAction, string) {
	host, err := ParsePolicyURL(target)
	if err != nil {
		return PolicyFileDeny, "invalid URL"
	}
	if !policyDomainAllowed(policy, host) {
		return PolicyFileDeny, "domain denied"
	}
	if policy.actionConfigured && policy.Rules == nil {
		return PolicyFileAllow, "domain constraint satisfied"
	}
	urlDecision := policy.Default
	for i := range policy.Rules {
		if policy.Rules[i].matchRule(target) {
			urlDecision = policy.Rules[i].Action
			break
		}
	}
	return urlDecision, "URL rule decision"
}
