package artemis

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"strings"
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"

	"github.com/Christopher-Schulze/Artemis/bridge"
	"github.com/Christopher-Schulze/Artemis/bridge/actions"
	bridgeobserve "github.com/Christopher-Schulze/Artemis/bridge/observe"
	"github.com/Christopher-Schulze/Artemis/observe"
)

// visual_grounding.go connects screenshots to the reference model (spec
// L4178-L4195 observation, L4264 Snapshot Annotations). A visual observation
// binds pixels, viewport, document epoch and the annotated references of one
// capture. A visual action at a point of that observation is refused when
// the document, viewport or pixels at the point changed, or when the point
// is covered or ambiguous; it executes through the element reference when
// the point hits an interactive node and only falls back to a raw pointer
// event when the caller explicitly allows it. Labels are drawn into the
// pixels in Go, never injected into the page.

const (
	defaultVisualAnnotations = 50
	maxVisualAnnotations     = 200
	// visualPatchRadius is the CSS-pixel half size of the region around a
	// point whose pixels must be unchanged before dispatch.
	visualPatchRadius = 24
	// visualPatchTolerance is the largest changed-cell ratio still treated
	// as the same pixels at the point.
	visualPatchTolerance = 0.02
)

// VisualGrounder is the injected host model capability that turns an
// instruction into a point of a visual observation.
type VisualGrounder interface {
	Ground(ctx context.Context, request VisualGroundingRequest) (VisualPoint, error)
}

// VisualGroundingRequest carries the observed pixels and their annotations.
type VisualGroundingRequest struct {
	Instruction string             `json:"instruction"`
	MIME        string             `json:"mime"`
	Image       []byte             `json:"image"`
	Width       float64            `json:"width"`
	Height      float64            `json:"height"`
	Annotations []VisualAnnotation `json:"annotations"`
}

// VisualPoint is a point in CSS pixels of a visual observation's image.
type VisualPoint struct {
	ObservationID string  `json:"observationId"`
	X             float64 `json:"x"`
	Y             float64 `json:"y"`
	Confidence    float64 `json:"confidence"`
}

// VisualCaptureOptions select annotation drawing, their bound and a crop.
type VisualCaptureOptions struct {
	// Annotate draws numbered reference labels into the returned image.
	Annotate bool `json:"annotate,omitempty"`
	// MaxAnnotations bounds the annotated references (default 50, max 200).
	MaxAnnotations int `json:"maxAnnotations,omitempty"`
	// CropRef crops the capture to this element's visible viewport box.
	CropRef string `json:"cropRef,omitempty"`
}

// VisualAnnotation names one interactive reference visible in the image.
// Box is in CSS pixels relative to the image origin.
type VisualAnnotation struct {
	Label   string             `json:"label"`
	Ref     string             `json:"ref"`
	Role    string             `json:"role"`
	Name    string             `json:"name"`
	FrameID string             `json:"frameId"`
	Box     bridgeobserve.Rect `json:"box"`
}

// VisualObservation is one bound capture: the image covers Crop of the
// viewport (the whole viewport when Crop is nil) at PixelRatio image pixels
// per CSS pixel.
type VisualObservation struct {
	ID          string                      `json:"id"`
	PageID      string                      `json:"pageId"`
	URL         string                      `json:"url"`
	Epoch       uint64                      `json:"epoch"`
	Viewport    bridgeobserve.ViewportState `json:"viewport"`
	Crop        *bridgeobserve.Rect         `json:"crop,omitempty"`
	MIME        string                      `json:"mime"`
	Image       []byte                      `json:"image"`
	PixelWidth  int                         `json:"pixelWidth"`
	PixelHeight int                         `json:"pixelHeight"`
	PixelRatio  float64                     `json:"pixelRatio"`
	Annotations []VisualAnnotation          `json:"annotations"`
	CapturedAt  time.Time                   `json:"capturedAt"`
}

// VisualActionRequest acts at a point of the page's current observation.
type VisualActionRequest struct {
	ObservationID string       `json:"observationId"`
	Kind          actions.Kind `json:"kind"`
	X             float64      `json:"x"`
	Y             float64      `json:"y"`
	Button        string       `json:"button,omitempty"`
	// AllowPointerFallback permits a raw pointer event when the point has
	// no interactive DOM/AX target (canvas, inaccessible controls).
	AllowPointerFallback bool `json:"allowPointerFallback,omitempty"`
}

// Visual action paths.
const (
	VisualPathReference = "reference"
	VisualPathPointer   = "pointer"
)

// VisualActionResult reports the executed path and both change signals: the
// semantic AX change stays authoritative, the image change is a bounded
// visual complement.
type VisualActionResult struct {
	Path            string                `json:"path"`
	Ref             string                `json:"ref,omitempty"`
	Point           actions.PointerTarget `json:"point"`
	Outcome         actions.Outcome       `json:"outcome"`
	SemanticChanged bool                  `json:"semanticChanged"`
	ImageChange     observe.ImageChange   `json:"imageChange"`
}

type visualBinding struct {
	id          string
	epoch       uint64
	viewport    bridgeobserve.ViewportState
	crop        bridgeobserve.Rect
	ratio       float64
	pixels      image.Image
	mime        string
	image       []byte
	annotations []boundAnnotation
	public      []VisualAnnotation
}

type boundAnnotation struct {
	node bridgeobserve.Node
	box  bridgeobserve.Rect
}

type visualScreenshotParams struct {
	Format                string               `json:"format"`
	FromSurface           bool                 `json:"fromSurface"`
	CaptureBeyondViewport bool                 `json:"captureBeyondViewport"`
	Clip                  visualScreenshotClip `json:"clip"`
}

type visualScreenshotClip struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
	Scale  float64 `json:"scale"`
}

// CaptureVisual captures the page's visible viewport (or a crop) bound to
// the current document epoch, viewport and annotated references, and makes
// it the page's current visual observation.
func (s *Session) CaptureVisual(ctx context.Context, pageID string, options VisualCaptureOptions) (VisualObservation, *TaskError) {
	if options.MaxAnnotations < 0 || options.MaxAnnotations > maxVisualAnnotations {
		return VisualObservation{}, newTaskError(TaskErrorInvalidInput, "visual_capture", fmt.Errorf("maxAnnotations must be 0..%d", maxVisualAnnotations))
	}
	if options.MaxAnnotations == 0 {
		options.MaxAnnotations = defaultVisualAnnotations
	}
	execCtx, done, taskErr := s.chromiumOperation(ctx)
	if taskErr != nil {
		return VisualObservation{}, taskErr
	}
	defer done()
	page, lease, taskErr := s.leaseChromiumPage(execCtx, pageID)
	if taskErr != nil {
		return VisualObservation{}, taskErr
	}
	defer lease.Release()
	execCtx = lease.Context()
	if taskErr := s.authorizeChromiumOperation(execCtx, page, string(actions.KindScreenshot), ""); taskErr != nil {
		return VisualObservation{}, taskErr
	}
	evidence, err := page.observer.Capture(execCtx, bridgeobserve.ModeFull, "")
	if err != nil {
		return VisualObservation{}, classifyTaskError("visual_capture", err)
	}
	s.markObserved(page)
	collector := page.observer.SnapshotCollector()
	viewport, err := collector.Viewport(execCtx)
	if err != nil {
		return VisualObservation{}, classifyTaskError("visual_capture", err)
	}
	crop := bridgeobserve.Rect{Width: viewport.Width, Height: viewport.Height}
	if options.CropRef != "" {
		if crop, taskErr = s.visualCrop(execCtx, collector, pageID, options.CropRef, evidence.Snapshot, viewport); taskErr != nil {
			return VisualObservation{}, taskErr
		}
	}
	raw, pixels, err := captureVisualPixels(execCtx, page.page, viewport, crop)
	if err != nil {
		return VisualObservation{}, classifyTaskError("visual_capture", err)
	}
	if after, err := collector.Viewport(execCtx); err != nil || after != viewport {
		return VisualObservation{}, newTaskError(TaskErrorStaleReference, "visual_capture", errors.Join(errors.New("viewport changed during capture"), err))
	}
	binding := &visualBinding{epoch: evidence.Snapshot.Epoch, viewport: viewport, crop: crop, pixels: pixels, mime: "image/png", image: raw}
	binding.ratio = float64(pixels.Bounds().Dx()) / crop.Width
	binding.annotations, binding.public = visualAnnotations(execCtx, collector, pageID, evidence.Snapshot, viewport, crop, options.MaxAnnotations)
	if options.Annotate && len(binding.public) > 0 {
		if binding.image, err = drawVisualAnnotations(pixels, binding.public, binding.ratio); err != nil {
			return VisualObservation{}, classifyTaskError("visual_capture", err)
		}
	}
	if binding.id, err = randomToken(); err != nil {
		return VisualObservation{}, classifyTaskError("visual_capture", err)
	}
	pageURL, err := page.page.CurrentURL(execCtx)
	if err != nil {
		return VisualObservation{}, classifyTaskError("visual_capture", err)
	}
	s.chromiumMu.Lock()
	page.visual = binding
	s.chromiumMu.Unlock()
	observation := VisualObservation{ID: binding.id, PageID: pageID, URL: pageURL, Epoch: binding.epoch, Viewport: viewport, MIME: binding.mime, Image: binding.image, PixelWidth: pixels.Bounds().Dx(), PixelHeight: pixels.Bounds().Dy(), PixelRatio: binding.ratio, Annotations: binding.public, CapturedAt: time.Now().UTC()}
	if options.CropRef != "" {
		observation.Crop = &crop
	}
	return observation, nil
}

func (s *Session) visualCrop(ctx context.Context, collector *bridgeobserve.Collector, pageID, cropRef string, snapshot bridgeobserve.Snapshot, viewport bridgeobserve.ViewportState) (bridgeobserve.Rect, *TaskError) {
	ref, err := pageReference(pageID, cropRef)
	if err != nil {
		return bridgeobserve.Rect{}, classifyTaskError("visual_capture", err)
	}
	for _, node := range snapshot.Nodes {
		if node.Ref != ref {
			continue
		}
		rect, err := collector.ViewportRect(ctx, node, viewport)
		if err != nil {
			return bridgeobserve.Rect{}, newTaskError(TaskErrorInvalidInput, "visual_capture", fmt.Errorf("crop target is not visible in the viewport: %w", err))
		}
		return rect, nil
	}
	return bridgeobserve.Rect{}, newTaskError(TaskErrorStaleReference, "visual_capture", fmt.Errorf("crop ref %s is not in the current observation", cropRef))
}

// captureVisualPixels screenshots a viewport rectangle; clip coordinates are
// document coordinates, so the viewport scroll offset is added.
func captureVisualPixels(ctx context.Context, page *bridge.Page, viewport bridgeobserve.ViewportState, rect bridgeobserve.Rect) ([]byte, image.Image, error) {
	if rect.Width <= 0 || rect.Height <= 0 || math.Ceil(rect.Width)*math.Ceil(rect.Height) > float64(bridge.JPEGPixelsLimit) {
		return nil, nil, fmt.Errorf("visual capture region is empty or exceeds the pixel budget")
	}
	var shot struct {
		Data string `json:"data"`
	}
	params := visualScreenshotParams{Format: "png", FromSurface: true, Clip: visualScreenshotClip{X: rect.X + viewport.PageX, Y: rect.Y + viewport.PageY, Width: rect.Width, Height: rect.Height, Scale: 1}}
	if err := page.Call(ctx, "Page.captureScreenshot", params, &shot); err != nil {
		return nil, nil, err
	}
	raw, err := base64.StdEncoding.DecodeString(shot.Data)
	if err != nil {
		return nil, nil, fmt.Errorf("decode screenshot: %w", err)
	}
	config, err := png.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, nil, fmt.Errorf("screenshot header: %w", err)
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > bridge.JPEGPixelsLimit/config.Height {
		return nil, nil, fmt.Errorf("screenshot exceeds the pixel budget")
	}
	pixels, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, nil, fmt.Errorf("decode screenshot pixels: %w", err)
	}
	return raw, pixels, nil
}

// visualAnnotations selects visible, uncovered interactive references in the
// crop, in document order, with boxes relative to the crop.
func visualAnnotations(ctx context.Context, collector *bridgeobserve.Collector, pageID string, snapshot bridgeobserve.Snapshot, viewport bridgeobserve.ViewportState, crop bridgeobserve.Rect, limit int) ([]boundAnnotation, []VisualAnnotation) {
	var bound []boundAnnotation
	var public []VisualAnnotation
	for _, node := range snapshot.Nodes {
		if len(bound) == limit {
			break
		}
		if node.Ref == "" || !node.Interactive || !node.Visible || node.Box == nil || node.Hit == bridgeobserve.HitCovered {
			continue
		}
		// Main-frame layout boxes are document coordinates: skip nodes that
		// cannot reach the crop without per-node geometry calls.
		if node.FrameID == viewport.MainFrameID {
			document := bridgeobserve.Rect{X: node.Box.X - viewport.PageX - 1, Y: node.Box.Y - viewport.PageY - 1, Width: node.Box.Width + 2, Height: node.Box.Height + 2}
			if _, ok := intersectRect(document, crop); !ok {
				continue
			}
		}
		box, err := collector.ViewportRect(ctx, node, viewport)
		if err != nil {
			continue
		}
		clipped, ok := intersectRect(box, crop)
		if !ok {
			continue
		}
		bound = append(bound, boundAnnotation{node: node, box: clipped})
		public = append(public, VisualAnnotation{Label: node.Ref, Ref: pageID + ":" + node.Ref, Role: node.Role, Name: node.Name, FrameID: node.FrameID, Box: bridgeobserve.Rect{X: clipped.X - crop.X, Y: clipped.Y - crop.Y, Width: clipped.Width, Height: clipped.Height}})
	}
	return bound, public
}

func intersectRect(rect, bounds bridgeobserve.Rect) (bridgeobserve.Rect, bool) {
	left, top := max(rect.X, bounds.X), max(rect.Y, bounds.Y)
	right, bottom := min(rect.X+rect.Width, bounds.X+bounds.Width), min(rect.Y+rect.Height, bounds.Y+bounds.Height)
	if right <= left || bottom <= top {
		return bridgeobserve.Rect{}, false
	}
	return bridgeobserve.Rect{X: left, Y: top, Width: right - left, Height: bottom - top}, true
}

func rectContains(rect bridgeobserve.Rect, x, y float64) bool {
	return x >= rect.X && y >= rect.Y && x < rect.X+rect.Width && y < rect.Y+rect.Height
}

var (
	annotationBox   = color.RGBA{R: 230, G: 0, B: 0, A: 255}
	annotationLabel = color.RGBA{R: 255, G: 255, B: 255, A: 255}
)

// drawVisualAnnotations draws a 2px box and a ref label per annotation onto
// a copy of the pixels and encodes it as PNG.
func drawVisualAnnotations(pixels image.Image, annotations []VisualAnnotation, ratio float64) ([]byte, error) {
	canvas := image.NewRGBA(pixels.Bounds())
	draw.Draw(canvas, canvas.Bounds(), pixels, pixels.Bounds().Min, draw.Src)
	face := basicfont.Face7x13
	stroke := max(2, int(math.Round(2*ratio)))
	for _, annotation := range annotations {
		box := image.Rect(int(annotation.Box.X*ratio), int(annotation.Box.Y*ratio), int(math.Ceil((annotation.Box.X+annotation.Box.Width)*ratio)), int(math.Ceil((annotation.Box.Y+annotation.Box.Height)*ratio))).Intersect(canvas.Bounds())
		for _, edge := range []image.Rectangle{
			image.Rect(box.Min.X, box.Min.Y, box.Max.X, box.Min.Y+stroke), image.Rect(box.Min.X, box.Max.Y-stroke, box.Max.X, box.Max.Y),
			image.Rect(box.Min.X, box.Min.Y, box.Min.X+stroke, box.Max.Y), image.Rect(box.Max.X-stroke, box.Min.Y, box.Max.X, box.Max.Y),
		} {
			draw.Draw(canvas, edge.Intersect(canvas.Bounds()), image.NewUniform(annotationBox), image.Point{}, draw.Src)
		}
		width := font.MeasureString(face, annotation.Label).Ceil() + 6
		top := box.Min.Y - 14
		if top < 0 {
			top = box.Min.Y + stroke
		}
		label := image.Rect(box.Min.X, top, box.Min.X+width, top+14).Intersect(canvas.Bounds())
		draw.Draw(canvas, label, image.NewUniform(annotationBox), image.Point{}, draw.Src)
		drawer := font.Drawer{Dst: canvas, Src: image.NewUniform(annotationLabel), Face: face, Dot: fixed.P(box.Min.X+3, top+11)}
		drawer.DrawString(annotation.Label)
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, canvas); err != nil {
		return nil, fmt.Errorf("encode annotated screenshot: %w", err)
	}
	return encoded.Bytes(), nil
}

// GroundVisual asks the injected host VisualGrounder for a point of the
// page's current visual observation. Without a grounder it is unavailable.
func (s *Session) GroundVisual(ctx context.Context, pageID, observationID, instruction string) (VisualPoint, *TaskError) {
	s.owner.mu.RLock()
	grounder := s.owner.config.VisualGrounder
	s.owner.mu.RUnlock()
	if grounder == nil {
		return VisualPoint{}, newTaskError(TaskErrorCapabilityUnavailable, "visual_ground", errors.New("no visual grounding model is configured; use annotated references"))
	}
	if strings.TrimSpace(instruction) == "" {
		return VisualPoint{}, newTaskError(TaskErrorInvalidInput, "visual_ground", errors.New("instruction required"))
	}
	binding, taskErr := s.currentVisual(pageID, observationID)
	if taskErr != nil {
		return VisualPoint{}, taskErr
	}
	point, err := grounder.Ground(ctx, VisualGroundingRequest{Instruction: instruction, MIME: binding.mime, Image: binding.image, Width: binding.crop.Width, Height: binding.crop.Height, Annotations: binding.public})
	if err != nil {
		return VisualPoint{}, classifyTaskError("visual_ground", err)
	}
	if !rectContains(bridgeobserve.Rect{Width: binding.crop.Width, Height: binding.crop.Height}, point.X, point.Y) || math.IsNaN(point.Confidence) || point.Confidence < 0 || point.Confidence > 1 {
		return VisualPoint{}, newTaskError(TaskErrorExecutionFailed, "visual_ground", fmt.Errorf("grounding returned an ungrounded point (%.1f, %.1f, confidence %.2f)", point.X, point.Y, point.Confidence))
	}
	if _, taskErr := s.currentVisual(pageID, observationID); taskErr != nil {
		return VisualPoint{}, taskErr
	}
	point.ObservationID = observationID
	return point, nil
}

func (s *Session) currentVisual(pageID, observationID string) (*visualBinding, *TaskError) {
	s.chromiumMu.Lock()
	defer s.chromiumMu.Unlock()
	page := s.chromiumPages[pageID]
	if page == nil {
		return nil, newTaskError(TaskErrorPageNotFound, "visual", fmt.Errorf("unknown page %q", pageID))
	}
	if page.visual == nil || page.visual.id != observationID {
		return nil, newTaskError(TaskErrorStaleReference, "visual", errors.New("visual observation is not the page's current observation; capture again"))
	}
	return page.visual, nil
}
