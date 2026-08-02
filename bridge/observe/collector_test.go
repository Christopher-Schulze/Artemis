package observe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
)

type scriptedCaller struct {
	mu         sync.Mutex
	documents  []map[string]any
	roots      []int64
	capture    int
	failMethod string
}

func (s *scriptedCaller) Call(_ context.Context, method string, _ any, result any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if method == s.failMethod {
		return errors.New("injected failure")
	}
	var value any
	switch method {
	case "Accessibility.enable", "DOM.enable":
		value = map[string]any{}
	case "Page.getFrameTree":
		value = map[string]any{"frameTree": map[string]any{"frame": map[string]any{"id": "main"}, "childFrames": []any{map[string]any{"frame": map[string]any{"id": "child", "parentId": "main"}}}}}
	case "Accessibility.getFullAXTree":
		value = axFixture()
	case "DOMSnapshot.captureSnapshot":
		index := s.capture
		if index >= len(s.documents) {
			index = len(s.documents) - 1
		}
		value = s.documents[index]
		s.capture++
	case "DOM.getNodeForLocation":
		value = map[string]any{"backendNodeId": 4}
	case "DOM.getDocument":
		root := int64(1)
		if len(s.roots) > 0 {
			index := s.capture - 1
			if index >= len(s.roots) {
				index = len(s.roots) - 1
			}
			root = s.roots[index]
		}
		value = map[string]any{"root": map[string]any{"backendNodeId": root, "children": []any{map[string]any{"backendNodeId": 3, "shadowRoots": []any{map[string]any{"backendNodeId": 30, "shadowRootType": "open", "children": []any{map[string]any{"backendNodeId": 4}}}}}}}}
	default:
		return fmt.Errorf("unexpected method %s", method)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, result)
}

func domFixture(root, button int64, name string, duplicate bool) map[string]any {
	stringsTable := []string{"main", "#document", "", "html", "body", "button", "type", "password", "aria-label", name, "open", "display", "visibility", "opacity", "pointer-events", "child", "input", "value", "token"}
	backend := []int64{root, 2, 3, button}
	parent := []int{-1, 0, 1, 2}
	names := []int{1, 3, 4, 5}
	attrs := [][]int{{}, {}, {}, {8, 9}}
	bounds := [][]float64{{0, 0, 1000, 800}, {0, 0, 1000, 800}, {0, 0, 1000, 800}, {20, 30, 140, 40}}
	if duplicate {
		backend = append(backend, button+1)
		parent = append(parent, 2)
		names = append(names, 5)
		attrs = append(attrs, []int{8, 9})
		bounds = append(bounds, []float64{20, 30, 140, 40})
	}
	nodeIndex := make([]int, len(backend))
	styles := make([][]int, len(backend))
	nodeValue := make([]int, len(backend))
	nodeType := make([]int, len(backend))
	attributes := make([][]int, len(backend))
	for i := range backend {
		nodeIndex[i] = i
		styles[i] = []int{10, 10, 10, 10}
		nodeType[i] = 1
		if i < len(attrs) {
			attributes[i] = attrs[i]
		}
	}
	return map[string]any{"strings": stringsTable, "documents": []any{map[string]any{"frameId": 0, "nodes": map[string]any{"parentIndex": parent, "nodeType": nodeType, "nodeName": names, "nodeValue": nodeValue, "backendNodeId": backend, "attributes": attributes, "shadowRootType": map[string]any{"index": []int{2}, "value": []int{10}}}, "layout": map[string]any{"nodeIndex": nodeIndex, "styles": styles, "bounds": bounds}}}}
}

func withFrame(document map[string]any, frameIndex int) map[string]any {
	documents := document["documents"].([]any)
	documents[0].(map[string]any)["frameId"] = frameIndex
	return document
}

func axFixture() map[string]any {
	return map[string]any{"nodes": []any{map[string]any{"backendDOMNodeId": 4, "role": map[string]any{"value": "button"}, "name": map[string]any{"value": "Login"}, "properties": []any{map[string]any{"name": "focused", "value": map[string]any{"value": true}}}}}}
}

func TestCaptureStableRefsRedactionGeometryAndShadow(t *testing.T) {
	caller := &scriptedCaller{documents: []map[string]any{domFixture(1, 4, "Login", false), domFixture(1, 4, "Login", false)}}
	collector, err := NewCollector(caller, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	first, err := collector.Capture(context.Background(), ModeFull, "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := collector.Capture(context.Background(), ModeFull, "")
	if err != nil {
		t.Fatal(err)
	}
	button := findBackend(t, first.Nodes, 4)
	if button.Ref == "" || button.Ref != findBackend(t, second.Nodes, 4).Ref {
		t.Fatalf("stable ref not preserved: %#v", button)
	}
	if button.Box == nil || button.Box.Width != 140 || button.Box.X != 20 {
		t.Fatalf("real snapshot geometry not retained: %#v", button.Box)
	}
	if len(button.ShadowPath) != 1 || button.ShadowPath[0] != "open" {
		t.Fatalf("shadow ancestry missing: %#v", button.ShadowPath)
	}
	if !button.Visible || !button.Interactive || !button.Interactable || !button.Focused {
		t.Fatalf("state incomplete: %#v", button)
	}
}

func TestRedactAttributesDoesNotLeakSensitiveValue(t *testing.T) {
	attributes := map[string]string{
		"type":       "password",
		"value":      "super-secret",
		"aria-label": "Password",
	}
	redacted := redactAttributes(attributes, DefaultConfig().SensitiveAttributes)
	if redacted["value"] != "[REDACTED]" {
		t.Fatalf("sensitive value leaked: %#v", redacted)
	}
	if redacted["type"] != "password" || redacted["aria-label"] != "Password" {
		t.Fatalf("non-secret metadata was removed: %#v", redacted)
	}
}

func TestResolveExactReResolvedDetachedAndAmbiguous(t *testing.T) {
	t.Run("exact", func(t *testing.T) {
		c, _ := NewCollector(&scriptedCaller{documents: []map[string]any{domFixture(1, 4, "Login", false), domFixture(1, 4, "Login", false)}}, DefaultConfig())
		s, _ := c.Capture(context.Background(), ModeFull, "")
		r, err := c.Resolve(context.Background(), findBackend(t, s.Nodes, 4).Ref)
		if err != nil || r.Status != ResolutionExact {
			t.Fatalf("got %#v %v snapshot=%#v", r, err, c.last)
		}
	})
	t.Run("re-resolved", func(t *testing.T) {
		c, _ := NewCollector(&scriptedCaller{documents: []map[string]any{domFixture(1, 4, "Login", false), domFixture(9, 8, "Login", false)}}, DefaultConfig())
		s, _ := c.Capture(context.Background(), ModeFull, "")
		r, err := c.Resolve(context.Background(), findBackend(t, s.Nodes, 4).Ref)
		if err != nil || r.Status != ResolutionReResolved || r.Node.BackendNodeID != 8 {
			t.Fatalf("got %#v %v snapshot=%#v", r, err, c.last)
		}
	})
	t.Run("detached", func(t *testing.T) {
		c, _ := NewCollector(&scriptedCaller{documents: []map[string]any{domFixture(1, 4, "Login", false), domFixture(9, 8, "Different", false)}}, DefaultConfig())
		s, _ := c.Capture(context.Background(), ModeFull, "")
		r, _ := c.Resolve(context.Background(), findBackend(t, s.Nodes, 4).Ref)
		if r.Status != ResolutionDetached {
			t.Fatalf("got %#v snapshot=%#v", r, c.last)
		}
	})
	t.Run("ambiguous", func(t *testing.T) {
		c, _ := NewCollector(&scriptedCaller{documents: []map[string]any{domFixture(1, 4, "Login", false), domFixture(9, 8, "Login", true)}}, DefaultConfig())
		s, _ := c.Capture(context.Background(), ModeFull, "")
		r, _ := c.Resolve(context.Background(), findBackend(t, s.Nodes, 4).Ref)
		if r.Status != ResolutionAmbiguous {
			t.Fatalf("got %#v snapshot=%#v", r, c.last)
		}
	})
	t.Run("cross-frame", func(t *testing.T) {
		caller := &scriptedCaller{documents: []map[string]any{domFixture(1, 4, "Login", false), withFrame(domFixture(9, 8, "Login", false), 15)}, roots: []int64{1, 9}}
		c, _ := NewCollector(caller, DefaultConfig())
		s, _ := c.Capture(context.Background(), ModeFull, "")
		r, _ := c.Resolve(context.Background(), findBackend(t, s.Nodes, 4).Ref)
		if r.Status != ResolutionCrossFrame {
			t.Fatalf("got %#v snapshot=%#v", r, c.last)
		}
	})
}

func TestCaptureBudgetsViewsSchemaAndFailures(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxNodes = 2
	cfg.MaxSnapshotBytes = 100000
	c, _ := NewCollector(&scriptedCaller{documents: []map[string]any{domFixture(1, 4, "Login", true)}}, cfg)
	s, err := c.Capture(context.Background(), ModeFull, "")
	if err != nil {
		t.Fatal(err)
	}
	if s.Schema != Schema || !s.Truncated || len(s.Nodes) != 2 || s.TruncationReasons[0] != "max_nodes" {
		t.Fatalf("budget/schema mismatch: %#v", s)
	}
	if _, err = c.Capture(context.Background(), Mode("wrong"), ""); err == nil {
		t.Fatal("invalid mode accepted")
	}
	failing, _ := NewCollector(&scriptedCaller{documents: []map[string]any{domFixture(1, 4, "Login", false)}, failMethod: "DOMSnapshot.captureSnapshot"}, DefaultConfig())
	if _, err = failing.Capture(context.Background(), ModeFull, ""); err == nil {
		t.Fatal("CDP failure hidden")
	}
}

func TestCaptureInteractiveSubtreeEvidenceAndByteBudget(t *testing.T) {
	caller := &scriptedCaller{documents: []map[string]any{domFixture(1, 4, "Login", true), domFixture(1, 4, "Login", true), domFixture(1, 4, "Login", true), domFixture(1, 4, "Login", true)}}
	config := DefaultConfig()
	config.MaxSnapshotBytes = 700
	c, _ := NewCollector(caller, config)
	full, err := c.Capture(context.Background(), ModeFull, "")
	if err != nil {
		t.Fatal(err)
	}
	if !full.Truncated || !contains(full.TruncationReasons, "max_snapshot_bytes") {
		t.Fatalf("byte budget not enforced: %#v", full)
	}
	config.MaxSnapshotBytes = 100000
	c.config = config
	interactive, err := c.Capture(context.Background(), ModeInteractive, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(interactive.Nodes) != 2 {
		t.Fatalf("interactive nodes=%d", len(interactive.Nodes))
	}
	evidence, err := c.Capture(context.Background(), ModeEvidence, "")
	if err != nil || len(evidence.Nodes) != 2 {
		t.Fatalf("evidence=%#v err=%v", evidence, err)
	}
	subtree, err := c.Capture(context.Background(), ModeSubtree, interactive.Nodes[0].Ref)
	if err != nil || len(subtree.Nodes) != 1 {
		t.Fatalf("subtree=%#v err=%v", subtree, err)
	}
	if _, err = c.Capture(context.Background(), ModeSubtree, "e999"); err == nil {
		t.Fatal("unknown subtree ref accepted")
	}
}

func TestCollectorSnapshotsAndConfigAreImmutable(t *testing.T) {
	config := DefaultConfig()
	caller := &scriptedCaller{documents: []map[string]any{
		domFixture(1, 4, "Login", false),
		domFixture(1, 4, "Login", false),
		domFixture(1, 4, "Login", false),
	}}
	collector, err := NewCollector(caller, config)
	if err != nil {
		t.Fatal(err)
	}
	for i := range config.SensitiveAttributes {
		config.SensitiveAttributes[i] = "aria-label"
	}

	first, err := collector.Capture(context.Background(), ModeFull, "")
	if err != nil {
		t.Fatal(err)
	}
	button := findBackend(t, first.Nodes, 4)
	ref := button.Ref
	for i := range first.Nodes {
		if first.Nodes[i].BackendNodeID != 4 {
			continue
		}
		first.Nodes[i].Name = "mutated"
		first.Nodes[i].Attributes["aria-label"] = "mutated"
		first.Nodes[i].ShadowPath[0] = "mutated"
		first.Nodes[i].Box.Width = -1
	}
	first.TruncationReasons = append(first.TruncationReasons, "mutated")
	first.Warnings = append(first.Warnings, "mutated")

	last := collector.LastSnapshot()
	lastButton := findBackend(t, last.Nodes, 4)
	if lastButton.Name != "Login" || lastButton.Attributes["aria-label"] != "Login" || lastButton.ShadowPath[0] != "open" || lastButton.Box.Width != 140 {
		t.Fatalf("capture mutated collector state: %#v", lastButton)
	}
	lastButton.Attributes["aria-label"] = "second mutation"
	lastButton.ShadowPath[0] = "second mutation"
	lastButton.Box.Width = -2
	last.Nodes[0].Name = "second mutation"

	resolution, err := collector.Resolve(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Status != ResolutionExact || resolution.Node == nil || resolution.Node.Name != "Login" || resolution.Node.Attributes["aria-label"] != "Login" || resolution.Node.Box.Width != 140 {
		t.Fatalf("external mutation corrupted stable ref: %#v", resolution)
	}
}

func TestDiffSnapshotsDeterministic(t *testing.T) {
	a := Snapshot{Nodes: []Node{{Ref: "e1", FrameID: "main", Order: 0, Name: "A"}, {Ref: "e2", FrameID: "main", Order: 1, Name: "B"}}}
	b := Snapshot{Nodes: []Node{{Ref: "e1", FrameID: "main", Order: 0, Name: "changed"}, {Ref: "e3", FrameID: "main", Order: 2, Name: "C"}}}
	d := DiffSnapshots(a, b)
	if len(d.Added) != 1 || len(d.Changed) != 1 || len(d.Removed) != 1 || d.Added[0].Ref != "e3" {
		t.Fatalf("bad diff: %#v", d)
	}
}

func TestTraceRecordIsDeterministicAndSchemaBound(t *testing.T) {
	snapshot := Snapshot{Schema: Schema, Nodes: []Node{{Ref: "e1", Value: "[REDACTED]"}}}
	first, err := NewTraceRecord(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewTraceRecord(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if first.SnapshotHash == "" || first.SnapshotHash != second.SnapshotHash {
		t.Fatalf("non-deterministic trace hashes: %#v %#v", first, second)
	}
	if _, err := NewTraceRecord(Snapshot{Schema: "drift"}); err == nil {
		t.Fatal("trace accepted schema drift")
	}
}

func FuzzNormalizeRef(f *testing.F) {
	f.Add("@e12")
	f.Add("ref=e9")
	f.Fuzz(func(t *testing.T, value string) {
		got := normalizeRef(value)
		if len(got) > len(value) {
			t.Fatalf("normalization grew input: %q -> %q", value, got)
		}
	})
}

func findBackend(t *testing.T, nodes []Node, id int64) Node {
	t.Helper()
	for _, n := range nodes {
		if n.BackendNodeID == id {
			return n
		}
	}
	t.Fatalf("backend node %d missing: %#v", id, nodes)
	return Node{}
}
