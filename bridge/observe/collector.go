package observe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
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
	ref          string
	node         Node
	epoch        uint64
	bytes        int
	older, newer *remembered
}

type Collector struct {
	caller         Caller
	config         Config
	mu             sync.Mutex
	epoch          uint64
	root           int64
	frame          string
	nextRef        uint64
	refs           map[string]*remembered
	identities     map[identity]string
	last           Snapshot
	oldest, newest *remembered
	referenceBytes int
	queryLease     chan struct{}
	queryBaseline  *queryBaseline
}

func NewCollector(caller Caller, config Config) (*Collector, error) {
	if caller == nil {
		return nil, errors.New("observation: CDP caller required")
	}
	defaults := DefaultConfig()
	var err error
	config, err = retentionConfig(config, defaults)
	if err != nil {
		return nil, err
	}
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
	return &Collector{caller: caller, config: config, refs: make(map[string]*remembered), identities: make(map[identity]string), queryLease: make(chan struct{}, 1)}, nil
}

func callCDP[R any, P any](ctx context.Context, caller Caller, method string, params P) (R, error) {
	var result R
	if err := caller.Call(ctx, method, params, &result); err != nil {
		return result, err
	}
	return result, nil
}

func callCDPFrame[R any, P any](ctx context.Context, caller FrameCaller, frameID, method string, params P) (R, error) {
	var result R
	if err := caller.CallFrame(ctx, frameID, method, params, &result); err != nil {
		return result, err
	}
	return result, nil
}

func (c *Collector) Capture(ctx context.Context, mode Mode, subtreeRef string) (Snapshot, error) {
	return c.captureSelected(ctx, mode, subtreeRef, nil)
}

func (c *Collector) captureSelected(ctx context.Context, mode Mode, subtreeRef string, scope *nodeScope) (Snapshot, error) {
	if ctx == nil {
		return Snapshot{}, errors.New("observation: context required")
	}
	if mode == "" {
		mode = ModeFull
	}
	if mode != ModeFull && mode != ModeInteractive && mode != ModeSubtree && mode != ModeEvidence {
		return Snapshot{}, fmt.Errorf("observation: unsupported mode %q", mode)
	}
	if _, err := callCDP[emptyResult](ctx, c.caller, "Accessibility.enable", emptyParams{}); err != nil {
		return Snapshot{}, fmt.Errorf("enable accessibility: %w", err)
	}
	if _, err := callCDP[emptyResult](ctx, c.caller, "DOM.enable", emptyParams{}); err != nil {
		return Snapshot{}, fmt.Errorf("enable DOM: %w", err)
	}
	params := captureSnapshotParams{ComputedStyles: []string{"display", "visibility", "opacity", "pointer-events"}, IncludeDOMRects: true, IncludePaintOrder: true}
	dom, err := callCDP[domSnapshotResult](ctx, c.caller, "DOMSnapshot.captureSnapshot", params)
	if err != nil {
		return Snapshot{}, fmt.Errorf("capture DOM snapshot: %w", err)
	}
	if len(dom.Documents) == 0 {
		return Snapshot{}, errors.New("capture DOM snapshot: no documents")
	}
	frames, err := callCDP[frameTreeResult](ctx, c.caller, "Page.getFrameTree", emptyParams{})
	if err != nil {
		return Snapshot{}, fmt.Errorf("get frame tree: %w", err)
	}
	frameIDs := flattenFrames(frames.FrameTree)
	if scope != nil {
		for _, frameID := range frameIDs {
			scope.frames[frameID] = true
		}
		if router, ok := c.caller.(FrameCaller); ok {
			for frameID := range router.FrameSessions() {
				scope.frames[frameID] = true
			}
		}
	}
	document, err := callCDP[documentResult](ctx, c.caller, "DOM.getDocument", getDocumentParams{Depth: -1, Pierce: true})
	if err != nil {
		return Snapshot{}, fmt.Errorf("get pierced DOM document: %w", err)
	}
	shadowPaths := make(map[int64][]string)
	collectShadowPaths(document.Root, nil, shadowPaths)
	axByBackend := make(map[int64]axNode)
	admitted := make(map[string]bool)
	warnings := make([]string, 0)
	frameIncomplete := false
	for _, frameID := range frameIDs {
		if router, ok := c.caller.(FrameCaller); ok {
			if _, attached := router.FrameSessions()[frameID]; attached {
				continue
			}
		}
		tree, err := callCDP[axTreeResult](ctx, c.caller, "Accessibility.getFullAXTree", getAXTreeParams{FrameID: frameID})
		if err == nil && len(tree.Nodes) == 0 {
			err = errors.New("no nodes")
		}
		if err != nil {
			if frameID == frames.FrameTree.Frame.ID {
				return Snapshot{}, fmt.Errorf("get main-frame AX tree: %w", err)
			}
			warnings = append(warnings, "frame "+frameID+" Accessibility.getFullAXTree: "+err.Error())
			frameIncomplete = true
			continue
		}
		admitted[frameID] = true
		for _, ax := range tree.Nodes {
			if !ax.Ignored && ax.BackendNodeID != 0 {
				axByBackend[ax.BackendNodeID] = ax
			}
		}
	}
	dom = admittedDocuments(dom, admitted)
	parents := make(map[string]map[int64]int64)
	addSnapshotParents(dom, parents)
	if scope != nil {
		scope.parents = parents
	}
	nodes, root, reasons := c.buildNodesSelected(dom, axByBackend, scope)
	applyShadowPaths(nodes, shadowPaths)
	if router, ok := c.caller.(FrameCaller); ok {
		frameSessions := router.FrameSessions()
		attachedFrameIDs := make([]string, 0, len(frameSessions))
		for frameID := range frameSessions {
			attachedFrameIDs = append(attachedFrameIDs, frameID)
		}
		sort.Strings(attachedFrameIDs)
		for _, frameID := range attachedFrameIDs {
			captured, err := captureFrame(ctx, router, frameID, params)
			if err != nil {
				warnings = append(warnings, "OOPIF observation unavailable: "+err.Error())
				frameIncomplete = true
				continue
			}
			addSnapshotParents(captured.dom, parents)
			frameNodes, _, frameReasons := c.buildNodesSelected(captured.dom, captured.ax, scope)
			frameShadowPaths := make(map[int64][]string)
			collectShadowPaths(captured.document, nil, frameShadowPaths)
			applyShadowPaths(frameNodes, frameShadowPaths)
			nodes = append(nodes, frameNodes...)
			for _, reason := range frameReasons {
				if !contains(reasons, reason) {
					reasons = append(reasons, reason)
				}
			}
		}
	}
	if document.Root.BackendNodeID != 0 {
		root = document.Root.BackendNodeID
	}
	if frameIncomplete {
		reasons = append(reasons, "frame_observation")
	}
	if scope != nil {
		scope.prioritizeTarget(nodes)
	}
	if scope != nil && len(nodes) > scope.options.MaxNodes {
		nodes = nodes[:scope.options.MaxNodes]
		if !contains(reasons, "max_nodes") {
			reasons = append(reasons, "max_nodes")
		}
	}
	warnings = append(warnings, c.applyHitTestsWithParents(ctx, nodes, frames.FrameTree, parents)...)
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	mainFrame := frames.FrameTree.Frame.ID
	if c.epoch == 0 || c.root != root || c.frame != mainFrame {
		if c.epoch == math.MaxUint64 {
			return Snapshot{}, errors.New("observation: navigation epoch exhausted")
		}
		c.epoch++
		c.root = root
		c.frame = mainFrame
	}
	c.pruneReferences()
	if scope != nil && (scope.root != nil && (scope.root.epoch != c.epoch || !scope.foundRoot) || scope.options.FrameID != "" && !scope.frames[scope.options.FrameID]) {
		return Snapshot{}, fmt.Errorf("query: subtree epoch or frame is no longer observed; frame failures: %v", warnings)
	}
	if scope != nil && scope.root != nil && scope.target != nil && scope.target.epoch == c.epoch {
		target := scope.target.node
		if _, observed := parents[target.FrameID][target.BackendNodeID]; observed && (target.FrameID != scope.root.node.FrameID || !isDescendantOf(target.BackendNodeID, scope.root.node.BackendNodeID, parents[target.FrameID])) {
			return Snapshot{}, errors.New("query: target is outside requested subtree")
		}
	}
	for i := range nodes {
		if !nodes[i].Interactive && nodes[i].Name == "" {
			continue
		}
		if err := c.rememberNode(&nodes[i]); err != nil {
			return Snapshot{}, err
		}
	}
	for i := range nodes {
		if nodes[i].Ref == "" && (nodes[i].Interactive || nodes[i].Name != "") || nodes[i].Ref != "" && c.refs[nodes[i].Ref] == nil {
			nodes[i].Ref = ""
			if !contains(reasons, "reference_retention") {
				reasons = append(reasons, "reference_retention")
			}
		}
	}
	if mode == ModeInteractive || mode == ModeEvidence {
		nodes = filterInteractive(nodes)
	}
	if mode == ModeSubtree {
		rememberedNode, ok := c.refs[normalizeRef(subtreeRef)]
		if !ok {
			return Snapshot{}, fmt.Errorf("observation: subtree ref %q not found", subtreeRef)
		}
		nodes = filterSubtree(nodes, rememberedNode.node.BackendNodeID, rememberedNode.node.FrameID)
	}
	snapshot := Snapshot{Schema: Schema, Epoch: c.epoch, CapturedAt: time.Now().UTC(), Mode: mode, RootBackendNodeID: root, Nodes: nodes, TotalNodes: len(nodes), Truncated: len(reasons) > 0, TruncationReasons: reasons, Warnings: warnings}
	if scope != nil {
		snapshot.Mode, snapshot.TotalNodes = scope.options.Mode, scope.matched
	}
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

func (c *Collector) applyHitTests(ctx context.Context, nodes []Node, tree frameTree) []string {
	parents := make(map[string]map[int64]int64)
	for _, node := range nodes {
		if parents[node.FrameID] == nil {
			parents[node.FrameID] = make(map[int64]int64)
		}
		parents[node.FrameID][node.BackendNodeID] = node.ParentBackendNodeID
	}
	return c.applyHitTestsWithParents(ctx, nodes, tree, parents)
}

func (c *Collector) applyHitTestsWithParents(ctx context.Context, nodes []Node, tree frameTree, parents map[string]map[int64]int64) []string {
	tested := 0
	warnings := make([]string, 0)
	geometry := newPointerGeometry(c.caller, tree)
	warnings = append(warnings, geometry.loadFrameTrees(ctx)...)
	for i := range nodes {
		node := &nodes[i]
		node.Interactable = false
		if !node.Interactive || !node.Visible || node.Disabled || node.Box == nil {
			continue
		}
		if tested >= c.config.MaxHitTests {
			if !contains(warnings, "hit-test budget exhausted") {
				warnings = append(warnings, "hit-test budget exhausted")
			}
			continue
		}
		tested++
		hit, _, _, err := geometry.hit(ctx, *node, parents[node.FrameID])
		if err != nil {
			warnings = append(warnings, "hit test unavailable for frame "+node.FrameID+" backend node "+strconv.FormatInt(node.BackendNodeID, 10)+": "+err.Error())
			continue
		}
		node.Hit = hit
		node.Interactable = ClassifyActionability(node) == ActionabilityReady
	}
	return warnings
}

func (c *Collector) buildNodesSelected(result domSnapshotResult, axNodes map[int64]axNode, scope *nodeScope) ([]Node, int64, []string) {
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
			if depths[i] > c.config.MaxDepth && scope == nil {
				if !contains(reasons, "max_depth") {
					reasons = append(reasons, "max_depth")
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
			node := Node{BackendNodeID: backend, FrameID: frameID, Role: role, Name: name, Value: value, Tag: tag, Attributes: redactAttributes(attributes, c.config.SensitiveAttributes), Depth: depths[i], Order: i, Disabled: disabled || hasAttribute(attributes, "disabled"), Focused: focused, Interactive: isInteractive(role, tag, attributes), Hit: HitUnknown}
			if parent >= 0 && parent < len(document.Nodes.BackendNodeID) {
				node.ParentBackendNodeID = document.Nodes.BackendNodeID[parent]
			}
			if item, ok := layout[i]; ok {
				box := item.box
				node.Box = &box
				node.Visible = visible(item.box, item.styles)
			}
			node.Interactable = false
			node.ShadowPath = shadowPath(document.Nodes, parent, result.Strings)
			limit := c.config.MaxNodes
			if scope != nil {
				if !scope.matches(node, c.config.ReResolveThreshold) {
					continue
				}
				scope.matched++
				limit = scope.options.MaxNodes
				if depths[i] > c.config.MaxDepth {
					if !contains(reasons, "max_depth") {
						reasons = append(reasons, "max_depth")
					}
					continue
				}
			}
			if len(out) >= limit {
				if scope != nil && scope.isTarget(node) {
					out[len(out)-1] = node
				}
				if !contains(reasons, "max_nodes") {
					reasons = append(reasons, "max_nodes")
				}
				continue
			}
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
	entry, ok := c.refs[ref]
	var rememberedNode remembered
	if ok {
		rememberedNode = *entry
	}
	c.mu.Unlock()
	if !ok {
		return Resolution{Status: ResolutionStale, Reason: "unknown stable reference"}, nil
	}
	scope := &nodeScope{options: QueryOptions{Mode: ModeFull, MaxNodes: c.config.MaxNodes}, target: &scopeAnchor{node: rememberedNode.node, epoch: rememberedNode.epoch}, parents: make(map[string]map[int64]int64), frames: make(map[string]bool), semanticOnly: true}
	snapshot, err := c.captureSelected(ctx, ModeFull, "", scope)
	if err != nil {
		return Resolution{}, err
	}
	c.mu.Lock()
	_, retained := c.refs[ref]
	c.mu.Unlock()
	if !retained {
		return Resolution{Status: ResolutionStale, Reason: "stable reference expired or evicted during observation"}, nil
	}
	for i := range snapshot.Nodes {
		n := snapshot.Nodes[i]
		if n.BackendNodeID == rememberedNode.node.BackendNodeID && n.FrameID == rememberedNode.node.FrameID && snapshot.Epoch == rememberedNode.epoch {
			return Resolution{Status: ResolutionExact, Node: &n, Confidence: 1}, nil
		}
	}
	if ClassifyMissingActionability(snapshot) == ActionabilityUnavailable {
		return Resolution{Status: ResolutionUnavailable, Reason: "fresh target observation is incomplete"}, nil
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
	if sameSnapshotNodes(before.Nodes, after.Nodes) {
		return Diff{Added: []Node{}, Changed: []Node{}, Removed: []Node{}}
	}
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
	raw, err := json.Marshal(s)
	if err != nil || len(raw) <= c.config.MaxSnapshotBytes || len(s.Nodes) == 0 {
		return s
	}
	// Estimate bytes per node from the first marshal and cut once to the
	// estimated fit instead of re-marshalling per dropped node (O(n^2) ->
	// amortized O(n)). A short tail loop corrects the estimate skew.
	s.Truncated = true
	if !contains(s.TruncationReasons, "max_snapshot_bytes") {
		s.TruncationReasons = append(s.TruncationReasons, "max_snapshot_bytes")
	}
	avg := len(raw) / len(s.Nodes)
	if avg < 1 {
		avg = 1
	}
	fit := c.config.MaxSnapshotBytes / avg
	if fit < len(s.Nodes) {
		s.Nodes = s.Nodes[:fit]
	}
	for len(s.Nodes) > 0 {
		raw, err = json.Marshal(s)
		if err == nil && len(raw) <= c.config.MaxSnapshotBytes {
			break
		}
		if err != nil {
			break
		}
		s.Nodes = s.Nodes[:len(s.Nodes)-1]
	}
	return s
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
	role := n.Role.String()
	name := n.Name.String()
	value := n.Value.String()
	disabled, focused := false, false
	for _, p := range n.Properties {
		v := p.Value.Bool()
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
	return tag == "button" || tag == "select" || tag == "textarea" || tag == "input" || a["contenteditable"] == "true" || a["draggable"] == "true"
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
		return boundedUTF8(value, c.config.MaxTextBytes)
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
func filterSubtree(nodes []Node, root int64, frameID string) []Node {
	included := map[int64]bool{root: true}
	out := []Node{}
	for _, n := range nodes {
		if n.FrameID != frameID {
			continue
		}
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
	return a.BackendNodeID != b.BackendNodeID || a.FrameID != b.FrameID || a.ParentBackendNodeID != b.ParentBackendNodeID || a.Role != b.Role || a.Name != b.Name || a.Value != b.Value || a.Tag != b.Tag || a.Depth != b.Depth || a.Order != b.Order || a.Visible != b.Visible || a.Disabled != b.Disabled || a.Focused != b.Focused || a.Interactive != b.Interactive || a.Interactable != b.Interactable || a.Hit != b.Hit || !maps.Equal(a.Attributes, b.Attributes) || !slices.Equal(a.ShadowPath, b.ShadowPath) || !rectEqual(a.Box, b.Box)
}

func sameSnapshotNodes(a, b []Node) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Ref != b[i].Ref || changed(a[i], b[i]) {
			return false
		}
	}
	return true
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
			if nodes[i].Order != nodes[j].Order {
				return nodes[i].Order < nodes[j].Order
			}
			if nodes[i].BackendNodeID != nodes[j].BackendNodeID {
				return nodes[i].BackendNodeID < nodes[j].BackendNodeID
			}
			return nodes[i].Ref < nodes[j].Ref
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
