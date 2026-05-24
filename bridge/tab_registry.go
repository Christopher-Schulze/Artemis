package bridge

import (
	"fmt"
	"sync"
)

// Tab describes one browser target with CDP context linkage.
type Tab struct {
	ID        string
	URL       string
	ContextID string
	ParentID  string
}

// TabRegistry owns active tabs for executor lifecycle (spec pinchtab tab_manager floor).
type TabRegistry struct {
	mu   sync.RWMutex
	tabs map[string]Tab
}

func NewTabRegistry() *TabRegistry {
	return &TabRegistry{tabs: make(map[string]Tab)}
}

func (r *TabRegistry) Register(tab Tab) error {
	if tab.ID == "" {
		return fmt.Errorf("tab registry: id required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tabs[tab.ID] = tab
	return nil
}

func (r *TabRegistry) Get(id string) (Tab, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tabs[id]
	return t, ok
}

func (r *TabRegistry) Remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.tabs, id)
}

func (r *TabRegistry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tabs)
}

func (r *TabRegistry) List() []Tab {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Tab, 0, len(r.tabs))
	for _, t := range r.tabs {
		out = append(out, t)
	}
	return out
}
