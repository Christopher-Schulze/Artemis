package tabs

import (
	"context"
	"testing"
)

func TestProcessSpecDefaults(t *testing.T) {
	spec := DefaultProcessSpec(ProcessSpecStream, "high")
	if spec.Kind != ProcessSpecStream {
		t.Errorf("Kind = %s, want stream", spec.Kind)
	}
	if spec.Parent != "browser_engine" {
		t.Errorf("Parent = %s, want browser_engine", spec.Parent)
	}
	if spec.MailboxCap != 32 {
		t.Errorf("MailboxCap = %d, want 32", spec.MailboxCap)
	}
	if spec.CancelReceiver != "browser_tab_cancel" {
		t.Errorf("CancelReceiver = %s, want browser_tab_cancel", spec.CancelReceiver)
	}
	if spec.ResourceBudget != "BrowserPool" {
		t.Errorf("ResourceBudget = %s, want BrowserPool", spec.ResourceBudget)
	}
}

func TestTabLifecycleTransitions(t *testing.T) {
	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	spec := DefaultProcessSpec(ProcessSpecTemporary, "normal")
	lc := NewTabLifecycle(spec, OwnerRefTurn, cancel)

	if lc.State() != TabLifecycleActive {
		t.Errorf("initial state = %s, want ACTIVE", lc.State())
	}

	// ACTIVE -> IDLE
	if err := lc.Transition(TabLifecycleIdle); err != nil {
		t.Fatalf("ACTIVE->IDLE: %v", err)
	}

	// IDLE -> SUSPENDED
	if err := lc.Transition(TabLifecycleSuspended); err != nil {
		t.Fatalf("IDLE->SUSPENDED: %v", err)
	}

	// SUSPENDED -> ACTIVE
	if err := lc.Transition(TabLifecycleActive); err != nil {
		t.Fatalf("SUSPENDED->ACTIVE: %v", err)
	}
}

func TestTabLifecycleClosedSendsDownSignal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	spec := DefaultProcessSpec(ProcessSpecTemporary, "normal")
	lc := NewTabLifecycle(spec, OwnerRefTurn, cancel)

	// ACTIVE -> CLOSED
	if err := lc.Transition(TabLifecycleClosed); err != nil {
		t.Fatalf("ACTIVE->CLOSED: %v", err)
	}

	// DownSignal channel should be closed
	select {
	case <-lc.DownSignal():
		// good
	default:
		t.Error("DownSignal should be closed after CLOSED transition")
	}

	// Context should be cancelled
	select {
	case <-ctx.Done():
		// good
	default:
		t.Error("context should be cancelled after CLOSED transition")
	}
}

func TestTabLifecycleLostSendsDownSignal(t *testing.T) {
	_, cancel := context.WithCancel(context.Background())
	lc := NewTabLifecycle(DefaultProcessSpec(ProcessSpecStream, "normal"), OwnerRefSubagent, cancel)

	if err := lc.Transition(TabLifecycleLost); err != nil {
		t.Fatalf("ACTIVE->LOST: %v", err)
	}

	select {
	case <-lc.DownSignal():
		// good
	default:
		t.Error("DownSignal should be closed after LOST transition")
	}
}

func TestTabLifecycleInvalidTransition(t *testing.T) {
	lc := NewTabLifecycle(DefaultProcessSpec(ProcessSpecStream, "normal"), OwnerRefUI, nil)

	// CLOSED -> ACTIVE should fail (terminal state)
	lc.Transition(TabLifecycleClosed)
	err := lc.Transition(TabLifecycleActive)
	if err == nil {
		t.Error("CLOSED->ACTIVE should fail (terminal state)")
	}
}

func TestPopupBlocker(t *testing.T) {
	pb := NewPopupBlocker(true)

	if !pb.ShouldBlock("tab-1", true) {
		t.Error("popup should be blocked when enabled")
	}
	if pb.ShouldBlock("tab-1", false) {
		t.Error("non-popup should not be blocked")
	}
	if pb.BlockedCount() != 1 {
		t.Errorf("BlockedCount = %d, want 1", pb.BlockedCount())
	}

	pb.Disable()
	if pb.ShouldBlock("tab-1", true) {
		t.Error("popup should not be blocked when disabled")
	}

	pb.Enable()
	if !pb.ShouldBlock("tab-1", true) {
		t.Error("popup should be blocked after re-enable")
	}
}

func TestTabStructProcessSpecFields(t *testing.T) {
	tab := Tab{
		ID:           "tab-1",
		UserID:       "user-1",
		CDPID:        "cdp-target-1",
		ProcessID:    "proc-1",
		OwnerRef:     string(OwnerRefTurn),
		PriorityLane: "high",
		PolicyState:  "allowed",
		MailboxCap:   32,
	}
	if tab.CDPID != "cdp-target-1" {
		t.Errorf("CDPID = %s", tab.CDPID)
	}
	if tab.ProcessID != "proc-1" {
		t.Errorf("ProcessID = %s", tab.ProcessID)
	}
	if tab.OwnerRef != "turn" {
		t.Errorf("OwnerRef = %s", tab.OwnerRef)
	}
	if tab.MailboxCap != 32 {
		t.Errorf("MailboxCap = %d", tab.MailboxCap)
	}
}
