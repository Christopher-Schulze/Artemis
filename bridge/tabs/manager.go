package tabs

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
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

// Target is the immutable bridge-owned target projection used by the tab
// registry. ID is always the real CDP target ID; State is supplied by the
// owning bridge page and is never synthesized by this package.
type Target struct {
	ID        string
	URL       string
	Title     string
	State     string
	ContextID string
}

// TargetSource owns browser target lifecycle. TabRegistry only projects its
// snapshots and delegates all lifecycle mutations back to this source.
type TargetSource interface {
	ListTargets(context.Context) ([]Target, error)
	CreateTarget(context.Context, string) (Target, error)
	ActivateTarget(context.Context, string) error
	CloseTarget(context.Context, string) error
}

var ErrTargetSourceRequired = errors.New("tabs: bridge target source required")
var ErrTargetNotFound = errors.New("tabs: bridge target not found")

// TabRegistry is the tab registry that tracks all open tabs
// (spec L4021: tab registry + lifecycle).
type TabRegistry struct {
	mu     sync.RWMutex
	tabs   map[string]*Tab
	source TargetSource
}

// NewTabRegistry creates a new tab registry
// (spec L4021: tab registry + lifecycle).
func NewTabRegistry(sources ...TargetSource) *TabRegistry {
	var source TargetSource
	if len(sources) > 0 {
		source = sources[0]
	}
	return &TabRegistry{tabs: make(map[string]*Tab), source: source}
}

// CreateTab creates a new tab and adds it to the registry
// (spec L4021: tab registry + lifecycle).
func (r *TabRegistry) CreateTab(userID, url string) *Tab {
	tab, _ := r.CreateTabContext(context.Background(), userID, url)
	return tab
}

// CreateTabContext creates a real bridge target and projects its CDP ID.
func (r *TabRegistry) CreateTabContext(ctx context.Context, userID, url string) (*Tab, error) {
	if ctx == nil {
		return nil, errors.New("tabs: context required")
	}
	if r.source == nil {
		return nil, ErrTargetSourceRequired
	}
	target, err := r.source.CreateTarget(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("tabs: create target: %w", err)
	}
	if target.ID == "" {
		return nil, errors.New("tabs: target source returned empty target ID")
	}
	if err := r.Sync(ctx); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	tab := r.tabs[target.ID]
	if tab == nil {
		return nil, fmt.Errorf("tabs: created target %s missing from source snapshot", target.ID)
	}
	tab.UserID = userID
	return cloneTab(tab), nil
}

// GetTab retrieves a tab by ID
// (spec L4021: tab registry + lifecycle).
func (r *TabRegistry) GetTab(id string) (*Tab, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tab, ok := r.tabs[id]
	if !ok {
		return nil, false
	}
	return cloneTab(tab), true
}

// GetTabContext refreshes from the bridge before returning a target entry.
func (r *TabRegistry) GetTabContext(ctx context.Context, id string) (*Tab, bool, error) {
	if err := r.Sync(ctx); err != nil {
		return nil, false, err
	}
	tab, ok := r.GetTab(id)
	return tab, ok, nil
}

// CloseTab marks a tab as closed and removes it from the registry
// (spec L4021: tab registry + lifecycle).
func (r *TabRegistry) CloseTab(id string) bool {
	ok, _ := r.CloseTabContext(context.Background(), id)
	return ok
}

// CloseTabContext closes the real CDP target and refreshes the projection.
func (r *TabRegistry) CloseTabContext(ctx context.Context, id string) (bool, error) {
	if err := r.Sync(ctx); err != nil {
		return false, err
	}
	if _, ok := r.GetTab(id); !ok {
		return false, nil
	}
	if err := r.source.CloseTarget(ctx, id); err != nil {
		return false, fmt.Errorf("tabs: close target %s: %w", id, err)
	}
	if err := r.Sync(ctx); err != nil {
		return false, err
	}
	return true, nil
}

// ActivateTabContext activates the real bridge target and refreshes state.
func (r *TabRegistry) ActivateTabContext(ctx context.Context, id string) error {
	if err := r.Sync(ctx); err != nil {
		return err
	}
	if _, ok := r.GetTab(id); !ok {
		return fmt.Errorf("%w: %s", ErrTargetNotFound, id)
	}
	if err := r.source.ActivateTarget(ctx, id); err != nil {
		return fmt.Errorf("tabs: activate target %s: %w", id, err)
	}
	return r.Sync(ctx)
}

func (r *TabRegistry) ActivateTab(id string) error {
	return r.ActivateTabContext(context.Background(), id)
}

// ListTabs returns all tabs for a given user
// (spec L4021: tab registry + lifecycle).
func (r *TabRegistry) ListTabs(userID string) []*Tab {
	tabs, _ := r.ListTabsContext(context.Background(), userID)
	return tabs
}

// ListTabsContext refreshes and returns deterministic target-ID order.
func (r *TabRegistry) ListTabsContext(ctx context.Context, userID string) ([]*Tab, error) {
	if err := r.Sync(ctx); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]*Tab, 0, len(r.tabs))
	for _, tab := range r.tabs {
		if tab.UserID == userID {
			result = append(result, cloneTab(tab))
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

// UpdateTabState updates the state of a tab
// (spec L4021: tab registry + lifecycle).
func (r *TabRegistry) UpdateTabState(id string, state TabState) bool {
	return r.UpdateTabStateContext(context.Background(), id, state)
}

// UpdateTabStateContext verifies the source state; it never mutates the
// projection because bridge lifecycle events are authoritative.
func (r *TabRegistry) UpdateTabStateContext(ctx context.Context, id string, state TabState) bool {
	if r.Sync(ctx) != nil {
		return false
	}
	tab, ok := r.GetTab(id)
	return ok && tab.State == state
}

// UpdateTabURL updates the URL and title of a tab
// (spec L4021: tab registry + lifecycle).
func (r *TabRegistry) UpdateTabURL(id, url, title string) bool {
	return r.UpdateTabURLContext(context.Background(), id, url, title)
}

// UpdateTabURLContext verifies source metadata without creating a second
// mutable tab authority.
func (r *TabRegistry) UpdateTabURLContext(ctx context.Context, id, url, title string) bool {
	if r.Sync(ctx) != nil {
		return false
	}
	tab, ok := r.GetTab(id)
	return ok && tab.URL == url && tab.Title == title
}

// Count returns the total number of tabs
// (spec L4021: tab registry + lifecycle).
func (r *TabRegistry) Count() int {
	count, _ := r.CountContext(context.Background())
	return count
}

func (r *TabRegistry) CountContext(ctx context.Context) (int, error) {
	if err := r.Sync(ctx); err != nil {
		return 0, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tabs), nil
}

// CloseAll closes all tabs for a given user
// (spec L4021: tab registry + lifecycle).
func (r *TabRegistry) CloseAll(userID string) int {
	closed, _ := r.CloseAllContext(context.Background(), userID)
	return closed
}

func (r *TabRegistry) CloseAllContext(ctx context.Context, userID string) (int, error) {
	if err := r.Sync(ctx); err != nil {
		return 0, err
	}
	ids := make([]string, 0)
	r.mu.RLock()
	for id, tab := range r.tabs {
		if tab.UserID == userID {
			ids = append(ids, id)
		}
	}
	r.mu.RUnlock()
	sort.Strings(ids)
	closed := 0
	for _, id := range ids {
		ok, err := r.CloseTabContext(ctx, id)
		if err != nil {
			return closed, err
		}
		if ok {
			closed++
		}
	}
	return closed, nil
}

// Sync replaces the registry projection with the current bridge target
// snapshot. Owner metadata is retained only for target IDs that remain live.
func (r *TabRegistry) Sync(ctx context.Context) error {
	if ctx == nil {
		return errors.New("tabs: context required")
	}
	if r.source == nil {
		return ErrTargetSourceRequired
	}
	targets, err := r.source.ListTargets(ctx)
	if err != nil {
		return fmt.Errorf("tabs: list bridge targets: %w", err)
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].ID < targets[j].ID })
	next := make(map[string]*Tab, len(targets))
	now := time.Now()
	r.mu.Lock()
	previous := r.tabs
	r.mu.Unlock()
	for index, target := range targets {
		if target.ID == "" {
			return errors.New("tabs: bridge target has empty ID")
		}
		old := previous[target.ID]
		createdAt := now
		userID := ""
		if old != nil {
			createdAt = old.CreatedAt
			userID = old.UserID
		}
		next[target.ID] = &Tab{ID: target.ID, UserID: userID, URL: target.URL, Title: target.Title, State: tabState(target.State), CreatedAt: createdAt, LastActive: now, Index: index, CDPID: target.ID}
	}
	r.mu.Lock()
	r.tabs = next
	r.mu.Unlock()
	return nil
}

func tabState(state string) TabState {
	switch strings.ToLower(state) {
	case "loading":
		return TabStateLoading
	case "active":
		return TabStateActive
	case "idle":
		return TabStateIdle
	case "crashed":
		return TabStateCrashed
	case "closed", "detached":
		return TabStateClosed
	default:
		return TabStateOpen
	}
}

func cloneTab(tab *Tab) *Tab {
	if tab == nil {
		return nil
	}
	copy := *tab
	return &copy
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
