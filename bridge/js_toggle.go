package bridge

import (
	"context"
	"fmt"
	"sync"
)

// JSToggleConfig controls whether arbitrary JavaScript evaluation is
// permitted for a browser profile (spec L4308). The restrictive default is
// EvaluateEnabled=false: an agent cannot execute arbitrary JS unless the
// profile explicitly opts in.
type JSToggleConfig struct {
	// EvaluateEnabled gates act:evaluate and wait-with-fn actions. Default
	// false (restrictive).
	EvaluateEnabled bool
	// ProfileName is the profile this config applies to.
	ProfileName string
	// Reason records why evaluation was enabled or disabled (audit trail).
	Reason string
}

// DefaultJSToggleConfig returns the restrictive default: evaluation disabled.
func DefaultJSToggleConfig(profileName string) JSToggleConfig {
	return JSToggleConfig{
		EvaluateEnabled: false,
		ProfileName:     profileName,
		Reason:          "restrictive default: arbitrary JS evaluation disabled",
	}
}

// JSEvalDisabledError is returned when an agent attempts to evaluate JS
// while the profile has EvaluateEnabled=false.
type JSEvalDisabledError struct {
	ProfileName string
	Action      string
	Reason      string
}

// Error implements the error interface.
func (e JSEvalDisabledError) Error() string {
	return fmt.Sprintf("JS evaluation disabled for profile %q: action %q blocked. %s", e.ProfileName, e.Action, e.Reason)
}

// JSToggle manages per-profile JS evaluation permissions. It is thread-safe.
type JSToggle struct {
	mu       sync.RWMutex
	profiles map[string]JSToggleConfig
}

// NewJSToggle builds a JSToggle with no profiles configured. Profiles must
// be added via SetProfile before Check will allow evaluation.
func NewJSToggle() *JSToggle {
	return &JSToggle{
		profiles: make(map[string]JSToggleConfig),
	}
}

// SetProfile registers or replaces the JS toggle config for a profile.
func (t *JSToggle) SetProfile(config JSToggleConfig) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.profiles[config.ProfileName] = config
}

// GetProfile returns the config for a profile, or the restrictive default
// if the profile is not registered.
func (t *JSToggle) GetProfile(profileName string) JSToggleConfig {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if cfg, ok := t.profiles[profileName]; ok {
		return cfg
	}
	return DefaultJSToggleConfig(profileName)
}

// IsEnabled reports whether JS evaluation is enabled for a profile.
func (t *JSToggle) IsEnabled(profileName string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if cfg, ok := t.profiles[profileName]; ok {
		return cfg.EvaluateEnabled
	}
	return false
}

// Check verifies that JS evaluation is permitted for the profile. Returns
// a JSEvalDisabledError if evaluation is disabled.
func (t *JSToggle) Check(profileName, action string) error {
	t.mu.RLock()
	defer t.mu.RUnlock()
	cfg, ok := t.profiles[profileName]
	if !ok {
		return JSEvalDisabledError{
			ProfileName: profileName,
			Action:      action,
			Reason:      "profile not registered; restrictive default applies",
		}
	}
	if !cfg.EvaluateEnabled {
		return JSEvalDisabledError{
			ProfileName: profileName,
			Action:      action,
			Reason:      cfg.Reason,
		}
	}
	return nil
}

// CheckContext is the context-aware variant of Check. It returns early if
// the context is cancelled, otherwise delegates to Check.
func (t *JSToggle) CheckContext(ctx context.Context, profileName, action string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("js toggle check cancelled: %w", err)
	}
	return t.Check(profileName, action)
}

// EnableEvaluation enables JS evaluation for a profile with the supplied
// reason. If the profile does not exist, it is created.
func (t *JSToggle) EnableEvaluation(profileName, reason string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	cfg, ok := t.profiles[profileName]
	if !ok {
		cfg = DefaultJSToggleConfig(profileName)
	}
	cfg.EvaluateEnabled = true
	cfg.Reason = reason
	t.profiles[profileName] = cfg
}

// DisableEvaluation disables JS evaluation for a profile with the supplied
// reason. If the profile does not exist, it is created with the restrictive
// default.
func (t *JSToggle) DisableEvaluation(profileName, reason string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	cfg, ok := t.profiles[profileName]
	if !ok {
		cfg = DefaultJSToggleConfig(profileName)
	}
	cfg.EvaluateEnabled = false
	cfg.Reason = reason
	t.profiles[profileName] = cfg
}

// RemoveProfile removes a profile from the toggle registry.
func (t *JSToggle) RemoveProfile(profileName string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.profiles[profileName]; !ok {
		return false
	}
	delete(t.profiles, profileName)
	return true
}

// ListProfiles returns the names of all registered profiles.
func (t *JSToggle) ListProfiles() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]string, 0, len(t.profiles))
	for name := range t.profiles {
		out = append(out, name)
	}
	return out
}

// ProfileCount returns the number of registered profiles.
func (t *JSToggle) ProfileCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.profiles)
}

// EnabledCount returns the number of profiles with evaluation enabled.
func (t *JSToggle) EnabledCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	count := 0
	for _, cfg := range t.profiles {
		if cfg.EvaluateEnabled {
			count++
		}
	}
	return count
}
