package input

import (
	"testing"
	"time"
)

// ==================== mouse.go tests ====================

// TestMousePointString verifies String method
// (mouse.go - Bezier curve movement).
func TestMousePointString(t *testing.T) {
	p := MousePoint{X: 100, Y: 200}
	s := p.String()
	if s == "" {
		t.Error("String should not be empty")
	}
}

// TestMousePointDistance verifies Distance method
// (mouse.go).
func TestMousePointDistance(t *testing.T) {
	p1 := MousePoint{X: 0, Y: 0}
	p2 := MousePoint{X: 3, Y: 4}
	d := p1.Distance(p2)
	if d != 5 {
		t.Errorf("distance: got %.1f, want 5", d)
	}
}

// TestBezierCurve verifies Bezier curve computation
// (Bezier curve movement).
func TestBezierCurve(t *testing.T) {
	p0 := MousePoint{X: 0, Y: 0}
	p1 := MousePoint{X: 50, Y: 100}
	p2 := MousePoint{X: 100, Y: 0}
	// t=0 should return p0
	result := BezierCurve(p0, p1, p2, 0)
	if result.X != 0 || result.Y != 0 {
		t.Errorf("t=0: got (%.1f, %.1f), want (0, 0)", result.X, result.Y)
	}
	// t=1 should return p2
	result = BezierCurve(p0, p1, p2, 1)
	if result.X != 100 || result.Y != 0 {
		t.Errorf("t=1: got (%.1f, %.1f), want (100, 0)", result.X, result.Y)
	}
}

// TestBezierCurveMidpoint verifies midpoint
// (Bezier curve movement).
func TestBezierCurveMidpoint(t *testing.T) {
	p0 := MousePoint{X: 0, Y: 0}
	p1 := MousePoint{X: 50, Y: 50}
	p2 := MousePoint{X: 100, Y: 0}
	// t=0.5 should return the midpoint of the curve
	result := BezierCurve(p0, p1, p2, 0.5)
	// At t=0.5: u=0.5, x = 0.25*0 + 0.5*50 + 0.25*100 = 50
	// y = 0.25*0 + 0.5*50 + 0.25*0 = 25
	if result.X != 50 || result.Y != 25 {
		t.Errorf("t=0.5: got (%.1f, %.1f), want (50, 25)", result.X, result.Y)
	}
}

// TestDefaultMouseMoveConfig verifies default config
// (Bezier curve movement + jitter).
func TestDefaultMouseMoveConfig(t *testing.T) {
	cfg := DefaultMouseMoveConfig()
	if cfg.Steps <= 0 {
		t.Error("steps should be positive")
	}
	if cfg.Jitter < 0 {
		t.Error("jitter should be non-negative")
	}
}

// TestGenerateMousePath verifies path generation
// (Bezier curve movement + jitter).
func TestGenerateMousePath(t *testing.T) {
	start := MousePoint{X: 0, Y: 0}
	end := MousePoint{X: 100, Y: 100}
	cfg := DefaultMouseMoveConfig()
	path := GenerateMousePath(start, end, cfg, nil)
	if len(path.Points) != cfg.Steps {
		t.Errorf("points: got %d, want %d", len(path.Points), cfg.Steps)
	}
	// First point should be approximately start (with jitter)
	if path.Points[0].X < -5 || path.Points[0].X > 5 {
		t.Errorf("first point X: got %.1f, want near 0", path.Points[0].X)
	}
	// Last point should be exactly end
	last := path.Points[len(path.Points)-1]
	if last.X != 100 || last.Y != 100 {
		t.Errorf("last point: got (%.1f, %.1f), want (100, 100)", last.X, last.Y)
	}
}

// TestGenerateMousePathNoJitter verifies path without jitter
// (Bezier curve movement).
func TestGenerateMousePathNoJitter(t *testing.T) {
	start := MousePoint{X: 0, Y: 0}
	end := MousePoint{X: 100, Y: 0}
	cfg := MouseMoveConfig{Steps: 10, Jitter: 0, CurveBias: 0.5}
	path := GenerateMousePath(start, end, cfg, nil)
	// Without jitter, first point should be exactly start
	if path.Points[0].X != 0 || path.Points[0].Y != 0 {
		t.Errorf("first point without jitter: got (%.1f, %.1f), want (0, 0)", path.Points[0].X, path.Points[0].Y)
	}
}

// TestGenerateMousePathDuration verifies duration is within
// range (human-like timing).
func TestGenerateMousePathDuration(t *testing.T) {
	start := MousePoint{X: 0, Y: 0}
	end := MousePoint{X: 500, Y: 500}
	cfg := DefaultMouseMoveConfig()
	path := GenerateMousePath(start, end, cfg, nil)
	if path.Duration < cfg.MinDuration {
		t.Errorf("duration %v < min %v", path.Duration, cfg.MinDuration)
	}
	if path.Duration > cfg.MaxDuration {
		t.Errorf("duration %v > max %v", path.Duration, cfg.MaxDuration)
	}
}

// TestMoveMouse verifies the convenience function
// (Bezier curve movement + jitter).
func TestMoveMouse(t *testing.T) {
	start := MousePoint{X: 0, Y: 0}
	end := MousePoint{X: 200, Y: 200}
	path := MoveMouse(start, end)
	if len(path.Points) == 0 {
		t.Error("path should not be empty")
	}
	if path.End().X != 200 || path.End().Y != 200 {
		t.Errorf("end: got (%.1f, %.1f), want (200, 200)", path.End().X, path.End().Y)
	}
}

// TestMousePathStartEnd verifies Start/End methods
// (mouse.go).
func TestMousePathStartEnd(t *testing.T) {
	path := MousePath{
		Points: []MousePoint{{X: 10, Y: 20}, {X: 50, Y: 60}, {X: 100, Y: 200}},
	}
	if path.Start().X != 10 {
		t.Errorf("start X: got %.1f, want 10", path.Start().X)
	}
	if path.End().X != 100 {
		t.Errorf("end X: got %.1f, want 100", path.End().X)
	}
}

// TestMousePathStartEndEmpty verifies empty path is safe.
func TestMousePathStartEndEmpty(t *testing.T) {
	path := MousePath{}
	if path.Start().X != 0 || path.End().X != 0 {
		t.Error("empty path should return zero points")
	}
}

// TestMousePathString verifies String method.
func TestMousePathString(t *testing.T) {
	path := MousePath{Points: []MousePoint{{X: 1, Y: 2}}, Duration: 100 * time.Millisecond}
	s := path.String()
	if s == "" {
		t.Error("String should not be empty")
	}
}

// TestNewMouseClick verifies click creation
// (human-like input).
func TestNewMouseClick(t *testing.T) {
	click := NewMouseClick(MousePoint{X: 50, Y: 50}, "left")
	if click.Button != "left" {
		t.Errorf("button: got %s, want left", click.Button)
	}
	if click.Duration <= 0 {
		t.Error("duration should be positive")
	}
}

// ==================== touch.go tests ====================

// TestTouchEventTypeConstants verifies event type constants
// (touch.go - touch events for mobile emulation).
func TestTouchEventTypeConstants(t *testing.T) {
	if TouchEventStart != "touchstart" {
		t.Error("touchstart mismatch")
	}
	if TouchEventMove != "touchmove" {
		t.Error("touchmove mismatch")
	}
	if TouchEventEnd != "touchend" {
		t.Error("touchend mismatch")
	}
	if TouchEventCancel != "touchcancel" {
		t.Error("touchcancel mismatch")
	}
}

// TestNewTouchPoint verifies touch point creation
// (touch.go - touch events for mobile emulation).
func TestNewTouchPoint(t *testing.T) {
	p := NewTouchPoint(1, 100, 200)
	if p.ID != 1 {
		t.Errorf("id: got %d, want 1", p.ID)
	}
	if p.X != 100 || p.Y != 200 {
		t.Errorf("coords: got (%.1f, %.1f), want (100, 200)", p.X, p.Y)
	}
	if p.Pressure != 1.0 {
		t.Error("default pressure should be 1.0")
	}
}

// TestTap verifies tap gesture
// (touch events for mobile emulation).
func TestTap(t *testing.T) {
	point := NewTouchPoint(1, 50, 50)
	seq := Tap(point, "button")
	if len(seq.Events) != 2 {
		t.Errorf("events: got %d, want 2", len(seq.Events))
	}
	if seq.Events[0].Type != TouchEventStart {
		t.Error("first event should be touchstart")
	}
	if seq.Events[1].Type != TouchEventEnd {
		t.Error("second event should be touchend")
	}
	if seq.Duration <= 0 {
		t.Error("duration should be positive")
	}
}

// TestSwipe verifies swipe gesture
// (touch events for mobile emulation).
func TestSwipe(t *testing.T) {
	start := NewTouchPoint(1, 0, 100)
	end := NewTouchPoint(1, 200, 100)
	seq := Swipe(start, end, 10, "container")
	// Should have 1 touchstart + 10 touchmove + 1 touchend = 12
	if len(seq.Events) != 12 {
		t.Errorf("events: got %d, want 12", len(seq.Events))
	}
	if seq.Events[0].Type != TouchEventStart {
		t.Error("first event should be touchstart")
	}
	if seq.Events[11].Type != TouchEventEnd {
		t.Error("last event should be touchend")
	}
}

// TestSwipeDefaultSteps verifies default steps when steps<=0.
func TestSwipeDefaultSteps(t *testing.T) {
	start := NewTouchPoint(1, 0, 0)
	end := NewTouchPoint(1, 100, 0)
	seq := Swipe(start, end, 0, "target")
	// 0 steps -> default 10 -> 1 + 10 + 1 = 12
	if len(seq.Events) != 12 {
		t.Errorf("events: got %d, want 12", len(seq.Events))
	}
}

// TestPinch verifies pinch gesture (multi-touch)
// (touch events for mobile emulation).
func TestPinch(t *testing.T) {
	center := NewTouchPoint(0, 100, 100)
	seq := Pinch(center, 50, 100, 5, "image")
	// 1 touchstart + 5 touchmove + 1 touchend = 7
	if len(seq.Events) != 7 {
		t.Errorf("events: got %d, want 7", len(seq.Events))
	}
	if !seq.IsMultiTouch() {
		t.Error("pinch should be multi-touch")
	}
	// Each event should have 2 touch points
	for _, e := range seq.Events {
		if len(e.Points) != 2 {
			t.Errorf("event %s should have 2 points, got %d", e.Type, len(e.Points))
		}
	}
}

// TestTouchSequenceIsMultiTouch verifies IsMultiTouch.
func TestTouchSequenceIsMultiTouch(t *testing.T) {
	// Single-touch sequence
	tap := Tap(NewTouchPoint(1, 0, 0), "target")
	if tap.IsMultiTouch() {
		t.Error("tap should not be multi-touch")
	}
	// Multi-touch sequence
	pinch := Pinch(NewTouchPoint(0, 100, 100), 50, 100, 5, "image")
	if !pinch.IsMultiTouch() {
		t.Error("pinch should be multi-touch")
	}
}

// TestTouchSequenceEventCount verifies EventCount.
func TestTouchSequenceEventCount(t *testing.T) {
	seq := Swipe(NewTouchPoint(1, 0, 0), NewTouchPoint(1, 100, 0), 10, "target")
	if seq.EventCount(TouchEventStart) != 1 {
		t.Error("should have 1 touchstart")
	}
	if seq.EventCount(TouchEventMove) != 10 {
		t.Error("should have 10 touchmove")
	}
	if seq.EventCount(TouchEventEnd) != 1 {
		t.Error("should have 1 touchend")
	}
}

// TestTouchPointString verifies String method.
func TestTouchPointString(t *testing.T) {
	p := NewTouchPoint(1, 50, 75)
	s := p.String()
	if s == "" {
		t.Error("String should not be empty")
	}
}

// TestTouchEventString verifies String method.
func TestTouchEventString(t *testing.T) {
	e := TouchEvent{Type: TouchEventStart, Points: []TouchPoint{NewTouchPoint(1, 0, 0)}, Target: "btn"}
	s := e.String()
	if s == "" {
		t.Error("String should not be empty")
	}
}

// TestTouchSequenceString verifies String method.
func TestTouchSequenceString(t *testing.T) {
	seq := Tap(NewTouchPoint(1, 0, 0), "target")
	s := seq.String()
	if s == "" {
		t.Error("String should not be empty")
	}
}

// ==================== parity test ====================

// TestMouseTouchParity verifies full parity.
// (Bezier curve movement + jitter, touch events).
func TestMouseTouchParity(t *testing.T) {
	// 1. Bezier curve
	p0 := MousePoint{X: 0, Y: 0}
	p1 := MousePoint{X: 50, Y: 50}
	p2 := MousePoint{X: 100, Y: 0}
	pt := BezierCurve(p0, p1, p2, 0.5)
	if pt.X != 50 {
		t.Error("Bezier curve should compute correctly")
	}

	// 2. Mouse path with jitter
	path := MoveMouse(MousePoint{X: 0, Y: 0}, MousePoint{X: 200, Y: 200})
	if len(path.Points) == 0 {
		t.Error("mouse path should not be empty")
	}

	// 3. Mouse click
	click := NewMouseClick(MousePoint{X: 50, Y: 50}, "left")
	if click.Button != "left" {
		t.Error("click should have left button")
	}

	// 4. Touch tap
	tap := Tap(NewTouchPoint(1, 50, 50), "button")
	if len(tap.Events) != 2 {
		t.Error("tap should have 2 events")
	}

	// 5. Touch swipe
	swipe := Swipe(NewTouchPoint(1, 0, 0), NewTouchPoint(1, 100, 0), 10, "container")
	if len(swipe.Events) != 12 {
		t.Error("swipe should have 12 events")
	}

	// 6. Touch pinch (multi-touch)
	pinch := Pinch(NewTouchPoint(0, 100, 100), 50, 100, 5, "image")
	if !pinch.IsMultiTouch() {
		t.Error("pinch should be multi-touch")
	}
}
