package bridge

import "github.com/Christopher-Schulze/Artemis/policy"

// Compatibility names retain the existing API while policy owns all decisions.
// policy has no browser/scraper dependency and serves their consumers directly.
type PolicyDecision = policy.PolicyDecision
type PolicyRequest = policy.PolicyRequest
type PolicyResponse = policy.PolicyResponse
type PolicyEngine = policy.PolicyEngine
type PolicyError = policy.PolicyError
type PolicyHook = policy.PolicyHook
type PolicyHookStats = policy.PolicyHookStats
type PolicyFileAction = policy.PolicyFileAction
type PolicyRule = policy.PolicyRule
type PolicyFile = policy.PolicyFile
type NavigationPolicyConfig = policy.NavigationPolicyConfig
type NavigationPolicyStats = policy.NavigationPolicyStats
type NavigationPolicy = policy.NavigationPolicy

const (
	PolicyDecisionAllow     = policy.PolicyDecisionAllow
	PolicyDecisionDeny      = policy.PolicyDecisionDeny
	PolicyDecisionChallenge = policy.PolicyDecisionChallenge
	PolicyFileAllow         = policy.PolicyFileAllow
	PolicyFileDeny          = policy.PolicyFileDeny
	PolicyFileChallenge     = policy.PolicyFileChallenge
	PolicyFileVersion       = policy.PolicyFileVersion
	DefaultReloadInterval   = policy.DefaultReloadInterval
)

var ErrNavigationPolicyStopped = policy.ErrNavigationPolicyStopped

func NewNavigationPolicy(config NavigationPolicyConfig) (*NavigationPolicy, error) {
	return policy.NewNavigationPolicy(config)
}

func NewStaticNavigationPolicy(data []byte) (*NavigationPolicy, error) {
	return policy.NewStaticNavigationPolicy(data)
}

func NewPolicyHook(engine PolicyEngine) *PolicyHook { return policy.NewPolicyHook(engine) }

func ParsePolicyURL(raw string) (string, error) { return policy.ParsePolicyURL(raw) }

func ParseConfirmActions(raw string) ([]string, error) { return policy.ParseConfirmActions(raw) }

func WriteDefaultPolicyFile(path string) error { return policy.WriteDefaultPolicyFile(path) }
