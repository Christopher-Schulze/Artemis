package observe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const Schema = "artemis.observation.v1"

type identity struct {
	epoch   uint64
	frame   string
	backend int64
}
type remembered struct {
	ref   string
	node  Node
	epoch uint64
}

type Collector struct {
	caller     Caller
	config     Config
	mu         sync.Mutex
	epoch      uint64
	root       int64
	frame      string
	nextRef    uint64
	refs       map[string]remembered
	identities map[identity]string
	last       Snapshot
}

func NewCollector(caller Caller, config Config) (*Collector, error) {
	if caller == nil {
		return nil, errors.New("observation: CDP caller required")
	}
	defaults := DefaultConfig()
	if config.MaxNodes <= 0 {
		config.MaxNodes = defaults.MaxNodes
	}
	if config.MaxDepth <= 0 {
		config.MaxDepth = defaults.MaxDepth
	}
	if config.MaxTextBytes <= 0 {
		config.MaxTextBytes = defaults.MaxTextBytes
	}
	if config.MaxSnapshotBytes <= 0 {
		config.MaxSnapshotBytes = defaults.MaxSnapshotBytes
	}
	if config.MaxHitTests < 0 {
		return nil, errors.New("observation: max hit tests cannot be negative")
	}
	if config.MaxHitTests == 0 {
		config.MaxHitTests = defaults.MaxHitTests
	}
	if len(config.SensitiveAttributes) == 0 {
		config.SensitiveAttributes = defaults.SensitiveAttributes
	}
	config.SensitiveAttributes = append([]string(nil), config.SensitiveAttributes...)
	if config.ReResolveThreshold <= 0 || config.ReResolveThreshold > 1 {
		config.ReResolveThreshold = defaults.ReResolveThreshold
	}
	return &Collector{caller: caller, config: config, refs: make(map[string]remembered), identities: make(map[identity]string)}, nil
}

func (c *Collector) Capture(ctx context.Context, mode Mode, subtreeRef string) (Snapshot, error) {
	if ctx == nil {
		return Snapshot{}, errors.New("observation: context required")
	}
	if mode == "" {
		mode = ModeFull
	}
	if mode != ModeFull && mode != ModeInteractive && mode != ModeSubtree && mode != ModeEvidence {
		return Snapshot{}, fmt.Errorf("observation: unsupported mode %q", mode)
	}
	if err := c.caller.Call(ctx, "Accessibility.enable", map[string]any{}, &struct{}{}); err != nil {
		return Snapshot{}, fmt.Errorf("enable accessibility: %w", err)
	}
	if err := c.caller.Call(ctx, "DOM.enable", map[string]any{}, &struct{}{}); err != nil {
		return Snapshot{}, fmt.Errorf("enable DOM: %w", err)
	}
	var dom domSnapshotResult
	params := map[string]any{"computedStyles": []string{"display", "visibility", "opacity", "pointer-events"}, "includeDOMRects": true, "includePaintOrder": true}
	if err := c.caller.Call(ctx, "DOMSnapshot.captureSnapshot", params, &dom); err != nil {
		return Snapshot{}, fmt.Errorf("capture DOM snapshot: %w", err)
	}
	if len(dom.Documents) == 0 {
		return Snapshot{}, errors.New("capture DOM snapshot: no documents")
	}
	var frames frameTreeResult
	if err := c.caller.Call(ctx, "Page.getFrameTree", map[string]any{}, &frames); err != nil {
		return Snapshot{}, fmt.Errorf("get frame tree: %w", err)
	}
	frameIDs := flattenFrames(frames.FrameTree)
	var document documentResult
	if err := c.caller.Call(ctx, "DOM.getDocument", map[string]any{"depth": -1, "pierce": true}, &document); err != nil {
		return Snapshot{}, fmt.Errorf("get pierced DOM document: %w", err)
	}
	shadowPaths := make(map[int64][]string)
	collectShadowPaths(document.Root, nil, shadowPaths)
	axByBackend := make(map[int64]axNode)
	warnings := make([]string, 0)
	for _, frameID := range frameIDs {
		if router, ok := c.caller.(FrameCaller); ok {
			if _, attached := router.FrameSessions()[frameID]; attached {
				continue
			}
		}
		var tree axTreeResult
		if err := c.caller.Call(ctx, "Accessibility.getFullAXTree", map[string]any{"frameId": frameID}, &tree); err != nil {
			if frameID == frames.FrameTree.Frame.ID {
				return Snapshot{}, fmt.Errorf("get main-frame AX tree: %w", err)
			}
			warnings = append(warnings, "AX tree unavailable for frame "+frameID)
			continue
		}
		for _, ax := range tree.Nodes {
			if !ax.Ignored && ax.BackendNodeID != 0 {
				axByBackend[ax.BackendNodeID] = ax
			}
		}
	}
	nodes, root, reasons := c.buildNodes(dom, axByBackend)
	if router, ok := c.caller.(FrameCaller); ok {
		frameSessions := router.FrameSessions()
		attachedFrameIDs := make([]string, 0, len(frameSessions))
		for frameID := range frameSessions {
			attachedFrameIDs = append(attachedFrameIDs, frameID)
		}
		sort.Strings(attachedFrameIDs)
		for _, frameID := range attachedFrameIDs {
			var frameDOM domSnapshotResult
			_ = router.CallFrame(ctx, frameID, "Accessibility.enable", map[string]any{}, &struct{}{})
			_ = router.CallFrame(ctx, frameID, "DOM.enable", map[string]any{}, &struct{}{})
			if err := router.CallFrame(ctx, frameID, "DOMSnapshot.captureSnapshot", params, &frameDOM); err != nil {
				warnings = append(warnings, "DOM snapshot unavailable for OOPIF "+frameID+": "+err.Error())
				continue
			}
			var frameAX axTreeResult
			if err := router.CallFrame(ctx, frameID, "Accessibility.getFullAXTree", map[string]any{}, &frameAX); err != nil {
				warnings = append(warnings, "AX tree unavailable for OOPIF "+frameID+": "+err.Error())
			}
			frameAXByBackend := make(map[int64]axNode)
			for _, ax := range frameAX.Nodes {
				if !ax.Ignored && ax.BackendNodeID != 0 {
					frameAXByBackend[ax.BackendNodeID] = ax
				}
			}
			frameNodes, _, frameReasons := c.buildNodes(frameDOM, frameAXByBackend)
			nodes = append(nodes, frameNodes...)
			for _, reason := range frameReasons {
				if !contains(reasons, reason) {
					reasons = append(reasons, reason)
				}
			}
			var frameDocument documentResult
			if err := router.CallFrame(ctx, frameID, "DOM.getDocument", map[string]any{"depth": -1, "pierce": true}, &frameDocument); err == nil {
				collectShadowPaths(frameDocument.Root, nil, shadowPaths)
			}
		}
	}
	if document.Root.BackendNodeID != 0 {
		root = document.Root.BackendNodeID
	}
	for i := range nodes {
		if path := shadowPaths[nodes[i].BackendNodeID]; len(path) > 0 {
			nodes[i].ShadowPath = path
		}
	}
	warnings = append(warnings, c.applyHitTests(ctx, nodes, frames.FrameTree.Frame.ID)...)
	c.mu.Lock()
	defer c.mu.Unlock()
	mainFrame := frames.FrameTree.Frame.ID
	if c.epoch == 0 || c.root != root || c.frame != mainFrame {
		c.epoch++
		c.root = root
		c.frame = mainFrame
	}
	for i := range nodes {
		if !nodes[i].Interactive && nodes[i].Name == "" {
			continue
		}
		key := identity{epoch: c.epoch, frame: nodes[i].FrameID, backend: nodes[i].BackendNodeID}
		ref := c.identities[key]
		if ref == "" {
			c.nextRef++
			ref = "e" + strconv.FormatUint(c.nextRef, 10)
			c.identities[key] = ref
		}
		nodes[i].Ref = ref
		c.refs[ref] = remembered{ref: ref, node: cloneNode(nodes[i]), epoch: c.epoch}
	}
	if mode == ModeInteractive || mode == ModeEvidence {
		nodes = filterInteractive(nodes)
	}
	if mode == ModeSubtree {
		rememberedNode, ok := c.refs[normalizeRef(subtreeRef)]
		if !ok {
			return Snapshot{}, fmt.Errorf("observation: subtree ref %q not found", subtreeRef)
		}
		nodes = filterSubtree(nodes, rememberedNode.node.BackendNodeID)
	}
	snapshot := Snapshot{Schema: Schema, Epoch: c.epoch, CapturedAt: time.Now().UTC(), Mode: mode, RootBackendNodeID: root, Nodes: nodes, TotalNodes: len(nodes), Truncated: len(reasons) > 0, TruncationReasons: reasons, Warnings: warnings}
	snapshot = c.boundJSON(snapshot)
	c.last = cloneSnapshot(snapshot)
	return cloneSnapshot(snapshot), nil
}

// LastSnapshot returns the most recent completed observation. It is useful for
// evidence collection while Chromium is blocked by a modal JavaScript dialog.
func (c *Collector) LastSnapshot() Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return cloneSnapshot(c.last)
}

func cloneSnapshot(snapshot Snapshot) Snapshot {
	clone := snapshot
	clone.Nodes = make([]Node, len(snapshot.Nodes))
	for i := range snapshot.Nodes {
		clone.Nodes[i] = cloneNode(snapshot.Nodes[i])
	}
	clone.TruncationReasons = append([]string(nil), snapshot.TruncationReasons...)
	clone.Warnings = append([]string(nil), snapshot.Warnings...)
	return clone
}

func cloneNode(node Node) Node {
	clone := node
	if node.Attributes != nil {
		clone.Attributes = make(map[string]string, len(node.Attributes))
		for key, value := range node.Attributes {
			clone.Attributes[key] = value
		}
	}
	clone.ShadowPath = append([]string(nil), node.ShadowPath...)
	if node.Box != nil {
		box := *node.Box
		clone.Box = &box
	}
	return clone
}

func (c *Collector) applyHitTests(ctx context.Context, nodes []Node, mainFrame string) []string {
	parents := make(map[int64]int64, len(nodes))
	for _, node := range nodes {
		parents[node.BackendNodeID] = node.ParentBackendNodeID
	}
	tested := 0
	warnings := make([]string, 0)
	for i := range nodes {
		node := &nodes[i]
		if !node.Interactive || !node.Visible || node.Box == nil || node.FrameID != mainFrame {
			continue
		}
		if tested >= c.config.MaxHitTests {
			warnings = append(warnings, "hit-test budget exhausted")
			break
		}
		tested++
		var result struct {
			BackendNodeID int64 `json:"backendNodeId"`
		}
		params := map[string]any{"x": int(math.Round(node.Box.X + node.Box.Width/2)), "y": int(math.Round(node.Box.Y + node.Box.Height/2)), "includeUserAgentShadowDOM": false, "ignorePointerEventsNone": false}
		if err := c.caller.Call(ctx, "DOM.getNodeForLocation", params, &result); err != nil {
			warnings = append(warnings, "hit test unavailable for backend node "+strconv.FormatInt(node.BackendNodeID, 10)+": "+err.Error())
			continue
		}
		if result.BackendNodeID == node.BackendNodeID || isDescendantOf(result.BackendNodeID, node.BackendNodeID, parents) {
			node.Hit = HitClear
			continue
		}
		node.Hit = HitCovered
		node.Interactable = false
	}
	return warnings
}

func (c *Collector) buildNodes(result domSnapshotResult, axNodes map[int64]axNode) ([]Node, int64, []string) {
	out := make([]Node, 0)
	reasons := make([]string, 0)
	root := int64(0)
	for _, document := range result.Documents {
		frameID := stringAt(result.Strings, document.FrameID)
		layout := make(map[int]struct {
			box    Rect
			styles []string
		})
		for i, nodeIndex := range document.Layout.NodeIndex {
			if i >= len(document.Layout.Bounds) {
				continue
			}
			b := document.Layout.Bounds[i]
			if len(b) < 4 {
				continue
			}
			styles := make([]string, 0, 4)
			if i < len(document.Layout.Styles) {
				for _, value := range document.Layout.Styles[i] {
					styles = append(styles, stringAt(result.Strings, value))
				}
			}
			layout[nodeIndex] = struct {
				box    Rect
				styles []string
			}{Rect{X: b[0], Y: b[1], Width: b[2], Height: b[3]}, styles}
		}
		depths := make([]int, len(document.Nodes.BackendNodeID))
		for i, backend := range document.Nodes.BackendNodeID {
			if root == 0 && backend != 0 {
				root = backend
			}
			parent := -1
			if i < len(document.Nodes.ParentIndex) {
				parent = document.Nodes.ParentIndex[i]
			}
			if parent >= 0 && parent < len(depths) {
				depths[i] = depths[parent] + 1
			}
			if depths[i] > c.config.MaxDepth {
				if !contains(reasons, "max_depth") {
					reasons = append(reasons, "max_depth")
				}
				continue
			}
			if len(out) >= c.config.MaxNodes {
				if !contains(reasons, "max_nodes") {
					reasons = append(reasons, "max_nodes")
				}
				continue
			}
			attributes := attributesAt(document.Nodes.Attributes, i, result.Strings)
			ax := axNodes[backend]
			role, name, value, disabled, focused := axFields(ax)
			tag := strings.ToLower(stringAt(result.Strings, document.Nodes.NodeName[i]))
			if role == "" {
				role = inferredRole(tag, attributes)
			}
			if name == "" {
				name = firstNonEmpty(attributes["aria-label"], attributes["alt"], attributes["title"])
			}
			if value == "" {
				value = rareStringAt(document.Nodes.InputValue, i, result.Strings)
			}
			name = c.redactAndBound(name, attributes, false)
			value = c.redactAndBound(value, attributes, true)
			node := Node{BackendNodeID: backend, FrameID: frameID, Role: role, Name: name, Value: value, Tag: tag, Attributes: redactAttributes(attributes, c.config.SensitiveAttributes), Depth: depths[i], Order: len(out), Disabled: disabled || hasAttribute(attributes, "disabled"), Focused: focused, Interactive: isInteractive(role, tag, attributes), Hit: HitUnknown}
			if parent >= 0 && parent < len(document.Nodes.BackendNodeID) {
				node.ParentBackendNodeID = document.Nodes.BackendNodeID[parent]
			}
			if item, ok := layout[i]; ok {
				box := item.box
				node.Box = &box
				node.Visible = visible(item.box, item.styles)
			}
			node.Interactable = node.Interactive && node.Visible && !node.Disabled
			node.ShadowPath = shadowPath(document.Nodes, parent, result.Strings)
			out = append(out, node)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].FrameID == out[j].FrameID {
			return out[i].Order < out[j].Order
		}
		return out[i].FrameID < out[j].FrameID
	})
	return out, root, reasons
}

func (c *Collector) Resolve(ctx context.Context, ref string) (Resolution, error) {
	ref = normalizeRef(ref)
	c.mu.Lock()
	rememberedNode, ok := c.refs[ref]
	c.mu.Unlock()
	if !ok {
		return Resolution{Status: ResolutionStale, Reason: "unknown stable reference"}, nil
	}
	snapshot, err := c.Capture(ctx, ModeFull, "")
	if err != nil {
		return Resolution{}, err
	}
	for i := range snapshot.Nodes {
		n := snapshot.Nodes[i]
		if n.BackendNodeID == rememberedNode.node.BackendNodeID && n.FrameID == rememberedNode.node.FrameID && snapshot.Epoch == rememberedNode.epoch {
			return Resolution{Status: ResolutionExact, Node: &n, Confidence: 1}, nil
		}
	}
	candidates := make([]struct {
		node  Node
		score float64
	}, 0)
	crossFrameMatch := false
	for _, n := range snapshot.Nodes {
		if n.FrameID != rememberedNode.node.FrameID {
			candidate := n
			candidate.FrameID = rememberedNode.node.FrameID
			if similarity(rememberedNode.node, candidate) >= c.config.ReResolveThreshold {
				crossFrameMatch = true
			}
			continue
		}
		score := similarity(rememberedNode.node, n)
		if score >= c.config.ReResolveThreshold {
			candidates = append(candidates, struct {
				node  Node
				score float64
			}{n, score})
		}
	}
	if len(candidates) == 0 {
		if crossFrameMatch {
			return Resolution{Status: ResolutionCrossFrame, Reason: "semantic match moved to another frame"}, nil
		}
		return Resolution{Status: ResolutionDetached, Reason: "backend node detached and no safe semantic match"}, nil
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].score > candidates[j].score })
	if len(candidates) > 1 && math.Abs(candidates[0].score-candidates[1].score) < 0.05 {
		return Resolution{Status: ResolutionAmbiguous, Reason: "multiple semantic matches have equivalent confidence"}, nil
	}
	n := candidates[0].node
	return Resolution{Status: ResolutionReResolved, Node: &n, Confidence: candidates[0].score, Reason: "document or backend node changed"}, nil
}

func DiffSnapshots(before, after Snapshot) Diff {
	b := make(map[string]Node, len(before.Nodes))
	a := make(map[string]Node, len(after.Nodes))
	for _, n := range before.Nodes {
		b[nodeKey(n)] = n
	}
	for _, n := range after.Nodes {
		a[nodeKey(n)] = n
	}
	d := Diff{Added: []Node{}, Changed: []Node{}, Removed: []Node{}}
	for k, n := range a {
		old, ok := b[k]
		if !ok {
			d.Added = append(d.Added, n)
		} else if changed(old, n) {
			d.Changed = append(d.Changed, n)
		}
	}
	for k, n := range b {
		if _, ok := a[k]; !ok {
			d.Removed = append(d.Removed, n)
		}
	}
	sortNodes(d.Added)
	sortNodes(d.Changed)
	sortNodes(d.Removed)
	return d
}

func (c *Collector) boundJSON(s Snapshot) Snapshot {
	for {
		raw, _ := json.Marshal(s)
		if len(raw) <= c.config.MaxSnapshotBytes || len(s.Nodes) == 0 {
			return s
		}
		s.Nodes = s.Nodes[:len(s.Nodes)-1]
		s.Truncated = true
		if !contains(s.TruncationReasons, "max_snapshot_bytes") {
			s.TruncationReasons = append(s.TruncationReasons, "max_snapshot_bytes")
		}
	}
}

func flattenFrames(root frameTree) []string {
	out := []string{}
	var walk func(frameTree)
	walk = func(f frameTree) {
		if f.Frame.ID != "" {
			out = append(out, f.Frame.ID)
		}
		for _, child := range f.ChildFrames {
			walk(child)
		}
	}
	walk(root)
	return out
}

func collectShadowPaths(node domNode, path []string, result map[int64][]string) {
	if node.ShadowRootType != "" {
		path = append(append([]string(nil), path...), node.ShadowRootType)
	}
	if len(path) > 0 && node.BackendNodeID != 0 {
		result[node.BackendNodeID] = append([]string(nil), path...)
	}
	for _, child := range node.Children {
		collectShadowPaths(child, path, result)
	}
	for _, shadow := range node.ShadowRoots {
		collectShadowPaths(shadow, path, result)
	}
}
func attributesAt(all [][]stringIndex, index int, values []string) map[string]string {
	out := map[string]string{}
	if index >= len(all) {
		return out
	}
	pairs := all[index]
	for i := 0; i+1 < len(pairs); i += 2 {
		out[strings.ToLower(stringAt(values, pairs[i]))] = stringAt(values, pairs[i+1])
	}
	return out
}
func axFields(n axNode) (string, string, string, bool, bool) {
	role := ""
	if n.Role.Value != nil {
		role = fmt.Sprint(n.Role.Value)
	}
	name := ""
	if n.Name.Value != nil {
		name = fmt.Sprint(n.Name.Value)
	}
	value := ""
	if n.Value.Value != nil {
		value = fmt.Sprint(n.Value.Value)
	}
	disabled, focused := false, false
	for _, p := range n.Properties {
		v, ok := p.Value.Value.(bool)
		if !ok {
			continue
		}
		if p.Name == "disabled" {
			disabled = v
		}
		if p.Name == "focused" {
			focused = v
		}
	}
	return role, name, value, disabled, focused
}
func inferredRole(tag string, a map[string]string) string {
	if a["role"] != "" {
		return a["role"]
	}
	switch tag {
	case "button":
		return "button"
	case "a":
		if a["href"] != "" {
			return "link"
		}
	case "input":
		switch a["type"] {
		case "checkbox":
			return "checkbox"
		case "radio":
			return "radio"
		default:
			return "textbox"
		}
	case "select":
		return "combobox"
	case "textarea":
		return "textbox"
	}
	return "generic"
}
func isInteractive(role, tag string, a map[string]string) bool {
	switch role {
	case "button", "link", "textbox", "combobox", "checkbox", "radio", "option", "menuitem", "tab", "switch", "slider":
		return true
	}
	return tag == "button" || tag == "select" || tag == "textarea" || tag == "input" || a["contenteditable"] == "true"
}
func visible(box Rect, styles []string) bool {
	if box.Width <= 0 || box.Height <= 0 {
		return false
	}
	for _, s := range styles {
		v := strings.ToLower(strings.TrimSpace(s))
		if v == "none" || v == "hidden" || v == "collapse" || v == "0" || v == "0.0" {
			return false
		}
	}
	return true
}
func shadowPath(nodes nodeTreeSnapshot, parent int, values []string) []string {
	path := []string{}
	for parent >= 0 && parent < len(nodes.ParentIndex) {
		if kind := rareStringAt(nodes.ShadowRootType, parent, values); kind != "" {
			path = append([]string{kind}, path...)
		}
		parent = nodes.ParentIndex[parent]
	}
	return path
}
func (c *Collector) redactAndBound(value string, a map[string]string, isValue bool) string {
	if isValue && (strings.EqualFold(a["type"], "password") || sensitive(a, c.config.SensitiveAttributes)) {
		return "[REDACTED]"
	}
	if len(value) > c.config.MaxTextBytes {
		return value[:c.config.MaxTextBytes]
	}
	return value
}
func sensitive(a map[string]string, names []string) bool {
	for k, v := range a {
		probe := strings.ToLower(k + " " + v)
		for _, name := range names {
			if strings.Contains(probe, strings.ToLower(name)) {
				return true
			}
		}
	}
	return false
}
func redactAttributes(a map[string]string, names []string) map[string]string {
	out := make(map[string]string, len(a))
	redactValue := strings.EqualFold(a["type"], "password") || sensitive(a, names)
	for k, v := range a {
		redact := strings.EqualFold(k, "value") && redactValue
		for _, name := range names {
			if strings.Contains(strings.ToLower(k), strings.ToLower(name)) {
				redact = true
				break
			}
		}
		if redact {
			out[k] = "[REDACTED]"
		} else {
			out[k] = v
		}
	}
	return out
}
func hasAttribute(a map[string]string, k string) bool { _, ok := a[k]; return ok }
func filterInteractive(nodes []Node) []Node {
	out := make([]Node, 0)
	for _, n := range nodes {
		if n.Interactive {
			out = append(out, n)
		}
	}
	return out
}
func filterSubtree(nodes []Node, root int64) []Node {
	included := map[int64]bool{root: true}
	out := []Node{}
	for _, n := range nodes {
		if included[n.BackendNodeID] || included[n.ParentBackendNodeID] {
			included[n.BackendNodeID] = true
			out = append(out, n)
		}
	}
	return out
}
func normalizeRef(ref string) string {
	ref = strings.TrimSpace(ref)
	ref = strings.TrimPrefix(ref, "@")
	ref = strings.TrimPrefix(ref, "ref=")
	return ref
}
func similarity(a, b Node) float64 {
	score := 0.0
	if a.Role == b.Role {
		score += .35
	}
	if a.Name == b.Name && a.Name != "" {
		score += .35
	}
	if a.Tag == b.Tag {
		score += .1
	}
	if a.FrameID == b.FrameID {
		score += .1
	}
	if rectNear(a.Box, b.Box) {
		score += .1
	}
	return score
}
func rectNear(a, b *Rect) bool {
	if a == nil || b == nil {
		return false
	}
	return math.Abs(a.X-b.X) <= 20 && math.Abs(a.Y-b.Y) <= 20 && math.Abs(a.Width-b.Width) <= 20 && math.Abs(a.Height-b.Height) <= 20
}
func nodeKey(n Node) string {
	if n.Ref != "" {
		return n.Ref
	}
	return n.FrameID + ":" + strconv.FormatInt(n.BackendNodeID, 10)
}
func changed(a, b Node) bool {
	return a.Name != b.Name || a.Value != b.Value || a.Visible != b.Visible || a.Disabled != b.Disabled || a.Focused != b.Focused || !rectEqual(a.Box, b.Box)
}
func rectEqual(a, b *Rect) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
func sortNodes(nodes []Node) {
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].FrameID == nodes[j].FrameID {
			return nodes[i].Order < nodes[j].Order
		}
		return nodes[i].FrameID < nodes[j].FrameID
	})
}
func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func isDescendantOf(candidate, ancestor int64, parents map[int64]int64) bool {
	seen := make(map[int64]struct{}, 8)
	for candidate != 0 {
		if candidate == ancestor {
			return true
		}
		if _, exists := seen[candidate]; exists {
			return false
		}
		seen[candidate] = struct{}{}
		candidate = parents[candidate]
	}
	return false
}
