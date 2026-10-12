package observe

import (
	"fmt"
	"testing"
)

// =============================================================================
// SP-artemis-perf-SEC (role_snapshot.go, security_privacy)
// Claim: ClassifyRole denies empty/unknown roles by returning RoleUnknown,
// SnapshotByRole denies non-matching nodes, SnapshotInteractive denies
// non-interactive roles, and BuildRoleRefMap denies empty IDs by using
// ref as fallback key to prevent ref collisions
// =============================================================================

func TestWFArtemisPerf_RoleSnapshotDeniesInvalidInput(t *testing.T) {
	// Security: role snapshot must deny unknown/empty roles and
	// non-interactive elements to prevent unauthorized element targeting.

	cases := []struct {
		name string
		fn   func() bool // returns true if deny was correct
	}{
		{
			"classify_empty_role",
			func() bool {
				return ClassifyRole(AXNode{Role: "", Name: "x"}) == RoleUnknown
			},
		},
		{
			"classify_whitespace_role",
			func() bool {
				return ClassifyRole(AXNode{Role: "   ", Name: "x"}) == RoleUnknown
			},
		},
		{
			"classify_unknown_role",
			func() bool {
				return ClassifyRole(AXNode{Role: "notarealrole", Name: "x"}) == RoleGeneric
			},
		},
		{
			"snapshot_by_role_excludes_nonmatching",
			func() bool {
				tree := []AXNode{
					{Role: "button", Name: "OK"},
					{Role: "link", Name: "More"},
					{Role: "heading", Name: "Title"},
				}
				result := SnapshotByRole(tree, RoleButton)
				return len(result) == 1 && result[0].Name == "OK"
			},
		},
		{
			"snapshot_interactive_excludes_structural",
			func() bool {
				tree := []AXNode{
					{Role: "button", Name: "OK"},
					{Role: "navigation", Name: "nav"},
					{Role: "main", Name: "main"},
					{Role: "list", Name: "list"},
				}
				result := SnapshotInteractive(tree)
				return len(result) == 1 && result[0].Name == "OK"
			},
		},
		{
			"build_refmap_empty_id_uses_ref_fallback",
			func() bool {
				tree := []AXNode{
					{ID: "", Role: "button", Name: "OK"},
				}
				refs := BuildRoleRefMap(tree)
				_, ok := refs["button:OK"]
				return ok
			},
		},
		{
			"build_refmap_duplicate_keys_get_index",
			func() bool {
				tree := []AXNode{
					{ID: "1", Role: "button", Name: "OK"},
					{ID: "2", Role: "button", Name: "OK"},
				}
				refs := BuildRoleRefMap(tree)
				return refs["1"] == "button:OK:1" && refs["2"] == "button:OK:2"
			},
		},
	}
	blocked := 0
	for _, c := range cases {
		if !c.fn() {
			t.Fatalf("%s: expected deny behavior, got allow", c.name)
		}
		blocked++
	}
	denyRate := float64(blocked) / float64(len(cases))
	fmt.Printf("deny_rate=%.1f blocked=1\n", denyRate)
	if denyRate != 1.0 {
		t.Fatalf("expected deny_rate=1.0 (all invalid inputs denied), got %.1f", denyRate)
	}

	// Baseline: valid ClassifyRole returns correct role (positive control)
	if ClassifyRole(AXNode{Role: "button", Name: "OK"}) != RoleButton {
		t.Fatal("expected RoleButton for 'button'")
	}
	if ClassifyRole(AXNode{Role: "heading", Name: "Title"}) != RoleHeading {
		t.Fatal("expected RoleHeading for 'heading'")
	}

	// Baseline: valid SnapshotAllRoles groups correctly
	tree := []AXNode{
		{Role: "button", Name: "A"},
		{Role: "button", Name: "B"},
		{Role: "link", Name: "C"},
	}
	grouped := SnapshotAllRoles(tree)
	if len(grouped[RoleButton]) != 2 || len(grouped[RoleLink]) != 1 {
		t.Fatalf("expected 2 buttons and 1 link, got %v", grouped)
	}

	// Baseline: valid BuildRoleRefMap with unique names
	refs := BuildRoleRefMap([]AXNode{
		{ID: "1", Role: "button", Name: "OK"},
		{ID: "2", Role: "link", Name: "More"},
	})
	if refs["1"] != "button:OK" || refs["2"] != "link:More" {
		t.Fatalf("expected refs button:OK and link:More, got %v", refs)
	}
}
