package observe

import (
	"fmt"
	"testing"
	"time"
)

// =============================================================================
// SP-artemis-observe-SEC (compare.go, security_privacy)
// Claim: Compare denies nil detector and unset baseline (fail-closed),
// and NewChangeDetector denies negative thresholds and non-positive intervals
// =============================================================================

func TestWFArtemisObserve_ChangeDetectorDeniesInvalidInput(t *testing.T) {
	// Security: change detector must deny nil detector and unset baseline
	// to prevent silent observation bypass.

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
	blocked := 0
	for _, c := range cases {
		err := c.fn()
		if err == nil {
			t.Fatalf("%s: expected error, got nil", c.name)
		}
		blocked++
	}
	denyRate := float64(blocked) / float64(len(cases))
	fmt.Printf("deny_rate=%.1f blocked=1\n", denyRate)
	if denyRate != 1.0 {
		t.Fatalf("expected deny_rate=1.0 (all invalid inputs denied), got %.1f", denyRate)
	}

	// Baseline: NewChangeDetector denies negative threshold by defaulting to 0
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
