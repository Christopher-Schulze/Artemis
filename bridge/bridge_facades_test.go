package bridge

import (
	"context"
	"testing"
)

// ==================== bridge.go tests ====================

// TestTASK2257_NewBridgeContextTree verifies context tree creation
// (spec L4018: Context Hierarchy).
func TestTASK2257_NewBridgeContextTree(t *testing.T) {
	tree := NewBridgeContextTree()
	if tree == nil {
		t.Fatal("tree should not be nil")
	}
}

// TestTASK2257_BridgeContextNode verifies context node
// (spec L4018: Context Hierarchy).
func TestTASK2257_BridgeContextNode(t *testing.T) {
	node := BridgeContextNode{ID: "ctx-1", ParentID: "root", Kind: "tab"}
	if node.ID != "ctx-1" {
		t.Error("ID mismatch")
	}
}

// TestTASK2257_NewBridgeProviderRegistry verifies registry creation
// (spec L4018: Chrome Launch).
func TestTASK2257_NewBridgeProviderRegistry(t *testing.T) {
	r := NewBridgeProviderRegistry()
	if r == nil {
		t.Fatal("registry should not be nil")
	}
}

// TestTASK2257_IsValidContextKind verifies validation
// (spec L4018: Context Hierarchy AllocCtx -> BrowserCtx -> TabCtx).
func TestTASK2257_IsValidContextKind(t *testing.T) {
	if !IsValidContextKind(ContextKindAlloc) {
		t.Error("alloc should be valid")
	}
	if !IsValidContextKind(ContextKindBrowser) {
		t.Error("browser should be valid")
	}
	if !IsValidContextKind(ContextKindTab) {
		t.Error("tab should be valid")
	}
	if IsValidContextKind(ContextKind("invalid")) {
		t.Error("invalid should not be valid")
	}
}

// TestTASK2257_IsParentContext verifies parent check
// (spec L4018: Context Hierarchy).
func TestTASK2257_IsParentContext(t *testing.T) {
	if !IsParentContext(ContextKindAlloc) {
		t.Error("alloc should be parent")
	}
	if !IsParentContext(ContextKindBrowser) {
		t.Error("browser should be parent")
	}
	if IsParentContext(ContextKindTab) {
		t.Error("tab should not be parent")
	}
}

// TestTASK2257_IsLeafContext verifies leaf check
// (spec L4018: Context Hierarchy).
func TestTASK2257_IsLeafContext(t *testing.T) {
	if !IsLeafContext(ContextKindTab) {
		t.Error("tab should be leaf")
	}
	if IsLeafContext(ContextKindBrowser) {
		t.Error("browser should not be leaf")
	}
}

// ==================== init.go tests ====================

// TestTASK2257_NewBridgeInitializer verifies creation
// (spec L4018: Lifecycle).
func TestTASK2257_NewBridgeInitializer(t *testing.T) {
	bi := NewBridgeInitializer(BridgeInitConfig{ProviderName: "local"})
	if bi == nil {
		t.Fatal("initializer should not be nil")
	}
	if bi.IsStarted() {
		t.Error("should not be started initially")
	}
}

// TestTASK2257_BridgeInitStart verifies start
// (spec L4018: Chrome Launch + Stealth Injection).
func TestTASK2257_BridgeInitStart(t *testing.T) {
	bi := NewBridgeInitializer(BridgeInitConfig{})
	err := bi.Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !bi.IsStarted() {
		t.Error("should be started")
	}
}

// TestTASK2257_BridgeInitStartTwice verifies double start fails.
func TestTASK2257_BridgeInitStartTwice(t *testing.T) {
	bi := NewBridgeInitializer(BridgeInitConfig{})
	bi.Start(context.Background())
	err := bi.Start(context.Background())
	if err == nil {
		t.Error("double start should fail")
	}
}

// TestTASK2257_BridgeInitStop verifies stop
// (spec L4018: Lifecycle).
func TestTASK2257_BridgeInitStop(t *testing.T) {
	bi := NewBridgeInitializer(BridgeInitConfig{})
	bi.Start(context.Background())
	err := bi.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if bi.IsStarted() {
		t.Error("should not be started after stop")
	}
}

// TestTASK2257_BridgeInitStopNotStarted verifies stop without start.
func TestTASK2257_BridgeInitStopNotStarted(t *testing.T) {
	bi := NewBridgeInitializer(BridgeInitConfig{})
	err := bi.Stop()
	if err == nil {
		t.Error("stop without start should fail")
	}
}

// TestTASK2257_BridgeInitConfigDefaults verifies defaults
// (spec L4018: Lifecycle).
func TestTASK2257_BridgeInitConfigDefaults(t *testing.T) {
	cfg := BridgeInitConfig{}
	cfg.ApplyDefaults()
	if cfg.ProviderName != "local-chrome" {
		t.Error("default provider should be local-chrome")
	}
	if cfg.MaxTabs != 10 {
		t.Error("default max tabs should be 10")
	}
}

// TestTASK2257_BridgeInitConfig verifies config retrieval
// (spec L4018: Lifecycle).
func TestTASK2257_BridgeInitConfig(t *testing.T) {
	bi := NewBridgeInitializer(BridgeInitConfig{ProviderName: "test", MaxTabs: 5})
	cfg := bi.Config()
	if cfg.ProviderName != "test" {
		t.Error("provider mismatch")
	}
	if cfg.MaxTabs != 5 {
		t.Error("max tabs mismatch")
	}
}

// TestTASK2257_BridgeInitRegistry verifies registry access
// (spec L4018: Chrome Launch).
func TestTASK2257_BridgeInitRegistry(t *testing.T) {
	bi := NewBridgeInitializer(BridgeInitConfig{})
	r := bi.Registry()
	if r == nil {
		t.Error("registry should not be nil")
	}
}

// ==================== state.go tests ====================

// TestTASK2257_NewBridgeStateMachine verifies creation
// (spec L4018: Bridge State Machine).
func TestTASK2257_NewBridgeStateMachine(t *testing.T) {
	sm := NewBridgeStateMachine()
	if sm == nil {
		t.Fatal("state machine should not be nil")
	}
	if sm.State() != BridgeStateUninitialized {
		t.Error("initial state should be uninitialized")
	}
}

// TestTASK2257_BridgeStateTransition verifies valid transition
// (spec L4018: Bridge State Machine).
func TestTASK2257_BridgeStateTransition(t *testing.T) {
	sm := NewBridgeStateMachine()
	err := sm.Transition(BridgeStateInitializing)
	if err != nil {
		t.Fatalf("transition to initializing: %v", err)
	}
	if sm.State() != BridgeStateInitializing {
		t.Error("state should be initializing")
	}
}

// TestTASK2257_BridgeStateInvalidTransition verifies invalid transition
// (spec L4018: Bridge State Machine).
func TestTASK2257_BridgeStateInvalidTransition(t *testing.T) {
	sm := NewBridgeStateMachine()
	err := sm.Transition(BridgeStateActive)
	if err == nil {
		t.Error("invalid transition should fail")
	}
}

// TestTASK2257_BridgeStateCanTransition verifies can-transition check
// (spec L4018: Bridge State Machine).
func TestTASK2257_BridgeStateCanTransition(t *testing.T) {
	sm := NewBridgeStateMachine()
	if !sm.CanTransition(BridgeStateInitializing) {
		t.Error("should be able to transition to initializing")
	}
	if sm.CanTransition(BridgeStateActive) {
		t.Error("should not be able to transition to active from uninitialized")
	}
}

// TestTASK2257_BridgeStatePrevious verifies previous state
// (spec L4018: Bridge State Machine).
func TestTASK2257_BridgeStatePrevious(t *testing.T) {
	sm := NewBridgeStateMachine()
	sm.Transition(BridgeStateInitializing)
	if sm.PreviousState() != BridgeStateUninitialized {
		t.Error("previous should be uninitialized")
	}
}

// TestTASK2257_BridgeStateFullCycle verifies full lifecycle
// (spec L4018: Bridge State Machine).
func TestTASK2257_BridgeStateFullCycle(t *testing.T) {
	sm := NewBridgeStateMachine()
	steps := []BridgeState{
		BridgeStateInitializing,
		BridgeStateReady,
		BridgeStateActive,
		BridgeStateShuttingDown,
		BridgeStateStopped,
	}
	for _, state := range steps {
		err := sm.Transition(state)
		if err != nil {
			t.Errorf("transition to %s: %v", state, err)
		}
	}
	if sm.State() != BridgeStateStopped {
		t.Error("should be stopped after full cycle")
	}
}

// TestTASK2257_IsValidBridgeState verifies validation.
func TestTASK2257_IsValidBridgeState(t *testing.T) {
	if !IsValidBridgeState(BridgeStateReady) {
		t.Error("ready should be valid")
	}
	if IsValidBridgeState(BridgeState("invalid")) {
		t.Error("invalid should not be valid")
	}
}

// TestTASK2257_IsTerminalState verifies terminal check.
func TestTASK2257_IsTerminalState(t *testing.T) {
	if !IsTerminalState(BridgeStateStopped) {
		t.Error("stopped should be terminal")
	}
	if !IsTerminalState(BridgeStateError) {
		t.Error("error should be terminal")
	}
	if IsTerminalState(BridgeStateActive) {
		t.Error("active should not be terminal")
	}
}

// TestTASK2257_IsActiveState verifies active check.
func TestTASK2257_IsActiveState(t *testing.T) {
	if !IsActiveState(BridgeStateActive) {
		t.Error("active should be active state")
	}
	if !IsActiveState(BridgeStateReady) {
		t.Error("ready should be active state")
	}
	if IsActiveState(BridgeStateStopped) {
		t.Error("stopped should not be active state")
	}
}

// ==================== full spec parity test ====================

// TestTASK2257_FullSpecParity verifies all 3 spec-mandated files
// (spec L4018: bridge.go, init.go, state.go).
func TestTASK2257_FullSpecParity(t *testing.T) {
	// 1. bridge.go - CDP Bridge context hierarchy
	tree := NewBridgeContextTree()
	if tree == nil {
		t.Error("bridge.go: context tree creation failed")
	}
	if !IsValidContextKind(ContextKindTab) {
		t.Error("bridge.go: context kind validation failed")
	}

	// 2. init.go - Bridge initialization and lifecycle
	bi := NewBridgeInitializer(BridgeInitConfig{})
	if err := bi.Start(context.Background()); err != nil {
		t.Error("init.go: start failed")
	}
	bi.Stop()

	// 3. state.go - Bridge State Machine
	sm := NewBridgeStateMachine()
	if sm.State() != BridgeStateUninitialized {
		t.Error("state.go: initial state wrong")
	}
	if err := sm.Transition(BridgeStateInitializing); err != nil {
		t.Error("state.go: transition failed")
	}
}
