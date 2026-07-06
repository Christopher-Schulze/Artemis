package bridge

import (
	"sync"
	"time"
)

// InactivityConfig controls session inactivity cleanup (spec L4281).
type InactivityConfig struct {
	IdleTimeout   time.Duration
	CheckInterval time.Duration
	Enabled       bool
}

// DefaultInactivityConfig is the canonical inactivity configuration.
var DefaultInactivityConfig = InactivityConfig{
	IdleTimeout:   5 * time.Minute,
	CheckInterval: 30 * time.Second,
	Enabled:       true,
}

// SessionActivity records the last-active timestamp for a session.
type SessionActivity struct {
	LastActive time.Time
	SessionID  string
}

// InactivityMonitor tracks session activity and reaps idle sessions.
type InactivityMonitor struct {
	mu       sync.RWMutex
	config   InactivityConfig
	sessions map[string]*SessionActivity
	now      func() time.Time
}

// NewInactivityMonitor builds an InactivityMonitor with the supplied config.
func NewInactivityMonitor(config InactivityConfig) *InactivityMonitor {
	if config.IdleTimeout <= 0 {
		config.IdleTimeout = DefaultInactivityConfig.IdleTimeout
	}
	if config.CheckInterval <= 0 {
		config.CheckInterval = DefaultInactivityConfig.CheckInterval
	}
	return &InactivityMonitor{
		config:   config,
		sessions: make(map[string]*SessionActivity),
		now:      time.Now,
	}
}

// RegisterActivity marks a session as active at the current time.
func (m *InactivityMonitor) RegisterActivity(sessionID string) {
	if sessionID == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if sa, ok := m.sessions[sessionID]; ok {
		sa.LastActive = m.now()
		return
	}
	m.sessions[sessionID] = &SessionActivity{
		LastActive: m.now(),
		SessionID:  sessionID,
	}
}

// CheckInactive returns the session IDs that have been idle longer than
// IdleTimeout without removing them.
func (m *InactivityMonitor) CheckInactive() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.config.Enabled {
		return nil
	}
	threshold := m.now().Add(-m.config.IdleTimeout)
	var expired []string
	for id, sa := range m.sessions {
		if sa.LastActive.Before(threshold) {
			expired = append(expired, id)
		}
	}
	return expired
}

// CleanupExpired removes expired sessions and returns the count removed.
func (m *InactivityMonitor) CleanupExpired() int {
	expired := m.CheckInactive()
	if len(expired) == 0 {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, id := range expired {
		if _, ok := m.sessions[id]; ok {
			delete(m.sessions, id)
			count++
		}
	}
	return count
}

// GetActiveSessions returns the number of tracked sessions (regardless of
// idle state).
func (m *InactivityMonitor) GetActiveSessions() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// GetSessionActivity returns a copy of the activity record for a session,
// or nil if the session is not tracked.
func (m *InactivityMonitor) GetSessionActivity(sessionID string) *SessionActivity {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if sa, ok := m.sessions[sessionID]; ok {
		copy := *sa
		return &copy
	}
	return nil
}

// ReapOrphans removes tracked sessions that are not present in the supplied
// knownSessions list. This is the startup orphan reap: any session the
// monitor knows about but the caller no longer recognizes is removed.
// Returns the number of orphans reaped.
func (m *InactivityMonitor) ReapOrphans(knownSessions []string) int {
	known := make(map[string]struct{}, len(knownSessions))
	for _, id := range knownSessions {
		known[id] = struct{}{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for id := range m.sessions {
		if _, ok := known[id]; !ok {
			delete(m.sessions, id)
			count++
		}
	}
	return count
}

// ForceExpire marks a session as expired immediately so the next
// CheckInactive/CleanupExpired will treat it as idle.
func (m *InactivityMonitor) ForceExpire(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if sa, ok := m.sessions[sessionID]; ok {
		sa.LastActive = m.now().Add(-m.config.IdleTimeout - time.Second)
	}
}

// SetNow replaces the clock used for activity timestamps. Intended for
// tests; not safe to call concurrently with RegisterActivity.
func (m *InactivityMonitor) SetNow(fn func() time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if fn == nil {
		fn = time.Now
	}
	m.now = fn
}

// Config returns a copy of the monitor's config.
func (m *InactivityMonitor) Config() InactivityConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config
}
