package input

import (
	"fmt"
	"time"
)

// touch.go (spec L4026: input/touch.go - touch events for mobile
// emulation).
//
// Human-like input: touch events for mobile emulation. Touch events
// include touch start, move, and end, with multi-touch support.

// TouchPoint is a single touch contact point
// (spec L4026: touch.go - touch events for mobile emulation).
type TouchPoint struct {
	ID       int     `json:"id"`       // touch identifier
	X        float64 `json:"x"`        // x coordinate
	Y        float64 `json:"y"`        // y coordinate
	Pressure float64 `json:"pressure"` // 0-1
	RadiusX  float64 `json:"radiusX"`  // contact area radius X
	RadiusY  float64 `json:"radiusY"`  // contact area radius Y
}

// TouchEventType enumerates touch event types
// (spec L4026: touch.go - touch events for mobile emulation).
type TouchEventType string

const (
	// TouchEventStart is the touchstart event.
	TouchEventStart TouchEventType = "touchstart"
	// TouchEventMove is the touchmove event.
	TouchEventMove TouchEventType = "touchmove"
	// TouchEventEnd is the touchend event.
	TouchEventEnd TouchEventType = "touchend"
	// TouchEventCancel is the touchcancel event.
	TouchEventCancel TouchEventType = "touchcancel"
)

// TouchEvent is a DOM touch event
// (spec L4026: touch.go - touch events for mobile emulation).
type TouchEvent struct {
	Type      TouchEventType `json:"type"`
	Points    []TouchPoint   `json:"points"` // active touch points
	Timestamp time.Time      `json:"timestamp"`
	Target    string         `json:"target,omitempty"` // element ref
}

// TouchSequence is a sequence of touch events forming a gesture
// (spec L4026: touch events for mobile emulation).
type TouchSequence struct {
	Events   []TouchEvent  `json:"events"`
	Duration time.Duration `json:"duration"`
}

// NewTouchPoint creates a new TouchPoint with the given coordinates
// (spec L4026: touch.go - touch events for mobile emulation).
func NewTouchPoint(id int, x, y float64) TouchPoint {
	return TouchPoint{
		ID:       id,
		X:        x,
		Y:        y,
		Pressure: 1.0,
		RadiusX:  5.0,
		RadiusY:  5.0,
	}
}

// Tap creates a simple tap gesture (touchstart + touchend)
// (spec L4026: touch events for mobile emulation).
func Tap(point TouchPoint, target string) TouchSequence {
	now := time.Now()
	return TouchSequence{
		Events: []TouchEvent{
			{
				Type:      TouchEventStart,
				Points:    []TouchPoint{point},
				Timestamp: now,
				Target:    target,
			},
			{
				Type:      TouchEventEnd,
				Points:    []TouchPoint{point},
				Timestamp: now.Add(50 * time.Millisecond), // typical tap duration
				Target:    target,
			},
		},
		Duration: 50 * time.Millisecond,
	}
}

// Swipe creates a swipe gesture from start to end
// (spec L4026: touch events for mobile emulation).
func Swipe(start, end TouchPoint, steps int, target string) TouchSequence {
	if steps <= 0 {
		steps = 10
	}
	now := time.Now()
	events := make([]TouchEvent, 0, steps+2)

	// touchstart
	events = append(events, TouchEvent{
		Type:      TouchEventStart,
		Points:    []TouchPoint{start},
		Timestamp: now,
		Target:    target,
	})

	// touchmove events
	for i := 1; i <= steps; i++ {
		t := float64(i) / float64(steps)
		pt := TouchPoint{
			ID:       start.ID,
			X:        start.X + (end.X-start.X)*t,
			Y:        start.Y + (end.Y-start.Y)*t,
			Pressure: 1.0,
			RadiusX:  5.0,
			RadiusY:  5.0,
		}
		events = append(events, TouchEvent{
			Type:      TouchEventMove,
			Points:    []TouchPoint{pt},
			Timestamp: now.Add(time.Duration(i*16) * time.Millisecond), // 60fps
			Target:    target,
		})
	}

	// touchend
	events = append(events, TouchEvent{
		Type:      TouchEventEnd,
		Points:    []TouchPoint{end},
		Timestamp: now.Add(time.Duration((steps+1)*16) * time.Millisecond),
		Target:    target,
	})

	return TouchSequence{
		Events:   events,
		Duration: time.Duration((steps+1)*16) * time.Millisecond,
	}
}

// Pinch creates a pinch gesture (two-finger)
// (spec L4026: touch events for mobile emulation).
func Pinch(center TouchPoint, startRadius, endRadius float64, steps int, target string) TouchSequence {
	if steps <= 0 {
		steps = 10
	}
	now := time.Now()
	events := make([]TouchEvent, 0, steps+2)

	// Initial two touch points
	finger1Start := TouchPoint{ID: 1, X: center.X - startRadius, Y: center.Y, Pressure: 1.0, RadiusX: 5.0, RadiusY: 5.0}
	finger2Start := TouchPoint{ID: 2, X: center.X + startRadius, Y: center.Y, Pressure: 1.0, RadiusX: 5.0, RadiusY: 5.0}

	// touchstart with two fingers
	events = append(events, TouchEvent{
		Type:      TouchEventStart,
		Points:    []TouchPoint{finger1Start, finger2Start},
		Timestamp: now,
		Target:    target,
	})

	// touchmove events
	for i := 1; i <= steps; i++ {
		t := float64(i) / float64(steps)
		radius := startRadius + (endRadius-startRadius)*t
		f1 := TouchPoint{ID: 1, X: center.X - radius, Y: center.Y, Pressure: 1.0, RadiusX: 5.0, RadiusY: 5.0}
		f2 := TouchPoint{ID: 2, X: center.X + radius, Y: center.Y, Pressure: 1.0, RadiusX: 5.0, RadiusY: 5.0}
		events = append(events, TouchEvent{
			Type:      TouchEventMove,
			Points:    []TouchPoint{f1, f2},
			Timestamp: now.Add(time.Duration(i*16) * time.Millisecond),
			Target:    target,
		})
	}

	// touchend
	finger1End := TouchPoint{ID: 1, X: center.X - endRadius, Y: center.Y, Pressure: 1.0, RadiusX: 5.0, RadiusY: 5.0}
	finger2End := TouchPoint{ID: 2, X: center.X + endRadius, Y: center.Y, Pressure: 1.0, RadiusX: 5.0, RadiusY: 5.0}
	events = append(events, TouchEvent{
		Type:      TouchEventEnd,
		Points:    []TouchPoint{finger1End, finger2End},
		Timestamp: now.Add(time.Duration((steps+1)*16) * time.Millisecond),
		Target:    target,
	})

	return TouchSequence{
		Events:   events,
		Duration: time.Duration((steps+1)*16) * time.Millisecond,
	}
}

// String returns a diagnostic summary.
func (p TouchPoint) String() string {
	return fmt.Sprintf("TouchPoint(id:%d %.1f,%.1f pressure:%.1f)", p.ID, p.X, p.Y, p.Pressure)
}

// String returns a diagnostic summary.
func (e TouchEvent) String() string {
	return fmt.Sprintf("TouchEvent{type:%s points:%d target:%s}", e.Type, len(e.Points), e.Target)
}

// String returns a diagnostic summary.
func (s TouchSequence) String() string {
	return fmt.Sprintf("TouchSequence{events:%d duration:%v}", len(s.Events), s.Duration)
}

// IsMultiTouch reports whether the sequence contains multi-touch events.
func (s TouchSequence) IsMultiTouch() bool {
	for _, e := range s.Events {
		if len(e.Points) > 1 {
			return true
		}
	}
	return false
}

// EventCount returns the number of events of a specific type.
func (s TouchSequence) EventCount(eventType TouchEventType) int {
	count := 0
	for _, e := range s.Events {
		if e.Type == eventType {
			count++
		}
	}
	return count
}
