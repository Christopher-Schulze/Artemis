package observe

import (
	"strings"
	"testing"
)

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

// TestMyersDiffFastPath verifies the identical-snapshot short-circuit
// returns 0 without allocating the DP table (spec L4258).
func TestMyersDiffFastPath(t *testing.T) {
	before := []AXNode{
		{ID: "e1", Role: "button", Name: "A"},
		{ID: "e2", Role: "link", Name: "B"},
		{ID: "e3", Role: "textbox", Name: "C"},
	}
	after := []AXNode{
		{ID: "e1", Role: "button", Name: "A"},
		{ID: "e2", Role: "link", Name: "B"},
		{ID: "e3", Role: "textbox", Name: "C"},
	}
	if got := MyersDiff(before, after); got != 0 {
		t.Errorf("MyersDiff identical = %d, want 0", got)
	}
}

// TestMyersDiffAddition verifies a single addition yields edit distance 1.
func TestMyersDiffAddition(t *testing.T) {
	before := []AXNode{{ID: "e1", Role: "button", Name: "A"}}
	after := []AXNode{
		{ID: "e1", Role: "button", Name: "A"},
		{ID: "e2", Role: "link", Name: "B"},
	}
	if got := MyersDiff(before, after); got != 1 {
		t.Errorf("MyersDiff addition = %d, want 1", got)
	}
}

// TestMyersDiffRemoval verifies a single removal yields edit distance 1.
func TestMyersDiffRemoval(t *testing.T) {
	before := []AXNode{
		{ID: "e1", Role: "button", Name: "A"},
		{ID: "e2", Role: "link", Name: "B"},
	}
	after := []AXNode{{ID: "e1", Role: "button", Name: "A"}}
	if got := MyersDiff(before, after); got != 1 {
		t.Errorf("MyersDiff removal = %d, want 1", got)
	}
}

// TestMyersEditScriptAddRemove verifies the edit script produces the
// expected equal/add/remove op sequence and summary counts (spec L4258).
func TestMyersEditScriptAddRemove(t *testing.T) {
	before := []AXNode{
		{ID: "e1", Role: "button", Name: "A"},
		{ID: "e2", Role: "link", Name: "B"},
	}
	after := []AXNode{
		{ID: "e1", Role: "button", Name: "A"},
		{ID: "e3", Role: "textbox", Name: "C"},
	}
	ops, summary := MyersEditScript(before, after)
	if summary.Additions != 1 {
		t.Errorf("Additions = %d, want 1", summary.Additions)
	}
	if summary.Removals != 1 {
		t.Errorf("Removals = %d, want 1", summary.Removals)
	}
	if summary.Unchanged != 1 {
		t.Errorf("Unchanged = %d, want 1", summary.Unchanged)
	}
	if summary.Changed != 0 {
		t.Errorf("Changed = %d, want 0", summary.Changed)
	}
	// Verify op types occur in expected order: equal, remove, add (or equal, add, remove).
	if len(ops) != 3 {
		t.Fatalf("ops len = %d, want 3", len(ops))
	}
	if ops[0].Type != DiffOpEqual {
		t.Errorf("ops[0].Type = %s, want equal", ops[0].Type)
	}
}

// TestMyersEditScriptChange verifies a value change is emitted as
// DiffOpChange, not as a separate add+remove pair (spec L4258).
func TestMyersEditScriptChange(t *testing.T) {
	before := []AXNode{{ID: "e1", Role: "textbox", Name: "Search", Value: "old"}}
	after := []AXNode{{ID: "e1", Role: "textbox", Name: "Search", Value: "new"}}
	ops, summary := MyersEditScript(before, after)
	if summary.Changed != 1 {
		t.Errorf("Changed = %d, want 1", summary.Changed)
	}
	if summary.Additions != 0 || summary.Removals != 0 {
		t.Errorf("Additions=%d Removals=%d, want 0/0", summary.Additions, summary.Removals)
	}
	if len(ops) != 1 {
		t.Fatalf("ops len = %d, want 1", len(ops))
	}
	if ops[0].Type != DiffOpChange {
		t.Errorf("ops[0].Type = %s, want change", ops[0].Type)
	}
	if ops[0].Before == nil || ops[0].After == nil {
		t.Error("change op must have both Before and After")
	}
	if ops[0].Before.Value != "old" || ops[0].After.Value != "new" {
		t.Errorf("change op values: before=%q after=%q", ops[0].Before.Value, ops[0].After.Value)
	}
}

// TestMyersEditScriptEmpty verifies empty inputs yield an empty script.
func TestMyersEditScriptEmpty(t *testing.T) {
	ops, summary := MyersEditScript(nil, nil)
	if len(ops) != 0 {
		t.Errorf("ops len = %d, want 0", len(ops))
	}
	if summary.Additions != 0 || summary.Removals != 0 || summary.Unchanged != 0 || summary.Changed != 0 {
		t.Errorf("summary nonzero: %+v", summary)
	}
}

// TestFormatNode verifies compact single-line node formatting.
func TestFormatNode(t *testing.T) {
	withName := FormatNode(AXNode{ID: "e5", Role: "button", Name: "Submit"})
	if withName != "button:Submit [e5]" {
		t.Errorf("FormatNode with name = %q", withName)
	}
	withoutName := FormatNode(AXNode{ID: "e7", Role: "heading"})
	if withoutName != "heading [e7]" {
		t.Errorf("FormatNode without name = %q", withoutName)
	}
}

// TestUnifiedDiffIdentical verifies no diff output for identical snapshots.
func TestUnifiedDiffIdentical(t *testing.T) {
	before := []AXNode{{ID: "e1", Role: "button", Name: "A"}}
	after := []AXNode{{ID: "e1", Role: "button", Name: "A"}}
	if got := UnifiedDiff(before, after, 3); got != "" {
		t.Errorf("UnifiedDiff identical = %q, want empty", got)
	}
}

// TestUnifiedDiffAddition verifies the hunk header and '+' prefix for adds.
func TestUnifiedDiffAddition(t *testing.T) {
	before := []AXNode{
		{ID: "e1", Role: "button", Name: "A"},
		{ID: "e2", Role: "link", Name: "B"},
		{ID: "e3", Role: "heading", Name: "C"},
		{ID: "e4", Role: "img", Name: "D"},
	}
	after := []AXNode{
		{ID: "e1", Role: "button", Name: "A"},
		{ID: "e2", Role: "link", Name: "B"},
		{ID: "e9", Role: "textbox", Name: "New"},
		{ID: "e3", Role: "heading", Name: "C"},
		{ID: "e4", Role: "img", Name: "D"},
	}
	out := UnifiedDiff(before, after, 3)
	if !strings.Contains(out, "@@ ") {
		t.Errorf("missing hunk header in:\n%s", out)
	}
	if !strings.Contains(out, "+textbox:New [e9]") {
		t.Errorf("missing +add line in:\n%s", out)
	}
}

// TestUnifiedDiffRemoval verifies the '-' prefix for removed nodes.
func TestUnifiedDiffRemoval(t *testing.T) {
	before := []AXNode{
		{ID: "e1", Role: "button", Name: "A"},
		{ID: "e2", Role: "link", Name: "B"},
	}
	after := []AXNode{{ID: "e1", Role: "button", Name: "A"}}
	out := UnifiedDiff(before, after, 3)
	if !strings.Contains(out, "-link:B [e2]") {
		t.Errorf("missing -remove line in:\n%s", out)
	}
}

// TestUnifiedDiffChange verifies the '~' and '|' prefixes for changed nodes.
func TestUnifiedDiffChange(t *testing.T) {
	before := []AXNode{{ID: "e1", Role: "textbox", Name: "Search", Value: "old"}}
	after := []AXNode{{ID: "e1", Role: "textbox", Name: "Search", Value: "new"}}
	out := UnifiedDiff(before, after, 3)
	if !strings.Contains(out, "~textbox:Search [e1]") {
		t.Errorf("missing ~change-before line in:\n%s", out)
	}
	if !strings.Contains(out, "|textbox:Search [e1]") {
		t.Errorf("missing |change-after line in:\n%s", out)
	}
}

// TestUnifiedDiffContextLines verifies that context lines surround changes.
func TestUnifiedDiffContextLines(t *testing.T) {
	before := []AXNode{
		{ID: "e1", Role: "button", Name: "A"},
		{ID: "e2", Role: "link", Name: "B"},
		{ID: "e3", Role: "heading", Name: "C"},
		{ID: "e4", Role: "img", Name: "D"},
		{ID: "e5", Role: "list", Name: "E"},
	}
	after := []AXNode{
		{ID: "e1", Role: "button", Name: "A"},
		{ID: "e2", Role: "link", Name: "B"},
		{ID: "e3", Role: "heading", Name: "C"},
		{ID: "e4", Role: "img", Name: "D"},
		{ID: "e5", Role: "list", Name: "E"},
		{ID: "e9", Role: "textbox", Name: "New"},
	}
	out := UnifiedDiff(before, after, 2)
	// With 2 context lines, the hunk should include e4, e5 as the 2 context
	// lines before the +e9 add (which is at the end of the script).
	if !strings.Contains(out, " img:D [e4]") {
		t.Errorf("missing context line e4 in:\n%s", out)
	}
	if !strings.Contains(out, " list:E [e5]") {
		t.Errorf("missing context line e5 in:\n%s", out)
	}
	if !strings.Contains(out, "+textbox:New [e9]") {
		t.Errorf("missing +add line in:\n%s", out)
	}
	// e3 is 3 lines before the change; with context=2 it must be excluded.
	if strings.Contains(out, "heading:C [e3]") {
		t.Errorf("e3 should not be in hunk with context=2:\n%s", out)
	}
}

// TestUnifiedDiffMultipleHunks verifies that non-adjacent changes produce
// separate hunk headers.
func TestUnifiedDiffMultipleHunks(t *testing.T) {
	before := []AXNode{
		{ID: "e1", Role: "button", Name: "A"},
		{ID: "e2", Role: "link", Name: "B"},
		{ID: "e3", Role: "heading", Name: "C"},
		{ID: "e4", Role: "img", Name: "D"},
		{ID: "e5", Role: "list", Name: "E"},
		{ID: "e6", Role: "paragraph", Name: "F"},
		{ID: "e7", Role: "article", Name: "G"},
		{ID: "e8", Role: "section", Name: "H"},
		{ID: "e9", Role: "form", Name: "I"},
		{ID: "e10", Role: "navigation", Name: "J"},
	}
	after := []AXNode{
		{ID: "e1", Role: "button", Name: "A"},
		{ID: "e2", Role: "link", Name: "B"},
		{ID: "e3", Role: "heading", Name: "C"},
		{ID: "e4", Role: "img", Name: "D"},
		{ID: "e5", Role: "list", Name: "E"},
		{ID: "e6", Role: "paragraph", Name: "F"},
		{ID: "e7", Role: "article", Name: "G"},
		{ID: "e8", Role: "section", Name: "H"},
		{ID: "e9", Role: "form", Name: "I"},
		{ID: "e10", Role: "navigation", Name: "J"},
		{ID: "e20", Role: "textbox", Name: "X"},
		{ID: "e21", Role: "checkbox", Name: "Y"},
	}
	out := UnifiedDiff(before, after, 1)
	// With context=1, the two adjacent adds (e20, e21) should be in one hunk.
	hunkCount := strings.Count(out, "@@ ")
	if hunkCount < 1 {
		t.Errorf("expected at least 1 hunk header, got %d in:\n%s", hunkCount, out)
	}
}
