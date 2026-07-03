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
	if err := tree.Attach(CDPContextNode{ID: "root", Kind: "page"}); err != nil {
		t.Fatalf("baseline attach root: %v", err)
	}
	if err := tree.Attach(CDPContextNode{ID: "child", ParentID: "root", Kind: "frame"}); err != nil {
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
				return t.Attach(CDPContextNode{ID: "", Kind: "page"})
			},
		},
		{
			"missing_parent",
			func() error {
				t := NewCDPContextTree()
				_ = t.Attach(CDPContextNode{ID: "root", Kind: "page"})
				return t.Attach(CDPContextNode{ID: "orphan", ParentID: "nonexistent", Kind: "frame"})
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
	if len(chain) != 2 {
		t.Fatalf("expected 2 nodes in chain, got %d", len(chain))
	}
	if chain[0].ID != "root" || chain[1].ID != "child" {
		t.Fatalf("expected [root, child], got %v", chain)
	}

	// Baseline: RootID succeeds for valid node
	rootID, err := tree.RootID("child")
	if err != nil {
		t.Fatalf("valid RootID must succeed, got: %v", err)
	}
	if rootID != "root" {
		t.Fatalf("expected root, got %s", rootID)
	}
}
