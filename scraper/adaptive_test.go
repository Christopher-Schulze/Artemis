package scraper

import (
	"path/filepath"
	"testing"
)

func TestAdaptiveSelectorCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "adaptive.db")
	cache, err := OpenAdaptiveCache(path, 128)
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	entry := AdaptiveEntry{
		Domain: "example.com", URLPattern: "/products/*",
		Selector: ".product-title", Confidence: 0.9,
	}
	if err := cache.Put(entry); err != nil {
		t.Fatal(err)
	}
	got, ok := cache.Get("example.com", "/products/*")
	if !ok || got.Selector != ".product-title" {
		t.Fatalf("cache miss: %+v ok=%v", got, ok)
	}
	cache2, err := OpenAdaptiveCache(path, 128)
	if err != nil {
		t.Fatal(err)
	}
	defer cache2.Close()
	got2, ok := cache2.Get("example.com", "/products/*")
	if !ok || got2.Selector != ".product-title" {
		t.Fatalf("sqlite L2 miss: %+v", got2)
	}
}
