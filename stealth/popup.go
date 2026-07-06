package stealth

import (
	"fmt"
	"sync"
)

// popup.go (spec L4023: stealth/popup.go - popup guard).
//
// Anti-detection: popup guard prevents unwanted popups from opening
// during automation. Popups can leak the real browser identity and
// break automation flows. The launch flag --disable-popup-blocking
// is used in stealth mode to prevent popups from blocking automation,
// but this file provides the Go-side popup guard management.

// PopupPolicy enumerates the popup guard policies
// (spec L4023: popup.go - popup guard).
type PopupPolicy string

const (
	// PopupPolicyBlock blocks all popups (default for non-stealth).
	PopupPolicyBlock PopupPolicy = "block"
	// PopupPolicyAllow allows popups (stealth mode: --disable-popup-blocking).
	PopupPolicyAllow PopupPolicy = "allow"
	// PopupPolicyStealth allows popups but patches window.open to
	// prevent identity leaks (spec L4023: popup guard).
	PopupPolicyStealth PopupPolicy = "stealth"
)

// PopupGuard manages popup blocking/allowing policies
// (spec L4023: popup.go - popup guard).
type PopupGuard struct {
	mu     sync.RWMutex
	policy PopupPolicy
	count  int // number of popups blocked/allowed
}

// NewPopupGuard creates a new PopupGuard with the default block policy
// (spec L4023: popup.go - popup guard).
func NewPopupGuard() *PopupGuard {
	return &PopupGuard{policy: PopupPolicyBlock}
}

// SetPolicy sets the popup policy
// (spec L4023: popup.go - popup guard).
func (g *PopupGuard) SetPolicy(p PopupPolicy) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.policy = p
}

// Policy returns the current popup policy
// (spec L4023).
func (g *PopupGuard) Policy() PopupPolicy {
	if g == nil {
		return PopupPolicyBlock
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.policy
}

// ShouldBlock reports whether a popup should be blocked
// (spec L4023: popup.go - popup guard).
func (g *PopupGuard) ShouldBlock() bool {
	if g == nil {
		return true
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.policy == PopupPolicyBlock
}

// RecordPopup records a popup event
// (spec L4023: popup.go - popup guard).
func (g *PopupGuard) RecordPopup() {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.count++
}

// Count returns the number of popup events recorded
// (spec L4023).
func (g *PopupGuard) Count() int {
	if g == nil {
		return 0
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.count
}

// IsStealth reports whether the stealth popup policy is active
// (spec L4023: popup guard).
func (g *PopupGuard) IsStealth() bool {
	if g == nil {
		return false
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.policy == PopupPolicyStealth
}

// String returns a diagnostic summary.
func (g *PopupGuard) String() string {
	if g == nil {
		return "PopupGuard(nil)"
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	return fmt.Sprintf("PopupGuard{policy:%s count:%d}", g.policy, g.count)
}
