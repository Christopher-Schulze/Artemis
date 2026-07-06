package bridge

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDefaultConfigHashConfig(t *testing.T) {
	c := DefaultConfigHashConfig
	if !c.Enabled {
		t.Fatal("default should be enabled")
	}
	if c.HotWindowSeconds != 300 {
		t.Fatalf("hotWindowSeconds=%d want 300", c.HotWindowSeconds)
	}
	if !c.WarnOnMismatch {
		t.Fatal("warnOnMismatch should be true")
	}
}

func TestNewConfigHasherDefaultsHotWindow(t *testing.T) {
	h := NewConfigHasher(ConfigHashConfig{Enabled: true})
	if got := h.Config().HotWindowSeconds; got != 300 {
		t.Fatalf("hotWindowSeconds=%d want 300", got)
	}
}

func TestConfigHasherComputeDeterministic(t *testing.T) {
	h := NewConfigHasher(DefaultConfigHashConfig)
	cfg := map[string]interface{}{"cdpPort": 9222, "headless": true}
	a, err := h.Compute(cfg)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	b, err := h.Compute(cfg)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if a.Hash != b.Hash {
		t.Fatalf("hash not deterministic: %s vs %s", a.Hash, b.Hash)
	}
	if len(a.Hash) != 64 {
		t.Fatalf("hash length=%d want 64", len(a.Hash))
	}
}

func TestConfigHasherComputeOrderIndependent(t *testing.T) {
	h := NewConfigHasher(DefaultConfigHashConfig)
	cfg1 := map[string]interface{}{"a": 1, "b": 2, "c": 3}
	cfg2 := map[string]interface{}{"c": 3, "a": 1, "b": 2}
	a, _ := h.Compute(cfg1)
	b, _ := h.Compute(cfg2)
	if a.Hash != b.Hash {
		t.Fatalf("hash should be order-independent: %s vs %s", a.Hash, b.Hash)
	}
}

func TestConfigHasherComputeChangesOnInput(t *testing.T) {
	h := NewConfigHasher(DefaultConfigHashConfig)
	a, _ := h.Compute(map[string]interface{}{"cdpPort": 9222})
	b, _ := h.Compute(map[string]interface{}{"cdpPort": 9223})
	if a.Hash == b.Hash {
		t.Fatal("hash should change when config changes")
	}
}

func TestConfigHasherComputeNilConfig(t *testing.T) {
	h := NewConfigHasher(DefaultConfigHashConfig)
	ch, err := h.Compute(nil)
	if err != nil {
		t.Fatalf("compute nil: %v", err)
	}
	if ch.Hash == "" {
		t.Fatal("hash should be non-empty for nil config")
	}
}

func TestConfigHasherComputeStats(t *testing.T) {
	h := NewConfigHasher(DefaultConfigHashConfig)
	_, _ = h.Compute(map[string]interface{}{"x": 1})
	_, _ = h.Compute(map[string]interface{}{"x": 2})
	s := h.Stats()
	if s.Computed != 2 {
		t.Fatalf("computed=%d want 2", s.Computed)
	}
	if s.Total != 2 {
		t.Fatalf("total=%d want 2", s.Total)
	}
	if s.Cached != 0 {
		t.Fatalf("cached=%d want 0", s.Cached)
	}
}

func TestConfigHasherGetCachedWithinWindow(t *testing.T) {
	h := NewConfigHasher(ConfigHashConfig{Enabled: true, HotWindowSeconds: 300, WarnOnMismatch: true})
	cfg := map[string]interface{}{"cdpPort": 9222}
	first, err := h.GetCached(cfg)
	if err != nil {
		t.Fatalf("getCached: %v", err)
	}
	second, err := h.GetCached(cfg)
	if err != nil {
		t.Fatalf("getCached: %v", err)
	}
	if first.Hash != second.Hash {
		t.Fatalf("cached hash should match computed: %s vs %s", first.Hash, second.Hash)
	}
	s := h.Stats()
	if s.Cached != 1 {
		t.Fatalf("cached=%d want 1", s.Cached)
	}
	if s.Computed != 1 {
		t.Fatalf("computed=%d want 1", s.Computed)
	}
}

func TestConfigHasherGetCachedExpiredRecomputes(t *testing.T) {
	h := NewConfigHasher(ConfigHashConfig{Enabled: true, HotWindowSeconds: 300, WarnOnMismatch: true})
	cfg := map[string]interface{}{"cdpPort": 9222}
	first, err := h.GetCached(cfg)
	if err != nil {
		t.Fatalf("getCached: %v", err)
	}

	// Advance the clock past the hot window.
	base := first.CreatedAt
	h.SetNow(func() time.Time { return base.Add(400 * time.Second) })
	second, err := h.GetCached(cfg)
	if err != nil {
		t.Fatalf("getCached: %v", err)
	}
	if !second.CreatedAt.After(first.CreatedAt) {
		t.Fatal("expired cache should recompute with new CreatedAt")
	}
	s := h.Stats()
	if s.Computed != 2 {
		t.Fatalf("computed=%d want 2", s.Computed)
	}
	if s.Cached != 0 {
		t.Fatalf("cached=%d want 0 (both calls were cache misses)", s.Cached)
	}
}

func TestConfigHasherGetCachedDisabledAlwaysComputes(t *testing.T) {
	h := NewConfigHasher(ConfigHashConfig{Enabled: false, HotWindowSeconds: 300, WarnOnMismatch: true})
	cfg := map[string]interface{}{"cdpPort": 9222}
	_, _ = h.GetCached(cfg)
	_, _ = h.GetCached(cfg)
	s := h.Stats()
	if s.Cached != 0 {
		t.Fatalf("cached=%d want 0 when disabled", s.Cached)
	}
	if s.Computed != 2 {
		t.Fatalf("computed=%d want 2 when disabled", s.Computed)
	}
}

func TestConfigHasherVerifyMatch(t *testing.T) {
	h := NewConfigHasher(DefaultConfigHashConfig)
	cfg := map[string]interface{}{"cdpPort": 9222, "headless": true}
	ch, _ := h.Compute(cfg)
	ok, warn := h.Verify(cfg, ch.Hash)
	if !ok {
		t.Fatal("verify should match")
	}
	if warn != "" {
		t.Fatalf("unexpected warning on match: %q", warn)
	}
	if s := h.Stats(); s.Mismatches != 0 {
		t.Fatalf("mismatches=%d want 0", s.Mismatches)
	}
}

func TestConfigHasherVerifyMismatch(t *testing.T) {
	h := NewConfigHasher(DefaultConfigHashConfig)
	cfg := map[string]interface{}{"cdpPort": 9222}
	ok, warn := h.Verify(cfg, "deadbeef")
	if ok {
		t.Fatal("verify should not match")
	}
	if warn == "" {
		t.Fatal("expected non-empty warning on mismatch")
	}
	if !strings.Contains(warn, "mismatch") {
		t.Fatalf("warning should mention mismatch: %q", warn)
	}
	if s := h.Stats(); s.Mismatches != 1 {
		t.Fatalf("mismatches=%d want 1", s.Mismatches)
	}
}

func TestConfigHasherVerifyMismatchNoWarn(t *testing.T) {
	h := NewConfigHasher(ConfigHashConfig{Enabled: true, HotWindowSeconds: 300, WarnOnMismatch: false})
	cfg := map[string]interface{}{"cdpPort": 9222}
	ok, warn := h.Verify(cfg, "deadbeef")
	if ok {
		t.Fatal("verify should not match")
	}
	if warn != "" {
		t.Fatalf("expected empty warning when WarnOnMismatch=false, got %q", warn)
	}
	if s := h.Stats(); s.Mismatches != 1 {
		t.Fatalf("mismatches=%d want 1", s.Mismatches)
	}
}

func TestConfigHasherBridgeMetadataSetGet(t *testing.T) {
	h := NewConfigHasher(DefaultConfigHashConfig)
	h.SetBridgeMetadata("container", "browser-bridge-1")
	h.SetBridgeMetadata("authToken", "secret")
	md := h.GetBridgeMetadata()
	if md["container"] != "browser-bridge-1" {
		t.Fatalf("container=%q", md["container"])
	}
	if md["authToken"] != "secret" {
		t.Fatalf("authToken=%q", md["authToken"])
	}
}

func TestConfigHasherBridgeMetadataInHash(t *testing.T) {
	h := NewConfigHasher(DefaultConfigHashConfig)
	h.SetBridgeMetadata("container", "browser-bridge-1")
	ch, err := h.Compute(map[string]interface{}{"x": 1})
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if ch.BridgeMetadata["container"] != "browser-bridge-1" {
		t.Fatalf("metadata not snapshotted: %v", ch.BridgeMetadata)
	}
}

func TestConfigHasherGetBridgeMetadataDefensiveCopy(t *testing.T) {
	h := NewConfigHasher(DefaultConfigHashConfig)
	h.SetBridgeMetadata("k", "v")
	md := h.GetBridgeMetadata()
	md["k"] = "mutated"
	if h.GetBridgeMetadata()["k"] != "v" {
		t.Fatal("mutation of returned metadata should not affect hasher")
	}
}

func TestConfigHasherSetBridgeMetadataEmptyKey(t *testing.T) {
	h := NewConfigHasher(DefaultConfigHashConfig)
	h.SetBridgeMetadata("", "v")
	if len(h.GetBridgeMetadata()) != 0 {
		t.Fatal("empty key should be ignored")
	}
}

func TestConfigHasherConfig(t *testing.T) {
	h := NewConfigHasher(ConfigHashConfig{Enabled: true, HotWindowSeconds: 120, WarnOnMismatch: false})
	c := h.Config()
	if c.HotWindowSeconds != 120 {
		t.Fatalf("hotWindowSeconds=%d want 120", c.HotWindowSeconds)
	}
	if c.WarnOnMismatch {
		t.Fatal("warnOnMismatch should be false")
	}
}

func TestConfigHasherConcurrent(t *testing.T) {
	h := NewConfigHasher(DefaultConfigHashConfig)
	cfg := map[string]interface{}{"cdpPort": 9222, "headless": true}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _ = h.GetCached(cfg)
			h.SetBridgeMetadata("worker", "w")
			_ = h.GetBridgeMetadata()
			_, _ = h.Compute(cfg)
		}(i)
	}
	wg.Wait()
	s := h.Stats()
	if s.Total == 0 {
		t.Fatal("expected some operations under concurrency")
	}
}
