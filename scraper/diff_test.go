package scraper

import (
	"testing"
	"time"
)

func TestDiffEngine_DiffRegions(t *testing.T) {
	e := NewDiffEngine()
	regions := map[string]string{
		"header":  "Welcome",
		"content": "New article",
	}
	result := e.DiffRegions("https://example.com/", regions)
	if len(result.ChangedRegions) != 2 {
		t.Fatalf("expected 2 changed regions first run, got %d", len(result.ChangedRegions))
	}

	// Second run with same content
	result2 := e.DiffRegions("https://example.com/", regions)
	if len(result2.Unchanged) != 2 {
		t.Fatalf("expected 2 unchanged regions, got %d", len(result2.Unchanged))
	}
	if len(result2.ChangedRegions) != 0 {
		t.Fatalf("expected 0 changed regions, got %d", len(result2.ChangedRegions))
	}

	// Third run with one changed region
	regions["content"] = "Updated article"
	result3 := e.DiffRegions("https://example.com/", regions)
	if len(result3.ChangedRegions) != 1 || result3.ChangedRegions[0].RegionID != "content" {
		t.Fatalf("expected 1 changed region (content), got %v", result3.ChangedRegions)
	}
	if len(result3.Unchanged) != 1 || result3.Unchanged[0].RegionID != "header" {
		t.Fatalf("expected 1 unchanged region (header), got %v", result3.Unchanged)
	}
}

func TestDiffEngine_RecordGlobalFingerprint(t *testing.T) {
	e := NewDiffEngine()
	e.RecordGlobalFingerprint("https://example.com/", "abc123", time.Time{})
	h := e.CheckConditionalGET("https://example.com/")
	if h.Get("If-None-Match") != "abc123" {
		t.Fatalf("expected If-None-Match abc123, got %s", h.Get("If-None-Match"))
	}
}

func TestIs304(t *testing.T) {
	if !Is304(304) {
		t.Fatal("expected 304 is conditional")
	}
	if Is304(200) {
		t.Fatal("expected 200 is not conditional")
	}
}

func TestDiffEngine_Prune(t *testing.T) {
	e := NewDiffEngine()
	e.fingerprints["https://example.com/|__global__"] = RegionFingerprint{
		RegionID:    "__global__",
		ContentHash: "abc",
		LastSeenAt:  time.Now().Add(-48 * time.Hour).Unix(),
	}
	removed := e.Prune(24 * time.Hour)
	if removed != 1 {
		t.Fatalf("expected 1 pruned, got %d", removed)
	}
}
