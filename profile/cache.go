package profile

import (
	"fmt"
	"sync"
	"time"
)

// CacheControlPolicy enumerates the browser cache control modes
// (spec L4028: browser cache control).
type CacheControlPolicy string

const (
	// CachePolicyDefault uses the browser's default caching behavior.
	CachePolicyDefault CacheControlPolicy = "default"
	// CachePolicyDisabled disables all caching (always fetch fresh).
	CachePolicyDisabled CacheControlPolicy = "disabled"
	// CachePolicyMaxAge enforces a max-age on cached resources.
	CachePolicyMaxAge CacheControlPolicy = "max_age"
	// CachePolicyClearOnExit clears the cache when the profile session ends.
	CachePolicyClearOnExit CacheControlPolicy = "clear_on_exit"
)

// BrowserCacheControl governs per-profile browser cache behavior
// (spec L4028: cache.go - browser cache control). Each profile gets
// isolated cache control: max-age, clear, disable, clear-on-exit.
type BrowserCacheControl struct {
	ProfileName   string             `json:"profile_name"`
	Policy        CacheControlPolicy `json:"policy"`
	MaxAgeSeconds int64              `json:"max_age_seconds,omitempty"`
	MaxSizeBytes  int64              `json:"max_size_bytes,omitempty"`
	Enabled       bool               `json:"enabled"`
	mu            sync.RWMutex
	stats         CacheStats
}

// CacheStats tracks cache usage metrics per profile
// (browser cache metrics).
type CacheStats struct {
	HitCount      int64     `json:"hit_count"`
	MissCount     int64     `json:"miss_count"`
	EvictionCount int64     `json:"eviction_count"`
	ClearCount    int64     `json:"clear_count"`
	CurrentBytes  int64     `json:"current_bytes"`
	LastClearedAt time.Time `json:"last_cleared_at,omitempty"`
}

// NewBrowserCacheControl creates a new cache control for a profile
// with default policy (enabled, max-age 3600).
func NewBrowserCacheControl(profileName string) *BrowserCacheControl {
	return &BrowserCacheControl{
		ProfileName:   profileName,
		Policy:        CachePolicyMaxAge,
		MaxAgeSeconds: 3600,
		Enabled:       true,
	}
}

// RecordHit records a cache hit (spec L4028: cache metrics).
func (c *BrowserCacheControl) RecordHit(bytes int64) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stats.HitCount++
	c.stats.CurrentBytes += bytes
}

// RecordMiss records a cache miss (spec L4028: cache metrics).
func (c *BrowserCacheControl) RecordMiss() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stats.MissCount++
}

// RecordEviction records a cache eviction (spec L4028: cache metrics).
func (c *BrowserCacheControl) RecordEviction(bytes int64) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stats.EvictionCount++
	c.stats.CurrentBytes -= bytes
	if c.stats.CurrentBytes < 0 {
		c.stats.CurrentBytes = 0
	}
}

// Clear clears the cache for this profile (spec L4028: clear-cache).
func (c *BrowserCacheControl) Clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stats.ClearCount++
	c.stats.CurrentBytes = 0
	c.stats.LastClearedAt = time.Now()
}

// Stats returns a snapshot of the current cache stats.
func (c *BrowserCacheControl) Stats() CacheStats {
	if c == nil {
		return CacheStats{}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.stats
}

// SetPolicy updates the cache policy.
func (c *BrowserCacheControl) SetPolicy(policy CacheControlPolicy, maxAge int64) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Policy = policy
	if maxAge > 0 {
		c.MaxAgeSeconds = maxAge
	}
}

// SetEnabled enables or disables caching for this profile.
func (c *BrowserCacheControl) SetEnabled(enabled bool) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Enabled = enabled
}

// SetMaxSize sets the maximum cache size in bytes.
func (c *BrowserCacheControl) SetMaxSize(bytes int64) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.MaxSizeBytes = bytes
}

// IsExpired returns true if a cached resource with the given timestamp
// is expired under the current policy (spec L4028: max-age enforcement).
func (c *BrowserCacheControl) IsExpired(cachedAt time.Time) bool {
	if c == nil {
		return true
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.Enabled || c.Policy == CachePolicyDisabled {
		return true
	}
	if c.Policy == CachePolicyDefault {
		return false
	}
	if c.Policy == CachePolicyMaxAge && c.MaxAgeSeconds > 0 {
		return time.Since(cachedAt).Seconds() > float64(c.MaxAgeSeconds)
	}
	return false
}

// ShouldEvict returns true if adding bytes would exceed the max size
// (spec L4028: cache size management).
func (c *BrowserCacheControl) ShouldEvict(incomingBytes int64) bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.MaxSizeBytes <= 0 {
		return false
	}
	return c.stats.CurrentBytes+incomingBytes > c.MaxSizeBytes
}

// HitRate returns the cache hit rate (0.0 to 1.0).
func (c *BrowserCacheControl) HitRate() float64 {
	if c == nil {
		return 0
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	total := c.stats.HitCount + c.stats.MissCount
	if total == 0 {
		return 0
	}
	return float64(c.stats.HitCount) / float64(total)
}

// String returns a human-readable summary.
func (c *BrowserCacheControl) String() string {
	if c == nil {
		return "BrowserCacheControl(nil)"
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return fmt.Sprintf("BrowserCacheControl(profile=%s, policy=%s, enabled=%v, hits=%d, misses=%d, bytes=%d)",
		c.ProfileName, c.Policy, c.Enabled, c.stats.HitCount, c.stats.MissCount, c.stats.CurrentBytes)
}

// CacheControlManager manages per-profile cache control instances
// (spec L4028: per-profile cache isolation).
type CacheControlManager struct {
	mu       sync.RWMutex
	controls map[string]*BrowserCacheControl
}

// NewCacheControlManager creates a new manager.
func NewCacheControlManager() *CacheControlManager {
	return &CacheControlManager{controls: make(map[string]*BrowserCacheControl)}
}

// Get returns the cache control for a profile, creating one if needed.
func (m *CacheControlManager) Get(profileName string) *BrowserCacheControl {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	c, ok := m.controls[profileName]
	m.mu.RUnlock()
	if ok {
		return c
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.controls[profileName]; ok {
		return c
	}
	c = NewBrowserCacheControl(profileName)
	m.controls[profileName] = c
	return c
}

// ClearAll clears the cache for all profiles.
func (m *CacheControlManager) ClearAll() {
	if m == nil {
		return
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, c := range m.controls {
		c.Clear()
	}
}

// AllStats returns cache stats for all profiles.
func (m *CacheControlManager) AllStats() map[string]CacheStats {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]CacheStats, len(m.controls))
	for name, c := range m.controls {
		out[name] = c.Stats()
	}
	return out
}
