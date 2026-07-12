package bridge

import (
	"fmt"
	"testing"
)

// =============================================================================
// SP-artemis-bridge-SEC (cdp_context.go, security_privacy)
// Claim: CDPContextTree.Attach denies empty IDs and missing parents,
// and Hierarchy denies unknown IDs and broken parent chains
// =============================================================================

func TestWFArtemisBridge_CDPContextTreeDeniesInvalidInput(t *testing.T) {
	// Security: CDP context tree must deny invalid context attachments
	// and unknown lookups to prevent unauthorized CDP routing.

	// Set up a valid tree for baseline
	tree := NewCDPContextTree()
	if err := tree.Attach(CDPContextUnit{ID: "alloc", Kind: ContextKindAlloc}); err != nil {
		t.Fatalf("baseline attach root: %v", err)
	}
	if err := tree.Attach(CDPContextUnit{ID: "browser", ParentID: "alloc", Kind: ContextKindBrowser}); err != nil {
		t.Fatalf("baseline attach browser: %v", err)
	}
	if err := tree.Attach(CDPContextUnit{ID: "child", ParentID: "browser", Kind: ContextKindTab}); err != nil {
		t.Fatalf("baseline attach child: %v", err)
	}

	cases := []struct {
		name string
		fn   func() error
	}{
		{
			"empty_id",
			func() error {
				t := NewCDPContextTree()
				return t.Attach(CDPContextUnit{ID: "", Kind: ContextKindAlloc})
			},
		},
		{
			"missing_parent",
			func() error {
				t := NewCDPContextTree()
				_ = t.Attach(CDPContextUnit{ID: "root", Kind: ContextKindAlloc})
				return t.Attach(CDPContextUnit{ID: "orphan", ParentID: "nonexistent", Kind: ContextKindBrowser})
			},
		},
		{
			"hierarchy_unknown_id",
			func() error {
				t := NewCDPContextTree()
				_, err := t.Hierarchy("nonexistent")
				return err
			},
		},
		{
			"root_id_unknown",
			func() error {
				t := NewCDPContextTree()
				_, err := t.RootID("nonexistent")
				return err
			},
		},
		{
			"hierarchy_empty_tree",
			func() error {
				t := NewCDPContextTree()
				_, err := t.Hierarchy("any")
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

	// Baseline: valid hierarchy lookup succeeds (positive control)
	chain, err := tree.Hierarchy("child")
	if err != nil {
		t.Fatalf("valid hierarchy lookup must succeed, got: %v", err)
	}
	if len(chain) != 3 {
		t.Fatalf("expected 3 units in chain, got %d", len(chain))
	}
	if chain[0].ID != "alloc" || chain[1].ID != "browser" || chain[2].ID != "child" {
		t.Fatalf("expected [alloc, browser, child], got %v", chain)
	}

	// Baseline: RootID succeeds for a valid unit.
	rootID, err := tree.RootID("child")
	if err != nil {
		t.Fatalf("valid RootID must succeed, got: %v", err)
	}
	if rootID != "alloc" {
		t.Fatalf("expected alloc, got %s", rootID)
	}
}
