package cdpops

import (
	"fmt"
	"math"
)

// geometry.go (spec L4019: bridge/cdpops/geometry.go - coordinate
// transforms).
//
// Low-level CDP ops: coordinate transforms between CSS pixels,
// device pixels, and layout coordinates. Handles viewport scaling,
// scroll offsets, and element-relative positioning.

// Point represents a 2D coordinate
// (spec L4019: coordinate transforms).
type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Rect represents a rectangle
// (spec L4019: coordinate transforms).
type Rect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// Viewport represents the browser viewport
// (spec L4019: coordinate transforms).
type Viewport struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
	Scale  float64 `json:"scale"` // device pixel ratio
}

// CSSToDevicePixels converts CSS pixels to device pixels
// (spec L4019: coordinate transforms).
func CSSToDevicePixels(p Point, scale float64) Point {
	if scale <= 0 {
		scale = 1
	}
	return Point{X: p.X * scale, Y: p.Y * scale}
}

// DeviceToCSSPixels converts device pixels to CSS pixels
// (spec L4019: coordinate transforms).
func DeviceToCSSPixels(p Point, scale float64) Point {
	if scale <= 0 {
		scale = 1
	}
	return Point{X: p.X / scale, Y: p.Y / scale}
}

// ApplyScrollOffset applies scroll offset to a point
// (spec L4019: coordinate transforms).
func ApplyScrollOffset(p Point, scrollX, scrollY float64) Point {
	return Point{X: p.X + scrollX, Y: p.Y + scrollY}
}

// RemoveScrollOffset removes scroll offset from a point
// (spec L4019: coordinate transforms).
func RemoveScrollOffset(p Point, scrollX, scrollY float64) Point {
	return Point{X: p.X - scrollX, Y: p.Y - scrollY}
}

// PointInRect checks if a point is inside a rectangle
// (spec L4019: coordinate transforms).
func PointInRect(p Point, r Rect) bool {
	return p.X >= r.X && p.X <= r.X+r.Width &&
		p.Y >= r.Y && p.Y <= r.Y+r.Height
}

// RectCenter returns the center point of a rectangle
// (spec L4019: coordinate transforms).
func RectCenter(r Rect) Point {
	return Point{
		X: r.X + r.Width/2,
		Y: r.Y + r.Height/2,
	}
}

// Distance calculates the Euclidean distance between two points
// (spec L4019: coordinate transforms).
func Distance(a, b Point) float64 {
	dx := a.X - b.X
	dy := a.Y - b.Y
	return math.Sqrt(dx*dx + dy*dy)
}

// Midpoint calculates the midpoint between two points
// (spec L4019: coordinate transforms).
func Midpoint(a, b Point) Point {
	return Point{
		X: (a.X + b.X) / 2,
		Y: (a.Y + b.Y) / 2,
	}
}

// Lerp linearly interpolates between two points
// (spec L4019: coordinate transforms).
func Lerp(a, b Point, t float64) Point {
	return Point{
		X: a.X + (b.X-a.X)*t,
		Y: a.Y + (b.Y-a.Y)*t,
	}
}

// ClampPoint clamps a point to viewport bounds
// (spec L4019: coordinate transforms).
func ClampPoint(p Point, vp Viewport) Point {
	result := p
	if result.X < vp.X {
		result.X = vp.X
	}
	if result.X > vp.X+vp.Width {
		result.X = vp.X + vp.Width
	}
	if result.Y < vp.Y {
		result.Y = vp.Y
	}
	if result.Y > vp.Y+vp.Height {
		result.Y = vp.Y + vp.Height
	}
	return result
}

// QuadToRect converts a Quad to a bounding Rect
// (spec L4019: coordinate transforms).
func QuadToRect(q Quad) Rect {
	minX := minFloat(q.X1, q.X2, q.X3, q.X4)
	maxX := maxFloat(q.X1, q.X2, q.X3, q.X4)
	minY := minFloat(q.Y1, q.Y2, q.Y3, q.Y4)
	maxY := maxFloat(q.Y1, q.Y2, q.Y3, q.Y4)
	return Rect{
		X:      minX,
		Y:      minY,
		Width:  maxX - minX,
		Height: maxY - minY,
	}
}

func minFloat(a, b, c, d float64) float64 {
	result := a
	for _, v := range []float64{b, c, d} {
		if v < result {
			result = v
		}
	}
	return result
}

func maxFloat(a, b, c, d float64) float64 {
	result := a
	for _, v := range []float64{b, c, d} {
		if v > result {
			result = v
		}
	}
	return result
}

// String returns a diagnostic summary.
func (p Point) String() string {
	return fmt.Sprintf("Point{x:%.1f y:%.1f}", p.X, p.Y)
}

// String returns a diagnostic summary.
func (r Rect) String() string {
	return fmt.Sprintf("Rect{x:%.1f y:%.1f w:%.1f h:%.1f}", r.X, r.Y, r.Width, r.Height)
}

// String returns a diagnostic summary.
func (vp Viewport) String() string {
	return fmt.Sprintf("Viewport{x:%.1f y:%.1f w:%.1f h:%.1f scale:%.2f}",
		vp.X, vp.Y, vp.Width, vp.Height, vp.Scale)
}
