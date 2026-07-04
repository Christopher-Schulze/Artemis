package tabs

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// manager.go (spec L4021: bridge/tabs/manager.go - tab registry +
// lifecycle).
//
// Multi-tab management: tab registry and lifecycle management.
// Tracks all open tabs, their state, and provides CRUD operations.

// TabState enumerates tab lifecycle states
// (spec L4021: tab registry + lifecycle).
type TabState string

const (
	TabStateOpen    TabState = "open"
	TabStateLoading TabState = "loading"
	TabStateActive  TabState = "active"
	TabStateIdle    TabState = "idle"
	TabStateClosed  TabState = "closed"
	TabStateCrashed TabState = "crashed"
)

// Tab represents a browser tab in the registry
// (spec L4021: tab registry + lifecycle).
// TabEntry is the spec-mandated tab entry with full lifecycle fields
// (spec L4224: TabEntry: context + cancelFunc + CDPID + timestamps +
// policy state + process_id + owner_ref=turn|subagent|connector|ui).
type Tab struct {
	ID         string    `json:"id"`
	UserID     string    `json:"userId"`
	URL        string    `json:"url"`
	Title      string    `json:"title"`
	State      TabState  `json:"state"`
	CreatedAt  time.Time `json:"createdAt"`
	LastActive time.Time `json:"lastActive"`
	Index      int       `json:"index"`
	// ProcessSpec lifecycle fields (spec L4224-L4226)
	CDPID        string `json:"cdpId,omitempty"`        // CDP target ID
	ProcessID    string `json:"processId,omitempty"`    // ProcessSpec ID
	OwnerRef     string `json:"ownerRef,omitempty"`     // turn|subagent|connector|ui
	PriorityLane string `json:"priorityLane,omitempty"` // scheduling priority
	PolicyState  string `json:"policyState,omitempty"`  // policy engine state
	MailboxCap   int    `json:"mailboxCap,omitempty"`   // default 32
}

// TabRegistry is the tab registry that tracks all open tabs
// (spec L4021: tab registry + lifecycle).
type TabRegistry struct {
	mu     sync.RWMutex
	tabs   map[string]*Tab
	nextID atomic.Int64
}

// NewTabRegistry creates a new tab registry
// (spec L4021: tab registry + lifecycle).
func NewTabRegistry() *TabRegistry {
	return &TabRegistry{
		tabs: make(map[string]*Tab),
	}
}

// CreateTab creates a new tab and adds it to the registry
// (spec L4021: tab registry + lifecycle).
func (r *TabRegistry) CreateTab(userID, url string) *Tab {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := fmt.Sprintf("tab-%d", r.nextID.Add(1))
	tab := &Tab{
		ID:         id,
		UserID:     userID,
		URL:        url,
		State:      TabStateOpen,
		CreatedAt:  time.Now(),
		LastActive: time.Now(),
		Index:      len(r.tabs),
	}
	r.tabs[id] = tab
	return tab
}

// GetTab retrieves a tab by ID
// (spec L4021: tab registry + lifecycle).
func (r *TabRegistry) GetTab(id string) (*Tab, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tab, ok := r.tabs[id]
	return tab, ok
}

// CloseTab marks a tab as closed and removes it from the registry
// (spec L4021: tab registry + lifecycle).
func (r *TabRegistry) CloseTab(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	tab, ok := r.tabs[id]
	if !ok {
		return false
	}
	tab.State = TabStateClosed
	delete(r.tabs, id)
	return true
}

// ListTabs returns all tabs for a given user
// (spec L4021: tab registry + lifecycle).
func (r *TabRegistry) ListTabs(userID string) []*Tab {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []*Tab
	for _, tab := range r.tabs {
		if tab.UserID == userID {
			result = append(result, tab)
		}
	}
	return result
}

// UpdateTabState updates the state of a tab
// (spec L4021: tab registry + lifecycle).
func (r *TabRegistry) UpdateTabState(id string, state TabState) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	tab, ok := r.tabs[id]
	if !ok {
		return false
	}
	tab.State = state
	tab.LastActive = time.Now()
	return true
}

// UpdateTabURL updates the URL and title of a tab
// (spec L4021: tab registry + lifecycle).
func (r *TabRegistry) UpdateTabURL(id, url, title string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	tab, ok := r.tabs[id]
	if !ok {
		return false
	}
	tab.URL = url
	tab.Title = title
	tab.LastActive = time.Now()
	return true
}

// Count returns the total number of tabs
// (spec L4021: tab registry + lifecycle).
func (r *TabRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tabs)
}

// CloseAll closes all tabs for a given user
// (spec L4021: tab registry + lifecycle).
func (r *TabRegistry) CloseAll(userID string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	closed := 0
	for id, tab := range r.tabs {
		if tab.UserID == userID {
			tab.State = TabStateClosed
			delete(r.tabs, id)
			closed++
		}
	}
	return closed
}

// IsValidTabState reports whether a tab state is valid
// (spec L4021: tab registry + lifecycle).
func IsValidTabState(state TabState) bool {
	switch state {
	case TabStateOpen, TabStateLoading, TabStateActive, TabStateIdle, TabStateClosed, TabStateCrashed:
		return true
	}
	return false
}

// String returns a diagnostic summary.
func (t Tab) String() string {
	return fmt.Sprintf("Tab{id:%s user:%s url:%s state:%s}", t.ID, t.UserID, t.URL, t.State)
}
