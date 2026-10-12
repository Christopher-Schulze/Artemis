package stealth

import (
	"fmt"
	"sync"
)

// fingerprint.go (spec L4023: stealth/fingerprint.go - browser
// fingerprint spoofing).
//
// Anti-detection: browser fingerprint spoofing for canvas, WebGL,
// audio context, and navigator properties. The actual JS patches
// live in patches.go; this file provides the Go-side fingerprint
// configuration and management types.

// FingerprintConfig describes browser fingerprint spoofing settings
// (spec L4023: fingerprint.go - browser fingerprint spoofing).
type FingerprintConfig struct {
	CanvasNoise         bool   `json:"canvasNoise"`         // add noise to canvas rendering
	WebGLOverride       bool   `json:"webglOverride"`       // override WebGL renderer
	AudioNoise          bool   `json:"audioNoise"`          // add noise to AudioContext
	PlatformOverride    string `json:"platformOverride"`    // override navigator.platform
	HardwareConcurrency int    `json:"hardwareConcurrency"` // override hardwareConcurrency
	DeviceMemory        int    `json:"deviceMemory"`        // override deviceMemory
}

// DefaultFingerprintConfig returns the default fingerprint config
// (spec L4023: fingerprint.go - browser fingerprint spoofing).
func DefaultFingerprintConfig() FingerprintConfig {
	return FingerprintConfig{
		CanvasNoise:         true,
		WebGLOverride:       true,
		AudioNoise:          true,
		PlatformOverride:    "MacIntel",
		HardwareConcurrency: 8,
		DeviceMemory:        8,
	}
}

// FingerprintManager manages browser fingerprint spoofing
// (spec L4023: fingerprint.go - browser fingerprint spoofing).
type FingerprintManager struct {
	mu      sync.RWMutex
	current FingerprintConfig
}

// NewFingerprintManager creates a new FingerprintManager with defaults
// (spec L4023).
func NewFingerprintManager() *FingerprintManager {
	return &FingerprintManager{current: DefaultFingerprintConfig()}
}

// SetConfig sets the fingerprint configuration
// (spec L4023: fingerprint.go - browser fingerprint spoofing).
func (m *FingerprintManager) SetConfig(c FingerprintConfig) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.current = c
}

// Current returns the current fingerprint config
// (spec L4023).
func (m *FingerprintManager) Current() FingerprintConfig {
	if m == nil {
		return DefaultFingerprintConfig()
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current
}

// IsCanvasNoiseEnabled reports whether canvas noise is enabled
// (spec L4023: fingerprint.go).
func (m *FingerprintManager) IsCanvasNoiseEnabled() bool {
	if m == nil {
		return true
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current.CanvasNoise
}

// IsWebGLOverrideEnabled reports whether WebGL override is enabled
// (spec L4023: fingerprint.go).
func (m *FingerprintManager) IsWebGLOverrideEnabled() bool {
	if m == nil {
		return true
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current.WebGLOverride
}

// Validate checks that the fingerprint config is valid
// (spec L4023: fingerprint.go - browser fingerprint spoofing).
func (c FingerprintConfig) Validate() error {
	if c.HardwareConcurrency < 1 || c.HardwareConcurrency > 64 {
		return fmt.Errorf("fingerprint: hardwareConcurrency %d out of range [1,64]", c.HardwareConcurrency)
	}
	if c.DeviceMemory < 1 || c.DeviceMemory > 32 {
		return fmt.Errorf("fingerprint: deviceMemory %d out of range [1,32]", c.DeviceMemory)
	}
	return nil
}

// String returns a diagnostic summary.
func (c FingerprintConfig) String() string {
	return fmt.Sprintf("FingerprintConfig{canvas:%v webgl:%v audio:%v platform:%s cores:%d mem:%d}",
		c.CanvasNoise, c.WebGLOverride, c.AudioNoise, c.PlatformOverride, c.HardwareConcurrency, c.DeviceMemory)
}
