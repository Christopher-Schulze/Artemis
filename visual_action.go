package artemis

import (
	"context"
	"errors"
	"fmt"
	"image"
	"math"

	"github.com/Christopher-Schulze/Artemis/bridge/actions"
	bridgeobserve "github.com/Christopher-Schulze/Artemis/bridge/observe"
	"github.com/Christopher-Schulze/Artemis/observe"
)

// ActVisual clicks or hovers at a point of the page's current visual
// observation. Every check runs before dispatch: the document epoch,
// viewport and the pixels around the point must be those observed, and
// every annotated box containing the point must contain the hit node. A hit
// on an interactive node executes through its reference; otherwise the
// action needs AllowPointerFallback. Any dispatch consumes the observation.
func (s *Session) ActVisual(ctx context.Context, pageID string, request VisualActionRequest) (VisualActionResult, *TaskError) {
	if request.Kind != actions.KindClick && request.Kind != actions.KindHover {
		return VisualActionResult{}, newTaskError(TaskErrorInvalidInput, "visual_act", errors.New("visual actions are click or hover"))
	}
	execCtx, done, taskErr := s.chromiumOperation(ctx)
	if taskErr != nil {
		return VisualActionResult{}, taskErr
	}
	defer done()
	page, lease, taskErr := s.leaseChromiumPage(execCtx, pageID)
	if taskErr != nil {
		return VisualActionResult{}, taskErr
	}
	defer lease.Release()
	execCtx = lease.Context()
	if taskErr := s.authorizeWrite(ctx, page, true); taskErr != nil {
		return VisualActionResult{}, taskErr
	}
	binding, taskErr := s.currentVisual(pageID, request.ObservationID)
	if taskErr != nil {
		return VisualActionResult{}, taskErr
	}
	if !rectContains(bridgeobserve.Rect{Width: binding.crop.Width, Height: binding.crop.Height}, request.X, request.Y) {
		return VisualActionResult{}, newTaskError(TaskErrorInvalidInput, "visual_act", fmt.Errorf("point (%.1f, %.1f) is outside the observed image", request.X, request.Y))
	}
	point := actions.PointerTarget{X: binding.crop.X + request.X, Y: binding.crop.Y + request.Y}
	collector := page.observer.SnapshotCollector()
	snapshot, taskErr := s.verifyVisualBinding(execCtx, page, binding, point)
	if taskErr != nil {
		return VisualActionResult{}, taskErr
	}
	hit, err := collector.HitTestPoint(execCtx, point.X, point.Y)
	if err != nil {
		return VisualActionResult{}, classifyTaskError("visual_act", err)
	}
	target, taskErr := visualTarget(execCtx, collector, binding, snapshot, hit, point)
	if taskErr != nil {
		return VisualActionResult{}, taskErr
	}
	result := VisualActionResult{Point: point}
	action := actions.Request{Kind: request.Kind, Button: request.Button}
	switch {
	case target != nil:
		result.Path, result.Ref, action.Ref = VisualPathReference, pageID+":"+target.Ref, target.Ref
	case request.AllowPointerFallback:
		result.Path, action.Pointer = VisualPathPointer, &point
		if hit.FrameID != binding.viewport.MainFrameID {
			action.FrameID = hit.FrameID
		}
	default:
		return VisualActionResult{}, newTaskError(TaskErrorInvalidInput, "visual_act", errors.New("the point has no interactive DOM/AX target; set allowPointerFallback to dispatch a raw pointer event"))
	}
	s.chromiumMu.Lock()
	page.visual = nil
	s.chromiumMu.Unlock()
	result.Outcome = page.actions.ExecuteLeased(action, lease)
	if result.Outcome.Evidence.Ref != "" {
		result.Outcome.Evidence.Ref = pageID + ":" + result.Outcome.Evidence.Ref
	}
	result.SemanticChanged = result.Outcome.Evidence.BeforeHash != "" && result.Outcome.Evidence.AfterHash != "" && result.Outcome.Evidence.BeforeHash != result.Outcome.Evidence.AfterHash
	if result.Outcome.Evidence.Effect == actions.EffectDispatched {
		result.ImageChange = s.visualImageChange(execCtx, page, binding)
	}
	return result, nil
}

// verifyVisualBinding refuses a stale observation before any dispatch and
// returns the fresh snapshot.
func (s *Session) verifyVisualBinding(ctx context.Context, page *ownedChromiumPage, binding *visualBinding, point actions.PointerTarget) (bridgeobserve.Snapshot, *TaskError) {
	evidence, err := page.observer.CaptureEvidence(ctx)
	if err != nil {
		return bridgeobserve.Snapshot{}, classifyTaskError("visual_act", err)
	}
	if evidence.Snapshot.Epoch != binding.epoch {
		return bridgeobserve.Snapshot{}, newTaskError(TaskErrorStaleReference, "visual_act", errors.New("the document changed since the visual observation"))
	}
	viewport, err := page.observer.SnapshotCollector().Viewport(ctx)
	if err != nil {
		return bridgeobserve.Snapshot{}, classifyTaskError("visual_act", err)
	}
	if viewport != binding.viewport {
		return bridgeobserve.Snapshot{}, newTaskError(TaskErrorStaleReference, "visual_act", errors.New("the viewport scrolled, resized or zoomed since the visual observation"))
	}
	patch, ok := intersectRect(bridgeobserve.Rect{X: point.X - visualPatchRadius, Y: point.Y - visualPatchRadius, Width: 2 * visualPatchRadius, Height: 2 * visualPatchRadius}, binding.crop)
	if !ok {
		return bridgeobserve.Snapshot{}, newTaskError(TaskErrorInvalidInput, "visual_act", errors.New("point is outside the observed region"))
	}
	pixelRect, cssRect := alignedPatch(binding, patch)
	_, fresh, err := captureVisualPixels(ctx, page.page, viewport, cssRect)
	if err != nil {
		return bridgeobserve.Snapshot{}, classifyTaskError("visual_act", err)
	}
	observed, ok := binding.pixels.(interface {
		SubImage(image.Rectangle) image.Image
	})
	if !ok {
		return bridgeobserve.Snapshot{}, newTaskError(TaskErrorExecutionFailed, "visual_act", errors.New("observed pixels cannot be cropped"))
	}
	change := observe.CompareImages(observed.SubImage(pixelRect.Add(binding.pixels.Bounds().Min)), fresh, image.Rectangle{})
	if change.Resized || change.Ratio > visualPatchTolerance {
		return bridgeobserve.Snapshot{}, newTaskError(TaskErrorStaleReference, "visual_act", fmt.Errorf("the pixels at the point changed since the visual observation (%.0f%% of the region)", change.Ratio*100))
	}
	return evidence.Snapshot, nil
}

// alignedPatch snaps a viewport patch to whole image pixels so the fresh
// capture and the stored pixels cover exactly the same region.
func alignedPatch(binding *visualBinding, patch bridgeobserve.Rect) (image.Rectangle, bridgeobserve.Rect) {
	ratio := binding.ratio
	pixels := image.Rect(int(math.Floor((patch.X-binding.crop.X)*ratio)), int(math.Floor((patch.Y-binding.crop.Y)*ratio)), int(math.Ceil((patch.X+patch.Width-binding.crop.X)*ratio)), int(math.Ceil((patch.Y+patch.Height-binding.crop.Y)*ratio)))
	pixels = pixels.Intersect(image.Rect(0, 0, binding.pixels.Bounds().Dx(), binding.pixels.Bounds().Dy()))
	css := bridgeobserve.Rect{X: binding.crop.X + float64(pixels.Min.X)/ratio, Y: binding.crop.Y + float64(pixels.Min.Y)/ratio, Width: float64(pixels.Dx()) / ratio, Height: float64(pixels.Dy()) / ratio}
	return pixels, css
}

// visualTarget resolves the hit to its nearest interactive referenced node
// and refuses points whose annotated boxes do not contain the hit (covered)
// or that lie in unrelated overlapping boxes (ambiguous).
func visualTarget(ctx context.Context, collector *bridgeobserve.Collector, binding *visualBinding, snapshot bridgeobserve.Snapshot, hit bridgeobserve.PointHit, point actions.PointerTarget) (*bridgeobserve.Node, *TaskError) {
	target := nearestInteractive(snapshot, hit)
	var containing []bridgeobserve.Node
	for _, annotation := range binding.annotations {
		if !rectContains(annotation.box, point.X, point.Y) {
			continue
		}
		contains, err := collector.ContainsHit(ctx, annotation.node, hit)
		if err != nil {
			return nil, classifyTaskError("visual_act", err)
		}
		if !contains {
			return nil, newTaskError(TaskErrorInvalidInput, "visual_act", fmt.Errorf("the point is inside @%s but another element covers it; observe again or act on the reference", annotation.node.Ref))
		}
		containing = append(containing, annotation.node)
	}
	if target != nil {
		return target, nil
	}
	if len(containing) == 0 {
		return nil, nil
	}
	innermost := containing[0]
	for _, candidate := range containing[1:] {
		nested, err := collector.ContainsHit(ctx, innermost, bridgeobserve.PointHit{FrameID: candidate.FrameID, BackendNodeID: candidate.BackendNodeID})
		if err != nil {
			return nil, classifyTaskError("visual_act", err)
		}
		if !nested {
			return nil, newTaskError(TaskErrorInvalidInput, "visual_act", fmt.Errorf("the point matches unrelated targets @%s and @%s", innermost.Ref, candidate.Ref))
		}
		innermost = candidate
	}
	return &innermost, nil
}

// nearestInteractive walks the snapshot's parent chain from the hit node to
// the closest interactive node that carries a reference.
func nearestInteractive(snapshot bridgeobserve.Snapshot, hit bridgeobserve.PointHit) *bridgeobserve.Node {
	byBackend := make(map[int64]*bridgeobserve.Node, len(snapshot.Nodes))
	for index := range snapshot.Nodes {
		if snapshot.Nodes[index].FrameID == hit.FrameID {
			byBackend[snapshot.Nodes[index].BackendNodeID] = &snapshot.Nodes[index]
		}
	}
	for current, depth := hit.BackendNodeID, 0; current != 0 && depth < 256; depth++ {
		node := byBackend[current]
		if node == nil {
			return nil
		}
		if node.Interactive && node.Ref != "" {
			return node
		}
		current = node.ParentBackendNodeID
	}
	return nil
}

// visualImageChange compares the observed region with a fresh capture; an
// unavailable capture reports no measurement rather than failing the action.
func (s *Session) visualImageChange(ctx context.Context, page *ownedChromiumPage, binding *visualBinding) observe.ImageChange {
	viewport, err := page.observer.SnapshotCollector().Viewport(ctx)
	if err != nil {
		return observe.ImageChange{}
	}
	if viewport != binding.viewport {
		bounds := binding.pixels.Bounds()
		return observe.ImageChange{Width: bounds.Dx(), Height: bounds.Dy(), Cells: 1, ChangedCells: 1, Ratio: 1, Bounds: &bounds, Resized: true}
	}
	_, after, err := captureVisualPixels(ctx, page.page, viewport, binding.crop)
	if err != nil {
		return observe.ImageChange{}
	}
	return observe.CompareImages(binding.pixels, after, image.Rectangle{})
}
