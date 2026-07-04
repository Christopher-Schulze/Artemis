package observe

import "testing"

func TestAXNodeFields(t *testing.T) {
	n := AXNode{
		ID:            "e5",
		BackendNodeID: 42,
		Role:          "button",
		Name:          "Submit",
		Value:         "",
		Focused:       true,
		Disabled:      false,
		Visible:       true,
		Depth:         3,
		FrameID:       "frame-1",
	}
	if n.ID != "e5" {
		t.Errorf("ID = %s", n.ID)
	}
	if n.BackendNodeID != 42 {
		t.Errorf("BackendNodeID = %d", n.BackendNodeID)
	}
	if !n.Focused {
		t.Error("Focused should be true")
	}
}

func TestAXSnapshotConfigPierce(t *testing.T) {
	cfg := DefaultAXSnapshotConfig()
	if !cfg.Pierce {
		t.Error("default config should have Pierce=true")
	}
	if !cfg.FilterByVisibility {
		t.Error("default config should filter by visibility")
	}
}

func TestInteractiveAXRoles(t *testing.T) {
	required := []string{"button", "link", "textbox", "combobox", "checkbox", "radio", "option", "menuitem", "tab"}
	for _, role := range required {
		if !IsInteractiveRole(role) {
			t.Errorf("IsInteractiveRole(%q) = false, want true", role)
		}
	}
	if IsInteractiveRole("heading") {
		t.Error("IsInteractiveRole(heading) should be false")
	}
}

func TestFilterAXSnapshotByDepth(t *testing.T) {
	nodes := []AXNode{
		{ID: "e1", Role: "button", Depth: 1, Visible: true},
		{ID: "e2", Role: "link", Depth: 5, Visible: true},
		{ID: "e3", Role: "textbox", Depth: 10, Visible: true},
	}
	cfg := AXSnapshotConfig{MaxDepth: 3, FilterByVisibility: false}
	filtered := FilterAXSnapshot(nodes, cfg)
	if len(filtered) != 1 {
		t.Errorf("filtered len = %d, want 1", len(filtered))
	}
	if filtered[0].ID != "e1" {
		t.Errorf("filtered[0].ID = %s, want e1", filtered[0].ID)
	}
}

func TestFilterAXSnapshotByRole(t *testing.T) {
	nodes := []AXNode{
		{ID: "e1", Role: "button", Visible: true},
		{ID: "e2", Role: "heading", Visible: true},
		{ID: "e3", Role: "link", Visible: true},
	}
	cfg := AXSnapshotConfig{
		FilterByRole:       []string{"button", "link"},
		FilterByVisibility: false,
	}
	filtered := FilterAXSnapshot(nodes, cfg)
	if len(filtered) != 2 {
		t.Errorf("filtered len = %d, want 2", len(filtered))
	}
}

func TestFilterAXSnapshotByVisibility(t *testing.T) {
	nodes := []AXNode{
		{ID: "e1", Role: "button", Visible: true},
		{ID: "e2", Role: "link", Visible: false},
	}
	cfg := AXSnapshotConfig{FilterByVisibility: true}
	filtered := FilterAXSnapshot(nodes, cfg)
	if len(filtered) != 1 {
		t.Errorf("filtered len = %d, want 1", len(filtered))
	}
	if filtered[0].ID != "e1" {
		t.Errorf("filtered[0].ID = %s, want e1", filtered[0].ID)
	}
}

func TestMergeAXFrames(t *testing.T) {
	frame1 := []AXNode{{ID: "e1", Role: "button", FrameID: "f1"}}
	frame2 := []AXNode{{ID: "e2", Role: "link", FrameID: "f2"}}
	merged := MergeAXFrames([][]AXNode{frame1, frame2})
	if len(merged) != 2 {
		t.Errorf("merged len = %d, want 2", len(merged))
	}
}

func TestDedupKey(t *testing.T) {
	n := AXNode{ID: "e5", Role: "button", Name: "Submit"}
	key := DedupKey(n)
	want := "button:Submit:e5"
	if key != want {
		t.Errorf("DedupKey = %q, want %q", key, want)
	}
}

func TestDiffAXSnapshotsAdded(t *testing.T) {
	prev := []AXNode{{ID: "e1", Role: "button", Name: "A"}}
	curr := []AXNode{
		{ID: "e1", Role: "button", Name: "A"},
		{ID: "e2", Role: "link", Name: "B"},
	}
	diff := DiffAXSnapshots(prev, curr)
	if len(diff.Added) != 1 {
		t.Fatalf("Added len = %d, want 1", len(diff.Added))
	}
	if diff.Added[0].ID != "e2" {
		t.Errorf("Added[0].ID = %s, want e2", diff.Added[0].ID)
	}
	if len(diff.Removed) != 0 {
		t.Errorf("Removed len = %d, want 0", len(diff.Removed))
	}
	if len(diff.Changed) != 0 {
		t.Errorf("Changed len = %d, want 0", len(diff.Changed))
	}
}

func TestDiffAXSnapshotsRemoved(t *testing.T) {
	prev := []AXNode{
		{ID: "e1", Role: "button", Name: "A"},
		{ID: "e2", Role: "link", Name: "B"},
	}
	curr := []AXNode{{ID: "e1", Role: "button", Name: "A"}}
	diff := DiffAXSnapshots(prev, curr)
	if len(diff.Removed) != 1 {
		t.Fatalf("Removed len = %d, want 1", len(diff.Removed))
	}
	if diff.Removed[0].ID != "e2" {
		t.Errorf("Removed[0].ID = %s, want e2", diff.Removed[0].ID)
	}
}

func TestDiffAXSnapshotsValueChanged(t *testing.T) {
	prev := []AXNode{{ID: "e1", Role: "textbox", Name: "Search", Value: "old"}}
	curr := []AXNode{{ID: "e1", Role: "textbox", Name: "Search", Value: "new"}}
	diff := DiffAXSnapshots(prev, curr)
	if len(diff.Changed) != 1 {
		t.Fatalf("Changed len = %d, want 1", len(diff.Changed))
	}
	if diff.Changed[0].Value != "new" {
		t.Errorf("Changed[0].Value = %s, want new", diff.Changed[0].Value)
	}
}

func TestDiffAXSnapshotsFocusChanged(t *testing.T) {
	prev := []AXNode{{ID: "e1", Role: "button", Name: "A", Focused: false}}
	curr := []AXNode{{ID: "e1", Role: "button", Name: "A", Focused: true}}
	diff := DiffAXSnapshots(prev, curr)
	if len(diff.Changed) != 1 {
		t.Fatalf("Changed len = %d, want 1", len(diff.Changed))
	}
}

func TestDiffAXSnapshotsDisabledChanged(t *testing.T) {
	prev := []AXNode{{ID: "e1", Role: "button", Name: "A", Disabled: false}}
	curr := []AXNode{{ID: "e1", Role: "button", Name: "A", Disabled: true}}
	diff := DiffAXSnapshots(prev, curr)
	if len(diff.Changed) != 1 {
		t.Fatalf("Changed len = %d, want 1", len(diff.Changed))
	}
}

func TestDiffAXSnapshotsNoChanges(t *testing.T) {
	prev := []AXNode{{ID: "e1", Role: "button", Name: "A"}}
	curr := []AXNode{{ID: "e1", Role: "button", Name: "A"}}
	diff := DiffAXSnapshots(prev, curr)
	if diff.HasChanges() {
		t.Error("HasChanges should be false for identical snapshots")
	}
	if diff.TotalChanges() != 0 {
		t.Errorf("TotalChanges = %d, want 0", diff.TotalChanges())
	}
}

func TestDiffAXSnapshotTreesFacade(t *testing.T) {
	prev := []AXTreeNode{{ID: "e1", Role: "button", Name: "A"}}
	curr := []AXTreeNode{{ID: "e1", Role: "button", Name: "A"}, {ID: "e2", Role: "link", Name: "B"}}
	diff := DiffAXSnapshotTrees(prev, curr)
	if len(diff.Added) != 1 {
		t.Errorf("Added len = %d, want 1", len(diff.Added))
	}
}
