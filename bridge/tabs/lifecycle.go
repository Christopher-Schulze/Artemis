package tabs

import (
	"context"
	"sync"
)

// ProcessSpecKind enumerates the process spec kinds for tabs
// (spec L4225: ProcessSpec kind = stream or short-lived).
type ProcessSpecKind string

const (
	ProcessSpecStream    ProcessSpecKind = "stream"
	ProcessSpecTemporary ProcessSpecKind = "temporary"
)

// OwnerRef enumerates the possible tab owners
// (spec L4224: owner_ref=turn|subagent|connector|ui).
type OwnerRef string

const (
	OwnerRefTurn      OwnerRef = "turn"
	OwnerRefSubagent  OwnerRef = "subagent"
	OwnerRefConnector OwnerRef = "connector"
	OwnerRefUI        OwnerRef = "ui"
)

// ProcessSpec is the process spec under which each active tab registers
// (spec L4225: ProcessSpec{kind, parent=browser_engine,
// priority_lane, mailbox_cap=32, cancel_receiver=browser_tab_cancel,
// resource_budget=BrowserPool}).
type ProcessSpec struct {
	Kind           ProcessSpecKind `json:"kind"`
	Parent         string          `json:"parent"` // "browser_engine"
	PriorityLane   string          `json:"priorityLane"`
	MailboxCap     int             `json:"mailboxCap"`     // default 32
	CancelReceiver string          `json:"cancelReceiver"` // "browser_tab_cancel"
	ResourceBudget string          `json:"resourceBudget"` // "BrowserPool"
}

// DefaultProcessSpec returns the default ProcessSpec for a new tab
// (spec L4225).
func DefaultProcessSpec(kind ProcessSpecKind, priorityLane string) ProcessSpec {
	return ProcessSpec{
		Kind:           kind,
		Parent:         "browser_engine",
		PriorityLane:   priorityLane,
		MailboxCap:     32,
		CancelReceiver: "browser_tab_cancel",
		ResourceBudget: "BrowserPool",
	}
}

// TabLifecycleState enumerates the tab lifecycle states
// (spec L4226: ACTIVE|IDLE|SUSPENDED|CLOSED|LOST).
type TabLifecycleState string

const (
	TabLifecycleActive    TabLifecycleState = "ACTIVE"
	TabLifecycleIdle      TabLifecycleState = "IDLE"
	TabLifecycleSuspended TabLifecycleState = "SUSPENDED"
	TabLifecycleClosed    TabLifecycleState = "CLOSED"
	TabLifecycleLost      TabLifecycleState = "LOST"
)

// TabLifecycle manages the lifecycle state transitions for a tab
// (spec L4226: maps ACTIVE|IDLE|SUSPENDED|CLOSED|LOST to
// StateTransitionRegistry, sends DownSignal to owner on tab crash/CDP loss).
type TabLifecycle struct {
	mu     sync.Mutex
	state  TabLifecycleState
	spec   ProcessSpec
	owner  OwnerRef
	cancel context.CancelFunc // called on tab crash/CDP loss
	downCh chan struct{}      // closed when DownSignal is sent
}

// NewTabLifecycle creates a new lifecycle manager for a tab.
func NewTabLifecycle(spec ProcessSpec, owner OwnerRef, cancel context.CancelFunc) *TabLifecycle {
	return &TabLifecycle{
		state:  TabLifecycleActive,
		spec:   spec,
		owner:  owner,
		cancel: cancel,
		downCh: make(chan struct{}),
	}
}

// State returns the current lifecycle state.
func (l *TabLifecycle) State() TabLifecycleState {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.state
}

// Transition transitions to a new state. Returns an error if the
// transition is invalid (spec L4226).
func (l *TabLifecycle) Transition(newstate TabLifecycleState) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !isValidTransition(l.state, newstate) {
		return ErrInvalidTransition{From: l.state, To: newstate}
	}
	l.state = newstate
	// On CLOSED or LOST, send DownSignal to owner and cancel context
	if newstate == TabLifecycleClosed || newstate == TabLifecycleLost {
		if l.cancel != nil {
			l.cancel()
		}
		select {
		case <-l.downCh:
			// already closed
		default:
			close(l.downCh)
		}
	}
	return nil
}

// DownSignal returns a channel that is closed when a DownSignal is
// sent (on tab crash/CDP loss, spec L4226).
func (l *TabLifecycle) DownSignal() <-chan struct{} {
	return l.downCh
}

// isValidTransition checks if a state transition is valid
// (spec L4226: ACTIVE|IDLE|SUSPENDED|CLOSED|LOST).
func isValidTransition(from, to TabLifecycleState) bool {
	switch from {
	case TabLifecycleActive:
		return to == TabLifecycleIdle || to == TabLifecycleSuspended ||
			to == TabLifecycleClosed || to == TabLifecycleLost
	case TabLifecycleIdle:
		return to == TabLifecycleActive || to == TabLifecycleSuspended ||
			to == TabLifecycleClosed || to == TabLifecycleLost
	case TabLifecycleSuspended:
		return to == TabLifecycleActive || to == TabLifecycleClosed ||
			to == TabLifecycleLost
	case TabLifecycleClosed, TabLifecycleLost:
		return false // terminal states
	}
	return false
}

// ErrInvalidTransition is returned when a lifecycle transition is invalid.
type ErrInvalidTransition struct {
	From TabLifecycleState
	To   TabLifecycleState
}

func (e ErrInvalidTransition) Error() string {
	return "invalid tab lifecycle transition: " + string(e.From) + " -> " + string(e.To)
}

// PopupBlocker auto-closes popup tabs via target.EventTargetCreated
// (spec L4227: Popup blocking: auto-close via target.EventTargetCreated).
type PopupBlocker struct {
	mu      sync.Mutex
	enabled bool
	blocked int
}

// NewPopupBlocker creates a new popup blocker.
func NewPopupBlocker(enabled bool) *PopupBlocker {
	return &PopupBlocker{enabled: enabled}
}

// ShouldBlock reports whether a new target (tab) should be blocked
// as a popup (spec L4227).
func (p *PopupBlocker) ShouldBlock(openerTabID string, isPopup bool) bool {
	if !p.enabled {
		return false
	}
	if !isPopup {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.blocked++
	return true
}

// BlockedCount returns the total number of blocked popups.
func (p *PopupBlocker) BlockedCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.blocked
}

// Enable enables popup blocking.
func (p *PopupBlocker) Enable() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.enabled = true
}

// Disable disables popup blocking.
func (p *PopupBlocker) Disable() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.enabled = false
}
