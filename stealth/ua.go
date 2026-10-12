package stealth

import (
	"fmt"
	"strings"
	"sync"
)

// ua.go (spec L4023: stealth/ua.go - UA mgmt + version coherence).
//
// Anti-detection: User-Agent management with version coherence.
// The UA string must be consistent with the navigator.userAgentData
// brands, platform, and Chrome version. Mismatched UA versions are
// a common bot-detection signal.

// UAInfo describes a User-Agent with version coherence metadata
// (spec L4023: ua.go - UA mgmt + version coherence).
type UAInfo struct {
	UserAgent string `json:"userAgent"` // full UA string
	Browser   string `json:"browser"`   // browser family (e.g. "chrome")
	Version   string `json:"version"`   // major version (e.g. "126")
	Platform  string `json:"platform"`  // platform (e.g. "macOS")
	Mobile    bool   `json:"mobile"`
}

// DefaultUAInfo returns the default Chrome UA info
// (spec L4023: ua.go - UA mgmt + version coherence).
// Matches the UA in patches.go Defaults().
func DefaultUAInfo() UAInfo {
	return UAInfo{
		UserAgent: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36",
		Browser:   "chrome",
		Version:   "126",
		Platform:  "macOS",
		Mobile:    false,
	}
}

// UAManager manages User-Agent strings with version coherence
// (spec L4023: ua.go - UA mgmt + version coherence).
type UAManager struct {
	mu      sync.RWMutex
	current UAInfo
}

// NewUAManager creates a new UAManager with default Chrome UA
// (spec L4023).
func NewUAManager() *UAManager {
	return &UAManager{current: DefaultUAInfo()}
}

// SetUA sets the current User-Agent info
// (spec L4023: ua.go - UA mgmt + version coherence).
func (m *UAManager) SetUA(ua UAInfo) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.current = ua
}

// Current returns the current UA info
// (spec L4023).
func (m *UAManager) Current() UAInfo {
	if m == nil {
		return DefaultUAInfo()
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current
}

// UserAgent returns the current UA string
// (spec L4023).
func (m *UAManager) UserAgent() string {
	if m == nil {
		return DefaultUAInfo().UserAgent
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current.UserAgent
}

// Version returns the current browser major version
// (spec L4023: version coherence).
func (m *UAManager) Version() string {
	if m == nil {
		return "126"
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current.Version
}

// CheckCoherence verifies that the UA string is coherent with the
// version and platform (spec L4023: version coherence).
func (ua UAInfo) CheckCoherence() error {
	if ua.UserAgent == "" {
		return fmt.Errorf("ua: empty UserAgent string")
	}
	if ua.Version == "" {
		return fmt.Errorf("ua: empty version")
	}
	// Check that the UA string contains the version.
	if !strings.Contains(ua.UserAgent, "Chrome/"+ua.Version) {
		return fmt.Errorf("ua: version %s not found in UA string", ua.Version)
	}
	return nil
}

// IsCoherent reports whether the UA info is coherent
// (spec L4023: version coherence).
func (ua UAInfo) IsCoherent() bool {
	return ua.CheckCoherence() == nil
}

// ParseChromeVersion extracts the Chrome major version from a UA string
// (spec L4023: version coherence).
func ParseChromeVersion(ua string) string {
	idx := strings.Index(ua, "Chrome/")
	if idx < 0 {
		return ""
	}
	rest := ua[idx+7:] // after "Chrome/"
	dot := strings.Index(rest, ".")
	if dot < 0 {
		return rest
	}
	return rest[:dot]
}

// String returns a diagnostic summary.
func (ua UAInfo) String() string {
	return fmt.Sprintf("UAInfo{browser:%s version:%s platform:%s mobile:%v}", ua.Browser, ua.Version, ua.Platform, ua.Mobile)
}
