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
	TabStateLost    TabState = "lost"
)

const maxTargetSnapshotAttempts = 3
const targetCompensationTimeout = 2 * time.Second

// Tab represents a browser tab in the registry
// (spec L4021: tab registry + lifecycle).
// TabEntry is the spec-mandated tab entry with full lifecycle fields
// (spec L4216: TabEntry: context + cancelFunc + CDPID + timestamps +
// policy state + process_id + owner_ref=turn|subagent|connector|ui).
type Tab struct {
	ID            string    `json:"id"`
	UserID        string    `json:"userId"`
	URL           string    `json:"url"`
	Title         string    `json:"title"`
	State         TabState  `json:"state"`
	CreatedAt     time.Time `json:"createdAt"`
	LastActive    time.Time `json:"lastActive"`
	Index         int       `json:"index"`
	metadataKnown bool
	// ProcessSpec lifecycle fields (spec L4216)
	CDPID            string `json:"cdpId,omitempty"` // CDP target ID
	BrowserContextID string `json:"browserContextId,omitempty"`
	ProcessID        string `json:"processId,omitempty"`    // ProcessSpec ID
	OwnerRef         string `json:"ownerRef,omitempty"`     // turn|subagent|connector|ui
	PriorityLane     string `json:"priorityLane,omitempty"` // scheduling priority
	PolicyState      string `json:"policyState,omitempty"`  // policy engine state
	MailboxCap       int    `json:"mailboxCap,omitempty"`   // default 32
}

// TabOwnerMetadata is the atomically replaced local owner/process projection
// retained while the bridge confirms that the target remains live.
type TabOwnerMetadata struct {
	UserID       string
	ProcessID    string
	OwnerRef     string
	PriorityLane string
	PolicyState  string
	MailboxCap   int
}

// Target is the immutable bridge-owned target projection used by the tab
// registry. ID is always the real CDP target ID; State is supplied by the
// owning bridge page and is never synthesized by this package.
type Target struct {
	ID            string
	UserID        string
	URL           string
	Title         string
	State         string
	ContextID     string
	CreatedAt     time.Time
	LastActive    time.Time
	MetadataKnown bool // URL and title were read successfully, including valid empty values.
	OwnerMetadata TabOwnerMetadata
}

// TargetCreateError retains target identity and cleanup evidence when creation
// fails after the browser may already have created a target. OutcomeUnknown
// marks a lost receipt that did not provide a recoverable target ID.
type TargetCreateError struct {
	TargetID       string
	Compensated    bool
	OutcomeUnknown bool
	Err            error
}

func (e *TargetCreateError) Error() string {
	if e.TargetID == "" && e.OutcomeUnknown {
		if e.Err != nil {
			return fmt.Sprintf("tabs: target creation outcome unknown because its target ID was not received: %v", e.Err)
		}
		return "tabs: target creation outcome unknown because its target ID was not received"
	}
	if e.Err == nil {
		if e.Compensated {
			return fmt.Sprintf("tabs: target %s creation failed and was compensated", e.TargetID)
		}
		return fmt.Sprintf("tabs: target %s creation failed; target remains live or uncertain", e.TargetID)
	}
	if e.Compensated {
		return fmt.Sprintf("tabs: target %s creation failed and was compensated: %v", e.TargetID, e.Err)
	}
	return fmt.Sprintf("tabs: target %s creation failed; target remains live or uncertain: %v", e.TargetID, e.Err)
}

func (e *TargetCreateError) Unwrap() error { return e.Err }

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
	mu              sync.RWMutex
	operationInitMu sync.Mutex
	operationGate   chan struct{}
	tabs            map[string]Tab
	source          TargetSource
	syncStarted     uint64
	syncApplied     uint64
	generation      uint64
	// pool holds the IDs of tabs this registry created; only they recycle.
	pool    map[string]struct{}
	recycle recycleState
}

// NewTabRegistry creates a new tab registry
// (spec L4021: tab registry + lifecycle).
func NewTabRegistry(sources ...TargetSource) *TabRegistry {
	var source TargetSource
	if len(sources) > 0 {
		source = sources[0]
	}
	return &TabRegistry{tabs: make(map[string]Tab), source: source}
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
	unlock, err := r.lockOperation(ctx)
	if err != nil {
		return nil, fmt.Errorf("tabs: wait for lifecycle operation: %w", err)
	}
	defer unlock()
	knownIDs := r.snapshotIDs()
	target, err := r.source.CreateTarget(ctx, url)
	if err != nil {
		return nil, r.failCreate(ctx, target, userID, knownIDs, err)
	}
	if target.ID == "" {
		return nil, &TargetCreateError{OutcomeUnknown: true, Err: errors.New("target source returned no target ID")}
	}
	tab, err := tabFromTarget(target)
	if err != nil {
		return nil, r.failCreate(ctx, target, userID, knownIDs, err)
	}
	if tab.State == TabStateClosed {
		cause := &TargetCreateError{TargetID: target.ID, Compensated: true, Err: errors.New("target was already closed when creation completed")}
		return nil, r.failCreate(ctx, target, userID, knownIDs, cause)
	}
	if tab.State == TabStateCrashed || tab.State == TabStateLost {
		return nil, r.failCreate(ctx, target, userID, knownIDs, fmt.Errorf("target creation completed in non-live state %q", tab.State))
	}
	r.mu.Lock()
	if _, existedBefore := knownIDs[target.ID]; existedBefore {
		r.mu.Unlock()
		return nil, &TargetCreateError{TargetID: target.ID, Err: errors.New("target source reused an existing target ID")}
	}
	if old, exists := r.tabs[target.ID]; exists {
		mergeLocalMetadata(&tab, old)
	}
	tab.UserID = userID
	r.generation++
	r.tabs[target.ID] = tab
	r.reindexLocked()
	created := cloneTab(&tab)
	r.mu.Unlock()
	r.markCreated(target.ID)
	return created, nil
}

func (r *TabRegistry) failCreate(ctx context.Context, target Target, userID string, knownIDs map[string]struct{}, cause error) error {
	var sourceError *TargetCreateError
	if errors.As(cause, &sourceError) {
		if target.ID == "" {
			target.ID = sourceError.TargetID
		}
		if target.ID == "" && sourceError.OutcomeUnknown {
			return sourceError
		}
		if target.ID != "" && sourceError.Compensated {
			if _, existedBefore := knownIDs[target.ID]; existedBefore {
				return sourceError
			}
			r.mu.Lock()
			r.generation++
			delete(r.tabs, target.ID)
			r.reindexLocked()
			r.mu.Unlock()
			return sourceError
		}
	}
	if target.ID == "" {
		return fmt.Errorf("tabs: create target: %w", cause)
	}
	if _, existedBefore := knownIDs[target.ID]; existedBefore {
		return &TargetCreateError{TargetID: target.ID, Err: errors.Join(cause, errors.New("target source reused an existing target ID"))}
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), targetCompensationTimeout)
	closeErr := r.source.CloseTarget(cleanupCtx, target.ID)
	cancel()
	if closeErr == nil {
		r.mu.Lock()
		r.generation++
		delete(r.tabs, target.ID)
		r.reindexLocked()
		r.mu.Unlock()
		return &TargetCreateError{TargetID: target.ID, Compensated: true, Err: cause}
	} else {
		cause = errors.Join(cause, fmt.Errorf("tabs: compensate target %s: %w", target.ID, closeErr))
	}
	r.projectUncompensatedTarget(target, userID)
	return &TargetCreateError{TargetID: target.ID, Err: cause}
}

func (r *TabRegistry) projectUncompensatedTarget(target Target, userID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.generation++
	state, err := tabState(target.State)
	if err != nil || state == TabStateClosed {
		state = TabStateLost
	}
	tab := Tab{ID: target.ID, UserID: userID, URL: target.URL, Title: target.Title, State: state,
		CreatedAt: target.CreatedAt, LastActive: target.LastActive, CDPID: target.ID, BrowserContextID: target.ContextID}
	if old, ok := r.tabs[target.ID]; ok {
		mergeLocalMetadata(&tab, old)
	}
	tab.UserID = userID
	r.tabs[target.ID] = tab
	r.reindexLocked()
}

func (r *TabRegistry) snapshotIDs() map[string]struct{} {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make(map[string]struct{}, len(r.tabs))
	for id := range r.tabs {
		ids[id] = struct{}{}
	}
	return ids
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
	return cloneTab(&tab), true
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

// CloseTabContext closes the real CDP target and removes only its confirmed projection.
func (r *TabRegistry) CloseTabContext(ctx context.Context, id string) (bool, error) {
	unlock, err := r.lockOperation(ctx)
	if err != nil {
		return false, fmt.Errorf("tabs: wait for lifecycle operation: %w", err)
	}
	defer unlock()
	if err := r.Sync(ctx); err != nil {
		return false, err
	}
	if _, ok := r.GetTab(id); !ok {
		return false, nil
	}
	if err := r.source.CloseTarget(ctx, id); err != nil {
		return false, fmt.Errorf("tabs: close target %s: %w", id, err)
	}
	r.mu.Lock()
	r.generation++
	delete(r.tabs, id)
	delete(r.pool, id)
	r.reindexLocked()
	r.mu.Unlock()
	return true, nil
}

// ActivateTabContext activates the real bridge target and records that event.
func (r *TabRegistry) ActivateTabContext(ctx context.Context, id string) error {
	unlock, err := r.lockOperation(ctx)
	if err != nil {
		return fmt.Errorf("tabs: wait for lifecycle operation: %w", err)
	}
	defer unlock()
	if err := r.Sync(ctx); err != nil {
		return err
	}
	if _, ok := r.GetTab(id); !ok {
		return fmt.Errorf("%w: %s", ErrTargetNotFound, id)
	}
	if err := r.source.ActivateTarget(ctx, id); err != nil {
		return fmt.Errorf("tabs: activate target %s: %w", id, err)
	}
	r.mu.Lock()
	r.generation++
	if tab, ok := r.tabs[id]; ok {
		for otherID, other := range r.tabs {
			if other.State == TabStateActive {
				other.State = TabStateOpen
				r.tabs[otherID] = other
			}
		}
		tab.State = TabStateActive
		tab.LastActive = time.Now()
		r.tabs[id] = tab
	}
	r.mu.Unlock()
	return nil
}

func (r *TabRegistry) ActivateTab(id string) error {
	return r.ActivateTabContext(context.Background(), id)
}

// lockOperation serializes lifecycle mutations while allowing queued callers to cancel admission.
func (r *TabRegistry) lockOperation(ctx context.Context) (func(), error) {
	if ctx == nil {
		return nil, errors.New("tabs: context required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.operationInitMu.Lock()
	if r.operationGate == nil {
		r.operationGate = make(chan struct{}, 1)
	}
	gate := r.operationGate
	r.operationInitMu.Unlock()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case gate <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-gate
			return nil, err
		}
		return func() { <-gate }, nil
	}
}

// RecordActivity advances the projection only for a real observed operation.
func (r *TabRegistry) RecordActivity(id string, at time.Time) bool {
	if id == "" || at.IsZero() {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	tab, ok := r.tabs[id]
	if !ok {
		r.generation++
		return false
	}
	if tab.State == TabStateClosed || tab.State == TabStateLost {
		return false
	}
	if at.After(tab.LastActive) {
		tab.LastActive = at
		r.tabs[id] = tab
		r.generation++
	}
	return true
}

// UpdateOwnerMetadata atomically replaces the owner and process metadata for a
// currently projected target. A missing or closed target cannot acquire an owner.
func (r *TabRegistry) UpdateOwnerMetadata(id string, metadata TabOwnerMetadata) bool {
	if id == "" {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	tab, ok := r.tabs[id]
	if !ok || tab.State == TabStateClosed {
		return false
	}
	if tab.UserID == metadata.UserID && tab.ProcessID == metadata.ProcessID && tab.OwnerRef == metadata.OwnerRef &&
		tab.PriorityLane == metadata.PriorityLane && tab.PolicyState == metadata.PolicyState && tab.MailboxCap == metadata.MailboxCap {
		return true
	}
	tab.UserID = metadata.UserID
	tab.ProcessID = metadata.ProcessID
	tab.OwnerRef = metadata.OwnerRef
	tab.PriorityLane = metadata.PriorityLane
	tab.PolicyState = metadata.PolicyState
	tab.MailboxCap = metadata.MailboxCap
	r.tabs[id] = tab
	r.generation++
	return true
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
	for _, value := range r.tabs {
		tab := value
		if tab.UserID == userID {
			result = append(result, cloneTab(&tab))
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
	for attempt := 0; attempt < maxTargetSnapshotAttempts; attempt++ {
		r.mu.Lock()
		r.syncStarted++
		sequence := r.syncStarted
		generation := r.generation
		r.mu.Unlock()
		targets, err := r.source.ListTargets(ctx)
		if err != nil {
			return fmt.Errorf("tabs: list bridge targets: %w", err)
		}
		targets = append([]Target(nil), targets...)
		sort.Slice(targets, func(i, j int) bool { return targets[i].ID < targets[j].ID })
		projected := make([]Tab, 0, len(targets))
		for index, target := range targets {
			if target.ID == "" {
				return errors.New("tabs: bridge target has empty ID")
			}
			if index > 0 && targets[index-1].ID == target.ID {
				return fmt.Errorf("tabs: bridge target snapshot repeats ID %s", target.ID)
			}
			tab, err := tabFromTarget(target)
			if err != nil {
				return err
			}
			tab.Index = index
			projected = append(projected, tab)
		}
		r.mu.Lock()
		if generation != r.generation {
			r.mu.Unlock()
			continue
		}
		if sequence < r.syncApplied {
			r.mu.Unlock()
			return nil
		}
		next := make(map[string]Tab, len(projected))
		for _, tab := range projected {
			if old, ok := r.tabs[tab.ID]; ok {
				mergeLocalMetadata(&tab, old)
			}
			next[tab.ID] = tab
		}
		r.tabs = next
		r.syncApplied = sequence
		r.mu.Unlock()
		return nil
	}
	return errors.New("tabs: bridge snapshots repeatedly superseded by lifecycle changes")
}

func tabFromTarget(target Target) (Tab, error) {
	state, err := tabState(target.State)
	if err != nil {
		return Tab{}, err
	}
	return Tab{ID: target.ID, UserID: target.UserID, URL: target.URL, Title: target.Title, State: state, CreatedAt: target.CreatedAt,
		LastActive: target.LastActive, CDPID: target.ID, BrowserContextID: target.ContextID, metadataKnown: target.MetadataKnown,
		ProcessID: target.OwnerMetadata.ProcessID, OwnerRef: target.OwnerMetadata.OwnerRef, PriorityLane: target.OwnerMetadata.PriorityLane,
		PolicyState: target.OwnerMetadata.PolicyState, MailboxCap: target.OwnerMetadata.MailboxCap}, nil
}

func mergeLocalMetadata(tab *Tab, old Tab) {
	if old.UserID != "" {
		tab.UserID = old.UserID
	}
	if !old.CreatedAt.IsZero() {
		tab.CreatedAt = old.CreatedAt
	}
	if old.LastActive.After(tab.LastActive) {
		tab.LastActive = old.LastActive
	}
	if !tab.metadataKnown {
		if tab.URL == "" {
			tab.URL = old.URL
		}
		if tab.Title == "" {
			tab.Title = old.Title
		}
	}
	if tab.ProcessID == "" {
		tab.ProcessID = old.ProcessID
		tab.OwnerRef = old.OwnerRef
		tab.PriorityLane = old.PriorityLane
		tab.MailboxCap = old.MailboxCap
	}
	tab.PolicyState = old.PolicyState
}

func (r *TabRegistry) reindexLocked() {
	ids := make([]string, 0, len(r.tabs))
	for id := range r.tabs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for index, id := range ids {
		tab := r.tabs[id]
		tab.Index = index
		r.tabs[id] = tab
	}
}

func tabState(state string) (TabState, error) {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "open", "attached":
		return TabStateOpen, nil
	case "loading":
		return TabStateLoading, nil
	case "active":
		return TabStateActive, nil
	case "idle":
		return TabStateIdle, nil
	case "crashed":
		return TabStateCrashed, nil
	case "closed":
		return TabStateClosed, nil
	case "detached", "lost":
		return TabStateLost, nil
	default:
		return "", fmt.Errorf("tabs: unsupported bridge target state %q", state)
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
	case TabStateOpen, TabStateLoading, TabStateActive, TabStateIdle, TabStateClosed, TabStateCrashed, TabStateLost:
		return true
	}
	return false
}

// String returns a diagnostic summary.
func (t Tab) String() string {
	return fmt.Sprintf("Tab{id:%s user:%s url:%s state:%s}", t.ID, t.UserID, t.URL, t.State)
}
