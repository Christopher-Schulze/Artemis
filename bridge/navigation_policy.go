package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// navigation_policy.go (spec L4259: JSON navigation policy file + hot-reload).
//
// A NavigationPolicyFile is a JSON document that defines allow/deny rules
// for browser navigation. The policy is loaded from a JSON file on disk and
// hot-reloaded when the file's mtime changes. The policy implements the
// PolicyEngine interface so it can be wired into the existing PolicyHook
// (spec L4247) without a separate integration path.
//
// Reference: research/webstack/agent-browser-main/cli/src/native/policy.rs
//
// JSON schema:
//
//	{
//	  "version": 1,
//	  "default": "allow",
//	  "rules": [
//	    {"action": "deny",    "pattern": "https://internal.corp.local/.*"},
//	    {"action": "challenge","pattern": "https://admin.example.com/.*"},
//	    {"action": "allow",   "pattern": "https://*.example.com/.*"}
//	  ]
//	}
//
// Rules are evaluated in order; the first match wins. If no rule matches,
// the `default` action applies. Patterns are matched against the full URL
// using Go regexp syntax. A pattern without regex metacharacters is treated
// as a literal prefix match for usability.

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
	Version int              `json:"version"`
	Default PolicyFileAction `json:"default"`
	Rules   []PolicyRule     `json:"rules"`
}

// NavigationPolicyConfig controls the file-backed navigation policy
// (spec L4259).
type NavigationPolicyConfig struct {
	// Path is the JSON policy file path. Required.
	Path string
	// ReloadInterval is the mtime-poll interval for hot-reload. Default 5s.
	ReloadInterval time.Duration
	// DisabledDefault is the action when the file is missing or unreadable.
	// Default is PolicyFileDeny (fail-closed).
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
	mu      sync.RWMutex
	cfg     NavigationPolicyConfig
	policy  *PolicyFile
	mtime   time.Time
	stats   NavigationPolicyStats
	stopCh  chan struct{}
	stopped atomic.Bool
}

// NewNavigationPolicy creates a policy from the given config. The initial
// load happens synchronously; if the file is missing the policy falls back
// to DisabledDefault (fail-closed). Call StartHotReload to begin polling.
func NewNavigationPolicy(cfg NavigationPolicyConfig) (*NavigationPolicy, error) {
	if cfg.Path == "" {
		return nil, errors.New("navigation policy: empty path")
	}
	if cfg.ReloadInterval <= 0 {
		cfg.ReloadInterval = DefaultReloadInterval
	}
	if cfg.DisabledDefault == "" {
		cfg.DisabledDefault = PolicyFileDeny
	}
	p := &NavigationPolicy{
		cfg:    cfg,
		stopCh: make(chan struct{}),
	}
	if err := p.load(); err != nil {
		// Non-fatal: policy starts in degraded mode with DisabledDefault.
		p.stats.Errors.Add(1)
	}
	return p, nil
}

// load reads and parses the policy file, updating the in-memory policy
// atomically. Returns an error if the file cannot be read or parsed.
func (p *NavigationPolicy) load() error {
	data, err := os.ReadFile(p.cfg.Path)
	if err != nil {
		return fmt.Errorf("navigation policy: read %s: %w", p.cfg.Path, err)
	}
	var pf PolicyFile
	if err := json.Unmarshal(data, &pf); err != nil {
		return fmt.Errorf("navigation policy: parse %s: %w", p.cfg.Path, err)
	}
	if pf.Version != PolicyFileVersion {
		return fmt.Errorf("navigation policy: unsupported version %d (want %d)", pf.Version, PolicyFileVersion)
	}
	if pf.Default == "" {
		pf.Default = PolicyFileAllow
	}
	// Compile rules.
	for i := range pf.Rules {
		rule := &pf.Rules[i]
		if rule.Action == "" {
			return fmt.Errorf("navigation policy: rule %d: empty action", i)
		}
		if rule.Pattern == "" {
			return fmt.Errorf("navigation policy: rule %d: empty pattern", i)
		}
		if hasRegexMeta(rule.Pattern) {
			re, err := regexp.Compile(rule.Pattern)
			if err != nil {
				return fmt.Errorf("navigation policy: rule %d: compile %q: %w", i, rule.Pattern, err)
			}
			rule.compiled = re
			rule.literal = false
		} else {
			rule.compiled = nil
			rule.literal = true
		}
	}
	info, statErr := os.Stat(p.cfg.Path)
	var mt time.Time
	if statErr == nil {
		mt = info.ModTime()
	}
	p.mu.Lock()
	p.policy = &pf
	p.mtime = mt
	p.mu.Unlock()
	return nil
}

// hasRegexMeta reports whether a pattern contains regex metacharacters.
// Patterns without metacharacters are treated as literal prefix matches.
func hasRegexMeta(pattern string) bool {
	return strings.ContainsAny(pattern, `.^$*+?()[]{}|\`)
}

// matchRule tests a URL against a single rule.
func (r *PolicyRule) matchRule(testURL string) bool {
	if r.literal {
		return strings.HasPrefix(testURL, r.Pattern)
	}
	return r.compiled.MatchString(testURL)
}

// Evaluate implements PolicyEngine. It evaluates the current policy rules
// against the request URL and returns the decision (spec L4259).
func (p *NavigationPolicy) Evaluate(ctx context.Context, req PolicyRequest) (PolicyResponse, error) {
	if err := ctx.Err(); err != nil {
		return PolicyResponse{Decision: PolicyDecisionDeny, Reason: "context cancelled"}, err
	}
	p.stats.Total.Add(1)

	p.mu.RLock()
	policy := p.policy
	disabledDefault := p.cfg.DisabledDefault
	p.mu.RUnlock()

	if policy == nil {
		// Degraded mode: no policy loaded.
		p.stats.Errors.Add(1)
		return p.decisionFor(disabledDefault, "policy not loaded (degraded mode)"), nil
	}

	testURL := req.URL
	if testURL == "" {
		p.stats.Errors.Add(1)
		return PolicyResponse{Decision: PolicyDecisionDeny, Reason: "empty URL"}, nil
	}

	for i := range policy.Rules {
		rule := &policy.Rules[i]
		if rule.matchRule(testURL) {
			return p.decisionFor(rule.Action, fmt.Sprintf("rule %d: %s %s", i, rule.Action, rule.Pattern)), nil
		}
	}

	// No rule matched: apply default.
	return p.decisionFor(policy.Default, "default"), nil
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

// StartHotReload begins polling the policy file's mtime at ReloadInterval.
// When the mtime changes, the policy is reloaded. If reload fails, the
// previous policy remains in effect and the error is counted in stats.
// Calling StartHotReload more than once is a no-op.
func (p *NavigationPolicy) StartHotReload() {
	go p.hotReloadLoop()
}

// hotReloadLoop is the background mtime-poll loop.
func (p *NavigationPolicy) hotReloadLoop() {
	ticker := time.NewTicker(p.cfg.ReloadInterval)
	defer ticker.Stop()
	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.checkAndReload()
		}
	}
}

// checkAndReload stats the policy file and reloads if the mtime changed.
func (p *NavigationPolicy) checkAndReload() {
	info, err := os.Stat(p.cfg.Path)
	if err != nil {
		p.stats.Errors.Add(1)
		return
	}
	p.mu.RLock()
	oldMtime := p.mtime
	p.mu.RUnlock()
	if !info.ModTime().After(oldMtime) {
		return
	}
	if err := p.load(); err != nil {
		p.stats.Errors.Add(1)
		return
	}
	p.stats.Reloads.Add(1)
}

// Stop terminates the hot-reload goroutine. Safe to call multiple times.
func (p *NavigationPolicy) Stop() {
	if p.stopped.CompareAndSwap(false, true) {
		close(p.stopCh)
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
	return p.load()
}

// CurrentPolicy returns a copy of the currently loaded policy file, or nil
// if no policy is loaded (degraded mode).
func (p *NavigationPolicy) CurrentPolicy() *PolicyFile {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.policy == nil {
		return nil
	}
	cp := *p.policy
	return &cp
}

// ValidateURL checks a URL against the current policy without recording
// stats. Returns the decision that Evaluate would return. Useful for
// pre-flight checks.
func (p *NavigationPolicy) ValidateURL(rawURL string) PolicyDecision {
	p.mu.RLock()
	policy := p.policy
	disabledDefault := p.cfg.DisabledDefault
	p.mu.RUnlock()
	if policy == nil {
		return toDecision(disabledDefault)
	}
	for i := range policy.Rules {
		rule := &policy.Rules[i]
		if rule.matchRule(rawURL) {
			return toDecision(rule.Action)
		}
	}
	return toDecision(policy.Default)
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

// WriteDefaultPolicyFile writes a minimal allow-by-default policy file to
// the given path. This is a convenience for bootstrapping a new deployment.
func WriteDefaultPolicyFile(path string) error {
	pf := PolicyFile{
		Version: PolicyFileVersion,
		Default: PolicyFileAllow,
		Rules: []PolicyRule{
			{Action: PolicyFileDeny, Pattern: "https://localhost"},
			{Action: PolicyFileDeny, Pattern: "https://127.0.0.1"},
		},
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
	return os.WriteFile(path, data, 0o600)
}

// ParsePolicyURL extracts the host from a URL for policy matching. Returns
// an error if the URL is not parseable.
func ParsePolicyURL(rawURL string) (host string, err error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("navigation policy: parse URL: %w", err)
	}
	return u.Hostname(), nil
}
