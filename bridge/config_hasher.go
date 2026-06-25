package bridge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"
)

// ConfigHashConfig controls sandbox config hashing behavior (spec L4310).
// A config hash is computed over the JSON-normalized sandbox config and is
// used to decide whether a running browser bridge can be reused or must be
// recreated. The hot window is the grace period during which a mismatch
// reuses the running bridge and warns instead of recreating.
type ConfigHashConfig struct {
	Enabled          bool
	HotWindowSeconds int
	WarnOnMismatch   bool
}

// DefaultConfigHashConfig is the canonical config-hash configuration: a
// 5-minute (300s) hot window with mismatch warnings enabled.
var DefaultConfigHashConfig = ConfigHashConfig{
	Enabled:          true,
	HotWindowSeconds: 300,
	WarnOnMismatch:   true,
}

// ConfigHash is a computed sandbox config hash together with the
// BROWSER_BRIDGES metadata captured at creation time.
type ConfigHash struct {
	Hash           string
	CreatedAt      time.Time
	BridgeMetadata map[string]string
}

// ConfigHashStats reports counters for a ConfigHasher.
type ConfigHashStats struct {
	Total      int64
	Cached     int64
	Computed   int64
	Mismatches int64
}

// ConfigHasher computes, caches and verifies sandbox config hashes. It is
// thread-safe. The cache holds the most recently computed hash; GetCached
// returns it without recomputation as long as it is within the hot window.
type ConfigHasher struct {
	mu       sync.RWMutex
	config   ConfigHashConfig
	cached   *ConfigHash
	stats    ConfigHashStats
	metadata map[string]string
	now      func() time.Time
}

// NewConfigHasher builds a ConfigHasher with the supplied config. Zero-value
// hot-window seconds fall back to DefaultConfigHashConfig.HotWindowSeconds.
func NewConfigHasher(config ConfigHashConfig) *ConfigHasher {
	if config.HotWindowSeconds <= 0 {
		config.HotWindowSeconds = DefaultConfigHashConfig.HotWindowSeconds
	}
	return &ConfigHasher{
		config:   config,
		metadata: make(map[string]string),
		now:      time.Now,
	}
}

// canonicalJSON serializes config to JSON with deterministically sorted keys
// so the hash is stable regardless of map iteration order.
func canonicalJSON(config map[string]interface{}) ([]byte, error) {
	if config == nil {
		config = map[string]interface{}{}
	}
	keys := make([]string, 0, len(config))
	for k := range config {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	type kv struct {
		Key   string
		Value interface{}
	}
	ordered := make([]kv, 0, len(keys))
	for _, k := range keys {
		ordered = append(ordered, kv{Key: k, Value: config[k]})
	}
	// json.Marshal of a slice preserves element order, giving us a stable
	// canonical form even though map iteration order is non-deterministic.
	return json.Marshal(ordered)
}

// copyMetadataLocked returns a defensive copy of the current BROWSER_BRIDGES
// metadata. Caller must hold h.mu (or h.mu.RLock for read-only callers).
func (h *ConfigHasher) copyMetadataLocked() map[string]string {
	if len(h.metadata) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(h.metadata))
	for k, v := range h.metadata {
		out[k] = v
	}
	return out
}

// Compute computes a fresh SHA-256 hash of the JSON-serialized config. It
// does not consult or update the cache. The returned ConfigHash snapshots
// the current BROWSER_BRIDGES metadata.
func (h *ConfigHasher) Compute(config map[string]interface{}) (ConfigHash, error) {
	raw, err := canonicalJSON(config)
	if err != nil {
		return ConfigHash{}, fmt.Errorf("config hash: marshal: %w", err)
	}
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	h.mu.Lock()
	ch := ConfigHash{
		Hash:           digest,
		CreatedAt:      h.now(),
		BridgeMetadata: h.copyMetadataLocked(),
	}
	h.stats.Total++
	h.stats.Computed++
	h.mu.Unlock()
	return ch, nil
}

// GetCached returns the cached hash if one exists and is still within the
// hot window; otherwise it computes a fresh hash, stores it as the cache,
// and returns it. When disabled it always computes fresh.
func (h *ConfigHasher) GetCached(config map[string]interface{}) (ConfigHash, error) {
	hotWindow := time.Duration(h.config.HotWindowSeconds) * time.Second
	now := h.now()

	h.mu.RLock()
	if h.config.Enabled && h.cached != nil && now.Sub(h.cached.CreatedAt) < hotWindow {
		out := *h.cached
		out.BridgeMetadata = h.copyMetadataLocked()
		h.mu.RUnlock()
		h.mu.Lock()
		h.stats.Total++
		h.stats.Cached++
		h.mu.Unlock()
		return out, nil
	}
	h.mu.RUnlock()

	// Cache miss or expired: compute fresh and store as cache.
	ch, err := h.Compute(config)
	if err != nil {
		return ConfigHash{}, err
	}
	h.mu.Lock()
	cached := ch
	cached.BridgeMetadata = h.copyMetadataLocked()
	h.cached = &cached
	h.mu.Unlock()
	return ch, nil
}

// Verify computes the hash of config and compares it to expectedHash.
// Returns (true, "") on match. On mismatch it returns (false, warning)
// where warning is non-empty when WarnOnMismatch is enabled. The mismatch
// counter is incremented on every mismatch.
func (h *ConfigHasher) Verify(config map[string]interface{}, expectedHash string) (bool, string) {
	ch, err := h.Compute(config)
	if err != nil {
		return false, fmt.Sprintf("config hash: %v", err)
	}
	if ch.Hash == expectedHash {
		return true, ""
	}
	h.mu.Lock()
	h.stats.Mismatches++
	warnOnMismatch := h.config.WarnOnMismatch
	h.mu.Unlock()
	if !warnOnMismatch {
		return false, ""
	}
	return false, fmt.Sprintf(
		"sandbox config hash mismatch: expected %s, got %s (recreate browser bridge to apply)",
		expectedHash, ch.Hash,
	)
}

// SetBridgeMetadata stores a BROWSER_BRIDGES metadata key/value pair.
func (h *ConfigHasher) SetBridgeMetadata(key, value string) {
	if key == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.metadata[key] = value
}

// GetBridgeMetadata returns a defensive copy of the BROWSER_BRIDGES metadata.
func (h *ConfigHasher) GetBridgeMetadata() map[string]string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.copyMetadataLocked()
}

// Stats returns a snapshot of the hasher counters.
func (h *ConfigHasher) Stats() ConfigHashStats {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.stats
}

// Config returns a copy of the hasher configuration.
func (h *ConfigHasher) Config() ConfigHashConfig {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.config
}

// SetNow replaces the clock used for CreatedAt timestamps. Intended for
// tests; not safe to call concurrently with Compute/GetCached.
func (h *ConfigHasher) SetNow(fn func() time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if fn == nil {
		fn = time.Now
	}
	h.now = fn
}
