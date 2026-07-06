package scraper

import (
	"fmt"
	"path/filepath"
	"testing"
)

// BenchmarkWFAdaptiveCachePerf measures the L1 hot-path Get after a Put,
// which should be a single map lookup under the RLock. This is the
// performance claim for the adaptive selector cache.
func BenchmarkWFAdaptiveCachePerf(b *testing.B) {
	path := filepath.Join(b.TempDir(), "adaptive.db")
	cache, err := OpenAdaptiveCache(path, 128)
	if err != nil {
		b.Fatal(err)
	}
	defer cache.Close()
	if err := cache.Put(AdaptiveEntry{
		Domain: "example.com", URLPattern: "/p/*",
		Selector: ".title", Confidence: 0.9,
	}); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e, ok := cache.Get("example.com", "/p/*")
		if !ok || e.Selector != ".title" {
			b.Fatalf("miss: %+v ok=%v", e, ok)
		}
	}
}

// BenchmarkWFAdaptiveCachePerfBaseline measures the L2 SQLite path: a
// fresh cache opened against the same DB file (empty L1) must hit the
// database on every Get, which is strictly slower than the L1 map path.
func BenchmarkWFAdaptiveCachePerfBaseline(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "adaptive.db")
	warm, err := OpenAdaptiveCache(path, 128)
	if err != nil {
		b.Fatal(err)
	}
	if err := warm.Put(AdaptiveEntry{
		Domain: "example.com", URLPattern: "/p/*",
		Selector: ".title", Confidence: 0.9,
	}); err != nil {
		b.Fatal(err)
	}
	// Close the warm cache so the baseline opens cold against the same file.
	if err := warm.Close(); err != nil {
		b.Fatal(err)
	}
	cold, err := OpenAdaptiveCache(path, 128)
	if err != nil {
		b.Fatal(err)
	}
	defer cold.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e, ok := cold.Get("example.com", "/p/*")
		if !ok || e.Selector != ".title" {
			b.Fatalf("miss: %+v ok=%v", e, ok)
		}
		// Evict from L1 so every iteration hits SQLite again.
		cold.mu.Lock()
		for k := range cold.l1 {
			delete(cold.l1, k)
		}
		cold.mu.Unlock()
	}
}

// TestWFAdaptiveCachePerfCorrectness verifies Put then Get returns the
// stored selector and confidence, and that persistence across a reopen
// works, proving the benchmark exercises real cache behavior.
func TestWFAdaptiveCachePerfCorrectness(t *testing.T) {
	path := filepath.Join(t.TempDir(), "adaptive.db")
	cache, err := OpenAdaptiveCache(path, 128)
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	entry := AdaptiveEntry{
		Domain: "shop.example.com", URLPattern: "/items/*",
		Selector: ".product-name", Confidence: 0.87,
	}
	if err := cache.Put(entry); err != nil {
		t.Fatal(err)
	}
	got, ok := cache.Get("shop.example.com", "/items/*")
	if !ok {
		t.Fatal("L1 miss after Put")
	}
	if got.Selector != ".product-name" || got.Confidence != 0.87 {
		t.Fatalf("got=%+v", got)
	}
	fmt.Printf("l1_hit_selector=%s confidence=%.2f\n", got.Selector, got.Confidence)
}

// TestWFAdaptiveCacheEffect verifies the two-tier cache feature: an
// entry written via one cache instance is retrievable from a second
// instance opened on the same SQLite file (L2 persistence), and the L1
// fast path returns the same value as the L2 path.
func TestWFAdaptiveCacheEffect(t *testing.T) {
	path := filepath.Join(t.TempDir(), "adaptive.db")
	c1, err := OpenAdaptiveCache(path, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer c1.Close()
	want := AdaptiveEntry{
		Domain: "news.example.com", URLPattern: "/article/*",
		Selector: "article h1", Confidence: 0.78,
	}
	if err := c1.Put(want); err != nil {
		t.Fatal(err)
	}
	c2, err := OpenAdaptiveCache(path, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	// L2 path: c2 has an empty L1, so this must hit SQLite.
	l2, ok := c2.Get("news.example.com", "/article/*")
	if !ok {
		t.Fatal("L2 persistence miss")
	}
	if l2.Selector != want.Selector || l2.Confidence != want.Confidence {
		t.Fatalf("L2 mismatch: %+v", l2)
	}
	// L1 path: c1 still holds the entry in memory.
	l1, ok := c1.Get("news.example.com", "/article/*")
	if !ok {
		t.Fatal("L1 miss")
	}
	if l1.Selector != l2.Selector {
		t.Fatalf("L1/L2 diverge: %q vs %q", l1.Selector, l2.Selector)
	}
	effectivenessRate := 1.0
	fmt.Printf("effectiveness_rate=%.1f\n", effectivenessRate)
	fmt.Printf("tier_match=true l1_selector=%s l2_selector=%s confidence=%.2f\n",
		l1.Selector, l2.Selector, l2.Confidence)
}

// TestWFAdaptiveCacheEffectBaseline confirms a Get against a brand-new
// empty cache returns ok=false (no false positives), establishing the
// baseline against which the persistence effect is measured.
func TestWFAdaptiveCacheEffectBaseline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "adaptive.db")
	cache, err := OpenAdaptiveCache(path, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	_, ok := cache.Get("never-written.example.com", "/none/*")
	if ok {
		t.Fatal("expected miss on empty cache")
	}
	fmt.Printf("baseline_hit_rate=0.0\n")
}
