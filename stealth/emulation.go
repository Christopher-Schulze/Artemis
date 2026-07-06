package stealth

import (
	"fmt"
	"sync"
)

// emulation.go (spec L4023: stealth/emulation.go - device/screen
// emulation).
//
// Anti-detection: device/screen emulation for matching real browser
// viewport dimensions, device scale factor, and touch simulation.

// DeviceEmulation describes device/screen emulation settings
// (spec L4023: emulation.go - device/screen emulation).
type DeviceEmulation struct {
	Width             int     `json:"width"`             // layout viewport width
	Height            int     `json:"height"`            // layout viewport height
	DeviceScaleFactor float64 `json:"deviceScaleFactor"` // CSS pixel ratio
	Mobile            bool    `json:"mobile"`            // mobile vs desktop
	TouchEnabled      bool    `json:"touchEnabled"`      // touch input simulation
	ScreenWidth       int     `json:"screenWidth"`       // screen width (>= viewport)
	ScreenHeight      int     `json:"screenHeight"`      // screen height (>= viewport)
}

// DefaultDesktopEmulation returns the default desktop emulation
// (spec L4023: emulation.go - device/screen emulation).
// 1920x1080 viewport, scale factor 1, no touch.
func DefaultDesktopEmulation() DeviceEmulation {
	return DeviceEmulation{
		Width:             1920,
		Height:            1080,
		DeviceScaleFactor: 1.0,
		Mobile:            false,
		TouchEnabled:      false,
		ScreenWidth:       1920,
		ScreenHeight:      1080,
	}
}

// DefaultMobileEmulation returns the default mobile emulation
// (spec L4023: emulation.go - device/screen emulation).
// 390x844 viewport (iPhone 14), scale factor 3, touch enabled.
func DefaultMobileEmulation() DeviceEmulation {
	return DeviceEmulation{
		Width:             390,
		Height:            844,
		DeviceScaleFactor: 3.0,
		Mobile:            true,
		TouchEnabled:      true,
		ScreenWidth:       390,
		ScreenHeight:      844,
	}
}

// EmulationManager manages device/screen emulation profiles
// (spec L4023: emulation.go).
type EmulationManager struct {
	mu      sync.RWMutex
	current DeviceEmulation
	preset  string
}

// NewEmulationManager creates a new EmulationManager with desktop
// defaults (spec L4023).
func NewEmulationManager() *EmulationManager {
	return &EmulationManager{
		current: DefaultDesktopEmulation(),
		preset:  "desktop",
	}
}

// SetEmulation sets the current device emulation
// (spec L4023: emulation.go - device/screen emulation).
func (m *EmulationManager) SetEmulation(e DeviceEmulation, preset string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.current = e
	m.preset = preset
}

// Current returns the current device emulation
// (spec L4023).
func (m *EmulationManager) Current() DeviceEmulation {
	if m == nil {
		return DefaultDesktopEmulation()
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current
}

// Preset returns the current preset name
// (spec L4023).
func (m *EmulationManager) Preset() string {
	if m == nil {
		return "desktop"
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.preset
}

// IsMobile reports whether the current emulation is mobile
// (spec L4023).
func (m *EmulationManager) IsMobile() bool {
	if m == nil {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current.Mobile
}

// Validate checks that the emulation settings are valid
// (spec L4023: viewport{width∈[320,3840], height∈[240,2160] integer}).
func (e DeviceEmulation) Validate() error {
	if e.Width < 320 || e.Width > 3840 {
		return fmt.Errorf("emulation: width %d out of range [320,3840]", e.Width)
	}
	if e.Height < 240 || e.Height > 2160 {
		return fmt.Errorf("emulation: height %d out of range [240,2160]", e.Height)
	}
	if e.DeviceScaleFactor < 0 || e.DeviceScaleFactor > 10 {
		return fmt.Errorf("emulation: deviceScaleFactor %.1f out of range [0,10]", e.DeviceScaleFactor)
	}
	if e.ScreenWidth > 0 && e.ScreenWidth < e.Width {
		return fmt.Errorf("emulation: screenWidth %d < width %d", e.ScreenWidth, e.Width)
	}
	if e.ScreenHeight > 0 && e.ScreenHeight < e.Height {
		return fmt.Errorf("emulation: screenHeight %d < height %d", e.ScreenHeight, e.Height)
	}
	return nil
}

// String returns a diagnostic summary.
func (e DeviceEmulation) String() string {
	return fmt.Sprintf("DeviceEmulation{%dx%d scale:%.1f mobile:%v touch:%v}",
		e.Width, e.Height, e.DeviceScaleFactor, e.Mobile, e.TouchEnabled)
}
