package bridge

import (
	"context"
	"fmt"
	"strings"
	"sync"

	browserprocess "github.com/Christopher-Schulze/Artemis/process"
)

// init.go (spec L4018: bridge/init.go - Bridge initialization and
// lifecycle).
//
// This file provides the spec-mandated bridge initialization and
// lifecycle management. Handles validated Chromium startup/shutdown
// and accepts only a validated target-script contract for stealth.

// BridgeInitConfig configures bridge initialization
// (spec L4018: Lifecycle, Chrome Launch + Stealth Injection).
type BridgeInitConfig struct {
	ProviderName         string                              `json:"providerName"`
	Headless             bool                                `json:"headless"`
	StealthEnabled       bool                                `json:"stealthEnabled"`
	TargetScripts        TargetScriptConfig                  `json:"-"`
	MaxTabs              int                                 `json:"maxTabs"`
	UserDataDir          string                              `json:"userDataDir,omitempty"`
	ChromePath           string                              `json:"chromePath,omitempty"`
	DependencyAuthorizer browserprocess.DependencyAuthorizer `json:"-"`
	ArtifactVersion      string                              `json:"artifactVersion,omitempty"`
}

// BridgeInitializer manages bridge initialization and lifecycle
// (spec L4018: Lifecycle, Chrome Launch + Stealth Injection).
type BridgeInitializer struct {
	mu       sync.Mutex
	config   BridgeInitConfig
	registry *BridgeProviderRegistry
	provider BrowserProvider
	session  *BridgeSession
	state    *BridgeStateMachine
	started  bool
}

// NewBridgeInitializer creates a new BridgeInitializer
// (spec L4018: Lifecycle).
func NewBridgeInitializer(config BridgeInitConfig) *BridgeInitializer {
	config.ApplyDefaults()
	return &BridgeInitializer{
		config:   config,
		registry: NewBridgeProviderRegistry(),
		state:    NewBridgeStateMachine(),
	}
}

// Start initializes the bridge and launches validated Chromium.
func (bi *BridgeInitializer) Start(ctx context.Context) error {
	bi.mu.Lock()
	defer bi.mu.Unlock()
	if bi.started {
		return fmt.Errorf("init: bridge already started")
	}
	if ctx == nil {
		return fmt.Errorf("init: context required")
	}
	if bi.config.StealthEnabled && bi.config.TargetScripts.PageScript == "" && bi.config.TargetScripts.WorkerScript == "" {
		return fmt.Errorf("init: stealth enabled without a validated target script contract")
	}
	if bi.config.DependencyAuthorizer == nil || strings.TrimSpace(bi.config.ArtifactVersion) == "" {
		return fmt.Errorf("init: Chromium dependency authority and artifact version are required")
	}
	if err := bi.state.Transition(BridgeStateInitializing); err != nil {
		return err
	}
	provider, err := bi.registry.Get(bi.config.ProviderName)
	if err != nil {
		_ = bi.state.Transition(BridgeStateError)
		return err
	}
	session, err := provider.Launch(ctx, ProviderConfig{
		Headless: bi.config.Headless, SessionName: "artemis-bridge", ProfileDir: bi.config.UserDataDir,
		ChromePath: bi.config.ChromePath, MaxTabs: bi.config.MaxTabs,
		DependencyAuthorizer: bi.config.DependencyAuthorizer, Artifact: "chromium",
		ArtifactVersion: bi.config.ArtifactVersion, RequireDependencyAuthorization: true,
	})
	if err != nil {
		_ = bi.state.Transition(BridgeStateError)
		return err
	}
	if bi.config.TargetScripts.PageScript != "" || bi.config.TargetScripts.WorkerScript != "" {
		if session.Runtime == nil {
			_ = provider.Close()
			_ = bi.state.Transition(BridgeStateError)
			return fmt.Errorf("init: target scripts require a CDP runtime")
		}
		if err := session.Runtime.ConfigureTargetScripts(bi.config.TargetScripts); err != nil {
			_ = provider.Close()
			_ = bi.state.Transition(BridgeStateError)
			return fmt.Errorf("init: configure target scripts: %w", err)
		}
	}
	if err := bi.state.Transition(BridgeStateReady); err != nil {
		_ = provider.Close()
		return err
	}
	bi.provider = provider
	bi.session = session
	bi.started = true
	return nil
}

// Stop shuts down the bridge
// (spec L4018: Lifecycle).
func (bi *BridgeInitializer) Stop() error {
	bi.mu.Lock()
	defer bi.mu.Unlock()
	if !bi.started {
		return fmt.Errorf("init: bridge not started")
	}
	state := bi.state.State()
	if state != BridgeStateError {
		if err := bi.state.Transition(BridgeStateShuttingDown); err != nil {
			return err
		}
	}
	err := bi.provider.Close()
	if err != nil {
		if state != BridgeStateError {
			_ = bi.state.Transition(BridgeStateError)
		}
		return err
	}
	if err := bi.state.Transition(BridgeStateStopped); err != nil {
		return err
	}
	bi.provider = nil
	bi.session = nil
	bi.started = false
	return nil
}

// IsStarted reports whether the bridge is started
// (spec L4018: Lifecycle).
func (bi *BridgeInitializer) IsStarted() bool {
	bi.mu.Lock()
	defer bi.mu.Unlock()
	return bi.started
}

// Config returns the initialization config
// (spec L4018: Lifecycle).
func (bi *BridgeInitializer) Config() BridgeInitConfig {
	bi.mu.Lock()
	defer bi.mu.Unlock()
	return bi.config
}

// Registry returns the provider registry
// (spec L4018: Chrome Launch).
func (bi *BridgeInitializer) Registry() *BridgeProviderRegistry {
	bi.mu.Lock()
	defer bi.mu.Unlock()
	return bi.registry
}

// ApplyDefaults applies default values to the init config
// (spec L4018: Lifecycle).
func (c *BridgeInitConfig) ApplyDefaults() {
	if c.ProviderName == "" {
		c.ProviderName = "local-chrome"
	}
	if c.MaxTabs <= 0 {
		c.MaxTabs = 10
	}
}

// Session returns the active validated bridge session.
func (bi *BridgeInitializer) Session() *BridgeSession {
	bi.mu.Lock()
	defer bi.mu.Unlock()
	return bi.session
}

// State returns the bridge lifecycle state.
func (bi *BridgeInitializer) State() BridgeState {
	return bi.state.State()
}

// String returns a diagnostic summary.
func (bi *BridgeInitializer) String() string {
	return fmt.Sprintf("BridgeInitializer{started:%v provider:%s maxTabs:%d}",
		bi.IsStarted(), bi.config.ProviderName, bi.config.MaxTabs)
}
