package cdpops

import (
	"fmt"
	"math"
	"sync"
	"time"
)

// pointer.go (spec L4019: bridge/cdpops/pointer.go - mouse/touch
// events).
//
// Low-level CDP ops: mouse and touch event dispatch via CDP.
// Provides functions for mouse movement, clicks, and touch events.

// MouseButton enumerates mouse buttons
// (spec L4019: mouse/touch events).
type MouseButton string

const (
	MouseButtonLeft   MouseButton = "left"
	MouseButtonRight  MouseButton = "right"
	MouseButtonMiddle MouseButton = "middle"
)

// MouseEvent represents a mouse event
// (spec L4019: mouse/touch events).
type MouseEvent struct {
	Type       MouseButton `json:"type"`
	X          float64     `json:"x"`
	Y          float64     `json:"y"`
	Button     MouseButton `json:"button"`
	ClickCount int         `json:"clickCount"`
	Timestamp  time.Time   `json:"timestamp"`
}

// TouchEvent represents a touch event
// (spec L4019: mouse/touch events).
type TouchEvent struct {
	Type      string    `json:"type"` // "touchStart", "touchMove", "touchEnd"
	X         float64   `json:"x"`
	Y         float64   `json:"y"`
	Timestamp time.Time `json:"timestamp"`
}

// PointerDispatcher dispatches mouse and touch events
// (spec L4019: mouse/touch events).
type PointerDispatcher struct {
	mu     sync.Mutex
	events []interface{}
}

// NewPointerDispatcher creates a new PointerDispatcher
// (spec L4019: mouse/touch events).
func NewPointerDispatcher() *PointerDispatcher {
	return &PointerDispatcher{}
}

// DispatchMouse dispatches a mouse event
// (spec L4019: mouse/touch events).
func (d *PointerDispatcher) DispatchMouse(event MouseEvent) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !IsValidMouseButton(event.Button) {
		return fmt.Errorf("pointer: invalid mouse button %q", event.Button)
	}
	if event.ClickCount <= 0 {
		event.ClickCount = 1
	}
	event.Timestamp = time.Now()
	d.events = append(d.events, event)
	return nil
}

// DispatchTouch dispatches a touch event
// (spec L4019: mouse/touch events).
func (d *PointerDispatcher) DispatchTouch(event TouchEvent) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if event.Type == "" {
		return fmt.Errorf("pointer: empty touch event type")
	}
	event.Timestamp = time.Now()
	d.events = append(d.events, event)
	return nil
}

// Click dispatches a click at the given coordinates
// (spec L4019: mouse/touch events).
func (d *PointerDispatcher) Click(x, y float64, button MouseButton) error {
	return d.DispatchMouse(MouseEvent{
		Type:       button,
		X:          x,
		Y:          y,
		Button:     button,
		ClickCount: 1,
	})
}

// DoubleClick dispatches a double-click
// (spec L4019: mouse/touch events).
func (d *PointerDispatcher) DoubleClick(x, y float64) error {
	return d.DispatchMouse(MouseEvent{
		Type:       MouseButtonLeft,
		X:          x,
		Y:          y,
		Button:     MouseButtonLeft,
		ClickCount: 2,
	})
}

// MouseMove dispatches a mouse move
// (spec L4019: mouse/touch events).
func (d *PointerDispatcher) MouseMove(x, y float64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.events = append(d.events, MouseEvent{
		X:         x,
		Y:         y,
		Timestamp: time.Now(),
	})
	return nil
}

// TouchTap dispatches a touch tap (start + end)
// (spec L4019: mouse/touch events).
func (d *PointerDispatcher) TouchTap(x, y float64) error {
	if err := d.DispatchTouch(TouchEvent{Type: "touchStart", X: x, Y: y}); err != nil {
		return err
	}
	return d.DispatchTouch(TouchEvent{Type: "touchEnd", X: x, Y: y})
}

// EventCount returns the total number of dispatched events
// (spec L4019: mouse/touch events).
func (d *PointerDispatcher) EventCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.events)
}

// Clear clears all events
// (spec L4019: mouse/touch events).
func (d *PointerDispatcher) Clear() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	count := len(d.events)
	d.events = nil
	return count
}

// IsValidMouseButton reports whether a mouse button is valid
// (spec L4019: mouse/touch events).
func IsValidMouseButton(b MouseButton) bool {
	switch b {
	case MouseButtonLeft, MouseButtonRight, MouseButtonMiddle:
		return true
	}
	return false
}

// GenerateBezierPath generates a Bezier curve path from start to end
// (spec L4019: mouse/touch events).
func GenerateBezierPath(start, end Point, steps int) []Point {
	if steps <= 0 {
		steps = 10
	}
	// Control points for a natural-looking curve
	cp1 := Point{
		X: start.X + (end.X-start.X)*0.3,
		Y: start.Y + (end.Y-start.Y)*0.1,
	}
	cp2 := Point{
		X: start.X + (end.X-start.X)*0.7,
		Y: start.Y + (end.Y-start.Y)*0.9,
	}
	var path []Point
	for i := 0; i <= steps; i++ {
		t := float64(i) / float64(steps)
		// Cubic Bezier: B(t) = (1-t)^3*P0 + 3(1-t)^2*t*P1 + 3(1-t)*t^2*P2 + t^3*P3
		mt := 1 - t
		x := mt*mt*mt*start.X + 3*mt*mt*t*cp1.X + 3*mt*t*t*cp2.X + t*t*t*end.X
		y := mt*mt*mt*start.Y + 3*mt*mt*t*cp1.Y + 3*mt*t*t*cp2.Y + t*t*t*end.Y
		path = append(path, Point{X: x, Y: y})
	}
	return path
}

// AddJitter adds random jitter to a point
// (spec L4019: mouse/touch events).
func AddJitter(p Point, maxJitter float64) Point {
	if maxJitter <= 0 {
		return p
	}
	// Simple deterministic jitter based on coordinates
	jitterX := math.Sin(p.X*0.1) * maxJitter
	jitterY := math.Cos(p.Y*0.1) * maxJitter
	return Point{X: p.X + jitterX, Y: p.Y + jitterY}
}

// String returns a diagnostic summary.
func (e MouseEvent) String() string {
	return fmt.Sprintf("MouseEvent{x:%.1f y:%.1f button:%s clicks:%d}", e.X, e.Y, e.Button, e.ClickCount)
}

// String returns a diagnostic summary.
func (e TouchEvent) String() string {
	return fmt.Sprintf("TouchEvent{type:%s x:%.1f y:%.1f}", e.Type, e.X, e.Y)
}

// String returns a diagnostic summary.
func (d *PointerDispatcher) String() string {
	return fmt.Sprintf("PointerDispatcher{events:%d}", d.EventCount())
}
