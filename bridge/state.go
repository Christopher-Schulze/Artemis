package bridge

import (
	"fmt"
	"sync"
	"time"
)

// state.go (spec L4018: bridge/state.go - Bridge State Machine).
//
// This file provides the spec-mandated Bridge State Machine.
// Tracks the bridge lifecycle state and enforces valid transitions.

// BridgeState enumerates bridge states
// (spec L4018: Bridge State Machine).
type BridgeState string

const (
	BridgeStateUninitialized BridgeState = "uninitialized"
	BridgeStateInitializing  BridgeState = "initializing"
	BridgeStateReady         BridgeState = "ready"
	BridgeStateActive        BridgeState = "active"
	BridgeStateDegraded      BridgeState = "degraded"
	BridgeStateShuttingDown  BridgeState = "shutting_down"
	BridgeStateStopped       BridgeState = "stopped"
	BridgeStateError         BridgeState = "error"
)

// BridgeStateMachine manages bridge state transitions
// (spec L4018: Bridge State Machine).
type BridgeStateMachine struct {
	mu            sync.RWMutex
	state         BridgeState
	previousState BridgeState
	enteredAt     time.Time
	transitions   map[BridgeState][]BridgeState // valid transitions
}

// NewBridgeStateMachine creates a new BridgeStateMachine
// (spec L4018: Bridge State Machine).
func NewBridgeStateMachine() *BridgeStateMachine {
	return &BridgeStateMachine{
		state:       BridgeStateUninitialized,
		enteredAt:   time.Now(),
		transitions: defaultTransitions(),
	}
}

func defaultTransitions() map[BridgeState][]BridgeState {
	return map[BridgeState][]BridgeState{
		BridgeStateUninitialized: {BridgeStateInitializing},
		BridgeStateInitializing:  {BridgeStateReady, BridgeStateError},
		BridgeStateReady:         {BridgeStateActive, BridgeStateShuttingDown, BridgeStateError},
		BridgeStateActive:        {BridgeStateDegraded, BridgeStateShuttingDown, BridgeStateError},
		BridgeStateDegraded:      {BridgeStateActive, BridgeStateShuttingDown, BridgeStateError},
		BridgeStateShuttingDown:  {BridgeStateStopped, BridgeStateError},
		BridgeStateStopped:       {BridgeStateInitializing},
		BridgeStateError:         {BridgeStateInitializing, BridgeStateStopped},
	}
}

// Transition attempts to transition to a new state
// (spec L4018: Bridge State Machine).
func (sm *BridgeStateMachine) Transition(newState BridgeState) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	valid, ok := sm.transitions[sm.state]
	if !ok {
		return fmt.Errorf("state: unknown current state %q", sm.state)
	}
	allowed := false
	for _, s := range valid {
		if s == newState {
			allowed = true
			break
		}
	}
	if !allowed {
		return fmt.Errorf("state: invalid transition %s -> %s", sm.state, newState)
	}
	sm.previousState = sm.state
	sm.state = newState
	sm.enteredAt = time.Now()
	return nil
}

// State returns the current state
// (spec L4018: Bridge State Machine).
func (sm *BridgeStateMachine) State() BridgeState {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.state
}

// PreviousState returns the previous state
// (spec L4018: Bridge State Machine).
func (sm *BridgeStateMachine) PreviousState() BridgeState {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.previousState
}

// EnteredAt returns when the current state was entered
// (spec L4018: Bridge State Machine).
func (sm *BridgeStateMachine) EnteredAt() time.Time {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.enteredAt
}

// CanTransition reports whether a transition is valid
// (spec L4018: Bridge State Machine).
func (sm *BridgeStateMachine) CanTransition(newState BridgeState) bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	valid, ok := sm.transitions[sm.state]
	if !ok {
		return false
	}
	for _, s := range valid {
		if s == newState {
			return true
		}
	}
	return false
}

// IsValidBridgeState reports whether a state is valid
// (spec L4018: Bridge State Machine).
func IsValidBridgeState(s BridgeState) bool {
	switch s {
	case BridgeStateUninitialized, BridgeStateInitializing, BridgeStateReady,
		BridgeStateActive, BridgeStateDegraded, BridgeStateShuttingDown,
		BridgeStateStopped, BridgeStateError:
		return true
	}
	return false
}

// IsTerminalState reports whether a state is terminal (no further
// transitions except restart)
// (spec L4018: Bridge State Machine).
func IsTerminalState(s BridgeState) bool {
	return s == BridgeStateStopped || s == BridgeStateError
}

// IsActiveState reports whether a state is an active/running state
// (spec L4018: Bridge State Machine).
func IsActiveState(s BridgeState) bool {
	return s == BridgeStateReady || s == BridgeStateActive || s == BridgeStateDegraded
}

// String returns a diagnostic summary.
func (sm *BridgeStateMachine) String() string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return fmt.Sprintf("BridgeStateMachine{state:%s previous:%s}", sm.state, sm.previousState)
}
