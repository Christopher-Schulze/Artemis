package tabs

import (
	"fmt"
	"sync"
	"time"
)

// lock.go (spec L4021: bridge/tabs/lock.go - per-tab locking).
//
// Multi-tab management: per-tab locking for concurrent access.
// Ensures only one goroutine can operate on a tab at a time.

// TabLock provides per-tab locking
// (spec L4021: per-tab locking).
type TabLock struct {
	mu     sync.Mutex
	locks  map[string]*sync.Mutex
	owners map[string]string // tabID -> owner description
}

// NewTabLock creates a new TabLock manager
// (spec L4021: per-tab locking).
func NewTabLock() *TabLock {
	return &TabLock{
		locks:  make(map[string]*sync.Mutex),
		owners: make(map[string]string),
	}
}

// Lock acquires a lock for a specific tab
// (spec L4021: per-tab locking).
func (l *TabLock) Lock(tabID, owner string) {
	l.mu.Lock()
	lock, ok := l.locks[tabID]
	if !ok {
		lock = &sync.Mutex{}
		l.locks[tabID] = lock
	}
	l.mu.Unlock()
	lock.Lock()
	l.mu.Lock()
	l.owners[tabID] = owner
	l.mu.Unlock()
}

// Unlock releases the lock for a specific tab
// (spec L4021: per-tab locking).
func (l *TabLock) Unlock(tabID string) {
	l.mu.Lock()
	lock, ok := l.locks[tabID]
	if !ok {
		l.mu.Unlock()
		return
	}
	delete(l.owners, tabID)
	l.mu.Unlock()
	lock.Unlock()
}

// TryLock attempts to acquire a lock without blocking
// (spec L4021: per-tab locking).
func (l *TabLock) TryLock(tabID, owner string) bool {
	l.mu.Lock()
	lock, ok := l.locks[tabID]
	if !ok {
		lock = &sync.Mutex{}
		l.locks[tabID] = lock
	}
	l.mu.Unlock()
	if !lock.TryLock() {
		return false
	}
	l.mu.Lock()
	l.owners[tabID] = owner
	l.mu.Unlock()
	return true
}

// LockWithTimeout attempts to acquire a lock with a timeout
// (spec L4021: per-tab locking).
func (l *TabLock) LockWithTimeout(tabID, owner string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if l.TryLock(tabID, owner) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Owner returns the current owner of a tab lock
// (spec L4021: per-tab locking).
func (l *TabLock) Owner(tabID string) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.owners[tabID]
}

// IsLocked reports whether a tab is currently locked
// (spec L4021: per-tab locking).
func (l *TabLock) IsLocked(tabID string) bool {
	l.mu.Lock()
	lock, ok := l.locks[tabID]
	l.mu.Unlock()
	if !ok {
		return false
	}
	return !lock.TryLock()
}

// Remove removes a tab lock entry (only if not currently locked)
// (spec L4021: per-tab locking).
func (l *TabLock) Remove(tabID string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.owners[tabID]; ok {
		return false // still locked
	}
	delete(l.locks, tabID)
	return true
}

// LockedTabs returns the IDs of all currently locked tabs
// (spec L4021: per-tab locking).
func (l *TabLock) LockedTabs() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var result []string
	for tabID := range l.owners {
		result = append(result, tabID)
	}
	return result
}

// Count returns the number of currently locked tabs
// (spec L4021: per-tab locking).
func (l *TabLock) Count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.owners)
}

// String returns a diagnostic summary.
func (l *TabLock) String() string {
	return fmt.Sprintf("TabLock{locked:%d}", l.Count())
}
