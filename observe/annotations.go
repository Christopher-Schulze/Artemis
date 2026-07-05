package observe

import (
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"
)

// annotations.go (spec L4264: Snapshot Annotations).
//
// Produces numbered overlay boxes over interactive DOM elements so an
// agent can refer to elements by ordinal ("click [3]") instead of
// fragile CSS selectors. Thread-safe via RWMutex.
//
// Reference: research/webstack/pinchtab-main/internal/bridge/observe/snapshot.go

// BoundingBox is an axis-aligned rectangle in viewport pixels
// (spec L4264).
type BoundingBox struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

// Annotation is a single numbered overlay box pointing at an
// interactive element (spec L4264).
type Annotation struct {
	ID           int         `json:"id"`
	Text         string      `json:"text"`
	BoundingBox  BoundingBox `json:"bounding_box"`
	ElementType  string      `json:"element_type"`
	ScrollOffset BoundingBox `json:"scroll_offset,omitempty"`
}

// AnnotationOverlay is the full set of annotations rendered over a
// snapshot (spec L4264).
type AnnotationOverlay struct {
	Annotations    []Annotation `json:"annotations"`
	TotalCount     int          `json:"total_count"`
	ViewportWidth  int          `json:"viewport_width"`
	ViewportHeight int          `json:"viewport_height"`
}

// AnnotatableUnit is a DOM element candidate for annotation
// (spec L4264). ElementType is the lowercased tag name (e.g.
// "button", "a", "input"). Text is a short label derived from the
// element's visible text or aria-label.
type AnnotatableUnit struct {
	ElementType string      `json:"element_type"`
	Text        string      `json:"text"`
	BoundingBox BoundingBox `json:"bounding_box"`
}

// interactiveElementTypes is the set of element types considered
// interactive and therefore eligible for numbered annotations
// (spec L4264).
var interactiveElementTypes = map[string]bool{
	"button":        true,
	"a":             true,
	"input":         true,
	"select":        true,
	"textarea":      true,
	"summary":       true,
	"role=button":   true,
	"role=link":     true,
	"role=checkbox": true,
	"role=tab":      true,
	"role=menuitem": true,
}

// DefaultViewportWidth is the fallback viewport width when none is
// supplied to Annotate (spec L4264).
const DefaultViewportWidth = 1280

// DefaultViewportHeight is the fallback viewport height when none is
// supplied to Annotate (spec L4264).
const DefaultViewportHeight = 720

// Annotator assigns numbered IDs to interactive elements
// (spec L4264). It is safe for concurrent use.
type Annotator struct {
	mu             sync.RWMutex
	viewportWidth  int
	viewportHeight int
}

// NewAnnotator returns an Annotator with the given viewport
// dimensions. Non-positive values fall back to the defaults.
func NewAnnotator(viewportWidth, viewportHeight int) *Annotator {
	if viewportWidth <= 0 {
		viewportWidth = DefaultViewportWidth
	}
	if viewportHeight <= 0 {
		viewportHeight = DefaultViewportHeight
	}
	return &Annotator{
		viewportWidth:  viewportWidth,
		viewportHeight: viewportHeight,
	}
}

// ViewportWidth returns the configured viewport width.
func (a *Annotator) ViewportWidth() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.viewportWidth
}

// ViewportHeight returns the configured viewport height.
func (a *Annotator) ViewportHeight() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.viewportHeight
}

// SetViewport updates the viewport dimensions used by Annotate.
func (a *Annotator) SetViewport(width, height int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if width > 0 {
		a.viewportWidth = width
	}
	if height > 0 {
		a.viewportHeight = height
	}
}

// isInteractive reports whether a node's element type is eligible
// for annotation (spec L4264).
func isInteractive(elementType string) bool {
	elementType = strings.ToLower(strings.TrimSpace(elementType))
	if interactiveElementTypes[elementType] {
		return true
	}
	// ARIA role shorthand like "role=button".
	if strings.HasPrefix(elementType, "role=") {
		role := strings.TrimPrefix(elementType, "role=")
		return interactiveElementTypes["role="+role]
	}
	return false
}

// hasValidBox reports whether a bounding box has positive area and
// is at least partially within the viewport.
func (a *Annotator) hasValidBox(box BoundingBox) bool {
	a.mu.RLock()
	vw, vh := a.viewportWidth, a.viewportHeight
	a.mu.RUnlock()
	if box.Width <= 0 || box.Height <= 0 {
		return false
	}
	// Element must intersect the viewport rectangle.
	if box.X+box.Width <= 0 || box.Y+box.Height <= 0 {
		return false
	}
	if box.X >= vw || box.Y >= vh {
		return false
	}
	return true
}

// Annotate assigns sequential numbered IDs (starting at 1) to the
// interactive, in-viewport nodes among the supplied candidates
// (spec L4264). Non-interactive or off-screen nodes are skipped.
func (a *Annotator) Annotate(nodes []AnnotatableUnit) AnnotationOverlay {
	a.mu.RLock()
	vw, vh := a.viewportWidth, a.viewportHeight
	a.mu.RUnlock()

	overlay := AnnotationOverlay{
		Annotations:    make([]Annotation, 0, len(nodes)),
		ViewportWidth:  vw,
		ViewportHeight: vh,
	}
	id := 0
	for _, node := range nodes {
		if !isInteractive(node.ElementType) {
			continue
		}
		if !a.hasValidBox(node.BoundingBox) {
			continue
		}
		id++
		overlay.Annotations = append(overlay.Annotations, Annotation{
			ID:          id,
			Text:        normalizeAnnotationText(node.Text),
			BoundingBox: node.BoundingBox,
			ElementType: strings.ToLower(strings.TrimSpace(node.ElementType)),
		})
	}
	overlay.TotalCount = len(overlay.Annotations)
	return overlay
}

// normalizeAnnotationText trims and collapses whitespace in the
// label text and caps its length so overlay strings stay readable.
func normalizeAnnotationText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	// Collapse internal whitespace runs to single spaces.
	fields := strings.Fields(text)
	text = strings.Join(fields, " ")
	const maxLen = 80
	const ellipsis = "…"
	if len(text) > maxLen {
		cut := maxLen - len(ellipsis)
		if cut < 0 {
			cut = 0
		}
		// Back up to a valid UTF-8 boundary so we never split a rune.
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		text = text[:cut] + ellipsis
	}
	return text
}

// ProjectScrollOffset returns a copy of the given bounding box with
// its coordinates offset by the supplied scroll position
// (spec L4264). This converts page-relative coordinates into
// viewport-relative coordinates (or vice-versa by negating the
// offsets).
func ProjectScrollOffset(box BoundingBox, scrollX, scrollY int) BoundingBox {
	return BoundingBox{
		X:      box.X - scrollX,
		Y:      box.Y - scrollY,
		Width:  box.Width,
		Height: box.Height,
	}
}

// FormatAnnotation formats a single annotation as the spec-mandated
// overlay string: " [1] button 'Submit' at (100,200) 80x30"
// (spec L4264).
func FormatAnnotation(annotation Annotation) string {
	text := annotation.Text
	if text == "" {
		return fmt.Sprintf(" [%d] %s at (%d,%d) %dx%d",
			annotation.ID,
			annotation.ElementType,
			annotation.BoundingBox.X, annotation.BoundingBox.Y,
			annotation.BoundingBox.Width, annotation.BoundingBox.Height)
	}
	return fmt.Sprintf(" [%d] %s '%s' at (%d,%d) %dx%d",
		annotation.ID,
		annotation.ElementType,
		text,
		annotation.BoundingBox.X, annotation.BoundingBox.Y,
		annotation.BoundingBox.Width, annotation.BoundingBox.Height)
}
