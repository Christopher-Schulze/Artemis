package observe

import (
	"context"
	"fmt"
	"math"
	"sort"
)

type frameOrigin struct {
	x, y   float64
	parent string
	owner  int64
}

type pointerGeometry struct {
	caller    Caller
	router    FrameCaller
	main      string
	parents   map[string]string
	sessions  map[string]string
	origins   map[string]frameOrigin
	blocked   map[string]error
	viewports map[string]layoutViewport
}

func newPointerGeometry(caller Caller, tree frameTree) *pointerGeometry {
	g := &pointerGeometry{caller: caller, main: tree.Frame.ID, parents: make(map[string]string), origins: make(map[string]frameOrigin), blocked: make(map[string]error), viewports: make(map[string]layoutViewport)}
	g.addFrameTree(tree, tree.Frame.ParentID)
	if router, ok := caller.(FrameCaller); ok {
		g.router, g.sessions = router, router.FrameSessions()
	}
	return g
}

func (g *pointerGeometry) loadFrameTrees(ctx context.Context) []string {
	frameIDs := make([]string, 0, len(g.sessions))
	for frameID := range g.sessions {
		frameIDs = append(frameIDs, frameID)
	}
	sort.Strings(frameIDs)
	var warnings []string
	for _, frameID := range frameIDs {
		var tree frameTreeResult
		err := g.call(ctx, frameID, "Page.getFrameTree", emptyParams{}, &tree)
		if err == nil && (tree.FrameTree.Frame.ID != frameID || tree.FrameTree.Frame.ParentID == "") {
			err = fmt.Errorf("attached frame has no verified root/parent identity")
		}
		if err != nil {
			g.blocked[frameID] = fmt.Errorf("frame %s Page.getFrameTree: %w", frameID, err)
			warnings = append(warnings, fmt.Sprintf("frame %s Page.getFrameTree: %v", frameID, err))
			continue
		}
		g.addFrameTree(tree.FrameTree, tree.FrameTree.Frame.ParentID)
	}
	return warnings
}

func (g *pointerGeometry) addFrameTree(tree frameTree, parent string) {
	g.parents[tree.Frame.ID] = parent
	for _, child := range tree.ChildFrames {
		g.addFrameTree(child, tree.Frame.ID)
	}
}

func (g *pointerGeometry) frameRoot(frameID string) (string, error) {
	seen := make(map[string]bool)
	for frameID != "" {
		if err := g.blocked[frameID]; err != nil {
			return "", err
		}
		if seen[frameID] {
			return "", fmt.Errorf("frame hierarchy cycle at %s", frameID)
		}
		seen[frameID] = true
		if frameID == g.main || g.sessions[frameID] != "" {
			return frameID, nil
		}
		parent, exists := g.parents[frameID]
		if !exists {
			return "", fmt.Errorf("unknown frame %s", frameID)
		}
		frameID = parent
	}
	return "", fmt.Errorf("frame has no owned root")
}

func (g *pointerGeometry) call(ctx context.Context, root, method string, params, result any) error {
	if root == g.main {
		return g.caller.Call(ctx, method, params, result)
	}
	if g.router == nil || g.router.FrameSessions()[root] == "" {
		return fmt.Errorf("frame %s detached before %s", root, method)
	}
	return g.router.CallFrame(ctx, root, method, params, result)
}

func (g *pointerGeometry) origin(ctx context.Context, root string, seen map[string]bool) (frameOrigin, error) {
	if root == g.main {
		return frameOrigin{}, nil
	}
	if origin, exists := g.origins[root]; exists {
		return origin, nil
	}
	if seen[root] || len(seen) >= 64 {
		return frameOrigin{}, fmt.Errorf("invalid frame hierarchy at %s", root)
	}
	seen[root] = true
	parent, err := g.frameRoot(g.parents[root])
	if err != nil {
		return frameOrigin{}, err
	}
	base, err := g.origin(ctx, parent, seen)
	if err != nil {
		return frameOrigin{}, err
	}
	var owner frameOwnerResult
	if err := g.call(ctx, parent, "DOM.getFrameOwner", frameOwnerParams{FrameID: root}, &owner); err != nil {
		return frameOrigin{}, fmt.Errorf("frame %s DOM.getFrameOwner: %w", root, err)
	}
	if owner.BackendNodeID <= 0 {
		return frameOrigin{}, fmt.Errorf("frame %s has no owner node", root)
	}
	var model boxModelResult
	if err := g.call(ctx, parent, "DOM.getBoxModel", backendNodeParams(owner), &model); err != nil {
		return frameOrigin{}, fmt.Errorf("frame %s DOM.getBoxModel: %w", root, err)
	}
	x, y, err := frameTranslation(model)
	if err != nil {
		return frameOrigin{}, fmt.Errorf("frame %s: %w", root, err)
	}
	origin := frameOrigin{x: base.x + x, y: base.y + y, parent: parent, owner: owner.BackendNodeID}
	g.origins[root] = origin
	return origin, nil
}

func frameTranslation(result boxModelResult) (float64, float64, error) {
	b, c := result.Model.Border, result.Model.Content
	if len(b) != 8 || len(c) != 8 {
		return 0, 0, fmt.Errorf("frame geometry requires complete border/content quads")
	}
	for _, quad := range [][]float64{b, c} {
		for _, value := range quad {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return 0, 0, fmt.Errorf("non-finite frame geometry")
			}
		}
		if math.Abs(quad[0]-quad[6]) > .01 || math.Abs(quad[1]-quad[3]) > .01 || math.Abs(quad[2]-quad[4]) > .01 || math.Abs(quad[5]-quad[7]) > .01 {
			return 0, 0, fmt.Errorf("transformed frame geometry is unverified")
		}
	}
	if result.Model.Width <= 0 || result.Model.Height <= 0 || math.Abs(b[2]-b[0]-result.Model.Width) > .5 || math.Abs(b[5]-b[1]-result.Model.Height) > .5 {
		return 0, 0, fmt.Errorf("scaled or empty frame geometry is unverified")
	}
	return c[0], c[1], nil
}

func (g *pointerGeometry) point(ctx context.Context, node Node) (float64, float64, string, error) {
	root, err := g.frameRoot(node.FrameID)
	if err != nil {
		return 0, 0, "", err
	}
	var result contentQuadsResult
	if err := g.call(ctx, root, "DOM.getContentQuads", backendNodeParams{BackendNodeID: node.BackendNodeID}, &result); err != nil {
		return 0, 0, "", fmt.Errorf("frame %s DOM.getContentQuads: %w", node.FrameID, err)
	}
	x, y, err := quadCenter(result.Quads)
	if err != nil {
		return 0, 0, "", err
	}
	origin, err := g.origin(ctx, root, make(map[string]bool))
	if err != nil {
		return 0, 0, "", err
	}
	return x + origin.x, y + origin.y, root, nil
}

func quadCenter(quads [][]float64) (float64, float64, error) {
	for _, quad := range quads {
		if len(quad) != 8 {
			return 0, 0, fmt.Errorf("invalid content quad")
		}
		x, y, area := 0.0, 0.0, 0.0
		for i := 0; i < 4; i++ {
			px, py := quad[2*i], quad[2*i+1]
			if math.IsNaN(px) || math.IsNaN(py) || math.IsInf(px, 0) || math.IsInf(py, 0) {
				return 0, 0, fmt.Errorf("non-finite content quad")
			}
			j := (i + 1) % 4
			x += px
			y += py
			area += px*quad[2*j+1] - py*quad[2*j]
		}
		if math.Abs(area) > 1 {
			return x / 4, y / 4, nil
		}
	}
	return 0, 0, fmt.Errorf("node has no viewport content area")
}

func (g *pointerGeometry) hit(ctx context.Context, node Node, parents map[int64]int64) (HitState, float64, float64, error) {
	x, y, root, err := g.point(ctx, node)
	if err != nil {
		return HitUnknown, 0, 0, err
	}
	expected, expectedFrame := node.BackendNodeID, node.FrameID
	checkingOwner := false
	for {
		origin, err := g.origin(ctx, root, make(map[string]bool))
		if err != nil {
			return HitUnknown, 0, 0, err
		}
		var result getNodeForLocationResult
		location, err := g.documentPoint(ctx, root, x-origin.x, y-origin.y)
		if err != nil {
			return HitUnknown, 0, 0, err
		}
		if err := g.call(ctx, root, "DOM.getNodeForLocation", location, &result); err != nil {
			return HitUnknown, 0, 0, fmt.Errorf("frame %s DOM.getNodeForLocation: %w", root, err)
		}
		if result.FrameID == "" {
			return HitUnknown, 0, 0, fmt.Errorf("hit result has no frame identity")
		}
		targetHit := result.FrameID == node.FrameID && (result.BackendNodeID == node.BackendNodeID || isDescendantOf(result.BackendNodeID, node.BackendNodeID, parents))
		ownerHit := checkingOwner && result.BackendNodeID == expected && result.FrameID == expectedFrame
		if !targetHit && !ownerHit {
			return HitCovered, x, y, nil
		}
		if root == g.main {
			return HitClear, x, y, nil
		}
		expected, expectedFrame, root = origin.owner, g.parents[root], origin.parent
		checkingOwner = true
	}
}

func (g *pointerGeometry) documentPoint(ctx context.Context, root string, x, y float64) (getNodeForLocationParams, error) {
	viewport, exists := g.viewports[root]
	if !exists {
		var metrics layoutMetricsResult
		if err := g.call(ctx, root, "Page.getLayoutMetrics", emptyParams{}, &metrics); err != nil {
			return getNodeForLocationParams{}, fmt.Errorf("frame %s Page.getLayoutMetrics: %w", root, err)
		}
		if metrics.Viewport == nil || metrics.Viewport.Width <= 0 || metrics.Viewport.Height <= 0 {
			return getNodeForLocationParams{}, fmt.Errorf("frame %s Page.getLayoutMetrics: no CSS viewport", root)
		}
		viewport = *metrics.Viewport
		g.viewports[root] = viewport
	}
	if x < 0 || y < 0 || x >= float64(viewport.Width) || y >= float64(viewport.Height) {
		return getNodeForLocationParams{}, fmt.Errorf("frame %s pointer point outside viewport", root)
	}
	// Chromium's DOM agent hit-tests document coordinates, unlike input/quads.
	return getNodeForLocationParams{X: int(math.Round(x)) + viewport.PageX, Y: int(math.Round(y)) + viewport.PageY}, nil
}

// PointerPoint returns a freshly hit-verified top-level viewport point.
func (c *Collector) PointerPoint(ctx context.Context, node Node) (float64, float64, error) {
	if ctx == nil || node.BackendNodeID <= 0 {
		return 0, 0, fmt.Errorf("pointer point requires context and live node identity")
	}
	tree, err := callCDP[frameTreeResult](ctx, c.caller, "Page.getFrameTree", emptyParams{})
	if err != nil {
		return 0, 0, err
	}
	geometry := newPointerGeometry(c.caller, tree.FrameTree)
	geometry.loadFrameTrees(ctx)
	root, err := geometry.frameRoot(node.FrameID)
	if err != nil {
		return 0, 0, err
	}
	var described describeNodeResult
	if err := geometry.call(ctx, root, "DOM.describeNode", describeNodeParams{BackendNodeID: node.BackendNodeID, Depth: -1, Pierce: true}, &described); err != nil {
		return 0, 0, fmt.Errorf("frame %s DOM.describeNode: %w", node.FrameID, err)
	}
	if described.Node.BackendNodeID != node.BackendNodeID {
		return 0, 0, fmt.Errorf("frame %s DOM.describeNode: target identity mismatch", node.FrameID)
	}
	parents := make(map[int64]int64)
	collectNodeParents(described.Node, 0, parents)
	hit, x, y, err := geometry.hit(ctx, node, parents)
	if err != nil {
		return 0, 0, err
	}
	if hit != HitClear {
		return 0, 0, fmt.Errorf("pointer target is %s", hit)
	}
	return x, y, nil
}

func collectNodeParents(node domNode, parent int64, parents map[int64]int64) {
	parents[node.BackendNodeID] = parent
	for _, child := range node.Children {
		collectNodeParents(child, node.BackendNodeID, parents)
	}
	for _, shadow := range node.ShadowRoots {
		collectNodeParents(shadow, node.BackendNodeID, parents)
	}
}
