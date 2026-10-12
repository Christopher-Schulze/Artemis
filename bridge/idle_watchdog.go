package bridge

import (
	"sync"
	"time"
)

// IdleWatchdogConfig controls per-workspace idle monitoring (spec L4322).
// A background check runs every CheckIntervalSeconds; workspaces idle for
// longer than IdleThresholdSeconds are reported and (via AutoStop) marked
// auto-stopped.
type IdleWatchdogConfig struct {
	Enabled              bool
	CheckIntervalSeconds int
	IdleThresholdSeconds int
}

// DefaultIdleWatchdogConfig is the canonical idle-watchdog configuration:
// a 60s check interval and a 1800s (30 minute) idle threshold.
var DefaultIdleWatchdogConfig = IdleWatchdogConfig{
	Enabled:              true,
	CheckIntervalSeconds: 60,
	IdleThresholdSeconds: 1800,
}

// WorkspaceState records the monitoring state for a workspace.
type WorkspaceState struct {
	WorkspaceID string
	LastActive  time.Time
	AutoStopped bool
}

// IdleWatchdogStats reports counters for the watchdog.
type IdleWatchdogStats struct {
	TotalChecks      int64
	TotalAutoStopped int64
	TotalRegistered  int64
}

// IdleWatchdog monitors per-workspace idle time and reports/auto-stops
// workspaces that exceed the idle threshold (spec L4322). It is thread-safe.
type IdleWatchdog struct {
	mu         sync.RWMutex
	config     IdleWatchdogConfig
	workspaces map[string]*WorkspaceState
	stats      IdleWatchdogStats
	now        func() time.Time
}

// NewIdleWatchdog builds an IdleWatchdog with the supplied config. Zero-value
// intervals fall back to DefaultIdleWatchdogConfig.
func NewIdleWatchdog(config IdleWatchdogConfig) *IdleWatchdog {
	if config.CheckIntervalSeconds <= 0 {
		config.CheckIntervalSeconds = DefaultIdleWatchdogConfig.CheckIntervalSeconds
	}
	if config.IdleThresholdSeconds <= 0 {
		config.IdleThresholdSeconds = DefaultIdleWatchdogConfig.IdleThresholdSeconds
	}
	return &IdleWatchdog{
		config:     config,
		workspaces: make(map[string]*WorkspaceState),
		now:        time.Now,
	}
}

// RegisterWorkspace registers a workspace for monitoring. Re-registering an
// already-tracked workspace is a no-op (its existing state is preserved).
func (w *IdleWatchdog) RegisterWorkspace(workspaceID string) {
	if workspaceID == "" {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, ok := w.workspaces[workspaceID]; ok {
		return
	}
	w.workspaces[workspaceID] = &WorkspaceState{
		WorkspaceID: workspaceID,
		LastActive:  w.now(),
	}
	w.stats.TotalRegistered++
}

// UnregisterWorkspace removes a workspace from monitoring. Returns true if
// the workspace was registered.
func (w *IdleWatchdog) UnregisterWorkspace(workspaceID string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, ok := w.workspaces[workspaceID]; !ok {
		return false
	}
	delete(w.workspaces, workspaceID)
	return true
}

// RecordActivity updates the last-active time for a workspace. If the
// workspace is not registered this is a no-op.
func (w *IdleWatchdog) RecordActivity(workspaceID string) {
	if workspaceID == "" {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	st, ok := w.workspaces[workspaceID]
	if !ok {
		return
	}
	st.LastActive = w.now()
}

// CheckIdle returns the workspace IDs whose idle time exceeds the threshold.
// It does not modify any workspace state (does not auto-stop). Returns nil
// when disabled.
func (w *IdleWatchdog) CheckIdle() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.stats.TotalChecks++
	if !w.config.Enabled {
		return nil
	}
	threshold := time.Duration(w.config.IdleThresholdSeconds) * time.Second
	now := w.now()
	var idle []string
	for id, st := range w.workspaces {
		if now.Sub(st.LastActive) > threshold {
			idle = append(idle, id)
		}
	}
	return idle
}

// AutoStop marks workspaces whose idle time exceeds the threshold as
// auto-stopped and returns the IDs that were newly auto-stopped. Workspaces
// already marked auto-stopped are skipped. Returns nil when disabled.
func (w *IdleWatchdog) AutoStop() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.config.Enabled {
		return nil
	}
	threshold := time.Duration(w.config.IdleThresholdSeconds) * time.Second
	now := w.now()
	var stopped []string
	for id, st := range w.workspaces {
		if st.AutoStopped {
			continue
		}
		if now.Sub(st.LastActive) > threshold {
			st.AutoStopped = true
			stopped = append(stopped, id)
			w.stats.TotalAutoStopped++
		}
	}
	return stopped
}

// GetWorkspaceState returns a copy of the state for a workspace, or false if
// the workspace is not registered.
func (w *IdleWatchdog) GetWorkspaceState(workspaceID string) (WorkspaceState, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	st, ok := w.workspaces[workspaceID]
	if !ok {
		return WorkspaceState{}, false
	}
	return *st, true
}

// GetActiveWorkspaces returns the IDs of workspaces that are not auto-stopped.
func (w *IdleWatchdog) GetActiveWorkspaces() []string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := make([]string, 0, len(w.workspaces))
	for id, st := range w.workspaces {
		if !st.AutoStopped {
			out = append(out, id)
		}
	}
	return out
}

// IsAutoStopped reports whether a workspace is currently auto-stopped.
func (w *IdleWatchdog) IsAutoStopped(workspaceID string) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	st, ok := w.workspaces[workspaceID]
	if !ok {
		return false
	}
	return st.AutoStopped
}

// Reset clears the auto-stopped state for a workspace and updates its
// last-active time to now. Returns true if the workspace was registered.
func (w *IdleWatchdog) Reset(workspaceID string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	st, ok := w.workspaces[workspaceID]
	if !ok {
		return false
	}
	st.AutoStopped = false
	st.LastActive = w.now()
	return true
}

// Stats returns a snapshot of the watchdog counters.
func (w *IdleWatchdog) Stats() IdleWatchdogStats {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.stats
}

// Config returns a copy of the watchdog configuration.
func (w *IdleWatchdog) Config() IdleWatchdogConfig {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.config
}

// SetNow replaces the clock used for last-active timestamps. Intended for
// tests; not safe to call concurrently with RecordActivity/CheckIdle.
func (w *IdleWatchdog) SetNow(fn func() time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if fn == nil {
		fn = time.Now
	}
	w.now = fn
}
