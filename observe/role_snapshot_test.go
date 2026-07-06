package observe

import (
	"testing"
)

func TestClassifyRole(t *testing.T) {
	tests := []struct {
		node AXNode
		want RoleName
	}{
		{AXNode{Role: "button", Name: "Submit"}, RoleButton},
		{AXNode{Role: "link", Name: "Home"}, RoleLink},
		{AXNode{Role: "textbox", Name: "Email"}, RoleTextbox},
		{AXNode{Role: "heading", Name: "Welcome"}, RoleHeading},
		{AXNode{Role: "heading, level=1", Name: "Title"}, RoleHeading},
		{AXNode{Role: "list item", Name: "Item 1"}, RoleListItem},
		{AXNode{Role: "combo box", Name: "Select"}, RoleCombobox},
		{AXNode{Role: "menu item", Name: "Open"}, RoleMenuitem},
		{AXNode{Role: "", Name: "No role"}, RoleUnknown},
		{AXNode{Role: "unknown-role", Name: "X"}, RoleGeneric},
	}
	for _, tt := range tests {
		got := ClassifyRole(tt.node)
		if got != tt.want {
			t.Errorf("ClassifyRole(%+v) = %s, want %s", tt.node, got, tt.want)
		}
	}
}

func TestSnapshotByRole(t *testing.T) {
	tree := []AXNode{
		{ID: "1", Role: "button", Name: "Submit"},
		{ID: "2", Role: "link", Name: "Home"},
		{ID: "3", Role: "button", Name: "Cancel"},
		{ID: "4", Role: "heading", Name: "Title"},
	}
	buttons := SnapshotByRole(tree, RoleButton)
	if len(buttons) != 2 {
		t.Fatalf("expected 2 buttons, got %d", len(buttons))
	}
	links := SnapshotByRole(tree, RoleLink)
	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(links))
	}
	headings := SnapshotByRole(tree, RoleHeading)
	if len(headings) != 1 {
		t.Fatalf("expected 1 heading, got %d", len(headings))
	}
}

func TestSnapshotAllRoles(t *testing.T) {
	tree := []AXNode{
		{ID: "1", Role: "button", Name: "Submit"},
		{ID: "2", Role: "link", Name: "Home"},
		{ID: "3", Role: "button", Name: "Cancel"},
		{ID: "4", Role: "heading", Name: "Title"},
	}
	grouped := SnapshotAllRoles(tree)
	if len(grouped[RoleButton]) != 2 {
		t.Fatalf("expected 2 buttons, got %d", len(grouped[RoleButton]))
	}
	if len(grouped[RoleLink]) != 1 {
		t.Fatalf("expected 1 link, got %d", len(grouped[RoleLink]))
	}
	if len(grouped[RoleHeading]) != 1 {
		t.Fatalf("expected 1 heading, got %d", len(grouped[RoleHeading]))
	}
}

func TestSnapshotInteractive(t *testing.T) {
	tree := []AXNode{
		{ID: "1", Role: "button", Name: "Submit"},
		{ID: "2", Role: "link", Name: "Home"},
		{ID: "3", Role: "heading", Name: "Title"},
		{ID: "4", Role: "textbox", Name: "Email"},
		{ID: "5", Role: "paragraph", Name: "Lorem"},
	}
	interactive := SnapshotInteractive(tree)
	if len(interactive) != 3 {
		t.Fatalf("expected 3 interactive (button, link, textbox), got %d", len(interactive))
	}
}

func TestSnapshotByRoleEmpty(t *testing.T) {
	tree := []AXNode{}
	buttons := SnapshotByRole(tree, RoleButton)
	if len(buttons) != 0 {
		t.Fatalf("expected 0 buttons for empty tree, got %d", len(buttons))
	}
}

func TestSnapshotAllRolesEmpty(t *testing.T) {
	tree := []AXNode{}
	grouped := SnapshotAllRoles(tree)
	if len(grouped) != 0 {
		t.Fatalf("expected 0 groups for empty tree, got %d", len(grouped))
	}
}

func TestRoleNameTracker(t *testing.T) {
	tracker := NewRoleNameTracker()
	idx1 := tracker.GetNextIndex("button", "Submit")
	idx2 := tracker.GetNextIndex("button", "Submit")
	idx3 := tracker.GetNextIndex("button", "Cancel")
	if idx1 != 1 {
		t.Fatalf("expected first index 1, got %d", idx1)
	}
	if idx2 != 2 {
		t.Fatalf("expected second index 2, got %d", idx2)
	}
	if idx3 != 1 {
		t.Fatalf("expected first index for different name, got %d", idx3)
	}
}

func TestRoleNameTrackerDuplicates(t *testing.T) {
	tracker := NewRoleNameTracker()
	tracker.TrackRef("button", "Submit", "button:Submit:1")
	tracker.TrackRef("button", "Submit", "button:Submit:2")
	tracker.TrackRef("button", "Cancel", "button:Cancel")
	dups := tracker.GetDuplicateKeys()
	if len(dups) != 1 {
		t.Fatalf("expected 1 duplicate key, got %d", len(dups))
	}
	key := tracker.getKey("button", "Submit")
	if !dups[key] {
		t.Fatal("expected button|Submit to be a duplicate key")
	}
}

func TestBuildRoleRefMap(t *testing.T) {
	tree := []AXNode{
		{ID: "1", Role: "button", Name: "Submit"},
		{ID: "2", Role: "button", Name: "Submit"},
		{ID: "3", Role: "link", Name: "Home"},
	}
	refs := BuildRoleRefMap(tree)
	if len(refs) != 3 {
		t.Fatalf("expected 3 refs, got %d", len(refs))
	}
	// Duplicates should have nth index
	if refs["1"] != "button:Submit:1" {
		t.Fatalf("expected button:Submit:1, got %s", refs["1"])
	}
	if refs["2"] != "button:Submit:2" {
		t.Fatalf("expected button:Submit:2, got %s", refs["2"])
	}
	// Non-duplicate should not have nth index
	if refs["3"] != "link:Home" {
		t.Fatalf("expected link:Home, got %s", refs["3"])
	}
}

func TestGetSnapshotStats(t *testing.T) {
	snapshot := "button \"Submit\"\nlink \"Home\"\nheading \"Title\""
	refs := map[string]string{
		"1": "button:Submit",
		"2": "link:Home",
		"3": "heading:Title",
	}
	stats := GetSnapshotStats(snapshot, refs)
	if stats.Lines != 3 {
		t.Fatalf("expected 3 lines, got %d", stats.Lines)
	}
	if stats.Refs != 3 {
		t.Fatalf("expected 3 refs, got %d", stats.Refs)
	}
	if stats.Interactive != 2 {
		t.Fatalf("expected 2 interactive (button+link), got %d", stats.Interactive)
	}
}

func TestInteractiveRoles(t *testing.T) {
	if !InteractiveRoles[RoleButton] {
		t.Fatal("button should be interactive")
	}
	if !InteractiveRoles[RoleLink] {
		t.Fatal("link should be interactive")
	}
	if InteractiveRoles[RoleHeading] {
		t.Fatal("heading should not be interactive")
	}
}

func TestStructuralRoles(t *testing.T) {
	if !StructuralRoles[RoleList] {
		t.Fatal("list should be structural")
	}
	if !StructuralRoles[RoleNavigation] {
		t.Fatal("navigation should be structural")
	}
	if StructuralRoles[RoleButton] {
		t.Fatal("button should not be structural")
	}
}

func TestContentRoles(t *testing.T) {
	if !ContentRoles[RoleHeading] {
		t.Fatal("heading should be content")
	}
	if !ContentRoles[RoleImg] {
		t.Fatal("img should be content")
	}
	if ContentRoles[RoleButton] {
		t.Fatal("button should not be content")
	}
}
