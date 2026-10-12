package artemis

import (
	"testing"

	"github.com/Christopher-Schulze/Artemis/observe"
	"github.com/Christopher-Schulze/Artemis/scraper"
)

func TestAXSnapshotDiff(t *testing.T) {
	before := []observe.AXNode{{ID: "1", Role: "button", Name: "ok"}}
	after := []observe.AXNode{{ID: "2", Role: "button", Name: "ok"}}
	if observe.MyersDiff(before, after) == 0 {
		t.Fatal("expected diff")
	}
}

func TestNetworkRingBuffer(t *testing.T) {
	rb := observe.NewNetworkRingBuffer(1)
	rb.Push(observe.NetworkEvent{URL: "a"})
	rb.Push(observe.NetworkEvent{URL: "b"})
	if rb.Len() != 1 || rb.Snapshot()[0].URL != "b" {
		t.Fatalf("snap=%v", rb.Snapshot())
	}
}

func TestInfiniteScrollDetect(t *testing.T) {
	d := scraper.NewInfiniteScrollDetector(2)
	if !d.ShouldContinue(1, 10, 20) {
		t.Fatal("expected continue on growth")
	}
}

func TestPageCacheInvalidationFloor(t *testing.T) {
	c := scraper.NewPageCache()
	c.Put("u", "html")
	c.InvalidateOnNavigation("u", "v")
	if _, ok := c.Get("u"); ok {
		t.Fatal("expected invalidate")
	}
}
