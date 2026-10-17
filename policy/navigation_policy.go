package policy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

// navigation_policy.go (spec L4259: JSON navigation policy file + hot-reload).
//
// A NavigationPolicyFile is a JSON document that defines allow/deny rules
// for browser navigation. The policy is loaded from a JSON file on disk and
// hot-reloaded when its bounded content changes. The policy implements the
// PolicyEngine interface so it can be wired into the existing PolicyHook
// (spec L4247) without a separate integration path.
//
// Reference: research/webstack/agent-browser-main/cli/src/native/policy.rs
//
// JSON schema:
//
//	{
//	  "version": 1,
//	  "default": "deny",
//	  "rules": [
//	    {"action": "deny",    "pattern": "https://internal.corp.local/.*"},
//	    {"action": "challenge","pattern": "https://admin.example.com/.*"},
//	    {"action": "allow",   "pattern": "https://[a-z]+\\.example\\.com/.*"}
//	  ]
//	}
//
// Rules are evaluated in order; the first match wins. If no rule matches,
// the `default` action applies. Patterns are matched against the full URL
// using anchored Go regexp syntax. Literal patterns match the same URL
// authority and a path prefix; they cannot match a lookalike hostname.

// PolicyFileVersion is the supported schema version.
const PolicyFileVersion = 1

// PolicyFileAction is the action a rule prescribes.
type PolicyFileAction string

const (
	PolicyFileAllow     PolicyFileAction = "allow"
	PolicyFileDeny      PolicyFileAction = "deny"
	PolicyFileChallenge PolicyFileAction = "challenge"
)

// PolicyRule is a single allow/deny/challenge rule.
type PolicyRule struct {
	Action  PolicyFileAction `json:"action"`
	Pattern string           `json:"pattern"`
	// compiled is the precompiled regex; nil for literal-prefix rules.
	compiled *regexp.Regexp
	// literal is true when the pattern has no regex metacharacters.
	literal bool
}

// PolicyFile is the parsed JSON policy document.
type PolicyFile struct {
	Version          int              `json:"version"`
	Default          PolicyFileAction `json:"default"`
	Rules            []PolicyRule     `json:"rules"`
	Allow            []string         `json:"allow,omitempty"`
	Deny             []string         `json:"deny,omitempty"`
	Confirm          []string         `json:"confirm,omitempty"`
	DomainAllowlist  []string         `json:"domain_allowlist,omitempty"`
	actionAllow      map[string]struct{}
	actionDeny       map[string]struct{}
	actionConfirm    map[string]struct{}
	domainAllow      map[string]struct{}
	actionConfigured bool
}

// NavigationPolicyConfig controls the file-backed navigation policy
// (spec L4259).
type NavigationPolicyConfig struct {
	// Path is the JSON policy file path. Required.
	Path string
	// ReloadInterval is the content-poll interval for hot-reload. Default 5s.
	ReloadInterval time.Duration
	// DisabledDefault must be empty or PolicyFileDeny (fail-closed).
	DisabledDefault PolicyFileAction
}

// DefaultReloadInterval is the canonical hot-reload poll interval.
const DefaultReloadInterval = 5 * time.Second

// NavigationPolicyStats tracks policy evaluation counters.
type NavigationPolicyStats struct {
	Total      atomic.Int64
	Allowed    atomic.Int64
	Denied     atomic.Int64
	Challenged atomic.Int64
	Reloads    atomic.Int64
	Errors     atomic.Int64
}

// NavigationPolicy is a file-backed navigation policy with hot-reload
// (spec L4259). It implements PolicyEngine so it can be wired into
// PolicyHook directly. It is safe for concurrent use.
type NavigationPolicy struct {
	mu           sync.RWMutex
	cfg          NavigationPolicyConfig
	policy       *PolicyFile
	stats        NavigationPolicyStats
	reloadMu     sync.Mutex
	lastAttempt  string
	lastError    error
	activeDigest string
	initialized  bool
	started      bool
	stopCh       chan struct{}
	doneCh       chan struct{}
	stopped      atomic.Bool
}

// NewNavigationPolicy creates a policy from the given config. The initial
// load happens synchronously; if the file is missing the policy falls back
// to DisabledDefault (fail-closed). Call StartHotReload to begin polling.
func NewNavigationPolicy(cfg NavigationPolicyConfig) (*NavigationPolicy, error) {
	if cfg.Path == "" {
		return nil, errors.New("navigation policy: empty path")
	}
	if cfg.ReloadInterval <= 0 {
		if cfg.ReloadInterval < 0 {
			return nil, errors.New("navigation policy: negative reload interval")
		}
		cfg.ReloadInterval = DefaultReloadInterval
	}
	if cfg.ReloadInterval < time.Millisecond {
		return nil, errors.New("navigation policy: reload interval must be at least 1ms")
	}
	if cfg.DisabledDefault == "" {
		cfg.DisabledDefault = PolicyFileDeny
	}
	if cfg.DisabledDefault != PolicyFileDeny {
		return nil, errors.New("navigation policy: disabled default must deny")
	}
	p := &NavigationPolicy{
		cfg:    cfg,
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
	// Invalid or absent initial content remains observable and denies.
	p.reloadMu.Lock()
	p.lastError = p.load()
	p.initialized = true
	p.reloadMu.Unlock()
	return p, nil
}

// load reads and parses the policy file, updating the in-memory policy
// atomically. Returns an error if the file cannot be read or parsed.
func (p *NavigationPolicy) load() error {
	data, digest, err := readNavigationPolicyFile(p.cfg.Path)
	// A concurrent publish is not a stable candidate or configuration failure.
	// Retain both current policy and error evidence until the next stable read.
	if p.initialized && errors.Is(err, errNavigationPolicyFileChanged) {
		return p.lastError
	}
	if digest == p.lastAttempt {
		return p.lastError
	}
	p.lastAttempt = digest
	if err == nil {
		var candidate *PolicyFile
		candidate, err = decodeNavigationPolicy(data)
		if err == nil {
			if digest != p.activeDigest {
				p.publish(candidate, digest)
			}
			p.lastError = nil
			return nil
		}
	}
	p.lastError = err
	p.stats.Errors.Add(1)
	return err
}

// publish runs under reloadMu after complete candidate validation.
func (p *NavigationPolicy) publish(policy *PolicyFile, digest string) {
	p.mu.Lock()
	p.policy = policy
	p.activeDigest = digest
	p.mu.Unlock()
	if p.initialized {
		p.stats.Reloads.Add(1)
	}
}

// matchRule tests a URL against a single rule.
func (r *PolicyRule) matchRule(testURL string) bool {
	if r.literal {
		return matchLiteralPolicyURL(r.Pattern, testURL)
	}
	return r.compiled.MatchString(testURL)
}

// Evaluate implements PolicyEngine. It evaluates the current policy rules
// against the request URL and returns the decision (spec L4259).
func (p *NavigationPolicy) Evaluate(ctx context.Context, req PolicyRequest) (PolicyResponse, error) {
	if p == nil || ctx == nil {
		return PolicyResponse{Decision: PolicyDecisionDeny, Reason: "policy and context required"}, errors.New("navigation policy: policy and context required")
	}
	if err := ctx.Err(); err != nil {
		return PolicyResponse{Decision: PolicyDecisionDeny, Reason: "context cancelled"}, err
	}
	p.stats.Total.Add(1)
	if p.stopped.Load() {
		return p.decisionFor(PolicyFileDeny, "policy stopped"), ErrNavigationPolicyStopped
	}
	p.mu.RLock()
	policy := p.policy
	disabledDefault := p.cfg.DisabledDefault
	p.mu.RUnlock()

	if policy == nil {
		// Degraded mode: no policy loaded.
		return p.decisionFor(disabledDefault, "policy not loaded (degraded mode)"), nil
	}

	decision, reason := evaluatePolicyRequest(policy, req)
	return p.decisionFor(decision, reason), nil
}

// decisionFor maps a PolicyFileAction to a PolicyResponse and updates stats.
func (p *NavigationPolicy) decisionFor(action PolicyFileAction, reason string) PolicyResponse {
	switch action {
	case PolicyFileAllow:
		p.stats.Allowed.Add(1)
		return PolicyResponse{Decision: PolicyDecisionAllow, Reason: reason}
	case PolicyFileDeny:
		p.stats.Denied.Add(1)
		return PolicyResponse{Decision: PolicyDecisionDeny, Reason: reason}
	case PolicyFileChallenge:
		p.stats.Challenged.Add(1)
		return PolicyResponse{Decision: PolicyDecisionChallenge, Reason: reason}
	default:
		p.stats.Errors.Add(1)
		return PolicyResponse{Decision: PolicyDecisionDeny, Reason: "unknown action: " + string(action)}
	}
}

// StartHotReload begins bounded content polling at ReloadInterval.
// When content changes, the policy is reloaded. If reload fails, the
// previous policy remains in effect and the error is counted in stats.
// Calling StartHotReload more than once is a no-op.
func (p *NavigationPolicy) StartHotReload() {
	if p == nil {
		return
	}
	p.reloadMu.Lock()
	defer p.reloadMu.Unlock()
	if p.cfg.Path == "" || p.started || p.stopped.Load() {
		return
	}
	p.started = true
	go p.hotReloadLoop()
}

// hotReloadLoop is the single owned background content-poll loop.
func (p *NavigationPolicy) hotReloadLoop() {
	ticker := time.NewTicker(p.cfg.ReloadInterval)
	defer ticker.Stop()
	defer close(p.doneCh)
	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.reloadMu.Lock()
			if p.stopped.Load() {
				p.reloadMu.Unlock()
				return
			}
			p.lastError = p.load()
			p.reloadMu.Unlock()
		}
	}
}

// Stop permanently stops reloads and joins the owned poller and active IO.
func (p *NavigationPolicy) Stop() {
	if p == nil {
		return
	}
	p.reloadMu.Lock()
	if !p.stopped.Swap(true) {
		close(p.stopCh)
	}
	started := p.started
	p.reloadMu.Unlock()
	if started {
		<-p.doneCh
	}
}

// Stats returns a snapshot of the policy evaluation counters.
func (p *NavigationPolicy) Stats() (total, allowed, denied, challenged, reloads, errors int64) {
	return p.stats.Total.Load(),
		p.stats.Allowed.Load(),
		p.stats.Denied.Load(),
		p.stats.Challenged.Load(),
		p.stats.Reloads.Load(),
		p.stats.Errors.Load()
}

// Reload forces an immediate reload of the policy file. Returns an error
// if the reload fails; on failure the previous policy remains active.
func (p *NavigationPolicy) Reload() error {
	if p == nil {
		return errors.New("navigation policy: policy required")
	}
	p.reloadMu.Lock()
	defer p.reloadMu.Unlock()
	if p.stopped.Load() {
		return ErrNavigationPolicyStopped
	}
	if p.cfg.Path == "" {
		return errors.New("navigation policy: static snapshot has no file to reload")
	}
	return p.load()
}

// CurrentPolicy returns a copy of the currently loaded policy file, or nil
// if no policy is loaded (degraded mode).
func (p *NavigationPolicy) CurrentPolicy() *PolicyFile {
	if p == nil {
		return nil
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.policy == nil {
		return nil
	}
	cp := *p.policy
	cp.Allow = slices.Clone(p.policy.Allow)
	cp.Deny = slices.Clone(p.policy.Deny)
	cp.Confirm = slices.Clone(p.policy.Confirm)
	cp.DomainAllowlist = slices.Clone(p.policy.DomainAllowlist)
	cp.actionAllow, cp.actionDeny, cp.actionConfirm, cp.domainAllow = nil, nil, nil, nil
	cp.Rules = slices.Clone(p.policy.Rules)
	for i := range cp.Rules {
		cp.Rules[i].compiled = nil
	}
	return &cp
}

// ValidateURL checks a URL against the current policy without recording
// stats. Returns the decision that Evaluate would return. Useful for
// pre-flight checks.
func (p *NavigationPolicy) ValidateURL(rawURL string) PolicyDecision {
	if p == nil || p.stopped.Load() {
		return PolicyDecisionDeny
	}
	if _, err := ParsePolicyURL(rawURL); err != nil {
		return PolicyDecisionDeny
	}
	p.mu.RLock()
	policy := p.policy
	disabledDefault := p.cfg.DisabledDefault
	p.mu.RUnlock()
	if policy == nil {
		return toDecision(disabledDefault)
	}
	decision, _ := evaluatePolicyRequest(policy, PolicyRequest{URL: rawURL, Action: "navigate"})
	return toDecision(decision)
}

// toDecision converts a PolicyFileAction to a PolicyDecision.
func toDecision(action PolicyFileAction) PolicyDecision {
	switch action {
	case PolicyFileAllow:
		return PolicyDecisionAllow
	case PolicyFileDeny:
		return PolicyDecisionDeny
	case PolicyFileChallenge:
		return PolicyDecisionChallenge
	default:
		return PolicyDecisionDeny
	}
}

// WriteDefaultPolicyFile exclusively creates a deny-default policy file.
func WriteDefaultPolicyFile(path string) error {
	pf := PolicyFile{
		Version: PolicyFileVersion,
		Default: PolicyFileDeny,
		Rules:   []PolicyRule{},
	}
	data, err := json.MarshalIndent(pf, "", "  ")
	if err != nil {
		return fmt.Errorf("navigation policy: marshal: %w", err)
	}
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("navigation policy: mkdir: %w", err)
		}
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("navigation policy: create: %w", err)
	}
	_, writeErr := file.Write(data)
	return errors.Join(writeErr, file.Close())
}

// ParsePolicyURL extracts the host from a URL for policy matching. Returns
// an error if the URL is not parseable.
func ParsePolicyURL(rawURL string) (host string, err error) {
	u, err := parseNavigationPolicyURL(rawURL)
	if err != nil {
		return "", fmt.Errorf("navigation policy: parse URL: %w", err)
	}
	return u.Hostname(), nil
}
