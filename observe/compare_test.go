package observe

import (
	"testing"
	"time"
)

func TestNewChangeDetectorDefaults(t *testing.T) {
	d := NewChangeDetector(-5, nil, 0)
	if d.Threshold != 0 {
		t.Fatalf("negative threshold must clamp to 0, got %d", d.Threshold)
	}
	if d.Interval != 5*time.Second {
		t.Fatalf("zero interval must default 5s, got %v", d.Interval)
	}
}

func TestChangeDetectorNoBaselineErrors(t *testing.T) {
	d := NewChangeDetector(1, nil, 0)
	_, err := d.Compare([]AXNode{{ID: "1", Role: "button", Name: "x"}})
	if err == nil {
		t.Fatal("must error when baseline not set")
	}
}

func TestChangeDetectorNilReceiverErrors(t *testing.T) {
	var d *ChangeDetector
	_, err := d.Compare(nil)
	if err == nil {
		t.Fatal("nil receiver must error")
	}
}

func TestChangeDetectorNoChange(t *testing.T) {
	d := NewChangeDetector(1, nil, 0)
	d.SetBaseline([]AXNode{{ID: "1", Role: "button", Name: "Submit"}})
	res, err := d.Compare([]AXNode{{ID: "1", Role: "button", Name: "Submit"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("must not report change for identical snapshots")
	}
	if res.Notify {
		t.Fatal("must not notify when no change")
	}
	if res.Reason != "no_change" {
		t.Fatalf("expected no_change, got %s", res.Reason)
	}
}

func TestChangeDetectorBelowThreshold(t *testing.T) {
	d := NewChangeDetector(5, nil, 0)
	d.SetBaseline([]AXNode{{ID: "1", Role: "button", Name: "Submit"}})
	res, _ := d.Compare([]AXNode{
		{ID: "1", Role: "button", Name: "Submit"},
		{ID: "2", Role: "link", Name: "New"},
	})
	if !res.Changed {
		t.Fatal("must report change when nodes differ")
	}
	if res.Notify {
		t.Fatal("must not notify when below threshold")
	}
	if res.ChangedNodes != 1 {
		t.Fatalf("expected 1 changed, got %d", res.ChangedNodes)
	}
	if res.Reason != "below_threshold" {
		t.Fatalf("expected below_threshold, got %s", res.Reason)
	}
}

func TestChangeDetectorAboveThresholdNotifies(t *testing.T) {
	d := NewChangeDetector(2, nil, 0)
	d.SetBaseline([]AXNode{
		{ID: "1", Role: "button", Name: "A"},
		{ID: "2", Role: "button", Name: "B"},
		{ID: "3", Role: "button", Name: "C"},
	})
	res, _ := d.Compare([]AXNode{
		{ID: "1", Role: "button", Name: "A"},
		{ID: "4", Role: "link", Name: "D"},
		{ID: "5", Role: "link", Name: "E"},
		{ID: "6", Role: "link", Name: "F"},
	})
	if !res.Notify {
		t.Fatal("must notify when above threshold")
	}
	if res.Reason != "threshold_exceeded" {
		t.Fatalf("expected threshold_exceeded, got %s", res.Reason)
	}
	if res.ChangedNodes != 5 {
		t.Fatalf("expected 5 changed (3 removed + 3 added - 1 common... actually 2 removed + 3 added = 5), got %d", res.ChangedNodes)
	}
}

func TestChangeDetectorSemanticFilterIgnoresAds(t *testing.T) {
	d := NewChangeDetector(0, []string{"advertisement", "ads", "sponsored"}, 0)
	d.SetBaseline([]AXNode{
		{ID: "1", Role: "button", Name: "Buy"},
		{ID: "ad1", Role: "complementary", Name: "Advertisement"},
	})
	res, _ := d.Compare([]AXNode{
		{ID: "1", Role: "button", Name: "Buy"},
		{ID: "ad2", Role: "complementary", Name: "Sponsored Ad"},
	})
	if res.Changed {
		t.Fatal("ad-only changes must be filtered out")
	}
	if res.Reason != "no_change" {
		t.Fatalf("expected no_change after filter, got %s", res.Reason)
	}
}

func TestChangeDetectorSemanticFilterIgnoresTimestamps(t *testing.T) {
	d := NewChangeDetector(0, []string{"timestamp", "time"}, 0)
	d.SetBaseline([]AXNode{
		{ID: "1", Role: "button", Name: "Submit"},
		{ID: "ts1", Role: "text", Name: "Timestamp: 12:00"},
	})
	res, _ := d.Compare([]AXNode{
		{ID: "1", Role: "button", Name: "Submit"},
		{ID: "ts2", Role: "text", Name: "Time: 12:01"},
	})
	if res.Changed {
		t.Fatal("timestamp-only changes must be filtered out")
	}
}

func TestChangeDetectorSemanticFilterCaseInsensitive(t *testing.T) {
	d := NewChangeDetector(0, []string{"AD"}, 0)
	d.SetBaseline([]AXNode{{ID: "1", Role: "text", Name: "real"}})
	res, _ := d.Compare([]AXNode{
		{ID: "1", Role: "text", Name: "real"},
		{ID: "2", Role: "complementary", Name: "advertisement"},
	})
	if res.Changed {
		t.Fatal("case-insensitive filter must drop lowercase ad")
	}
}

func TestChangeDetectorSetBaselineNilReceiverSafe(t *testing.T) {
	var d *ChangeDetector
	d.SetBaseline(nil) // must not panic
}

func TestChangeDetectorAddedAndRemovedNodes(t *testing.T) {
	d := NewChangeDetector(0, nil, 0)
	d.SetBaseline([]AXNode{
		{ID: "1", Role: "button", Name: "A"},
		{ID: "2", Role: "button", Name: "B"},
	})
	res, _ := d.Compare([]AXNode{
		{ID: "1", Role: "button", Name: "A"},
		{ID: "3", Role: "link", Name: "C"},
	})
	if len(res.AddedNodes) != 1 || res.AddedNodes[0].ID != "3" {
		t.Fatalf("expected 1 added node ID=3, got %+v", res.AddedNodes)
	}
	if len(res.RemovedNodes) != 1 || res.RemovedNodes[0].ID != "2" {
		t.Fatalf("expected 1 removed node ID=2, got %+v", res.RemovedNodes)
	}
}

func TestChangeDetectorThresholdZeroNotifiesOnAnyChange(t *testing.T) {
	d := NewChangeDetector(0, nil, 0)
	d.SetBaseline([]AXNode{{ID: "1", Role: "button", Name: "A"}})
	res, _ := d.Compare([]AXNode{
		{ID: "1", Role: "button", Name: "A"},
		{ID: "2", Role: "link", Name: "B"},
	})
	if !res.Notify {
		t.Fatal("threshold 0 must notify on any change (>0 > 0)")
	}
}

func TestChangeDetectorEmptyFilterCopiesNodes(t *testing.T) {
	d := NewChangeDetector(0, nil, 0)
	d.SetBaseline([]AXNode{{ID: "1", Role: "button", Name: "A"}})
	res, _ := d.Compare([]AXNode{{ID: "1", Role: "button", Name: "A"}})
	if res.Changed {
		t.Fatal("identical must not change")
	}
}
