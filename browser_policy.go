package artemis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/Christopher-Schulze/Artemis/bridge"
)

// The implicit managed profile permits navigation/read commands only.
// Navigation can execute site scripts; this profile does not claim zero effects.
const defaultBrowserPolicyJSON = `{"default":"deny","allow":["navigate","wait","screenshot","tab_list","tab_close","observe","query","extract"]}`

// BrowserPolicy composes the local file constraint and trusted host authority.
// Its owner starts reloads and stops it; a host engine retains its own lifetime.
type BrowserPolicy struct {
	local          *bridge.NavigationPolicy
	host           *bridge.PolicyHook
	confirm        *bridge.NavigationPolicy
	closed         atomic.Bool
	fileConfigured bool
}

// BrowserPolicyHealth exposes bounded reload evidence without policy contents.
type BrowserPolicyHealth struct {
	Ready           bool   `json:"ready"`
	FileConfigured  bool   `json:"fileConfigured"`
	HostConfigured  bool   `json:"hostConfigured"`
	Reloads         int64  `json:"reloads"`
	ReloadErrors    int64  `json:"reloadErrors"`
	LastReloadError string `json:"lastReloadError,omitempty"`
}

func (p *BrowserPolicy) Health() BrowserPolicyHealth {
	if p == nil {
		return BrowserPolicyHealth{}
	}
	result := BrowserPolicyHealth{Ready: !p.closed.Load() && (p.local != nil || p.host != nil), FileConfigured: p.fileConfigured, HostConfigured: p.host != nil}
	if p.local != nil {
		_, _, _, _, result.Reloads, result.ReloadErrors = p.local.Stats()
		if err := p.local.LastReloadError(); err != nil {
			result.LastReloadError = err.Error()
		}
	}
	return result
}

func NewBrowserPolicy(ctx context.Context, config AgentConfig) (*BrowserPolicy, error) {
	if err := validateBrowserPolicyContext(ctx, config.PolicyEngine); err != nil {
		return nil, err
	}
	owner := &BrowserPolicy{fileConfigured: config.ActionPolicyPath != ""}
	var err error
	if config.ActionPolicyPath != "" {
		owner.local, err = bridge.NewNavigationPolicy(bridge.NavigationPolicyConfig{Path: config.ActionPolicyPath})
		if err == nil {
			err = owner.local.LastReloadError()
		}
	} else if config.PolicyEngine == nil {
		owner.local, err = bridge.NewStaticNavigationPolicy([]byte(defaultBrowserPolicyJSON))
	}
	if err != nil {
		owner.Stop()
		return nil, fmt.Errorf("browser policy: initial configuration: %w", err)
	}
	if config.PolicyEngine != nil {
		owner.host = bridge.NewPolicyHook(config.PolicyEngine)
	}
	if len(config.ConfirmActions) != 0 {
		data, marshalErr := json.Marshal(struct {
			Default string   `json:"default"`
			Confirm []string `json:"confirm"`
		}{"allow", config.ConfirmActions})
		if marshalErr != nil {
			owner.Stop()
			return nil, marshalErr
		}
		owner.confirm, err = bridge.NewStaticNavigationPolicy(data)
		if err != nil {
			owner.Stop()
			return nil, err
		}
	}
	return owner, nil
}

func validateBrowserPolicyContext(ctx context.Context, host bridge.PolicyEngine) error {
	if ctx == nil {
		return errors.New("browser policy: context required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if host != nil && dependencyMissing(host) {
		return errors.New("browser policy: typed nil host engine")
	}
	return nil
}

func (p *BrowserPolicy) StartHotReload() {
	if p == nil || p.closed.Load() {
		return
	}
	if p.local != nil {
		p.local.StartHotReload()
	}
}

func (p *BrowserPolicy) Stop() {
	if p == nil {
		return
	}
	p.closed.Store(true)
	if p.host != nil {
		p.host.SetEnabled(false)
	}
	if p.local != nil {
		p.local.Stop()
	}
	if p.confirm != nil {
		p.confirm.Stop()
	}
}

func (p *BrowserPolicy) Evaluate(ctx context.Context, request bridge.PolicyRequest) (bridge.PolicyResponse, error) {
	if p == nil || p.closed.Load() || (p.local == nil && p.host == nil) {
		return bridge.PolicyResponse{Decision: bridge.PolicyDecisionDeny, Reason: "browser policy unavailable"}, errors.New("browser policy unavailable")
	}
	response := bridge.PolicyResponse{Decision: bridge.PolicyDecisionAllow}
	if p.local != nil {
		local, err := p.local.Evaluate(ctx, request)
		if err != nil || local.Decision == bridge.PolicyDecisionDeny {
			return local, err
		}
		response = local
	}
	if p.host != nil {
		host, err := p.host.Check(ctx, request)
		if err != nil || host.Decision == bridge.PolicyDecisionDeny {
			return host, err
		}
		if host.Decision == bridge.PolicyDecisionChallenge || response.Decision == bridge.PolicyDecisionAllow {
			response = host
		}
	}
	if p.confirm != nil {
		confirmation, err := p.confirm.Evaluate(ctx, request)
		if err != nil || confirmation.Decision != bridge.PolicyDecisionAllow {
			return confirmation, err
		}
	}
	if p.closed.Load() {
		return bridge.PolicyResponse{Decision: bridge.PolicyDecisionDeny, Reason: "browser policy stopped"}, errors.New("browser policy stopped")
	}
	return response, nil
}

func (a *Agent) authorizeBrowserRequest(ctx context.Context, request bridge.PolicyRequest) error {
	a.mu.RLock()
	policy, state := a.browserPolicy, a.state
	a.mu.RUnlock()
	if state != AgentStateRunning || policy == nil {
		return newTaskError(TaskErrorPolicyDenied, "browser_policy", errors.New("browser policy owner unavailable"))
	}
	response, err := policy.Evaluate(ctx, request)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return classifyTaskError("browser_policy", err)
		}
		return newTaskError(TaskErrorPolicyDenied, "browser_policy", err)
	}
	if response.Decision != bridge.PolicyDecisionAllow {
		return newTaskError(TaskErrorPolicyDenied, "browser_policy", &bridge.PolicyError{Decision: response.Decision, Reason: response.Reason})
	}
	return nil
}
