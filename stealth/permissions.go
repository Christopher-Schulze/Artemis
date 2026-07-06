package stealth

import (
	"fmt"
	"sync"
)

// PermissionState enumerates the permission states
// (spec L4091: PASSIVE-ONLY override. permissions.query() -> "prompt"
// (passive, safe). requestPermission() NOT touched. Real requests ->
// "denied" (headless can't ask = natural)).
type PermissionState string

const (
	// PermissionStatePrompt is the "prompt" state (passive, safe).
	// Returned by permissions.query() for all permissions
	// (spec L4091).
	PermissionStatePrompt PermissionState = "prompt"
	// PermissionStateDenied is the "denied" state. Returned by
	// requestPermission() (headless can't ask = natural)
	// (spec L4091).
	PermissionStateDenied PermissionState = "denied"
	// PermissionStateGranted is the "granted" state (NOT used in
	// headless stealth mode).
	PermissionStateGranted PermissionState = "granted"
)

// PermissionName is the name of a browser permission
// (spec L4091: permissions.query() accepts a name).
type PermissionName string

const (
	PermissionGeolocation    PermissionName = "geolocation"
	PermissionNotifications  PermissionName = "notifications"
	PermissionCamera         PermissionName = "camera"
	PermissionMicrophone     PermissionName = "microphone"
	PermissionClipboardRead  PermissionName = "clipboard-read"
	PermissionClipboardWrite PermissionName = "clipboard-write"
)

// PermissionQueryResult is the result of permissions.query()
// (spec L4091: PASSIVE-ONLY override).
type PermissionQueryResult struct {
	State    PermissionState `json:"state"`
	Name     PermissionName  `json:"name"`
	Onchange func()          `json:"-"`
}

// PermissionAPI implements the PASSIVE-ONLY Permission API override
// (spec L4091: permissions.query() -> "prompt" (passive, safe).
// requestPermission() NOT touched. Real requests -> "denied"
// (headless can't ask = natural). Activation: StealthStealth (Patch 7)).
type PermissionAPI struct {
	mu     sync.RWMutex
	active bool
}

// NewPermissionAPI creates a new Permission API override instance
// (spec L4091).
func NewPermissionAPI() *PermissionAPI {
	return &PermissionAPI{active: false}
}

// Activate enables the PASSIVE-ONLY override
// (spec L4091: Activation: StealthStealth (Patch 7)).
func (p *PermissionAPI) Activate() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.active = true
}

// Deactivate disables the override.
func (p *PermissionAPI) Deactivate() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.active = false
}

// IsActive reports whether the override is active.
func (p *PermissionAPI) IsActive() bool {
	if p == nil {
		return false
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.active
}

// Query implements permissions.query() -> "prompt" (passive, safe)
// (spec L4091: PASSIVE-ONLY override. permissions.query() -> "prompt").
// When the override is NOT active, returns "denied" (natural headless).
func (p *PermissionAPI) Query(name PermissionName) PermissionQueryResult {
	if p == nil {
		return PermissionQueryResult{State: PermissionStateDenied, Name: name}
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.active {
		// PASSIVE-ONLY: query() -> "prompt" (safe, passive)
		return PermissionQueryResult{
			State: PermissionStatePrompt,
			Name:  name,
		}
	}
	// Not active: natural headless behavior -> "denied"
	return PermissionQueryResult{
		State: PermissionStateDenied,
		Name:  name,
	}
}

// Request implements requestPermission() -> "denied"
// (spec L4091: requestPermission() NOT touched. Real requests ->
// "denied" (headless can't ask = natural)).
// This is NOT an override - it's the natural headless behavior.
func (p *PermissionAPI) Request(name PermissionName) PermissionState {
	// Always returns "denied" regardless of active state
	// (spec L4091: headless can't ask = natural).
	return PermissionStateDenied
}

// QueryAll queries all known permissions and returns their states
// (spec L4091).
func (p *PermissionAPI) QueryAll() map[PermissionName]PermissionState {
	if p == nil {
		return nil
	}
	all := []PermissionName{
		PermissionGeolocation,
		PermissionNotifications,
		PermissionCamera,
		PermissionMicrophone,
		PermissionClipboardRead,
		PermissionClipboardWrite,
	}
	result := make(map[PermissionName]PermissionState, len(all))
	for _, name := range all {
		result[name] = p.Query(name).State
	}
	return result
}

// IsPassiveOnly reports whether the override is PASSIVE-ONLY
// (spec L4091: PASSIVE-ONLY override). Always true - requestPermission
// is never overridden.
func (p *PermissionAPI) IsPassiveOnly() bool {
	return true
}

// String returns a diagnostic summary.
func (p *PermissionAPI) String() string {
	if p == nil {
		return "PermissionAPI(nil)"
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return fmt.Sprintf("PermissionAPI{active:%v passive-only:%v}", p.active, true)
}
