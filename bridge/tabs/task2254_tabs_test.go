package tabs

import (
	"context"
	"sync"
	"testing"
	"time"
)

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

// TestTASK2254_CreateTab verifies tab creation
// (spec L4021: tab registry + lifecycle).
func TestTASK2254_CreateTab(t *testing.T) {
	r := NewTabRegistry()
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
	r := NewTabRegistry()
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
	r := NewTabRegistry()
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
	r := NewTabRegistry()
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
	r := NewTabRegistry()
	tab := r.CreateTab("user1", "https://example.com")
	if !r.UpdateTabState(tab.ID, TabStateActive) {
		t.Error("update should succeed")
	}
	got, _ := r.GetTab(tab.ID)
	if got.State != TabStateActive {
		t.Error("state should be active")
	}
}

// TestTASK2254_UpdateTabURL verifies URL updates
// (spec L4021: tab registry + lifecycle).
func TestTASK2254_UpdateTabURL(t *testing.T) {
	r := NewTabRegistry()
	tab := r.CreateTab("user1", "https://example.com")
	if !r.UpdateTabURL(tab.ID, "https://new.com", "New Title") {
		t.Error("update should succeed")
	}
	got, _ := r.GetTab(tab.ID)
	if got.URL != "https://new.com" {
		t.Error("url mismatch")
	}
	if got.Title != "New Title" {
		t.Error("title mismatch")
	}
}

// TestTASK2254_CloseAll verifies closing all tabs for a user
// (spec L4021: tab registry + lifecycle).
func TestTASK2254_CloseAll(t *testing.T) {
	r := NewTabRegistry()
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
	r := NewTabRegistry()
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
	r := NewTabRegistry()
	e := NewTabExecutor(r, 0)
	if e.MaxConcurrent() != 4 {
		t.Error("default max concurrent should be 4")
	}
}

// TestTASK2254_ExecuteTask verifies task execution
// (spec L4021: concurrent tab execution).
func TestTASK2254_ExecuteTask(t *testing.T) {
	r := NewTabRegistry()
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
	r := NewTabRegistry()
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
	r := NewTabRegistry()
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
	r := NewTabRegistry()
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
	r := NewTabRegistry()
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
	if len(locked) != 2 {
		t.Errorf("locked: got %d, want 2", len(locked))
	}
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
	r := NewTabRegistry()
	tab := r.CreateTab("user1", "https://example.com")
	if tab == nil || r.Count() != 1 {
		t.Error("manager.go: tab creation failed")
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
