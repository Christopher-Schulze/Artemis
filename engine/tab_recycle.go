// Package engine implements tab recycling with about:blank navigation
// to preserve TLS sessions (spec ss28.15.8 P5.2).
//
// P5.2 Tab Recycling: about:blank instead of close.
// Cleared: cookies, localStorage.
// Preserved: DNS cache, TLS sessions, HTTP/2 connections.
// 0ms vs 200-500ms setup.
package engine

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// TabState describes the lifecycle state of a browser tab.
type TabState int

const (
	TabStateActive   TabState = iota // tab is in use
	TabStateIdle                     // tab is idle, available for recycling
	TabStateRecycled                 // tab has been recycled to about:blank
	TabStateClosed                   // tab is permanently closed
)

// String returns the tab state name.
func (s TabState) String() string {
	switch s {
	case TabStateActive:
		return "active"
	case TabStateIdle:
		return "idle"
	case TabStateRecycled:
		return "recycled"
	case TabStateClosed:
		return "closed"
	default:
		return "unknown"
	}
}

// Tab represents a browser tab in the recycling pool.
type Tab struct {
	ID            string
	URL           string
	State         TabState
	LastUsed      time.Time
	RecycledCount int
	mu            sync.Mutex
}

// TabRecycler manages a pool of browser tabs for recycling.
// Instead of closing tabs (200-500ms setup cost), tabs are navigated
// to about:blank (0ms) which clears cookies and localStorage while
// preserving DNS cache, TLS sessions, and HTTP/2 connections.
type TabRecycler struct {
	mu       sync.Mutex
	tabs     map[string]*Tab
	maxTabs  int
	recycled atomic.Int64
	closed   atomic.Int64
}

// NewTabRecycler creates a recycler with the given max tab pool size.
func NewTabRecycler(maxTabs int) *TabRecycler {
	if maxTabs <= 0 {
		maxTabs = 8
	}
	return &TabRecycler{
		tabs:    make(map[string]*Tab),
		maxTabs: maxTabs,
	}
}

// Register adds a new tab to the recycler pool.
func (r *TabRecycler) Register(tabID, url string) *Tab {
	r.mu.Lock()
	defer r.mu.Unlock()
	tab := &Tab{
		ID:       tabID,
		URL:      url,
		State:    TabStateActive,
		LastUsed: time.Now(),
	}
	r.tabs[tabID] = tab
	return tab
}

// Get returns a tab by ID.
func (r *TabRecycler) Get(tabID string) (*Tab, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	tab, ok := r.tabs[tabID]
	return tab, ok
}

// Recycle navigates a tab to about:blank instead of closing it.
// This clears cookies and localStorage while preserving DNS cache,
// TLS sessions, and HTTP/2 connections.
//
// Per spec P5.2: 0ms vs 200-500ms setup.
// The navigateFn is called to perform the actual about:blank navigation
// via CDP. Returns the updated tab or an error.
func (r *TabRecycler) Recycle(ctx context.Context, tabID string, navigateFn func(ctx context.Context, url string) error) (*Tab, error) {
	if navigateFn == nil {
		return nil, fmt.Errorf("tab recycler: navigate function required")
	}

	r.mu.Lock()
	tab, ok := r.tabs[tabID]
	r.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("tab recycler: tab %s not found", tabID)
	}

	tab.mu.Lock()
	if tab.State == TabStateClosed {
		tab.mu.Unlock()
		return nil, fmt.Errorf("tab recycler: tab %s is closed", tabID)
	}
	tab.mu.Unlock()

	// Navigate to about:blank (the actual CDP call)
	if err := navigateFn(ctx, "about:blank"); err != nil {
		return nil, fmt.Errorf("tab recycler: navigate failed: %w", err)
	}

	tab.mu.Lock()
	tab.State = TabStateRecycled
	tab.URL = "about:blank"
	tab.RecycledCount++
	tab.LastUsed = time.Now()
	tab.mu.Unlock()

	r.recycled.Add(1)
	return tab, nil
}

// Reuse marks a recycled tab as active again for a new URL.
// The tab must be in Recycled state. Returns an error if the tab
// is not recycled.
func (r *TabRecycler) Reuse(tabID, newURL string) (*Tab, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	tab, ok := r.tabs[tabID]
	if !ok {
		return nil, fmt.Errorf("tab recycler: tab %s not found", tabID)
	}

	tab.mu.Lock()
	defer tab.mu.Unlock()
	if tab.State != TabStateRecycled && tab.State != TabStateIdle {
		return nil, fmt.Errorf("tab recycler: tab %s is not recycled (state=%s)", tabID, tab.State)
	}

	tab.State = TabStateActive
	tab.URL = newURL
	tab.LastUsed = time.Now()
	return tab, nil
}

// Close permanently closes a tab and removes it from the pool.
func (r *TabRecycler) Close(tabID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	tab, ok := r.tabs[tabID]
	if !ok {
		return fmt.Errorf("tab recycler: tab %s not found", tabID)
	}

	tab.mu.Lock()
	tab.State = TabStateClosed
	tab.mu.Unlock()

	delete(r.tabs, tabID)
	r.closed.Add(1)
	return nil
}

// Available returns a recycled tab that can be reused, or nil if none.
func (r *TabRecycler) Available() *Tab {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, tab := range r.tabs {
		tab.mu.Lock()
		if tab.State == TabStateRecycled {
			tab.mu.Unlock()
			return tab
		}
		tab.mu.Unlock()
	}
	return nil
}

// All returns all tabs in the pool.
func (r *TabRecycler) All() []*Tab {
	r.mu.Lock()
	defer r.mu.Unlock()
	tabs := make([]*Tab, 0, len(r.tabs))
	for _, tab := range r.tabs {
		tabs = append(tabs, tab)
	}
	return tabs
}

// Count returns the number of tabs in each state.
func (r *TabRecycler) Count() (active, idle, recycled, closed int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, tab := range r.tabs {
		tab.mu.Lock()
		switch tab.State {
		case TabStateActive:
			active++
		case TabStateIdle:
			idle++
		case TabStateRecycled:
			recycled++
		case TabStateClosed:
			closed++
		}
		tab.mu.Unlock()
	}
	closed = int(r.closed.Load())
	return
}

// RecycledCount returns the total number of recycle operations performed.
func (r *TabRecycler) RecycledCount() int64 {
	return r.recycled.Load()
}

// ClosedCount returns the total number of tabs permanently closed.
func (r *TabRecycler) ClosedCount() int64 {
	return r.closed.Load()
}

// MaxTabs returns the maximum pool size.
func (r *TabRecycler) MaxTabs() int {
	return r.maxTabs
}

// SetIdle marks a tab as idle (available for recycling but not yet recycled).
func (r *TabRecycler) SetIdle(tabID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	tab, ok := r.tabs[tabID]
	if !ok {
		return fmt.Errorf("tab recycler: tab %s not found", tabID)
	}
	tab.mu.Lock()
	tab.State = TabStateIdle
	tab.LastUsed = time.Now()
	tab.mu.Unlock()
	return nil
}
