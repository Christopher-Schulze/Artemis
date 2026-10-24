package observe

import (
	"testing"
	"time"
)

// TestArtemisObserve_ChangeDetectorDeniesInvalidInput verifies:
// Two actual local Compare rejections for a missing detector or baseline. Constructor defaults and snapshot-diff controls remain outside the denominator; no persistent observation, polling or silent-bypass prevention guarantee.

func TestArtemisObserve_ChangeDetectorDeniesInvalidInput(t *testing.T) {

	// Measure only the actual local rejection cases described above.

	cases := []struct {
		name string
		fn   func() error
	}{
		{
			"nil_detector",
			func() error {
				var d *ChangeDetector
				_, err := d.Compare(nil)
				return err
			},
		},
		{
			"unset_baseline",
			func() error {
				d := NewChangeDetector(5, nil, time.Second)
				_, err := d.Compare([]AXNode{{Role: "button", Name: "OK"}})
				return err
			},
		},
	}
	for _, c := range cases {
		err := c.fn()
		if err == nil {
			t.Fatalf("%s: expected error, got nil", c.name)
		}
	}

	// Baseline: NewChangeDetector defaults a negative threshold and nonpositive interval
	d := NewChangeDetector(-10, nil, 0)
	if d.Threshold != 0 {
		t.Fatalf("negative threshold must default to 0, got %d", d.Threshold)
	}
	if d.Interval != 5*time.Second {
		t.Fatalf("non-positive interval must default to 5s, got %v", d.Interval)
	}

	// Baseline: valid Compare with baseline set succeeds (positive control)
	d2 := NewChangeDetector(0, nil, time.Second)
	d2.SetBaseline([]AXNode{{Role: "button", Name: "OK"}})
	result, err := d2.Compare([]AXNode{{Role: "button", Name: "OK"}, {Role: "link", Name: "More"}})
	if err != nil {
		t.Fatalf("valid Compare must succeed, got: %v", err)
	}
	if !result.Changed {
		t.Fatal("expected Changed=true with 1 added node")
	}
	if result.ChangedNodes != 1 {
		t.Fatalf("expected 1 changed node, got %d", result.ChangedNodes)
	}
	if !result.Notify {
		t.Fatal("expected Notify=true with threshold=0 and 1 change")
	}

	// Baseline: no change returns Changed=false
	result2, err := d2.Compare([]AXNode{{Role: "button", Name: "OK"}})
	if err != nil {
		t.Fatalf("no-change Compare must succeed, got: %v", err)
	}
	if result2.Changed {
		t.Fatal("expected Changed=false with identical snapshots")
	}
	if result2.Reason != "no_change" {
		t.Fatalf("expected reason 'no_change', got %s", result2.Reason)
	}
}
