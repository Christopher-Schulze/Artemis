package tabs

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"
)

type testTargetSource struct {
	mu                 sync.Mutex
	nextID             int
	targets            map[string]Target
	failCreate         bool
	failActivate       bool
	failClose          bool
	failList           bool
	failListAfterClose bool
	listCalls          int
	closeCalls         int
	closeHadDeadline   bool
	createResult       Target
	useCreateResult    bool
	createErr          error
	cancelCreate       context.CancelFunc
	createGate         *targetCreateGate
	listGates          map[int]*targetListGate
}

type targetCreateGate struct {
	started chan struct{}
	release chan struct{}
}

type targetListGate struct {
	started chan struct{}
	release chan struct{}
}

type doneProbeContext struct {
	context.Context
	doneCalled chan struct{}
	once       sync.Once
}

func (c *doneProbeContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.doneCalled) })
	return c.Context.Done()
}

func newTestTargetSource() *testTargetSource {
	return &testTargetSource{targets: make(map[string]Target)}
}

func (s *testTargetSource) ListTargets(_ context.Context) ([]Target, error) {
	s.mu.Lock()
	s.listCalls++
	call := s.listCalls
	if s.failList || (s.failListAfterClose && s.closeCalls > 0) {
		s.mu.Unlock()
		return nil, fmt.Errorf("forced list failure")
	}
	ids := make([]string, 0, len(s.targets))
	for id := range s.targets {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]Target, 0, len(ids))
	for _, id := range ids {
		out = append(out, s.targets[id])
	}
	gate := s.listGates[call]
	s.mu.Unlock()
	if gate != nil {
		close(gate.started)
		<-gate.release
	}
	return out, nil
}

func (s *testTargetSource) CreateTarget(_ context.Context, url string) (Target, error) {
	s.mu.Lock()
	if s.failCreate {
		s.mu.Unlock()
		return Target{}, fmt.Errorf("forced create failure")
	}
	s.nextID++
	target := Target{ID: fmt.Sprintf("target-%d", s.nextID), URL: url, State: "attached"}
	if s.useCreateResult || s.createResult.ID != "" {
		target = s.createResult
	}
	if target.State != "closed" {
		s.targets[target.ID] = target
	}
	gate := s.createGate
	err := s.createErr
	cancel := s.cancelCreate
	s.mu.Unlock()
	if gate != nil {
		close(gate.started)
		<-gate.release
	}
	if cancel != nil {
		cancel()
	}
	return target, err
}

func (s *testTargetSource) ActivateTarget(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failActivate {
		return fmt.Errorf("forced activate failure")
	}
	target, ok := s.targets[id]
	if !ok {
		return fmt.Errorf("target %s not found", id)
	}
	target.State = "active"
	s.targets[id] = target
	return nil
}

func (s *testTargetSource) CloseTarget(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, s.closeHadDeadline = ctx.Deadline()
	if s.failClose {
		return fmt.Errorf("forced close failure")
	}
	if _, ok := s.targets[id]; !ok {
		return fmt.Errorf("target %s not found", id)
	}
	s.closeCalls++
	delete(s.targets, id)
	return nil
}

func (s *testTargetSource) setState(id, state string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	target := s.targets[id]
	target.State = state
	s.targets[id] = target
}

func newTestRegistry() *TabRegistry { return NewTabRegistry(newTestTargetSource()) }

// ==================== manager.go tests ====================

// TestTASK2254_NewTabRegistry verifies creation
// (spec L4021: tab registry + lifecycle).
func TestTASK2254_NewTabRegistry(t *testing.T) {
	r := NewTabRegistry()
	if r == nil {
		t.Fatal("registry should not be nil")
	}
	if r.Count() != 0 {
		t.Error("new registry should have 0 tabs")
	}
}

func TestTASK2568_TabRegistryRequiresBridgeSourceAndProjectsCrash(t *testing.T) {
	withoutSource := NewTabRegistry()
	if tab := withoutSource.CreateTab("user", "https://example.com"); tab != nil {
		t.Fatal("tab registry created an independent tab identity")
	}
	source := newTestTargetSource()
	registry := NewTabRegistry(source)
	tab := registry.CreateTab("user", "https://example.com")
	if tab == nil || tab.ID != "target-1" || tab.CDPID != tab.ID {
		t.Fatalf("tab=%#v, want bridge target identity", tab)
	}
	source.setState(tab.ID, "crashed")
	projected, err := registry.ListTabsContext(context.Background(), "user")
	if err != nil {
		t.Fatal(err)
	}
	if len(projected) != 1 || projected[0].State != TabStateCrashed {
		t.Fatalf("crash projection=%#v", projected)
	}
}

func TestTASK2568_TabLifecycleFailureDoesNotMutateProjection(t *testing.T) {
	source := newTestTargetSource()
	registry := NewTabRegistry(source)
	source.failCreate = true
	if tab := registry.CreateTab("user", "https://example.com"); tab != nil {
		t.Fatal("failed target creation produced a local tab")
	}
	source.failCreate = false
	tab := registry.CreateTab("user", "https://example.com")
	source.failClose = true
	if ok := registry.CloseTab(tab.ID); ok {
		t.Fatal("failed target close reported success")
	}
	if got, ok := registry.GetTab(tab.ID); !ok || got.ID != tab.ID {
		t.Fatal("failed close removed the live source projection")
	}
}

func TestTASK2784_CreateProjectsReturnedTargetWithoutListRefresh(t *testing.T) {
	source := newTestTargetSource()
	source.createResult = Target{
		ID: "created-target", URL: "https://example.com", Title: "Example", State: "attached",
		ContextID: "browser-context", CreatedAt: time.Unix(100, 0),
	}
	source.failList = true
	registry := NewTabRegistry(source)

	tab, err := registry.CreateTabContext(context.Background(), "owner-1", "https://example.com")
	if err != nil {
		t.Fatalf("create target: %v", err)
	}
	if tab.ID != "created-target" || tab.UserID != "owner-1" || tab.BrowserContextID != "browser-context" || !tab.CreatedAt.Equal(time.Unix(100, 0)) {
		t.Fatalf("created projection=%#v", tab)
	}
	source.mu.Lock()
	listCalls := source.listCalls
	source.mu.Unlock()
	if listCalls != 0 {
		t.Fatalf("create made %d full target-list calls, want 0", listCalls)
	}
}

func TestTASK2784_CreateCompensatesKnownPartialTarget(t *testing.T) {
	source := newTestTargetSource()
	source.createResult = Target{ID: "partial-target", State: "attached"}
	source.createErr = errors.New("created page metadata failed")
	registry := NewTabRegistry(source)

	_, err := registry.CreateTabContext(context.Background(), "owner-1", "https://example.com")
	var createErr *TargetCreateError
	if !errors.As(err, &createErr) || createErr.TargetID != "partial-target" || !createErr.Compensated {
		t.Fatalf("create error=%#v, want confirmed compensation with target identity", err)
	}
	if _, ok := registry.GetTab("partial-target"); ok {
		t.Fatal("compensated target remained in registry")
	}
	source.mu.Lock()
	_, sourceStillOwnsTarget := source.targets["partial-target"]
	closeCalls := source.closeCalls
	source.mu.Unlock()
	if sourceStillOwnsTarget || closeCalls != 1 {
		t.Fatalf("source target retained=%v close calls=%d", sourceStillOwnsTarget, closeCalls)
	}
}

func TestTASK2784_CreateWithoutTargetIDReportsUnknownOutcome(t *testing.T) {
	source := newTestTargetSource()
	source.createResult = Target{State: "attached"}
	source.useCreateResult = true
	registry := NewTabRegistry(source)

	_, err := registry.CreateTabContext(context.Background(), "owner-1", "https://example.com")
	var createErr *TargetCreateError
	if !errors.As(err, &createErr) || !createErr.OutcomeUnknown || createErr.TargetID != "" {
		t.Fatalf("create error=%#v, want explicit unknown outcome without target identity", err)
	}
	if _, found := registry.GetTab(""); found {
		t.Fatal("target without an ID was inserted into the registry")
	}
}

func TestTASK2784_CreateRetainsOwnedPartialTargetWhenCompensationFails(t *testing.T) {
	source := newTestTargetSource()
	source.createResult = Target{ID: "uncertain-target", URL: "https://example.com", State: "attached"}
	source.createErr = errors.New("target inspection failed")
	source.failClose = true
	registry := NewTabRegistry(source)

	_, err := registry.CreateTabContext(context.Background(), "owner-1", "https://example.com")
	var createErr *TargetCreateError
	if !errors.As(err, &createErr) || createErr.TargetID != "uncertain-target" || createErr.Compensated {
		t.Fatalf("create error=%#v, want explicit uncompensated target identity", err)
	}
	tab, ok := registry.GetTab("uncertain-target")
	if !ok || tab.UserID != "owner-1" || tab.State != TabStateOpen {
		t.Fatalf("uncertain target projection=%#v, found=%v", tab, ok)
	}
}

func TestTASK2784_CreateRejectsNonLiveTargetStates(t *testing.T) {
	for _, state := range []string{"closed", "crashed", "detached"} {
		t.Run(state, func(t *testing.T) {
			source := newTestTargetSource()
			source.createResult = Target{ID: "terminal-target", State: state}
			registry := NewTabRegistry(source)
			_, err := registry.CreateTabContext(context.Background(), "owner-1", "https://example.com")
			var createErr *TargetCreateError
			if !errors.As(err, &createErr) || !createErr.Compensated || createErr.TargetID != "terminal-target" {
				t.Fatalf("non-live create error=%#v, want identified compensated failure", err)
			}
			if _, ok := registry.GetTab("terminal-target"); ok {
				t.Fatal("non-live target was published as a successful tab")
			}
			source.mu.Lock()
			closeCalls := source.closeCalls
			_, targetRetained := source.targets["terminal-target"]
			source.mu.Unlock()
			wantCloseCalls := 1
			if state == "closed" {
				wantCloseCalls = 0
			}
			if targetRetained || closeCalls != wantCloseCalls {
				t.Fatalf("non-live target cleanup: retained=%v close calls=%d", targetRetained, closeCalls)
			}
		})
	}
}

func TestTASK2784_CanceledCreateUsesBoundedContextForCompensation(t *testing.T) {
	source := newTestTargetSource()
	source.createResult = Target{ID: "cancelled-create-target", State: "attached"}
	source.createErr = &TargetCreateError{TargetID: "cancelled-create-target", Err: errors.New("page setup failed")}
	ctx, cancel := context.WithCancel(context.Background())
	source.cancelCreate = cancel
	registry := NewTabRegistry(source)

	_, err := registry.CreateTabContext(ctx, "owner-1", "https://example.com")
	var createErr *TargetCreateError
	if !errors.As(err, &createErr) || !createErr.Compensated || createErr.TargetID != "cancelled-create-target" {
		t.Fatalf("create error=%#v, want successful compensation after caller cancellation", err)
	}
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("caller context error=%v, want canceled", ctx.Err())
	}
	if _, ok := registry.GetTab("cancelled-create-target"); ok {
		t.Fatal("compensated target remained in registry")
	}
	source.mu.Lock()
	_, sourceStillOwnsTarget := source.targets["cancelled-create-target"]
	closeCalls := source.closeCalls
	closeHadDeadline := source.closeHadDeadline
	source.mu.Unlock()
	if sourceStillOwnsTarget || closeCalls != 1 || !closeHadDeadline {
		t.Fatalf("source target retained=%v close calls=%d cleanup deadline=%v", sourceStillOwnsTarget, closeCalls, closeHadDeadline)
	}
}

func TestTASK2784_SyncRetainsMetadataAndDoesNotInventActivity(t *testing.T) {
	source := newTestTargetSource()
	source.createResult = Target{ID: "metadata-target", URL: "https://example.com", Title: "Example", State: "attached",
		ContextID: "context-1", CreatedAt: time.Unix(100, 0)}
	registry := NewTabRegistry(source)
	tab, err := registry.CreateTabContext(context.Background(), "owner-1", "https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	activity := time.Unix(200, 0)
	if !registry.UpdateOwnerMetadata(tab.ID, TabOwnerMetadata{
		UserID: "owner-1", ProcessID: "process-1", OwnerRef: "turn-1", PriorityLane: "interactive", PolicyState: "allowed", MailboxCap: 32,
	}) {
		t.Fatal("owner metadata update failed for a live target")
	}
	if !registry.RecordActivity(tab.ID, activity) {
		t.Fatal("activity update failed for a live target")
	}
	source.mu.Lock()
	target := source.targets[tab.ID]
	target.URL = ""
	target.Title = ""
	target.State = "detached"
	target.CreatedAt = time.Time{}
	target.LastActive = time.Time{}
	source.targets[tab.ID] = target
	source.mu.Unlock()

	if err := registry.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, ok := registry.GetTab(tab.ID)
	if !ok {
		t.Fatal("target missing after sync")
	}
	if got.UserID != "owner-1" || got.ProcessID != "process-1" || got.OwnerRef != "turn-1" || got.PriorityLane != "interactive" || got.PolicyState != "allowed" || got.MailboxCap != 32 {
		t.Fatalf("local metadata lost: %#v", got)
	}
	if !got.CreatedAt.Equal(time.Unix(100, 0)) || !got.LastActive.Equal(activity) {
		t.Fatalf("timestamps changed during observation: created=%v active=%v", got.CreatedAt, got.LastActive)
	}
	if got.URL != "https://example.com" || got.Title != "Example" || got.BrowserContextID != "context-1" || got.State != TabStateLost {
		t.Fatalf("source projection=%#v", got)
	}
}

func TestTASK2784_ConcurrentOwnerMetadataUpdatesSurviveSync(t *testing.T) {
	source := newTestTargetSource()
	source.targets["target-a"] = Target{ID: "target-a", State: "attached"}
	source.targets["target-b"] = Target{ID: "target-b", State: "attached"}
	registry := NewTabRegistry(source)
	if err := registry.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}

	gate := &targetListGate{started: make(chan struct{}), release: make(chan struct{})}
	source.mu.Lock()
	source.listGates = map[int]*targetListGate{2: gate}
	source.mu.Unlock()
	syncDone := make(chan error, 1)
	go func() { syncDone <- registry.Sync(context.Background()) }()
	waitForRegistrySignal(t, gate.started, "owner metadata snapshot")

	want := map[string]TabOwnerMetadata{
		"target-a": {UserID: "owner-a", ProcessID: "process-a", OwnerRef: "turn-a", PriorityLane: "interactive", PolicyState: "allowed", MailboxCap: 32},
		"target-b": {UserID: "owner-b", ProcessID: "process-b", OwnerRef: "subagent-b", PriorityLane: "background", PolicyState: "restricted", MailboxCap: 16},
	}
	updateStarted := make(chan struct{})
	updateErrors := make(chan string, len(want))
	var updates sync.WaitGroup
	for id, metadata := range want {
		id, metadata := id, metadata
		updates.Add(1)
		go func() {
			defer updates.Done()
			<-updateStarted
			if !registry.UpdateOwnerMetadata(id, metadata) {
				updateErrors <- id
			}
		}()
	}
	close(updateStarted)
	updates.Wait()
	close(gate.release)
	if err := <-syncDone; err != nil {
		t.Fatal(err)
	}
	close(updateErrors)
	for id := range updateErrors {
		t.Fatalf("owner metadata update rejected live target %s", id)
	}

	for id, metadata := range want {
		got, ok := registry.GetTab(id)
		if !ok || got.UserID != metadata.UserID || got.ProcessID != metadata.ProcessID || got.OwnerRef != metadata.OwnerRef ||
			got.PriorityLane != metadata.PriorityLane || got.PolicyState != metadata.PolicyState || got.MailboxCap != metadata.MailboxCap {
			t.Fatalf("owner metadata crossed or disappeared for %s: tab=%#v found=%v want=%#v", id, got, ok, metadata)
		}
	}
	source.mu.Lock()
	listCalls := source.listCalls
	source.mu.Unlock()
	if listCalls != 3 {
		t.Fatalf("sync did not retry the superseded snapshot: list calls=%d want=3", listCalls)
	}
}

func TestTASK2784_SyncClearsObservedEmptyPageMetadata(t *testing.T) {
	source := newTestTargetSource()
	source.createResult = Target{ID: "metadata-target", URL: "https://example.com", Title: "Example", State: "attached", MetadataKnown: true}
	registry := NewTabRegistry(source)
	tab, err := registry.CreateTabContext(context.Background(), "owner-1", "https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	source.mu.Lock()
	target := source.targets[tab.ID]
	target.URL = ""
	target.Title = ""
	target.MetadataKnown = true
	source.targets[tab.ID] = target
	source.mu.Unlock()

	if err := registry.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	current, ok := registry.GetTab(tab.ID)
	if !ok || current.URL != "" || current.Title != "" {
		t.Fatalf("observed empty page metadata was replaced by stale values: %#v", current)
	}
}

func TestTASK2784_ActivationRecordsOnlyConfirmedActivity(t *testing.T) {
	source := newTestTargetSource()
	registry := NewTabRegistry(source)
	tab, err := registry.CreateTabContext(context.Background(), "owner-1", "https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !tab.LastActive.IsZero() {
		t.Fatalf("creation fabricated last activity: %v", tab.LastActive)
	}
	source.failActivate = true
	if err := registry.ActivateTabContext(context.Background(), tab.ID); err == nil {
		t.Fatal("failed bridge activation reported success")
	}
	stillInactive, _ := registry.GetTab(tab.ID)
	if !stillInactive.LastActive.IsZero() {
		t.Fatalf("failed activation recorded activity: %v", stillInactive.LastActive)
	}
	source.failActivate = false
	if err := registry.ActivateTabContext(context.Background(), tab.ID); err != nil {
		t.Fatal(err)
	}
	active, _ := registry.GetTab(tab.ID)
	if active.State != TabStateActive || active.LastActive.IsZero() {
		t.Fatalf("successful activation projection=%#v", active)
	}
}

func TestTASK2784_InvalidSnapshotDoesNotPartiallyPublish(t *testing.T) {
	source := newTestTargetSource()
	registry := NewTabRegistry(source)
	tab := registry.CreateTab("owner-1", "https://example.com")
	if tab == nil {
		t.Fatal("tab creation failed")
	}
	source.mu.Lock()
	target := source.targets[tab.ID]
	target.URL = "https://changed.example"
	target.State = "unknown-lifecycle-state"
	source.targets[tab.ID] = target
	source.mu.Unlock()

	if err := registry.Sync(context.Background()); err == nil {
		t.Fatal("unsupported source state was accepted")
	}
	current, ok := registry.GetTab(tab.ID)
	if !ok || current.URL != "https://example.com" || current.State != TabStateOpen {
		t.Fatalf("invalid snapshot partially published: %#v", current)
	}
}

func TestTASK2784_ConcurrentCreateSyncRetainsOwner(t *testing.T) {
	gate := &targetCreateGate{started: make(chan struct{}), release: make(chan struct{})}
	source := newTestTargetSource()
	source.createResult = Target{ID: "concurrent-target", URL: "https://example.com", State: "attached"}
	source.createGate = gate
	registry := NewTabRegistry(source)
	created := make(chan error, 1)
	go func() {
		_, err := registry.CreateTabContext(context.Background(), "owner-1", "https://example.com")
		created <- err
	}()
	waitForRegistrySignal(t, gate.started, "create target source")
	if err := registry.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	close(gate.release)
	if err := <-created; err != nil {
		t.Fatal(err)
	}
	tab, ok := registry.GetTab("concurrent-target")
	if !ok || tab.UserID != "owner-1" {
		t.Fatalf("created target owner lost: tab=%#v found=%v", tab, ok)
	}
}

func TestTASK2784_CreateSurvivesEarlierDelayedEmptySnapshot(t *testing.T) {
	gate := &targetListGate{started: make(chan struct{}), release: make(chan struct{})}
	source := newTestTargetSource()
	source.listGates = map[int]*targetListGate{1: gate}
	registry := NewTabRegistry(source)
	synced := make(chan error, 1)
	go func() { synced <- registry.Sync(context.Background()) }()
	waitForRegistrySignal(t, gate.started, "pre-create empty snapshot")

	tab, err := registry.CreateTabContext(context.Background(), "owner-2", "https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	close(gate.release)
	if err := <-synced; err != nil {
		t.Fatal(err)
	}
	current, ok := registry.GetTab(tab.ID)
	if !ok || current.UserID != "owner-2" {
		t.Fatalf("delayed pre-create snapshot lost the created tab owner: tab=%#v found=%v", current, ok)
	}
}

func TestTASK2784_ActivityInvalidatesEarlierSnapshotBeforeInitialProjection(t *testing.T) {
	activity := time.Unix(300, 0)
	gate := &targetListGate{started: make(chan struct{}), release: make(chan struct{})}
	source := newTestTargetSource()
	source.targets["root-target"] = Target{ID: "root-target", State: "attached"}
	source.listGates = map[int]*targetListGate{1: gate}
	registry := NewTabRegistry(source)
	synced := make(chan error, 1)
	go func() { synced <- registry.Sync(context.Background()) }()
	waitForRegistrySignal(t, gate.started, "pre-activity target snapshot")
	if registry.RecordActivity("root-target", activity) {
		t.Fatal("activity was reported stored before the target was projected")
	}
	source.mu.Lock()
	target := source.targets["root-target"]
	target.LastActive = activity
	source.targets["root-target"] = target
	source.mu.Unlock()
	close(gate.release)
	if err := <-synced; err != nil {
		t.Fatal(err)
	}
	current, ok := registry.GetTab("root-target")
	if !ok || !current.LastActive.Equal(activity) {
		t.Fatalf("pre-activity snapshot overwrote confirmed activity: tab=%#v found=%v", current, ok)
	}
}

func TestTASK2784_OlderSyncCannotResurrectConfirmedClose(t *testing.T) {
	gate := &targetListGate{started: make(chan struct{}), release: make(chan struct{})}
	source := newTestTargetSource()
	source.targets["target-1"] = Target{ID: "target-1", URL: "https://example.com", State: "attached"}
	source.listGates = map[int]*targetListGate{1: gate}
	registry := NewTabRegistry(source)
	oldSync := make(chan error, 1)
	go func() { oldSync <- registry.Sync(context.Background()) }()
	waitForRegistrySignal(t, gate.started, "older sync snapshot")
	closed, err := registry.CloseTabContext(context.Background(), "target-1")
	if err != nil || !closed {
		t.Fatalf("close=(%v,%v), want confirmed close", closed, err)
	}
	close(gate.release)
	if err := <-oldSync; err != nil {
		t.Fatal(err)
	}
	if _, ok := registry.GetTab("target-1"); ok {
		t.Fatal("older source snapshot resurrected a confirmed closed target")
	}
}

func TestTASK2784_CloseDoesNotRefreshAfterConfirmedEffect(t *testing.T) {
	source := newTestTargetSource()
	source.failListAfterClose = true
	registry := NewTabRegistry(source)
	tab, err := registry.CreateTabContext(context.Background(), "owner-1", "https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	closed, err := registry.CloseTabContext(context.Background(), tab.ID)
	if err != nil || !closed {
		t.Fatalf("close=(%v,%v), want success despite any post-effect refresh failure", closed, err)
	}
	source.mu.Lock()
	listCalls := source.listCalls
	source.mu.Unlock()
	if listCalls != 1 {
		t.Fatalf("close performed %d full target-list calls, want only the pre-close observation", listCalls)
	}
}

func TestTASK2784_ConcurrentCloseConfirmsExactlyOnce(t *testing.T) {
	source := newTestTargetSource()
	registry := NewTabRegistry(source)
	tab, err := registry.CreateTabContext(context.Background(), "owner-1", "https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	type closeResult struct {
		closed bool
		err    error
	}
	results := make(chan closeResult, 2)
	for range 2 {
		go func() {
			closed, err := registry.CloseTabContext(context.Background(), tab.ID)
			results <- closeResult{closed: closed, err: err}
		}()
	}
	confirmed := 0
	for range 2 {
		result := <-results
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.closed {
			confirmed++
		}
	}
	source.mu.Lock()
	closeCalls := source.closeCalls
	source.mu.Unlock()
	if confirmed != 1 || closeCalls != 1 {
		t.Fatalf("confirmed closes=%d source close calls=%d, want one each", confirmed, closeCalls)
	}
}

func TestTASK2784_CanceledLifecycleWaitReturnsBeforePriorOperationFinishes(t *testing.T) {
	gate := &targetCreateGate{started: make(chan struct{}), release: make(chan struct{})}
	source := newTestTargetSource()
	source.targets["existing-target"] = Target{ID: "existing-target", State: "attached"}
	source.createResult = Target{ID: "created-target", State: "attached"}
	source.createGate = gate
	registry := NewTabRegistry(source)
	var releaseOnce sync.Once
	releaseCreate := func() { releaseOnce.Do(func() { close(gate.release) }) }
	t.Cleanup(releaseCreate)

	createDone := make(chan error, 1)
	go func() {
		_, err := registry.CreateTabContext(context.Background(), "owner", "https://example.com")
		createDone <- err
	}()
	waitForRegistrySignal(t, gate.started, "create operation holding lifecycle admission")

	baseCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	probe := &doneProbeContext{Context: baseCtx, doneCalled: make(chan struct{})}
	closeDone := make(chan error, 1)
	go func() {
		_, err := registry.CloseTabContext(probe, "existing-target")
		closeDone <- err
	}()
	waitForRegistrySignal(t, probe.doneCalled, "queued close observing its cancellation channel")
	cancel()

	select {
	case err := <-closeDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("queued close error=%v, want caller cancellation", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled close remained blocked behind the active lifecycle operation")
	}

	source.mu.Lock()
	listCalls, closeCalls := source.listCalls, source.closeCalls
	source.mu.Unlock()
	if listCalls != 0 || closeCalls != 0 {
		t.Fatalf("canceled queued close reached source: list calls=%d close calls=%d", listCalls, closeCalls)
	}

	releaseCreate()
	if err := <-createDone; err != nil {
		t.Fatalf("active create: %v", err)
	}
}

func waitForRegistrySignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

// TestTASK2254_CreateTab verifies tab creation
// (spec L4021: tab registry + lifecycle).
func TestTASK2254_CreateTab(t *testing.T) {
	r := newTestRegistry()
	tab := r.CreateTab("user1", "https://example.com")
	if tab == nil {
		t.Fatal("tab should not be nil")
	}
	if tab.UserID != "user1" {
		t.Error("user mismatch")
	}
	if tab.URL != "https://example.com" {
		t.Error("url mismatch")
	}
	if tab.State != TabStateOpen {
		t.Error("state should be open")
	}
	if r.Count() != 1 {
		t.Error("count should be 1")
	}
}

// TestTASK2254_GetTab verifies tab retrieval
// (spec L4021: tab registry + lifecycle).
func TestTASK2254_GetTab(t *testing.T) {
	r := newTestRegistry()
	tab := r.CreateTab("user1", "https://example.com")
	got, ok := r.GetTab(tab.ID)
	if !ok {
		t.Fatal("tab should be found")
	}
	if got.ID != tab.ID {
		t.Error("id mismatch")
	}
	_, ok = r.GetTab("nonexistent")
	if ok {
		t.Error("nonexistent tab should not be found")
	}
}

// TestTASK2254_CloseTab verifies tab closing
// (spec L4021: tab registry + lifecycle).
func TestTASK2254_CloseTab(t *testing.T) {
	r := newTestRegistry()
	tab := r.CreateTab("user1", "https://example.com")
	if !r.CloseTab(tab.ID) {
		t.Error("close should succeed")
	}
	if r.Count() != 0 {
		t.Error("count should be 0 after close")
	}
	if r.CloseTab("nonexistent") {
		t.Error("closing nonexistent should fail")
	}
}

// TestTASK2254_ListTabs verifies tab listing
// (spec L4021: tab registry + lifecycle).
func TestTASK2254_ListTabs(t *testing.T) {
	r := newTestRegistry()
	r.CreateTab("user1", "https://a.com")
	r.CreateTab("user1", "https://b.com")
	r.CreateTab("user2", "https://c.com")
	tabs := r.ListTabs("user1")
	if len(tabs) != 2 {
		t.Errorf("user1 tabs: got %d, want 2", len(tabs))
	}
	tabs = r.ListTabs("user2")
	if len(tabs) != 1 {
		t.Errorf("user2 tabs: got %d, want 1", len(tabs))
	}
}

// TestTASK2254_UpdateTabState verifies state updates
// (spec L4021: tab registry + lifecycle).
func TestTASK2254_UpdateTabState(t *testing.T) {
	r := newTestRegistry()
	tab := r.CreateTab("user1", "https://example.com")
	if !r.UpdateTabState(tab.ID, TabStateOpen) {
		t.Error("source state should be observed")
	}
	got, _ := r.GetTab(tab.ID)
	if got.State != TabStateOpen {
		t.Error("state should be open")
	}
}

// TestTASK2254_UpdateTabURL verifies URL updates
// (spec L4021: tab registry + lifecycle).
func TestTASK2254_UpdateTabURL(t *testing.T) {
	r := newTestRegistry()
	tab := r.CreateTab("user1", "https://example.com")
	if r.UpdateTabURL(tab.ID, "https://new.com", "New Title") {
		t.Error("registry must not mutate source metadata")
	}
	got, _ := r.GetTab(tab.ID)
	if got.URL != "https://example.com" {
		t.Error("source URL should remain authoritative")
	}
	if got.Title != "" {
		t.Error("source title should remain authoritative")
	}
}

// TestTASK2254_CloseAll verifies closing all tabs for a user
// (spec L4021: tab registry + lifecycle).
func TestTASK2254_CloseAll(t *testing.T) {
	r := newTestRegistry()
	r.CreateTab("user1", "https://a.com")
	r.CreateTab("user1", "https://b.com")
	r.CreateTab("user2", "https://c.com")
	closed := r.CloseAll("user1")
	if closed != 2 {
		t.Errorf("closed: got %d, want 2", closed)
	}
	if r.Count() != 1 {
		t.Errorf("remaining: got %d, want 1", r.Count())
	}
}

// TestTASK2254_IsValidTabState verifies validation.
func TestTASK2254_IsValidTabState(t *testing.T) {
	if !IsValidTabState(TabStateOpen) {
		t.Error("open should be valid")
	}
	if !IsValidTabState(TabStateClosed) {
		t.Error("closed should be valid")
	}
	if IsValidTabState(TabState("invalid")) {
		t.Error("invalid should not be valid")
	}
}

// TestTASK2254_TabString verifies String.
func TestTASK2254_TabString(t *testing.T) {
	tab := Tab{ID: "t1", UserID: "u1", URL: "https://x.com", State: TabStateOpen}
	s := tab.String()
	if s == "" {
		t.Error("String should not be empty")
	}
}

// ==================== executor.go tests ====================

// TestTASK2254_NewTabExecutor verifies creation
// (spec L4021: concurrent tab execution).
func TestTASK2254_NewTabExecutor(t *testing.T) {
	r := newTestRegistry()
	e := NewTabExecutor(r, 4)
	if e == nil {
		t.Fatal("executor should not be nil")
	}
	if e.MaxConcurrent() != 4 {
		t.Error("max concurrent should be 4")
	}
}

// TestTASK2254_NewTabExecutorDefault verifies default concurrency.
func TestTASK2254_NewTabExecutorDefault(t *testing.T) {
	r := newTestRegistry()
	e := NewTabExecutor(r, 0)
	if e.MaxConcurrent() != 4 {
		t.Error("default max concurrent should be 4")
	}
}

// TestTASK2254_ExecuteTask verifies task execution
// (spec L4021: concurrent tab execution).
func TestTASK2254_ExecuteTask(t *testing.T) {
	r := newTestRegistry()
	tab := r.CreateTab("user1", "https://example.com")
	e := NewTabExecutor(r, 4)
	task := TabTask{
		TabID: tab.ID,
		Action: func(ctx context.Context, tab *Tab) error {
			return nil
		},
	}
	result := e.ExecuteTask(context.Background(), task)
	if !result.Success {
		t.Error("task should succeed")
	}
}

// TestTASK2254_ExecuteTaskNotFound verifies nonexistent tab.
func TestTASK2254_ExecuteTaskNotFound(t *testing.T) {
	r := newTestRegistry()
	e := NewTabExecutor(r, 4)
	task := TabTask{
		TabID: "nonexistent",
		Action: func(ctx context.Context, tab *Tab) error {
			return nil
		},
	}
	result := e.ExecuteTask(context.Background(), task)
	if result.Success {
		t.Error("nonexistent tab should fail")
	}
}

// TestTASK2254_ExecuteTaskError verifies error propagation.
func TestTASK2254_ExecuteTaskError(t *testing.T) {
	r := newTestRegistry()
	tab := r.CreateTab("user1", "https://example.com")
	e := NewTabExecutor(r, 4)
	task := TabTask{
		TabID: tab.ID,
		Action: func(ctx context.Context, tab *Tab) error {
			return context.DeadlineExceeded
		},
	}
	result := e.ExecuteTask(context.Background(), task)
	if result.Success {
		t.Error("error task should fail")
	}
	if result.Error == "" {
		t.Error("error should be set")
	}
}

// TestTASK2254_ExecuteConcurrent verifies concurrent execution
// (spec L4021: concurrent tab execution).
func TestTASK2254_ExecuteConcurrent(t *testing.T) {
	r := newTestRegistry()
	tab1 := r.CreateTab("user1", "https://a.com")
	tab2 := r.CreateTab("user1", "https://b.com")
	e := NewTabExecutor(r, 4)
	tasks := []TabTask{
		{TabID: tab1.ID, Action: func(ctx context.Context, t *Tab) error { return nil }},
		{TabID: tab2.ID, Action: func(ctx context.Context, t *Tab) error { return nil }},
	}
	results := e.ExecuteConcurrent(context.Background(), tasks)
	if len(results) != 2 {
		t.Errorf("results: got %d, want 2", len(results))
	}
	for _, r := range results {
		if !r.Success {
			t.Error("all results should succeed")
		}
	}
}

// TestTASK2254_ExecuteSequential verifies sequential execution
// (spec L4021: concurrent tab execution).
func TestTASK2254_ExecuteSequential(t *testing.T) {
	r := newTestRegistry()
	tab1 := r.CreateTab("user1", "https://a.com")
	tab2 := r.CreateTab("user1", "https://b.com")
	e := NewTabExecutor(r, 4)
	tasks := []TabTask{
		{TabID: tab1.ID, Action: func(ctx context.Context, t *Tab) error { return nil }},
		{TabID: tab2.ID, Action: func(ctx context.Context, t *Tab) error { return nil }},
	}
	results := e.ExecuteSequential(context.Background(), tasks)
	if len(results) != 2 {
		t.Errorf("results: got %d, want 2", len(results))
	}
}

// TestTASK2254_TabTaskResultString verifies String.
func TestTASK2254_TabTaskResultString(t *testing.T) {
	r := TabTaskResult{TabID: "t1", Success: true, Duration: 10 * time.Millisecond}
	s := r.String()
	if s == "" {
		t.Error("String should not be empty")
	}
}

// ==================== dialog.go tests ====================

// TestTASK2254_DialogTypes verifies type constants
// (spec L4021: alert/confirm/prompt handling).
func TestTASK2254_DialogTypes(t *testing.T) {
	if DialogTypeAlert != "alert" {
		t.Error("alert mismatch")
	}
	if DialogTypeConfirm != "confirm" {
		t.Error("confirm mismatch")
	}
	if DialogTypePrompt != "prompt" {
		t.Error("prompt mismatch")
	}
}

// TestTASK2254_NewDialogHandler verifies creation
// (spec L4021: alert/confirm/prompt handling).
func TestTASK2254_NewDialogHandler(t *testing.T) {
	h := NewDialogHandler()
	if h == nil {
		t.Fatal("handler should not be nil")
	}
	if h.Pending("tab1") != 0 {
		t.Error("new handler should have 0 pending")
	}
}

// TestTASK2254_DialogEnqueueDequeue verifies queue operations
// (spec L4021: alert/confirm/prompt handling).
func TestTASK2254_DialogEnqueueDequeue(t *testing.T) {
	h := NewDialogHandler()
	d := NewAlertDialog("Hello", "https://example.com")
	h.Enqueue("tab1", d)
	if h.Pending("tab1") != 1 {
		t.Error("pending should be 1")
	}
	got, ok := h.Dequeue("tab1")
	if !ok {
		t.Fatal("dequeue should succeed")
	}
	if got.Message != "Hello" {
		t.Error("message mismatch")
	}
	if h.Pending("tab1") != 0 {
		t.Error("pending should be 0 after dequeue")
	}
}

// TestTASK2254_DialogDequeueEmpty verifies empty queue.
func TestTASK2254_DialogDequeueEmpty(t *testing.T) {
	h := NewDialogHandler()
	_, ok := h.Dequeue("tab1")
	if ok {
		t.Error("dequeue from empty should fail")
	}
}

// TestTASK2254_DialogPeek verifies peek
// (spec L4021: alert/confirm/prompt handling).
func TestTASK2254_DialogPeek(t *testing.T) {
	h := NewDialogHandler()
	d := NewConfirmDialog("Are you sure?", "https://example.com")
	h.Enqueue("tab1", d)
	got, ok := h.Peek("tab1")
	if !ok {
		t.Fatal("peek should succeed")
	}
	if got.Type != DialogTypeConfirm {
		t.Error("type should be confirm")
	}
	if h.Pending("tab1") != 1 {
		t.Error("peek should not remove dialog")
	}
}

// TestTASK2254_DialogClear verifies clear
// (spec L4021: alert/confirm/prompt handling).
func TestTASK2254_DialogClear(t *testing.T) {
	h := NewDialogHandler()
	h.Enqueue("tab1", NewAlertDialog("a", "url"))
	h.Enqueue("tab1", NewAlertDialog("b", "url"))
	cleared := h.Clear("tab1")
	if cleared != 2 {
		t.Errorf("cleared: got %d, want 2", cleared)
	}
	if h.Pending("tab1") != 0 {
		t.Error("pending should be 0 after clear")
	}
}

// TestTASK2254_DialogAcceptAll verifies accept all
// (spec L4021: alert/confirm/prompt handling).
func TestTASK2254_DialogAcceptAll(t *testing.T) {
	h := NewDialogHandler()
	h.Enqueue("tab1", NewAlertDialog("a", "url"))
	h.Enqueue("tab1", NewConfirmDialog("b", "url"))
	accepted := h.AcceptAll("tab1")
	if accepted != 2 {
		t.Errorf("accepted: got %d, want 2", accepted)
	}
}

// TestTASK2254_DialogDismissAll verifies dismiss all
// (spec L4021: alert/confirm/prompt handling).
func TestTASK2254_DialogDismissAll(t *testing.T) {
	h := NewDialogHandler()
	h.Enqueue("tab1", NewAlertDialog("a", "url"))
	h.Enqueue("tab1", NewConfirmDialog("b", "url"))
	dismissed := h.DismissAll("tab1")
	if dismissed != 2 {
		t.Errorf("dismissed: got %d, want 2", dismissed)
	}
}

// TestTASK2254_NewAlertDialog verifies alert creation.
func TestTASK2254_NewAlertDialog(t *testing.T) {
	d := NewAlertDialog("msg", "url")
	if d.Type != DialogTypeAlert {
		t.Error("type should be alert")
	}
	if d.Action != DialogActionAccept {
		t.Error("alert action should be accept")
	}
}

// TestTASK2254_NewConfirmDialog verifies confirm creation.
func TestTASK2254_NewConfirmDialog(t *testing.T) {
	d := NewConfirmDialog("msg", "url")
	if d.Type != DialogTypeConfirm {
		t.Error("type should be confirm")
	}
	if d.Action != DialogActionDismiss {
		t.Error("confirm default action should be dismiss")
	}
}

// TestTASK2254_NewPromptDialog verifies prompt creation.
func TestTASK2254_NewPromptDialog(t *testing.T) {
	d := NewPromptDialog("msg", "url", "default")
	if d.Type != DialogTypePrompt {
		t.Error("type should be prompt")
	}
	if d.DefaultPrompt != "default" {
		t.Error("default prompt mismatch")
	}
}

// TestTASK2254_IsValidDialogType verifies validation.
func TestTASK2254_IsValidDialogType(t *testing.T) {
	if !IsValidDialogType(DialogTypeAlert) {
		t.Error("alert should be valid")
	}
	if IsValidDialogType(DialogType("invalid")) {
		t.Error("invalid should not be valid")
	}
}

// TestTASK2254_IsValidDialogAction verifies validation.
func TestTASK2254_IsValidDialogAction(t *testing.T) {
	if !IsValidDialogAction(DialogActionAccept) {
		t.Error("accept should be valid")
	}
	if IsValidDialogAction(DialogAction("invalid")) {
		t.Error("invalid should not be valid")
	}
}

// TestTASK2254_PendingDialogString verifies String.
func TestTASK2254_PendingDialogString(t *testing.T) {
	d := PendingDialog{Type: DialogTypeAlert, Message: "msg", Action: DialogActionAccept}
	s := d.String()
	if s == "" {
		t.Error("String should not be empty")
	}
}

// ==================== lock.go tests ====================

// TestTASK2254_NewTabLock verifies creation
// (spec L4021: per-tab locking).
func TestTASK2254_NewTabLock(t *testing.T) {
	l := NewTabLock()
	if l == nil {
		t.Fatal("lock should not be nil")
	}
	if l.Count() != 0 {
		t.Error("new lock should have 0 locked tabs")
	}
}

// TestTASK2254_LockUnlock verifies basic lock/unlock
// (spec L4021: per-tab locking).
func TestTASK2254_LockUnlock(t *testing.T) {
	l := NewTabLock()
	l.Lock("tab1", "owner1")
	if !l.IsLocked("tab1") {
		t.Error("tab1 should be locked")
	}
	if l.Owner("tab1") != "owner1" {
		t.Error("owner should be owner1")
	}
	l.Unlock("tab1")
	if l.IsLocked("tab1") {
		t.Error("tab1 should be unlocked")
	}
	if !l.TryLock("tab1", "owner2") {
		t.Fatal("IsLocked retained the free tab mutex")
	}
	l.Unlock("tab1")
}

// TestTASK2254_TryLock verifies non-blocking lock
// (spec L4021: per-tab locking).
func TestTASK2254_TryLock(t *testing.T) {
	l := NewTabLock()
	if !l.TryLock("tab1", "owner1") {
		t.Error("first TryLock should succeed")
	}
	if l.TryLock("tab1", "owner2") {
		t.Error("second TryLock should fail")
	}
	l.Unlock("tab1")
	if !l.TryLock("tab1", "owner2") {
		t.Error("TryLock after unlock should succeed")
	}
	l.Unlock("tab1")
}

// TestTASK2254_LockWithTimeout verifies timeout
// (spec L4021: per-tab locking).
func TestTASK2254_LockWithTimeout(t *testing.T) {
	l := NewTabLock()
	l.Lock("tab1", "owner1")
	// Should timeout since tab1 is locked
	if l.LockWithTimeout("tab1", "owner2", 50*time.Millisecond) {
		t.Error("LockWithTimeout should fail when locked")
	}
	l.Unlock("tab1")
	// Should succeed now
	if !l.LockWithTimeout("tab1", "owner2", 100*time.Millisecond) {
		t.Error("LockWithTimeout should succeed when unlocked")
	}
	l.Unlock("tab1")
}

// TestTASK2254_LockConcurrent verifies concurrent locking
// (spec L4021: per-tab locking).
func TestTASK2254_LockConcurrent(t *testing.T) {
	l := NewTabLock()
	var wg sync.WaitGroup
	count := 0
	var mu sync.Mutex
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l.Lock("tab1", "worker")
			mu.Lock()
			count++
			mu.Unlock()
			time.Sleep(10 * time.Millisecond)
			l.Unlock("tab1")
		}()
	}
	wg.Wait()
	if count != 10 {
		t.Errorf("count: got %d, want 10", count)
	}
}

// TestTASK2254_LockedTabs verifies locked tab listing
// (spec L4021: per-tab locking).
func TestTASK2254_LockedTabs(t *testing.T) {
	l := NewTabLock()
	l.Lock("tab1", "owner1")
	l.Lock("tab2", "owner2")
	locked := l.LockedTabs()
	if len(locked) != 2 || locked[0] != "tab1" || locked[1] != "tab2" {
		t.Errorf("locked: got %d, want 2", len(locked))
	}
	l.Unlock("tab2")
	l.Unlock("tab1")
}

// TestTASK2254_LockRemove verifies removal
// (spec L4021: per-tab locking).
func TestTASK2254_LockRemove(t *testing.T) {
	l := NewTabLock()
	l.Lock("tab1", "owner1")
	if l.Remove("tab1") {
		t.Error("should not remove locked tab")
	}
	l.Unlock("tab1")
	if !l.Remove("tab1") {
		t.Error("should remove unlocked tab")
	}
}

// TestTASK2254_LockString verifies String.
func TestTASK2254_LockString(t *testing.T) {
	l := NewTabLock()
	l.Lock("tab1", "owner1")
	s := l.String()
	if s == "" {
		t.Error("String should not be empty")
	}
	l.Unlock("tab1")
}

// ==================== full spec parity test ====================

// TestTASK2254_FullSpecParity verifies all 4 spec-mandated files
// (spec L4021: manager.go, executor.go, dialog.go, lock.go).
func TestTASK2254_FullSpecParity(t *testing.T) {
	// 1. manager.go - tab registry + lifecycle
	r := newTestRegistry()
	tab := r.CreateTab("user1", "https://example.com")
	if tab == nil {
		t.Fatal("manager.go: tab creation failed")
	}
	if r.Count() != 1 {
		t.Fatalf("manager.go: tab count = %d, want 1", r.Count())
	}

	// 2. executor.go - concurrent tab execution
	e := NewTabExecutor(r, 4)
	task := TabTask{
		TabID:  tab.ID,
		Action: func(ctx context.Context, t *Tab) error { return nil },
	}
	result := e.ExecuteTask(context.Background(), task)
	if !result.Success {
		t.Error("executor.go: task execution failed")
	}

	// 3. dialog.go - alert/confirm/prompt handling
	h := NewDialogHandler()
	h.Enqueue(tab.ID, NewAlertDialog("test", "url"))
	if h.Pending(tab.ID) != 1 {
		t.Error("dialog.go: enqueue failed")
	}

	// 4. lock.go - per-tab locking
	l := NewTabLock()
	l.Lock(tab.ID, "test")
	if !l.IsLocked(tab.ID) {
		t.Error("lock.go: lock failed")
	}
	l.Unlock(tab.ID)
}
