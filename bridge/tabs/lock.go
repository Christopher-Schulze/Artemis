package tabs

import (
	"context"
	"fmt"
	"sort"
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
	mu      sync.Mutex
	owners  map[string]string // tabID -> owner description
	changed map[string]chan struct{}
	waiters map[string]int
}

// NewTabLock creates a new TabLock manager
// (spec L4021: per-tab locking).
func NewTabLock() *TabLock {
	return &TabLock{
		owners:  make(map[string]string),
		changed: make(map[string]chan struct{}),
		waiters: make(map[string]int),
	}
}

// Lock acquires a lock for a specific tab
// (spec L4021: per-tab locking).
func (l *TabLock) Lock(tabID, owner string) {
	if err := l.LockContext(context.Background(), tabID, owner); err != nil {
		return
	}
}

// LockContext acquires a tab without polling and honors cancellation while queued.
func (l *TabLock) LockContext(ctx context.Context, tabID, owner string) error {
	if ctx == nil {
		return fmt.Errorf("tab lock: context required")
	}
	l.mu.Lock()
	l.waiters[tabID]++
	defer func() {
		l.waiters[tabID]--
		if l.waiters[tabID] == 0 {
			delete(l.waiters, tabID)
			delete(l.changed, tabID)
		}
		l.mu.Unlock()
	}()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, held := l.owners[tabID]; !held {
			l.owners[tabID] = owner
			return nil
		}
		wait := l.changed[tabID]
		if wait == nil {
			wait = make(chan struct{})
			l.changed[tabID] = wait
		}
		l.mu.Unlock()
		select {
		case <-ctx.Done():
		case <-wait:
		}
		l.mu.Lock()
	}
}

// Unlock releases the lock for a specific tab
// (spec L4021: per-tab locking).
func (l *TabLock) Unlock(tabID string) {
	l.mu.Lock()
	_, ok := l.owners[tabID]
	if !ok {
		l.mu.Unlock()
		return
	}
	delete(l.owners, tabID)
	if ch := l.changed[tabID]; ch != nil {
		close(ch)
		delete(l.changed, tabID)
	}
	l.mu.Unlock()
}

// TryLock attempts to acquire a lock without blocking
// (spec L4021: per-tab locking).
func (l *TabLock) TryLock(tabID, owner string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, held := l.owners[tabID]; held {
		return false
	}
	l.owners[tabID] = owner
	return true
}

// LockWithTimeout attempts to acquire a lock with a timeout. Waiters block on
// a per-tab channel closed by Unlock instead of polling on a fixed interval.
func (l *TabLock) LockWithTimeout(tabID, owner string, timeout time.Duration) bool {
	if timeout <= 0 {
		return l.TryLock(tabID, owner)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return l.LockContext(ctx, tabID, owner) == nil
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
	defer l.mu.Unlock()
	_, held := l.owners[tabID]
	return held
}

// Remove removes a tab lock entry (only if not currently locked)
// (spec L4021: per-tab locking).
func (l *TabLock) Remove(tabID string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.owners[tabID]; ok {
		return false // still locked
	}
	if l.waiters[tabID] != 0 {
		return false
	}
	delete(l.changed, tabID)
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
	sort.Strings(result)
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
