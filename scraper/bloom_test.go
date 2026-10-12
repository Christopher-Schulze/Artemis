package scraper

import (
	"testing"
)

func TestURLDeduperMapModeBelow10K(t *testing.T) {
	d := NewURLDeduper(100, 0.001)
	if d.mapMode == nil {
		t.Fatal("expected map mode below 10K expected URLs")
	}
	if d.bloom != nil {
		t.Fatal("bloom filter should not be allocated in map mode")
	}
	if d.limit != 10000 {
		t.Fatalf("expected limit 10000, got %d", d.limit)
	}
}

func TestURLDeduperBloomModeAt10K(t *testing.T) {
	d := NewURLDeduper(10000, 0.001)
	if d.bloom == nil {
		t.Fatal("expected bloom filter at 10K expected URLs")
	}
	if d.mapMode != nil {
		t.Fatal("map should not be allocated in bloom mode")
	}
}

func TestURLDeduperSeenNewURLReturnsFalse(t *testing.T) {
	d := NewURLDeduper(100, 0.001)
	if d.Seen("https://example.com/a") {
		t.Fatal("first Seen for new URL must return false")
	}
}

func TestURLDeduperSeenDuplicateReturnsTrue(t *testing.T) {
	d := NewURLDeduper(100, 0.001)
	if d.Seen("https://example.com/a") {
		t.Fatal("first Seen must be false")
	}
	if !d.Seen("https://example.com/a") {
		t.Fatal("second Seen for same URL must return true")
	}
}

func TestURLDeduperSeenDistinctURLs(t *testing.T) {
	d := NewURLDeduper(100, 0.001)
	urls := []string{
		"https://example.com/a",
		"https://example.com/b",
		"https://example.com/c",
	}
	for i, u := range urls {
		if d.Seen(u) {
			t.Fatalf("url %d (%s) must be new", i, u)
		}
	}
	for _, u := range urls {
		if !d.Seen(u) {
			t.Fatalf("second Seen for %s must return true", u)
		}
	}
}

func TestURLDeduperSeenEmptyURLReturnsFalse(t *testing.T) {
	d := NewURLDeduper(100, 0.001)
	if d.Seen("") {
		t.Fatal("empty URL must return false, not record it")
	}
}

func TestURLDeduperSeenNilReceiverReturnsFalse(t *testing.T) {
	var d *URLDeduper
	if d.Seen("https://example.com/a") {
		t.Fatal("nil receiver must return false")
	}
}

func TestURLDeduperBloomModeDuplicateDetected(t *testing.T) {
	d := NewURLDeduper(10000, 0.001)
	if d.Seen("https://example.com/x") {
		t.Fatal("first Seen must be false")
	}
	if !d.Seen("https://example.com/x") {
		t.Fatal("second Seen in bloom mode must return true")
	}
}

func TestURLDeduperBloomModeDistinctURLs(t *testing.T) {
	d := NewURLDeduper(10000, 0.001)
	for i := 0; i < 100; i++ {
		u := "https://example.com/p/" + string(rune('a'+i%26)) + string(rune('0'+i/26))
		if d.Seen(u) {
			t.Fatalf("url %s must be new on first sight", u)
		}
	}
}

func TestURLDeduperDefaultFalsePositiveClamped(t *testing.T) {
	d := NewURLDeduper(10000, 0)
	if d.bloom == nil {
		t.Fatal("bloom must be allocated even with zero falsePositive input")
	}
}
