package bridge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// HotBrowserWindow is the 5-minute hot window during which a config hash
// mismatch reuses the running browser and warns instead of recreating
// (spec L4310).
const HotBrowserWindow = 5 * time.Minute

// BrowserBridgeMetadata is the per-bridge metadata stored in the
// BROWSER_BRIDGES registry (spec L4310).
type BrowserBridgeMetadata struct {
	ContainerName string
	ConfigHash    string
	LastUsedAt    time.Time
	Running       bool
	AuthToken     string
	AuthPassword  string
}

// ConfigHashInput is the set of fields that determine the sandbox browser
// config hash. The hash is computed over the JSON-normalized form.
type ConfigHashInput struct {
	CdpPort            int
	CdpSourceRange     string
	VncPort            int
	NoVncPort          int
	Headless           bool
	EnableNoVnc        bool
	AutoStartTimeout   int
	SecurityEpoch      string
	WorkspaceDir       string
	AgentWorkspaceDir  string
	MountFormatVersion int
}

// ConfigHashRegistry manages sandbox browser config hashes and the
// BROWSER_BRIDGES metadata store. It is thread-safe.
type ConfigHashRegistry struct {
	mu      sync.RWMutex
	bridges map[string]BrowserBridgeMetadata
	hashes  map[string]string
}

// NewConfigHashRegistry builds an empty ConfigHashRegistry.
func NewConfigHashRegistry() *ConfigHashRegistry {
	return &ConfigHashRegistry{
		bridges: make(map[string]BrowserBridgeMetadata),
		hashes:  make(map[string]string),
	}
}

// ComputeConfigHash computes the SHA-256 hash of the normalized JSON
// representation of the input. The normalization sorts object keys
// alphabetically and drops undefined/null fields so the hash is stable
// regardless of field declaration order.
func ComputeConfigHash(input ConfigHashInput) string {
	// Build a map and serialize with sorted keys for determinism.
	m := map[string]any{
		"cdpPort":            input.CdpPort,
		"cdpSourceRange":     input.CdpSourceRange,
		"vncPort":            input.VncPort,
		"noVncPort":          input.NoVncPort,
		"headless":           input.Headless,
		"enableNoVnc":        input.EnableNoVnc,
		"autoStartTimeout":   input.AutoStartTimeout,
		"securityEpoch":      input.SecurityEpoch,
		"workspaceDir":       input.WorkspaceDir,
		"agentWorkspaceDir":  input.AgentWorkspaceDir,
		"mountFormatVersion": input.MountFormatVersion,
	}
	// json.Marshal on a map produces sorted keys in Go.
	raw, err := json.Marshal(m)
	if err != nil {
		// Fallback: use fmt.Sprint which is deterministic for simple types.
		raw = []byte(fmt.Sprintf("%+v", input))
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// RegisterBridge stores metadata for a bridge identified by scopeKey.
func (r *ConfigHashRegistry) RegisterBridge(scopeKey string, meta BrowserBridgeMetadata) {
	r.mu.Lock()
	defer r.mu.Unlock()
	meta.LastUsedAt = time.Now()
	r.bridges[scopeKey] = meta
	r.hashes[scopeKey] = meta.ConfigHash
}

// GetBridge returns the metadata for a bridge, or false if not registered.
func (r *ConfigHashRegistry) GetBridge(scopeKey string) (BrowserBridgeMetadata, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	meta, ok := r.bridges[scopeKey]
	return meta, ok
}

// RemoveBridge removes a bridge from the registry.
func (r *ConfigHashRegistry) RemoveBridge(scopeKey string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.bridges[scopeKey]; !ok {
		return false
	}
	delete(r.bridges, scopeKey)
	delete(r.hashes, scopeKey)
	return true
}

// UpdateLastUsed updates the LastUsedAt timestamp for a bridge.
func (r *ConfigHashRegistry) UpdateLastUsed(scopeKey string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	meta, ok := r.bridges[scopeKey]
	if !ok {
		return false
	}
	meta.LastUsedAt = time.Now()
	r.bridges[scopeKey] = meta
	return true
}

// CheckHash compares the expected hash against the stored hash for a
// scopeKey. Returns:
//   - matched=true if the hashes match.
//   - isHot=true if the bridge is running and within the 5-min hot window.
//   - warning string if there is a mismatch within the hot window.
func (r *ConfigHashRegistry) CheckHash(scopeKey, expectedHash string) (matched bool, isHot bool, warning string) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	meta, ok := r.bridges[scopeKey]
	if !ok {
		return false, false, ""
	}
	if meta.ConfigHash == expectedHash {
		return true, false, ""
	}
	// Hash mismatch: check hot window.
	now := time.Now()
	isHot = meta.Running && now.Sub(meta.LastUsedAt) < HotBrowserWindow
	if isHot {
		warning = fmt.Sprintf(
			"Sandbox browser config changed for %s (recently used). Recreate to apply.",
			meta.ContainerName,
		)
	}
	return false, isHot, warning
}

// ShouldRecreate returns true when a hash mismatch occurs outside the hot
// window, meaning the container should be recreated.
func (r *ConfigHashRegistry) ShouldRecreate(scopeKey, expectedHash string) bool {
	matched, isHot, _ := r.CheckHash(scopeKey, expectedHash)
	return !matched && !isHot
}

// ListBridges returns the scope keys of all registered bridges.
func (r *ConfigHashRegistry) ListBridges() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.bridges))
	for key := range r.bridges {
		out = append(out, key)
	}
	return out
}

// BridgeCount returns the number of registered bridges.
func (r *ConfigHashRegistry) BridgeCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.bridges)
}

// SetRunning marks a bridge as running or stopped.
func (r *ConfigHashRegistry) SetRunning(scopeKey string, running bool) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	meta, ok := r.bridges[scopeKey]
	if !ok {
		return false
	}
	meta.Running = running
	r.bridges[scopeKey] = meta
	return true
}

// IsHotWindow reports whether the given lastUsed time falls within the
// 5-minute hot window from now.
func IsHotWindow(lastUsed time.Time) bool {
	return time.Since(lastUsed) < HotBrowserWindow
}
