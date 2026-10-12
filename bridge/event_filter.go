// Package bridge CDP domain enable/disable manager (spec L4495: P1.3 Event Filtering).
//
// P1.3 Event Filtering: only activate the CDP domains the current scrape
// actually needs. Page is always on; Network only when monitoring;
// Accessibility only for snapshotting; Runtime only for JS evaluation.
// Domains that are never needed for the configured feature set (CSS, DOM,
// DOMStorage, Profiler) must NOT be enabled so the browser does not spend
// time generating events nobody consumes.
//
// EventFilter computes the desired domain state set from an EventFilterConfig,
// exposes enable/disable command lists for the transition, and is safe for
// concurrent reconfiguration via SetConfig.
package bridge

import (
	"fmt"
	"sort"
	"sync"
)

// CDPDomain identifies a Chrome DevTools Protocol domain.
type CDPDomain string

const (
	DomainNetwork       CDPDomain = "Network"
	DomainPage          CDPDomain = "Page"
	DomainAccessibility CDPDomain = "Accessibility"
	DomainRuntime       CDPDomain = "Runtime"
	DomainCSS           CDPDomain = "CSS"
	DomainDOM           CDPDomain = "DOM"
	DomainDOMStorage    CDPDomain = "DOMStorage"
	DomainProfiler      CDPDomain = "Profiler"
	DomainOverlay       CDPDomain = "Overlay"
	DomainEmulation     CDPDomain = "Emulation"
)

// DomainState describes the desired state of a CDP domain.
type DomainState string

const (
	// DomainStateEnabled means the domain should be enabled.
	DomainStateEnabled DomainState = "enabled"
	// DomainStateDisabled means the domain should be explicitly disabled.
	DomainStateDisabled DomainState = "disabled"
	// DomainStateNotNeeded means the domain is not in the needed set and
	// requires no explicit enable/disable command.
	DomainStateNotNeeded DomainState = "not_needed"
)

// EventFilterConfig controls which CDP domains are needed for the current
// scrape. Flags map directly to feature requirements.
type EventFilterConfig struct {
	// NetworkMonitoring enables the Network domain for request/response
	// monitoring. Off by default.
	NetworkMonitoring bool
	// PageEnabled keeps the Page domain on. Defaults to true; the Page
	// domain is always required for navigation lifecycle events.
	PageEnabled bool
	// AccessibilitySnapshot enables the Accessibility domain for full-tree
	// accessibility snapshots.
	AccessibilitySnapshot bool
	// JSEvaluate enables the Runtime domain for JavaScript evaluation.
	JSEvaluate bool
	// CSSNeeded enables the CSS domain. Off by default; CSS is expensive.
	CSSNeeded bool
	// DOMNeeded enables the DOM domain. Off by default; prefer Accessibility.
	DOMNeeded bool
	// DOMStorageNeeded enables the DOMStorage domain. Off by default.
	DOMStorageNeeded bool
	// ProfilerNeeded enables the Profiler domain. Off by default.
	ProfilerNeeded bool
}

// DefaultEventFilterConfig returns the default configuration: Page always on,
// all other domains off. This matches the spec baseline where only Page is
// unconditionally required.
func DefaultEventFilterConfig() EventFilterConfig {
	return EventFilterConfig{
		PageEnabled: true,
	}
}

// CDPCommand is a single CDP method invocation targeting a domain.
type CDPCommand struct {
	Domain CDPDomain
	Method string
	Params map[string]interface{}
}

// EventFilterStats records counters about the filter's command generation.
type EventFilterStats struct {
	// EnabledCount is the number of domains in the enabled state.
	EnabledCount int
	// DisabledCount is the number of domains in the disabled state.
	DisabledCount int
	// CommandsIssued is the total number of enable+disable commands
	// produced by the most recent ApplyFilter call.
	CommandsIssued int
}

// EventFilter manages the desired CDP domain state set derived from a config.
// It is safe for concurrent use.
type EventFilter struct {
	mu     sync.RWMutex
	config EventFilterConfig
	states map[CDPDomain]DomainState
	stats  EventFilterStats
}

// allDomains is the full set of domains known to the filter, in a stable
// canonical order for deterministic command emission.
var allDomains = []CDPDomain{
	DomainNetwork,
	DomainPage,
	DomainAccessibility,
	DomainRuntime,
	DomainCSS,
	DomainDOM,
	DomainDOMStorage,
	DomainProfiler,
	DomainOverlay,
	DomainEmulation,
}

// NewEventFilter creates a filter with the initial domain states computed
// from the given config.
func NewEventFilter(config EventFilterConfig) *EventFilter {
	ef := &EventFilter{
		states: make(map[CDPDomain]DomainState),
	}
	ef.SetConfig(config)
	return ef
}

// computeStates derives the desired domain state map from a config.
// Needed domains become "enabled"; blocked domains become "disabled";
// remaining domains become "not_needed".
func computeStates(config EventFilterConfig) map[CDPDomain]DomainState {
	states := make(map[CDPDomain]DomainState, len(allDomains))
	for _, d := range allDomains {
		switch {
		case isNeeded(d, config):
			states[d] = DomainStateEnabled
		case isBlocked(d, config):
			states[d] = DomainStateDisabled
		default:
			states[d] = DomainStateNotNeeded
		}
	}
	return states
}

// isNeeded reports whether a domain is required by the config.
func isNeeded(d CDPDomain, c EventFilterConfig) bool {
	switch d {
	case DomainPage:
		// Page is always required; default to true even when the flag is
		// false to honor the spec invariant "Page always on".
		return true
	case DomainNetwork:
		return c.NetworkMonitoring
	case DomainAccessibility:
		return c.AccessibilitySnapshot
	case DomainRuntime:
		return c.JSEvaluate
	case DomainCSS:
		return c.CSSNeeded
	case DomainDOM:
		return c.DOMNeeded
	case DomainDOMStorage:
		return c.DOMStorageNeeded
	case DomainProfiler:
		return c.ProfilerNeeded
	default:
		// Overlay and Emulation are not driven by any current config flag;
		// they are not needed by default.
		return false
	}
}

// isBlocked reports whether a domain must be explicitly disabled. The blocked
// set is the complement of the needed set among the domains the spec calls out
// as never-needed by default (CSS, DOM, DOMStorage, Profiler). When one of
// those is explicitly needed via its config flag it is not blocked.
func isBlocked(d CDPDomain, c EventFilterConfig) bool {
	switch d {
	case DomainCSS:
		return !c.CSSNeeded
	case DomainDOM:
		return !c.DOMNeeded
	case DomainDOMStorage:
		return !c.DOMStorageNeeded
	case DomainProfiler:
		return !c.ProfilerNeeded
	default:
		return false
	}
}

// SetConfig updates the filter's config and recomputes all domain states.
func (ef *EventFilter) SetConfig(config EventFilterConfig) {
	ef.mu.Lock()
	defer ef.mu.Unlock()
	ef.config = config
	ef.states = computeStates(config)
	ef.recomputeStatsLocked()
}

// recomputeStatsLocked updates the stats counters from the current state map.
// Caller must hold ef.mu.
func (ef *EventFilter) recomputeStatsLocked() {
	enabled, disabled := 0, 0
	for _, s := range ef.states {
		switch s {
		case DomainStateEnabled:
			enabled++
		case DomainStateDisabled:
			disabled++
		}
	}
	ef.stats.EnabledCount = enabled
	ef.stats.DisabledCount = disabled
}

// ShouldEnable reports whether a domain should be enabled based on the
// current config (i.e., it is in the needed set).
func (ef *EventFilter) ShouldEnable(domain CDPDomain) bool {
	ef.mu.RLock()
	defer ef.mu.RUnlock()
	return ef.states[domain] == DomainStateEnabled
}

// ShouldDisable reports whether a domain should be explicitly disabled
// (i.e., it is in the blocked set).
func (ef *EventFilter) ShouldDisable(domain CDPDomain) bool {
	ef.mu.RLock()
	defer ef.mu.RUnlock()
	return ef.states[domain] == DomainStateDisabled
}

// IsNeeded reports whether a domain is in the needed set for the current
// config.
func (ef *EventFilter) IsNeeded(domain CDPDomain) bool {
	ef.mu.RLock()
	defer ef.mu.RUnlock()
	return ef.states[domain] == DomainStateEnabled
}

// GetDomainState returns the desired state for a single domain.
func (ef *EventFilter) GetDomainState(domain CDPDomain) DomainState {
	ef.mu.RLock()
	defer ef.mu.RUnlock()
	return ef.states[domain]
}

// AllDomainStates returns a copy of the full domain state map.
func (ef *EventFilter) AllDomainStates() map[CDPDomain]DomainState {
	ef.mu.RLock()
	defer ef.mu.RUnlock()
	out := make(map[CDPDomain]DomainState, len(ef.states))
	for k, v := range ef.states {
		out[k] = v
	}
	return out
}

// NeededDomains returns the sorted list of domains in the needed (enabled)
// set.
func (ef *EventFilter) NeededDomains() []CDPDomain {
	ef.mu.RLock()
	defer ef.mu.RUnlock()
	var out []CDPDomain
	for _, d := range allDomains {
		if ef.states[d] == DomainStateEnabled {
			out = append(out, d)
		}
	}
	return out
}

// BlockedDomains returns the sorted list of domains that must NOT be enabled
// (the blocked set).
func (ef *EventFilter) BlockedDomains() []CDPDomain {
	ef.mu.RLock()
	defer ef.mu.RUnlock()
	var out []CDPDomain
	for _, d := range allDomains {
		if ef.states[d] == DomainStateDisabled {
			out = append(out, d)
		}
	}
	return out
}

// EnableCommands returns the list of CDP enable commands for every needed
// domain, in canonical domain order.
func (ef *EventFilter) EnableCommands() []CDPCommand {
	ef.mu.RLock()
	defer ef.mu.RUnlock()
	var cmds []CDPCommand
	for _, d := range allDomains {
		if ef.states[d] == DomainStateEnabled {
			cmds = append(cmds, CDPCommand{
				Domain: d,
				Method: "enable",
			})
		}
	}
	return cmds
}

// DisableCommands returns the list of CDP disable commands for domains that
// are in the blocked set, in canonical domain order.
func (ef *EventFilter) DisableCommands() []CDPCommand {
	ef.mu.RLock()
	defer ef.mu.RUnlock()
	var cmds []CDPCommand
	for _, d := range allDomains {
		if ef.states[d] == DomainStateDisabled {
			cmds = append(cmds, CDPCommand{
				Domain: d,
				Method: "disable",
			})
		}
	}
	return cmds
}

// ApplyFilter returns the full command list to transition the browser to the
// desired domain state: all disable commands first (to stop unwanted event
// streams), then all enable commands. The stats.CommandsIssued counter is
// updated to reflect the number of commands returned.
func (ef *EventFilter) ApplyFilter() []CDPCommand {
	ef.mu.Lock()
	defer ef.mu.Unlock()

	var cmds []CDPCommand
	// Disables first so the browser stops emitting events from domains we
	// no longer need before we start new ones.
	for _, d := range allDomains {
		if ef.states[d] == DomainStateDisabled {
			cmds = append(cmds, CDPCommand{
				Domain: d,
				Method: "disable",
			})
		}
	}
	for _, d := range allDomains {
		if ef.states[d] == DomainStateEnabled {
			cmds = append(cmds, CDPCommand{
				Domain: d,
				Method: "enable",
			})
		}
	}

	ef.stats.CommandsIssued = len(cmds)
	ef.recomputeStatsLocked()
	return cmds
}

// Stats returns a snapshot of the filter's counters.
func (ef *EventFilter) Stats() EventFilterStats {
	ef.mu.RLock()
	defer ef.mu.RUnlock()
	return ef.stats
}

// String returns a human-readable description of a CDPCommand, e.g.
// "Network.enable".
func (c CDPCommand) String() string {
	return fmt.Sprintf("%s.%s", c.Domain, c.Method)
}

// sortedDomains returns a sorted copy of a domain slice for deterministic
// test comparisons.
func sortedDomains(in []CDPDomain) []CDPDomain {
	out := make([]CDPDomain, len(in))
	copy(out, in)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
