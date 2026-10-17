package observe

import (
	"context"
	"errors"
	"math"
	"testing"
)

func TestFrameTranslationRejectsUnverifiedGeometry(t *testing.T) {
	for _, test := range []struct {
		name            string
		border, content []float64
		width, height   float64
		valid           bool
	}{
		{"border inset", []float64{10, 20, 120, 20, 120, 130, 10, 130}, []float64{15, 25, 115, 25, 115, 125, 15, 125}, 110, 110, true},
		{"incomplete", nil, nil, 110, 110, false},
		{"non finite", []float64{math.NaN(), 0, 100, 0, 100, 100, 0, 100}, []float64{0, 0, 100, 0, 100, 100, 0, 100}, 100, 100, false},
		{"rotated", []float64{0, 0, 100, 10, 110, 100, 10, 90}, []float64{0, 0, 100, 10, 110, 100, 10, 90}, 100, 100, false},
		{"scaled", []float64{0, 0, 200, 0, 200, 200, 0, 200}, []float64{0, 0, 200, 0, 200, 200, 0, 200}, 100, 100, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var model boxModelResult
			model.Model.Border, model.Model.Content = test.border, test.content
			model.Model.Width, model.Model.Height = test.width, test.height
			x, y, err := frameTranslation(model)
			if (err == nil) != test.valid {
				t.Fatalf("geometry admission=%v want=%v error=%v", err == nil, test.valid, err)
			}
			if test.valid && (x != 15 || y != 25) {
				t.Fatalf("content origin ignored border inset: %v,%v", x, y)
			}
		})
	}
}

func TestPointerFrameRootDeniesUnknownAndCyclicOwnership(t *testing.T) {
	g := newPointerGeometry(&scriptedCaller{}, frameTree{Frame: frame{ID: "main"}})
	g.parents["a"], g.parents["b"] = "b", "a"
	for _, frameID := range []string{"", "foreign", "a"} {
		if root, err := g.frameRoot(frameID); err == nil {
			t.Fatalf("unowned/cyclic frame %q admitted as %q", frameID, root)
		}
	}
}

func TestHitBudgetNeverLeavesUnknownNodesInteractable(t *testing.T) {
	config := DefaultConfig()
	config.MaxHitTests = 1
	collector, err := NewCollector(&scriptedCaller{documents: []domSnapshotResult{domFixture(1, 4, "Login", true)}}, config)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := collector.Capture(t.Context(), ModeFull, "")
	if err != nil {
		t.Fatal(err)
	}
	if !contains(snapshot.Warnings, "hit-test budget exhausted") {
		t.Fatalf("budget omission hidden: %v", snapshot.Warnings)
	}
	first, second := findBackend(t, snapshot.Nodes, 4), findBackend(t, snapshot.Nodes, 5)
	if !first.Interactable || first.Hit != HitClear || second.Interactable || second.Hit != HitUnknown {
		t.Fatalf("budget actionability wrong: first=%+v second=%+v", first, second)
	}
}

func TestCaptureCancellationNeverReturnsSuccess(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	collector, err := NewCollector(&scriptedCaller{documents: []domSnapshotResult{domFixture(1, 4, "Login", false)}}, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := collector.Capture(ctx, ModeFull, ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled capture error=%v", err)
	}
}

func TestHitAncestryNeverCrossesRendererFrameIdentities(t *testing.T) {
	collector, err := NewCollector(&scriptedCaller{}, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	nodes := []Node{
		{BackendNodeID: 8, FrameID: "main", Interactive: true, Visible: true, Box: &Rect{Width: 100, Height: 40}},
		{BackendNodeID: 4, FrameID: "main"},
		{BackendNodeID: 4, FrameID: "foreign", ParentBackendNodeID: 8},
	}
	collector.applyHitTests(t.Context(), nodes, frameTree{Frame: frame{ID: "main"}})
	if nodes[0].Hit != HitCovered || nodes[0].Interactable {
		t.Fatalf("foreign ancestry admitted an unrelated main-frame hit: %+v", nodes[0])
	}
}
