package observe

import (
	"strings"
	"sync"
	"testing"
)

func TestNewAnnotator_DefaultViewport(t *testing.T) {
	a := NewAnnotator(0, 0)
	if a.ViewportWidth() != DefaultViewportWidth {
		t.Fatalf("expected default width=%d, got %d", DefaultViewportWidth, a.ViewportWidth())
	}
	if a.ViewportHeight() != DefaultViewportHeight {
		t.Fatalf("expected default height=%d, got %d", DefaultViewportHeight, a.ViewportHeight())
	}
}

func TestNewAnnotator_CustomViewport(t *testing.T) {
	a := NewAnnotator(800, 600)
	if a.ViewportWidth() != 800 {
		t.Fatalf("expected width=800, got %d", a.ViewportWidth())
	}
	if a.ViewportHeight() != 600 {
		t.Fatalf("expected height=600, got %d", a.ViewportHeight())
	}
}

func TestAnnotator_SetViewport(t *testing.T) {
	a := NewAnnotator(0, 0)
	a.SetViewport(1920, 1080)
	if a.ViewportWidth() != 1920 {
		t.Fatalf("expected width=1920, got %d", a.ViewportWidth())
	}
	if a.ViewportHeight() != 1080 {
		t.Fatalf("expected height=1080, got %d", a.ViewportHeight())
	}
}

func TestAnnotator_SetViewport_IgnoresNonPositive(t *testing.T) {
	a := NewAnnotator(500, 400)
	a.SetViewport(-1, 0)
	if a.ViewportWidth() != 500 {
		t.Fatalf("expected width unchanged=500, got %d", a.ViewportWidth())
	}
	if a.ViewportHeight() != 400 {
		t.Fatalf("expected height unchanged=400, got %d", a.ViewportHeight())
	}
}

func TestAnnotator_Annotate_Empty(t *testing.T) {
	a := NewAnnotator(1280, 720)
	overlay := a.Annotate(nil)
	if overlay.TotalCount != 0 {
		t.Fatalf("expected 0 annotations, got %d", overlay.TotalCount)
	}
	if len(overlay.Annotations) != 0 {
		t.Fatalf("expected 0-length annotations, got %d", len(overlay.Annotations))
	}
}

func TestAnnotator_Annotate_NumberingSequential(t *testing.T) {
	a := NewAnnotator(1280, 720)
	nodes := []AnnotatableUnit{
		{ElementType: "button", Text: "Submit", BoundingBox: BoundingBox{X: 10, Y: 10, Width: 80, Height: 30}},
		{ElementType: "a", Text: "Home", BoundingBox: BoundingBox{X: 100, Y: 10, Width: 50, Height: 20}},
		{ElementType: "input", Text: "Email", BoundingBox: BoundingBox{X: 10, Y: 50, Width: 200, Height: 25}},
	}
	overlay := a.Annotate(nodes)
	if overlay.TotalCount != 3 {
		t.Fatalf("expected 3 annotations, got %d", overlay.TotalCount)
	}
	for i, ann := range overlay.Annotations {
		if ann.ID != i+1 {
			t.Errorf("expected id=%d, got %d", i+1, ann.ID)
		}
	}
}

func TestAnnotator_Annotate_FiltersNonInteractive(t *testing.T) {
	a := NewAnnotator(1280, 720)
	nodes := []AnnotatableUnit{
		{ElementType: "div", Text: "container", BoundingBox: BoundingBox{X: 0, Y: 0, Width: 100, Height: 100}},
		{ElementType: "span", Text: "label", BoundingBox: BoundingBox{X: 0, Y: 0, Width: 100, Height: 100}},
		{ElementType: "button", Text: "OK", BoundingBox: BoundingBox{X: 0, Y: 0, Width: 50, Height: 30}},
	}
	overlay := a.Annotate(nodes)
	if overlay.TotalCount != 1 {
		t.Fatalf("expected 1 interactive annotation, got %d", overlay.TotalCount)
	}
	if overlay.Annotations[0].ElementType != "button" {
		t.Errorf("expected button, got %s", overlay.Annotations[0].ElementType)
	}
}

func TestAnnotator_Annotate_FiltersOffscreen(t *testing.T) {
	a := NewAnnotator(1280, 720)
	nodes := []AnnotatableUnit{
		{ElementType: "button", Text: "On", BoundingBox: BoundingBox{X: 10, Y: 10, Width: 50, Height: 30}},
		{ElementType: "button", Text: "OffRight", BoundingBox: BoundingBox{X: 2000, Y: 10, Width: 50, Height: 30}},
		{ElementType: "button", Text: "OffBottom", BoundingBox: BoundingBox{X: 10, Y: 2000, Width: 50, Height: 30}},
		{ElementType: "button", Text: "ZeroSize", BoundingBox: BoundingBox{X: 10, Y: 10, Width: 0, Height: 0}},
	}
	overlay := a.Annotate(nodes)
	if overlay.TotalCount != 1 {
		t.Fatalf("expected 1 in-viewport annotation, got %d", overlay.TotalCount)
	}
	if overlay.Annotations[0].Text != "On" {
		t.Errorf("expected 'On', got %s", overlay.Annotations[0].Text)
	}
}

func TestAnnotator_Annotate_ARIARoles(t *testing.T) {
	a := NewAnnotator(1280, 720)
	nodes := []AnnotatableUnit{
		{ElementType: "role=button", Text: "Custom", BoundingBox: BoundingBox{X: 10, Y: 10, Width: 50, Height: 30}},
		{ElementType: "role=tab", Text: "Tab1", BoundingBox: BoundingBox{X: 10, Y: 50, Width: 50, Height: 30}},
		{ElementType: "role=unknown", Text: "Skip", BoundingBox: BoundingBox{X: 10, Y: 90, Width: 50, Height: 30}},
	}
	overlay := a.Annotate(nodes)
	if overlay.TotalCount != 2 {
		t.Fatalf("expected 2 ARIA-role annotations, got %d", overlay.TotalCount)
	}
}

func TestAnnotator_Annotate_PreservesBox(t *testing.T) {
	a := NewAnnotator(1280, 720)
	box := BoundingBox{X: 100, Y: 200, Width: 80, Height: 30}
	nodes := []AnnotatableUnit{
		{ElementType: "button", Text: "Submit", BoundingBox: box},
	}
	overlay := a.Annotate(nodes)
	if overlay.Annotations[0].BoundingBox != box {
		t.Errorf("expected box %+v, got %+v", box, overlay.Annotations[0].BoundingBox)
	}
}

func TestAnnotator_Annotate_NormalizesText(t *testing.T) {
	a := NewAnnotator(1280, 720)
	nodes := []AnnotatableUnit{
		{ElementType: "button", Text: "  Hello    World  ", BoundingBox: BoundingBox{X: 10, Y: 10, Width: 50, Height: 30}},
	}
	overlay := a.Annotate(nodes)
	if overlay.Annotations[0].Text != "Hello World" {
		t.Errorf("expected normalized 'Hello World', got %q", overlay.Annotations[0].Text)
	}
}

func TestAnnotator_Annotate_TextTruncation(t *testing.T) {
	a := NewAnnotator(1280, 720)
	long := strings.Repeat("a", 200)
	nodes := []AnnotatableUnit{
		{ElementType: "button", Text: long, BoundingBox: BoundingBox{X: 10, Y: 10, Width: 50, Height: 30}},
	}
	overlay := a.Annotate(nodes)
	if len(overlay.Annotations[0].Text) > 80 {
		t.Errorf("expected text truncated to <=80 chars, got %d", len(overlay.Annotations[0].Text))
	}
	if !strings.HasSuffix(overlay.Annotations[0].Text, "…") {
		t.Errorf("expected truncated text to end with ellipsis, got %q", overlay.Annotations[0].Text)
	}
}

func TestAnnotator_Annotate_LowercasesElementType(t *testing.T) {
	a := NewAnnotator(1280, 720)
	nodes := []AnnotatableUnit{
		{ElementType: "BUTTON", Text: "X", BoundingBox: BoundingBox{X: 10, Y: 10, Width: 50, Height: 30}},
	}
	overlay := a.Annotate(nodes)
	if overlay.Annotations[0].ElementType != "button" {
		t.Errorf("expected lowercased 'button', got %q", overlay.Annotations[0].ElementType)
	}
}

func TestAnnotator_Annotate_ViewportInOverlay(t *testing.T) {
	a := NewAnnotator(1024, 768)
	overlay := a.Annotate(nil)
	if overlay.ViewportWidth != 1024 {
		t.Errorf("expected viewport width=1024, got %d", overlay.ViewportWidth)
	}
	if overlay.ViewportHeight != 768 {
		t.Errorf("expected viewport height=768, got %d", overlay.ViewportHeight)
	}
}

func TestProjectScrollOffset(t *testing.T) {
	box := BoundingBox{X: 500, Y: 800, Width: 100, Height: 50}
	projected := ProjectScrollOffset(box, 200, 600)
	if projected.X != 300 {
		t.Errorf("expected x=300, got %d", projected.X)
	}
	if projected.Y != 200 {
		t.Errorf("expected y=200, got %d", projected.Y)
	}
	if projected.Width != 100 {
		t.Errorf("expected width preserved=100, got %d", projected.Width)
	}
	if projected.Height != 50 {
		t.Errorf("expected height preserved=50, got %d", projected.Height)
	}
}

func TestProjectScrollOffset_ZeroScroll(t *testing.T) {
	box := BoundingBox{X: 100, Y: 200, Width: 80, Height: 30}
	projected := ProjectScrollOffset(box, 0, 0)
	if projected != box {
		t.Errorf("expected unchanged box, got %+v", projected)
	}
}

func TestProjectScrollOffset_NegativeScroll(t *testing.T) {
	box := BoundingBox{X: 100, Y: 200, Width: 80, Height: 30}
	projected := ProjectScrollOffset(box, -50, -100)
	if projected.X != 150 {
		t.Errorf("expected x=150, got %d", projected.X)
	}
	if projected.Y != 300 {
		t.Errorf("expected y=300, got %d", projected.Y)
	}
}

func TestFormatAnnotation_WithText(t *testing.T) {
	ann := Annotation{
		ID:          1,
		Text:        "Submit",
		BoundingBox: BoundingBox{X: 100, Y: 200, Width: 80, Height: 30},
		ElementType: "button",
	}
	got := FormatAnnotation(ann)
	want := " [1] button 'Submit' at (100,200) 80x30"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestFormatAnnotation_WithoutText(t *testing.T) {
	ann := Annotation{
		ID:          5,
		Text:        "",
		BoundingBox: BoundingBox{X: 10, Y: 20, Width: 40, Height: 15},
		ElementType: "input",
	}
	got := FormatAnnotation(ann)
	want := " [5] input at (10,20) 40x15"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestFormatAnnotation_MultiDigitID(t *testing.T) {
	ann := Annotation{
		ID:          42,
		Text:        "Next",
		BoundingBox: BoundingBox{X: 0, Y: 0, Width: 100, Height: 100},
		ElementType: "a",
	}
	got := FormatAnnotation(ann)
	want := " [42] a 'Next' at (0,0) 100x100"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestIsInteractive(t *testing.T) {
	tests := []struct {
		elementType string
		want        bool
	}{
		{"button", true},
		{"a", true},
		{"input", true},
		{"select", true},
		{"textarea", true},
		{"summary", true},
		{"div", false},
		{"span", false},
		{"p", false},
		{"", false},
		{"BUTTON", true}, // case-insensitive
		{"role=button", true},
		{"role=link", true},
		{"role=unknown", false},
	}
	for _, tt := range tests {
		if got := isInteractive(tt.elementType); got != tt.want {
			t.Errorf("isInteractive(%q) = %v, want %v", tt.elementType, got, tt.want)
		}
	}
}

func TestAnnotator_ConcurrentAnnotate(t *testing.T) {
	a := NewAnnotator(1280, 720)
	nodes := []AnnotatableUnit{
		{ElementType: "button", Text: "A", BoundingBox: BoundingBox{X: 10, Y: 10, Width: 50, Height: 30}},
		{ElementType: "a", Text: "B", BoundingBox: BoundingBox{X: 100, Y: 10, Width: 50, Height: 30}},
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			overlay := a.Annotate(nodes)
			if overlay.TotalCount != 2 {
				t.Errorf("expected 2, got %d", overlay.TotalCount)
			}
		}()
	}
	wg.Wait()
}
