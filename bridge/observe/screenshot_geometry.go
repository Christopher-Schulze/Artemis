package observe

import (
	"context"
	"fmt"
	"math"
)

type screenshotVisualViewport struct {
	PageX float64 `json:"pageX"`
	PageY float64 `json:"pageY"`
}

type screenshotLayoutMetrics struct {
	Viewport *screenshotVisualViewport `json:"cssVisualViewport"`
	Content  *Rect                     `json:"cssContentSize"`
}

// ScreenshotRect returns the current border bounds in root document coordinates.
// It reuses owned frame translations and clips attached-frame viewport overflow.
// It never scrolls, clicks, or substitutes another backend node.
func (c *Collector) ScreenshotRect(ctx context.Context, node Node) (Rect, error) {
	if c == nil || c.caller == nil || ctx == nil || node.BackendNodeID <= 0 || node.FrameID == "" {
		return Rect{}, fmt.Errorf("screenshot geometry requires context and owned node identity")
	}
	if err := ctx.Err(); err != nil {
		return Rect{}, err
	}
	tree, err := callCDP[frameTreeResult](ctx, c.caller, "Page.getFrameTree", emptyParams{})
	if err != nil {
		return Rect{}, err
	}
	geometry := newPointerGeometry(c.caller, tree.FrameTree)
	geometry.loadFrameTrees(ctx)
	root, err := geometry.frameRoot(node.FrameID)
	if err != nil {
		return Rect{}, err
	}
	rect, err := geometry.screenshotNodeRect(ctx, root, node)
	if err != nil {
		return Rect{}, err
	}
	var metrics screenshotLayoutMetrics
	if err := geometry.call(ctx, geometry.main, "Page.getLayoutMetrics", emptyParams{}, &metrics); err != nil {
		return Rect{}, err
	}
	if metrics.Viewport == nil || metrics.Content == nil {
		return Rect{}, fmt.Errorf("screenshot root document geometry unavailable")
	}
	if err := ctx.Err(); err != nil {
		return Rect{}, err
	}
	rect.X += metrics.Viewport.PageX
	rect.Y += metrics.Viewport.PageY
	return intersectScreenshotRects(rect, *metrics.Content)
}

func (g *pointerGeometry) screenshotNodeRect(ctx context.Context, root string, node Node) (Rect, error) {
	var described describeNodeResult
	if err := g.call(ctx, root, "DOM.describeNode", describeNodeParams{BackendNodeID: node.BackendNodeID}, &described); err != nil {
		return Rect{}, err
	}
	if described.Node.BackendNodeID != node.BackendNodeID {
		return Rect{}, fmt.Errorf("screenshot target identity changed")
	}
	var model boxModelResult
	if err := g.call(ctx, root, "DOM.getBoxModel", backendNodeParams{BackendNodeID: node.BackendNodeID}, &model); err != nil {
		return Rect{}, err
	}
	rect, err := screenshotQuadBounds(model.Model.Border)
	if err != nil {
		return Rect{}, err
	}
	origin, err := g.origin(ctx, root, make(map[string]bool))
	if err != nil {
		return Rect{}, err
	}
	rect.X, rect.Y = rect.X+origin.x, rect.Y+origin.y
	return g.clipScreenshotFrames(ctx, root, rect)
}

func (g *pointerGeometry) clipScreenshotFrames(ctx context.Context, root string, rect Rect) (Rect, error) {
	for depth := 0; root != g.main; depth++ {
		if depth >= 64 {
			return Rect{}, fmt.Errorf("screenshot frame hierarchy exceeds bound")
		}
		origin, err := g.origin(ctx, root, make(map[string]bool))
		if err != nil {
			return Rect{}, err
		}
		var metrics layoutMetricsResult
		if err := g.call(ctx, root, "Page.getLayoutMetrics", emptyParams{}, &metrics); err != nil {
			return Rect{}, err
		}
		if metrics.Viewport == nil {
			return Rect{}, fmt.Errorf("screenshot frame viewport unavailable")
		}
		rect, err = intersectScreenshotRects(rect, Rect{X: origin.x, Y: origin.y, Width: float64(metrics.Viewport.Width), Height: float64(metrics.Viewport.Height)})
		if err != nil {
			return Rect{}, err
		}
		root = origin.parent
	}
	return rect, nil
}

func screenshotQuadBounds(quad []float64) (Rect, error) {
	if len(quad) != 8 {
		return Rect{}, fmt.Errorf("screenshot requires a complete border quad")
	}
	if _, _, err := quadCenter([][]float64{quad}); err != nil {
		return Rect{}, fmt.Errorf("screenshot border area: %w", err)
	}
	left, top, right, bottom := quad[0], quad[1], quad[0], quad[1]
	for index := 0; index < 8; index += 2 {
		x, y := quad[index], quad[index+1]
		if math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) {
			return Rect{}, fmt.Errorf("screenshot geometry is nonfinite")
		}
		left, top, right, bottom = min(left, x), min(top, y), max(right, x), max(bottom, y)
	}
	rect := Rect{X: left, Y: top, Width: right - left, Height: bottom - top}
	if !validScreenshotRect(rect) {
		return Rect{}, fmt.Errorf("screenshot border is empty or invalid")
	}
	return rect, nil
}

func intersectScreenshotRects(rect, bounds Rect) (Rect, error) {
	if !validScreenshotRect(rect) || !validScreenshotRect(bounds) {
		return Rect{}, fmt.Errorf("screenshot clipping requires finite positive geometry")
	}
	left, top := max(rect.X, bounds.X), max(rect.Y, bounds.Y)
	right, bottom := min(rect.X+rect.Width, bounds.X+bounds.Width), min(rect.Y+rect.Height, bounds.Y+bounds.Height)
	result := Rect{X: left, Y: top, Width: right - left, Height: bottom - top}
	if !validScreenshotRect(result) {
		return Rect{}, fmt.Errorf("screenshot element is outside its rendered document")
	}
	return result, nil
}

func validScreenshotRect(rect Rect) bool {
	for _, value := range []float64{rect.X, rect.Y, rect.Width, rect.Height, rect.X + rect.Width, rect.Y + rect.Height} {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return false
		}
	}
	return rect.Width > 0 && rect.Height > 0
}
