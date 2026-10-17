package observe

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestDiffReportsEveryMaterialNodeField(t *testing.T) {
	before := Node{Ref: "e1", BackendNodeID: 4, FrameID: "main", ParentBackendNodeID: 3, Role: "button", Name: "Action", Value: "a", Tag: "button", Attributes: map[string]string{"type": "button"}, Depth: 3, Order: 4, ShadowPath: []string{"open"}, Box: &Rect{X: 10, Y: 20, Width: 100, Height: 40}, Visible: true, Interactive: true, Interactable: true, Hit: HitClear}
	for _, test := range []struct {
		name   string
		change func(*Node)
	}{
		{"backend", func(n *Node) { n.BackendNodeID++ }},
		{"frame", func(n *Node) { n.FrameID = "other" }},
		{"parent", func(n *Node) { n.ParentBackendNodeID++ }},
		{"role", func(n *Node) { n.Role = "link" }},
		{"name", func(n *Node) { n.Name = "changed" }},
		{"value", func(n *Node) { n.Value = "changed" }},
		{"tag", func(n *Node) { n.Tag = "a" }},
		{"attribute change", func(n *Node) { n.Attributes["type"] = "submit" }},
		{"attribute add", func(n *Node) { n.Attributes["aria-expanded"] = "true" }},
		{"attribute remove", func(n *Node) { delete(n.Attributes, "type") }},
		{"depth", func(n *Node) { n.Depth++ }},
		{"order", func(n *Node) { n.Order++ }},
		{"shadow", func(n *Node) { n.ShadowPath[0] = "closed" }},
		{"box", func(n *Node) { n.Box.X++ }},
		{"layout removed", func(n *Node) { n.Box = nil }},
		{"visible", func(n *Node) { n.Visible = false }},
		{"disabled", func(n *Node) { n.Disabled = true }},
		{"focused", func(n *Node) { n.Focused = true }},
		{"interactive", func(n *Node) { n.Interactive = false }},
		{"interactable", func(n *Node) { n.Interactable = false }},
		{"hit", func(n *Node) { n.Hit = HitCovered }},
	} {
		t.Run(test.name, func(t *testing.T) {
			after := cloneNode(before)
			test.change(&after)
			diff := DiffSnapshots(Snapshot{Nodes: []Node{before}}, Snapshot{Nodes: []Node{after}})
			if len(diff.Changed) != 1 || len(diff.Added) != 0 || len(diff.Removed) != 0 || !changed(diff.Changed[0], before) {
				t.Fatalf("material %s lost: %+v", test.name, diff)
			}
		})
	}
	after := cloneNode(before)
	after.Ref = "e2"
	diff := DiffSnapshots(Snapshot{Nodes: []Node{before}}, Snapshot{Nodes: []Node{after}})
	if len(diff.Added) != 1 || len(diff.Removed) != 1 || len(diff.Changed) != 0 {
		t.Fatalf("reference replacement lost identity: %+v", diff)
	}
}

func TestDiffOrderHasDeterministicIdentityTieBreaks(t *testing.T) {
	nodes := []Node{{Ref: "e3", FrameID: "main", Order: 1, BackendNodeID: 4}, {Ref: "e2", FrameID: "main", Order: 1, BackendNodeID: 4}, {Ref: "e1", FrameID: "main", Order: 1, BackendNodeID: 3}, {Ref: "e4", FrameID: "other", Order: 0, BackendNodeID: 1}}
	var expected []byte
	for i := range 100 {
		rotated := append(append([]Node(nil), nodes[i%len(nodes):]...), nodes[:i%len(nodes)]...)
		diff := DiffSnapshots(Snapshot{}, Snapshot{Nodes: rotated})
		raw, err := json.Marshal(diff)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			expected = raw
		}
		if string(raw) != string(expected) || diff.Added[0].Ref != "e1" || diff.Added[1].Ref != "e2" || diff.Added[2].Ref != "e3" {
			t.Fatalf("diff varies with map/slice order: %s expected %s", raw, expected)
		}
	}
}

func TestUnchangedDiffHasNoAllocationsOrFalseChanges(t *testing.T) {
	a := Snapshot{Nodes: []Node{{Ref: "e1", BackendNodeID: 4, FrameID: "main", Attributes: map[string]string{"role": "button"}, ShadowPath: []string{"open"}}, {BackendNodeID: 5, FrameID: "main", Role: "generic"}}}
	b := cloneSnapshot(a)
	b.CapturedAt = b.CapturedAt.Add(1)
	allocations := testing.AllocsPerRun(100, func() {
		diff := DiffSnapshots(a, b)
		if len(diff.Added)+len(diff.Changed)+len(diff.Removed) != 0 {
			t.Fatalf("unchanged nodes emitted diff: %+v", diff)
		}
	})
	if allocations != 0 {
		t.Fatalf("unchanged diff allocates: %v", allocations)
	}
}

func BenchmarkDiffSnapshotsUnchanged(b *testing.B) {
	snapshot := Snapshot{Nodes: make([]Node, 2000)}
	for i := range snapshot.Nodes {
		snapshot.Nodes[i] = Node{Ref: fmt.Sprintf("e%d", i), BackendNodeID: int64(i + 1), FrameID: "main", Role: "button", Name: "Action", Attributes: map[string]string{"role": "button"}, Box: &Rect{Width: 100, Height: 40}, Interactive: true, Interactable: true, Hit: HitClear}
		if i%4 != 0 {
			snapshot.Nodes[i].Ref, snapshot.Nodes[i].Role = "", "generic"
			snapshot.Nodes[i].Interactive, snapshot.Nodes[i].Interactable, snapshot.Nodes[i].Hit = false, false, HitUnknown
		}
	}
	after := cloneSnapshot(snapshot)
	b.ReportAllocs()
	for b.Loop() {
		diff := DiffSnapshots(snapshot, after)
		if len(diff.Added)+len(diff.Changed)+len(diff.Removed) != 0 {
			b.Fatal("unchanged benchmark emitted changes")
		}
	}
}
