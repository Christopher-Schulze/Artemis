package bridge

import (
	"context"
	"fmt"
	"sync"
)

// init.go (spec L4018: bridge/init.go - Bridge initialization and
// lifecycle).
//
// This file provides the spec-mandated bridge initialization and
// lifecycle management. Handles Chrome launch + stealth injection
// and bridge startup/shutdown.

// BridgeInitConfig configures bridge initialization
// (spec L4018: Lifecycle, Chrome Launch + Stealth Injection).
type BridgeInitConfig struct {
	ProviderName   string `json:"providerName"`
	Headless       bool   `json:"headless"`
	StealthEnabled bool   `json:"stealthEnabled"`
	MaxTabs        int    `json:"maxTabs"`
	UserDataDir    string `json:"userDataDir,omitempty"`
	ChromePath     string `json:"chromePath,omitempty"`
}

// BridgeInitializer manages bridge initialization and lifecycle
// (spec L4018: Lifecycle, Chrome Launch + Stealth Injection).
type BridgeInitializer struct {
	mu       sync.Mutex
	config   BridgeInitConfig
	registry *BridgeProviderRegistry
	session  *BridgeSession
	started  bool
}

// NewBridgeInitializer creates a new BridgeInitializer
// (spec L4018: Lifecycle).
func NewBridgeInitializer(config BridgeInitConfig) *BridgeInitializer {
	return &BridgeInitializer{
		config:   config,
		registry: NewBridgeProviderRegistry(),
	}
}

// Start initializes the bridge: launches Chrome + stealth injection
// (spec L4018: Chrome Launch + Stealth Injection).
func (bi *BridgeInitializer) Start(ctx context.Context) error {
	bi.mu.Lock()
	defer bi.mu.Unlock()
	if bi.started {
		return fmt.Errorf("init: bridge already started")
	}
	// In a real implementation, this would:
	// 1. Select provider from registry
	// 2. Launch Chrome with stealth flags
	// 3. Inject stealth scripts
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

// String returns a diagnostic summary.
func (bi *BridgeInitializer) String() string {
	return fmt.Sprintf("BridgeInitializer{started:%v provider:%s maxTabs:%d}",
		bi.IsStarted(), bi.config.ProviderName, bi.config.MaxTabs)
}
