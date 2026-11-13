package bridge

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// ==================== bridge.go tests ====================

// TestNewBridgeContextTree verifies context tree creation
// (Context Hierarchy).
func TestNewBridgeContextTree(t *testing.T) {
	tree := NewBridgeContextTree()
	if tree == nil {
		t.Fatal("tree should not be nil")
	}
}

// TestBridgeContextUnit verifies a context hierarchy unit
// (Context Hierarchy).
func TestBridgeContextUnit(t *testing.T) {
	unit := BridgeContextUnit{ID: "ctx-1", ParentID: "root", Kind: "tab"}
	if unit.ID != "ctx-1" {
		t.Error("ID mismatch")
	}
}

// TestNewBridgeProviderRegistry verifies registry creation
// (Chrome Launch).
func TestNewBridgeProviderRegistry(t *testing.T) {
	r := NewBridgeProviderRegistry()
	if r == nil {
		t.Fatal("registry should not be nil")
	}
}

// TestIsValidContextKind verifies validation
// (Context Hierarchy AllocCtx -> BrowserCtx -> TabCtx).
func TestIsValidContextKind(t *testing.T) {
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

// TestIsParentContext verifies parent check
// (Context Hierarchy).
func TestIsParentContext(t *testing.T) {
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

// TestIsLeafContext verifies leaf check
// (Context Hierarchy).
func TestIsLeafContext(t *testing.T) {
	if !IsLeafContext(ContextKindTab) {
		t.Error("tab should be leaf")
	}
	if IsLeafContext(ContextKindBrowser) {
		t.Error("browser should not be leaf")
	}
}

// ==================== init.go tests ====================

// TestNewBridgeInitializer verifies creation
// (Lifecycle).
func TestNewBridgeInitializer(t *testing.T) {
	bi := NewBridgeInitializer(BridgeInitConfig{ProviderName: "local"})
	if bi == nil {
		t.Fatal("initializer should not be nil")
	}
	if bi.IsStarted() {
		t.Error("should not be started initially")
	}
}

// TestBridgeInitStart verifies start
// (Chrome Launch + Stealth Injection).
func TestBridgeInitStart(t *testing.T) {
	bi := NewBridgeInitializer(testBridgeInitConfig())
	t.Cleanup(func() {
		if bi.IsStarted() {
			closeBridgeTestResource(t, "bridge initializer", bi.Stop)
		}
	})
	err := bi.Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !bi.IsStarted() {
		t.Error("should be started")
	}
}

// TestBridgeInitStartTwice verifies double start fails.
func TestBridgeInitStartTwice(t *testing.T) {
	bi := NewBridgeInitializer(testBridgeInitConfig())
	t.Cleanup(func() {
		if bi.IsStarted() {
			closeBridgeTestResource(t, "bridge initializer", bi.Stop)
		}
	})
	if err := bi.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	err := bi.Start(context.Background())
	if err == nil {
		t.Error("double start should fail")
	}
}

// TestBridgeInitStop verifies stop
// (Lifecycle).
func TestBridgeInitStop(t *testing.T) {
	bi := NewBridgeInitializer(testBridgeInitConfig())
	if err := bi.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	err := bi.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if bi.IsStarted() {
		t.Error("should not be started after stop")
	}
}

// TestBridgeInitStopNotStarted verifies stop without start.
func TestBridgeInitStopNotStarted(t *testing.T) {
	bi := NewBridgeInitializer(testBridgeInitConfig())
	err := bi.Stop()
	if err == nil {
		t.Error("stop without start should fail")
	}
}

func TestBridgeInitializerCanRestartAfterCloseFailure(t *testing.T) {
	provider := &initializerTestProvider{closeErr: errors.New("close failed")}
	config := testBridgeInitConfig()
	config.ProviderName = provider.Name()
	bi := NewBridgeInitializer(config)
	bi.Registry().Register(provider.Name(), provider)
	if err := bi.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := bi.Stop(); err == nil {
		t.Fatal("close failure was hidden")
	}
	if !bi.IsStarted() || bi.Session() == nil || bi.State() != BridgeStateError {
		t.Fatalf("failed close lost retry ownership: started=%v session=%v state=%s", bi.IsStarted(), bi.Session(), bi.State())
	}
	if err := bi.Stop(); err != nil {
		t.Fatalf("close retry: %v", err)
	}
	if bi.IsStarted() || bi.Session() != nil || bi.State() != BridgeStateStopped {
		t.Fatalf("successful close retry retained runtime: started=%v session=%v state=%s", bi.IsStarted(), bi.Session(), bi.State())
	}
	if err := bi.Start(context.Background()); err != nil {
		t.Fatalf("restart after close retry: %v", err)
	}
	if err := bi.Stop(); err != nil {
		t.Fatalf("final stop: %v", err)
	}
}

func TestBridgeInitializerRejectsMissingDependencyAuthority(t *testing.T) {
	config := testBridgeInitConfig()
	config.DependencyAuthorizer = nil
	bi := NewBridgeInitializer(config)
	if err := bi.Start(context.Background()); err == nil || !strings.Contains(err.Error(), "dependency authority") {
		t.Fatalf("missing Chromium dependency authority must fail before provider launch, got %v", err)
	}
	if bi.IsStarted() {
		t.Fatal("bridge must remain stopped after dependency-authority rejection")
	}
}

type initializerTestProvider struct {
	closeErr error
}

func (p *initializerTestProvider) Name() string { return "initializer-test" }

func (p *initializerTestProvider) Launch(context.Context, ProviderConfig) (*BrowserSession, error) {
	return &BrowserSession{ProviderName: p.Name(), SessionID: "test"}, nil
}

func (p *initializerTestProvider) Close() error {
	err := p.closeErr
	p.closeErr = nil
	return err
}

func (p *initializerTestProvider) Healthy() bool { return true }

// TestBridgeInitConfigDefaults verifies defaults
// (Lifecycle).
func TestBridgeInitConfigDefaults(t *testing.T) {
	cfg := BridgeInitConfig{}
	cfg.ApplyDefaults()
	if cfg.ProviderName != "local-chrome" {
		t.Error("default provider should be local-chrome")
	}
	if cfg.MaxTabs != 10 {
		t.Error("default max tabs should be 10")
	}
}

// TestBridgeInitConfig verifies config retrieval
// (Lifecycle).
func TestBridgeInitConfig(t *testing.T) {
	bi := NewBridgeInitializer(BridgeInitConfig{ProviderName: "test", MaxTabs: 5})
	cfg := bi.Config()
	if cfg.ProviderName != "test" {
		t.Error("provider mismatch")
	}
	if cfg.MaxTabs != 5 {
		t.Error("max tabs mismatch")
	}
}

// TestBridgeInitRegistry verifies registry access
// (Chrome Launch).
func TestBridgeInitRegistry(t *testing.T) {
	bi := NewBridgeInitializer(BridgeInitConfig{})
	r := bi.Registry()
	if r == nil {
		t.Error("registry should not be nil")
	}
}

// ==================== state.go tests ====================

// TestNewBridgeStateMachine verifies creation
// (Bridge State Machine).
func TestNewBridgeStateMachine(t *testing.T) {
	sm := NewBridgeStateMachine()
	if sm == nil {
		t.Fatal("state machine should not be nil")
	}
	if sm.State() != BridgeStateUninitialized {
		t.Error("initial state should be uninitialized")
	}
}

// TestBridgeStateTransition verifies valid transition
// (Bridge State Machine).
func TestBridgeStateTransition(t *testing.T) {
	sm := NewBridgeStateMachine()
	err := sm.Transition(BridgeStateInitializing)
	if err != nil {
		t.Fatalf("transition to initializing: %v", err)
	}
	if sm.State() != BridgeStateInitializing {
		t.Error("state should be initializing")
	}
}

// TestBridgeStateInvalidTransition verifies invalid transition
// (Bridge State Machine).
func TestBridgeStateInvalidTransition(t *testing.T) {
	sm := NewBridgeStateMachine()
	err := sm.Transition(BridgeStateActive)
	if err == nil {
		t.Error("invalid transition should fail")
	}
}

// TestBridgeStateCanTransition verifies can-transition check
// (Bridge State Machine).
func TestBridgeStateCanTransition(t *testing.T) {
	sm := NewBridgeStateMachine()
	if !sm.CanTransition(BridgeStateInitializing) {
		t.Error("should be able to transition to initializing")
	}
	if sm.CanTransition(BridgeStateActive) {
		t.Error("should not be able to transition to active from uninitialized")
	}
}

// TestBridgeStatePrevious verifies previous state
// (Bridge State Machine).
func TestBridgeStatePrevious(t *testing.T) {
	sm := NewBridgeStateMachine()
	if err := sm.Transition(BridgeStateInitializing); err != nil {
		t.Fatal(err)
	}
	if sm.PreviousState() != BridgeStateUninitialized {
		t.Error("previous should be uninitialized")
	}
}

// TestBridgeStateFullCycle verifies full lifecycle
// (Bridge State Machine).
func TestBridgeStateFullCycle(t *testing.T) {
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

// TestIsValidBridgeState verifies validation.
func TestIsValidBridgeState(t *testing.T) {
	if !IsValidBridgeState(BridgeStateReady) {
		t.Error("ready should be valid")
	}
	if IsValidBridgeState(BridgeState("invalid")) {
		t.Error("invalid should not be valid")
	}
}

// TestIsTerminalState verifies terminal check.
func TestIsTerminalState(t *testing.T) {
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

// TestIsActiveState verifies active check.
func TestIsActiveState(t *testing.T) {
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

// ==================== parity test ====================

// TestBridgeFacadesParity verifies all 3 spec-mandated files
// (bridge.go, init.go, state.go).
func TestBridgeFacadesParity(t *testing.T) {
	// 1. bridge.go - CDP Bridge context hierarchy
	tree := NewBridgeContextTree()
	if tree == nil {
		t.Error("bridge.go: context tree creation failed")
	}
	if !IsValidContextKind(ContextKindTab) {
		t.Error("bridge.go: context kind validation failed")
	}

	// 2. init.go - Bridge initialization and lifecycle
	bi := NewBridgeInitializer(testBridgeInitConfig())
	if err := bi.Start(context.Background()); err != nil {
		t.Error("init.go: start failed")
	}
	if err := bi.Stop(); err != nil {
		t.Error("init.go: stop failed")
	}

	// 3. state.go - Bridge State Machine
	sm := NewBridgeStateMachine()
	if sm.State() != BridgeStateUninitialized {
		t.Error("state.go: initial state wrong")
	}
	if err := sm.Transition(BridgeStateInitializing); err != nil {
		t.Error("state.go: transition failed")
	}
}
