package observe

import (
	"context"
	"fmt"
	"math"
)

// visual_geometry.go binds pixels to page geometry for reference-bound visual
// grounding (spec L4264 Snapshot Annotations): the top-level visual viewport
// identity a screenshot was taken in, element rectangles in that viewport and
// the DOM node under a viewport point, descending into owned frames.

// ViewportState identifies the visual viewport of the main frame. Two states
// are equal only when size, scroll, zoom and main document frame agree.
type ViewportState struct {
	MainFrameID string  `json:"mainFrameId"`
	Width       float64 `json:"width"`
	Height      float64 `json:"height"`
	PageX       float64 `json:"pageX"`
	PageY       float64 `json:"pageY"`
	Scale       float64 `json:"scale"`
}

// PointHit is the deepest DOM node under a viewport point.
type PointHit struct {
	FrameID       string `json:"frameId"`
	BackendNodeID int64  `json:"backendNodeId"`
}

type visualViewportMetrics struct {
	Viewport *struct {
		PageX  float64 `json:"pageX"`
		PageY  float64 `json:"pageY"`
		Width  float64 `json:"clientWidth"`
		Height float64 `json:"clientHeight"`
		Scale  float64 `json:"scale"`
	} `json:"cssVisualViewport"`
}

// Viewport returns the current main-frame visual viewport identity.
func (c *Collector) Viewport(ctx context.Context) (ViewportState, error) {
	if c == nil || c.caller == nil || ctx == nil {
		return ViewportState{}, fmt.Errorf("viewport requires context and collector")
	}
	tree, err := callCDP[frameTreeResult](ctx, c.caller, "Page.getFrameTree", emptyParams{})
	if err != nil {
		return ViewportState{}, err
	}
	metrics, err := callCDP[visualViewportMetrics](ctx, c.caller, "Page.getLayoutMetrics", emptyParams{})
	if err != nil {
		return ViewportState{}, err
	}
	if metrics.Viewport == nil || metrics.Viewport.Width <= 0 || metrics.Viewport.Height <= 0 {
		return ViewportState{}, fmt.Errorf("page has no CSS visual viewport")
	}
	viewport := metrics.Viewport
	return ViewportState{MainFrameID: tree.FrameTree.Frame.ID, Width: viewport.Width, Height: viewport.Height, PageX: viewport.PageX, PageY: viewport.PageY, Scale: viewport.Scale}, nil
}

// ViewportRect returns the node's border box in top-level viewport CSS
// pixels, clipped by its frames and the viewport. Off-screen nodes fail.
func (c *Collector) ViewportRect(ctx context.Context, node Node, viewport ViewportState) (Rect, error) {
	if c == nil || c.caller == nil || ctx == nil || node.BackendNodeID <= 0 || node.FrameID == "" {
		return Rect{}, fmt.Errorf("viewport geometry requires context and owned node identity")
	}
	geometry, err := c.geometry(ctx)
	if err != nil {
		return Rect{}, err
	}
	root, err := geometry.frameRoot(node.FrameID)
	if err != nil {
		return Rect{}, err
	}
	rect, err := geometry.screenshotNodeRect(ctx, root, node)
	if err != nil {
		return Rect{}, err
	}
	return intersectScreenshotRects(rect, Rect{Width: viewport.Width, Height: viewport.Height})
}

// HitTestPoint returns the node under a top-level viewport point. A hit on
// the owner of an attached out-of-process frame continues inside that frame.
func (c *Collector) HitTestPoint(ctx context.Context, x, y float64) (PointHit, error) {
	if c == nil || c.caller == nil || ctx == nil || math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) {
		return PointHit{}, fmt.Errorf("hit test requires context and a finite point")
	}
	geometry, err := c.geometry(ctx)
	if err != nil {
		return PointHit{}, err
	}
	root := geometry.main
	for depth := 0; depth < 64; depth++ {
		origin, err := geometry.origin(ctx, root, make(map[string]bool))
		if err != nil {
			return PointHit{}, err
		}
		location, err := geometry.documentPoint(ctx, root, x-origin.x, y-origin.y)
		if err != nil {
			return PointHit{}, err
		}
		var result getNodeForLocationResult
		if err := geometry.call(ctx, root, "DOM.getNodeForLocation", location, &result); err != nil {
			return PointHit{}, fmt.Errorf("frame %s DOM.getNodeForLocation: %w", root, err)
		}
		if result.FrameID == "" || result.BackendNodeID <= 0 {
			return PointHit{}, fmt.Errorf("hit result has no node identity")
		}
		child := geometry.ownedChildAt(ctx, root, result.BackendNodeID)
		if child == "" {
			return PointHit{FrameID: result.FrameID, BackendNodeID: result.BackendNodeID}, nil
		}
		root = child
	}
	return PointHit{}, fmt.Errorf("hit test frame hierarchy exceeds bound")
}

// ContainsHit reports whether hit is node itself or inside its subtree,
// including shadow roots.
func (c *Collector) ContainsHit(ctx context.Context, node Node, hit PointHit) (bool, error) {
	if hit.FrameID != node.FrameID {
		return false, nil
	}
	if hit.BackendNodeID == node.BackendNodeID {
		return true, nil
	}
	geometry, err := c.geometry(ctx)
	if err != nil {
		return false, err
	}
	root, err := geometry.frameRoot(node.FrameID)
	if err != nil {
		return false, err
	}
	var described describeNodeResult
	if err := geometry.call(ctx, root, "DOM.describeNode", describeNodeParams{BackendNodeID: node.BackendNodeID, Depth: -1, Pierce: true}, &described); err != nil {
		return false, fmt.Errorf("frame %s DOM.describeNode: %w", node.FrameID, err)
	}
	if described.Node.BackendNodeID != node.BackendNodeID {
		return false, fmt.Errorf("target identity changed")
	}
	parents := make(map[int64]int64)
	collectNodeParents(described.Node, 0, parents)
	return isDescendantOf(hit.BackendNodeID, node.BackendNodeID, parents), nil
}

func (c *Collector) geometry(ctx context.Context) (*pointerGeometry, error) {
	tree, err := callCDP[frameTreeResult](ctx, c.caller, "Page.getFrameTree", emptyParams{})
	if err != nil {
		return nil, err
	}
	geometry := newPointerGeometry(c.caller, tree.FrameTree)
	geometry.loadFrameTrees(ctx)
	return geometry, nil
}

// ownedChildAt returns the attached child frame session whose owner element
// in root is owner, or "" when the hit is an ordinary node.
func (g *pointerGeometry) ownedChildAt(ctx context.Context, root string, owner int64) string {
	for frameID := range g.sessions {
		if g.blocked[frameID] != nil || frameID == root {
			continue
		}
		parent, err := g.frameRoot(g.parents[frameID])
		if err != nil || parent != root {
			continue
		}
		origin, err := g.origin(ctx, frameID, make(map[string]bool))
		if err == nil && origin.owner == owner {
			return frameID
		}
	}
	return ""
}
