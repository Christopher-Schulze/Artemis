package input

import (
	"fmt"
	"math"
	"math/rand"
	"time"
)

// mouse.go (spec L4026: input/mouse.go - Bezier curve movement +
// jitter).
//
// Human-like input: Bezier curve mouse movement with jitter to
// simulate real human mouse behavior. Linear mouse movement is
// bot-detectable; Bezier curves with random jitter mimic human
// motor control.

// MousePoint is a 2D point on the screen
// (spec L4026: mouse.go - Bezier curve movement).
type MousePoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// MousePath is a sequence of mouse points forming a movement path
// (spec L4026: Bezier curve movement + jitter).
type MousePath struct {
	Points   []MousePoint `json:"points"`
	Duration time.Duration `json:"duration"`
}

// MouseMoveConfig configures mouse movement behavior
// (spec L4026: Bezier curve movement + jitter).
type MouseMoveConfig struct {
	Steps      int     // number of interpolation steps
	Jitter     float64 // max pixel jitter per point (0 = no jitter)
	CurveBias  float64 // control point bias (0-1, 0.5 = centered)
	MinDuration time.Duration // minimum total duration
	MaxDuration time.Duration // maximum total duration
}

// DefaultMouseMoveConfig returns the default mouse move config
// (spec L4026: Bezier curve movement + jitter).
func DefaultMouseMoveConfig() MouseMoveConfig {
	return MouseMoveConfig{
		Steps:       25,
		Jitter:      2.0,
		CurveBias:   0.5,
		MinDuration: 200 * time.Millisecond,
		MaxDuration: 800 * time.Millisecond,
	}
}

// BezierCurve computes a point on a quadratic Bezier curve
// (spec L4026: Bezier curve movement).
// p0, p1, p2 are the start, control, and end points.
// t is the parameter (0 <= t <= 1).
func BezierCurve(p0, p1, p2 MousePoint, t float64) MousePoint {
	u := 1 - t
	x := u*u*p0.X + 2*u*t*p1.X + t*t*p2.X
	y := u*u*p0.Y + 2*u*t*p1.Y + t*t*p2.Y
	return MousePoint{X: x, Y: y}
}

// GenerateMousePath generates a Bezier curve mouse path from start
// to end with jitter (spec L4026: Bezier curve movement + jitter).
// The path uses a quadratic Bezier curve with a randomized control
// point, and adds jitter to each interpolated point.
func GenerateMousePath(start, end MousePoint, cfg MouseMoveConfig, rng *rand.Rand) MousePath {
	if cfg.Steps <= 0 {
		cfg.Steps = 25
	}
	if cfg.Jitter < 0 {
		cfg.Jitter = 0
	}

	// Compute control point with curve bias and randomization
	// (spec L4026: Bezier curve movement).
	midX := (start.X + end.X) / 2
	midY := (start.Y + end.Y) / 2
	dx := end.X - start.X
	dy := end.Y - start.Y
	// Perpendicular offset for control point
	perpX := -dy * cfg.CurveBias
	perpY := dx * cfg.CurveBias
	// Add randomization to control point
	var randOffset float64
	if rng != nil {
		randOffset = (rng.Float64() - 0.5) * 0.3
	}
	control := MousePoint{
		X: midX + perpX + randOffset*dx,
		Y: midY + perpY + randOffset*dy,
	}

	// Generate points along the Bezier curve
	points := make([]MousePoint, cfg.Steps)
	for i := 0; i < cfg.Steps; i++ {
		t := float64(i) / float64(cfg.Steps-1)
		pt := BezierCurve(start, control, end, t)
		// Add jitter (spec L4026: jitter)
		if cfg.Jitter > 0 && rng != nil {
			pt.X += (rng.Float64() - 0.5) * 2 * cfg.Jitter
			pt.Y += (rng.Float64() - 0.5) * 2 * cfg.Jitter
		}
		points[i] = pt
	}
	// Ensure the last point is exactly the end point
	points[cfg.Steps-1] = end

	// Compute duration based on distance
	distance := math.Sqrt(dx*dx + dy*dy)
	duration := computeDuration(distance, cfg)

	return MousePath{
		Points:   points,
		Duration: duration,
	}
}

// computeDuration computes a random duration within the config range
// based on the distance (spec L4026: human-like timing).
func computeDuration(distance float64, cfg MouseMoveConfig) time.Duration {
	if cfg.MaxDuration <= cfg.MinDuration {
		return cfg.MinDuration
	}
	// Scale duration with distance (longer distance -> more time)
	scaled := cfg.MinDuration + time.Duration(distance/10)*time.Millisecond
	if scaled < cfg.MinDuration {
		scaled = cfg.MinDuration
	}
	if scaled > cfg.MaxDuration {
		scaled = cfg.MaxDuration
	}
	return scaled
}

// MoveMouse generates a mouse path from start to end
// (spec L4026: Bezier curve movement + jitter).
func MoveMouse(start, end MousePoint) MousePath {
	return GenerateMousePath(start, end, DefaultMouseMoveConfig(), rand.New(rand.NewSource(time.Now().UnixNano())))
}

// MouseClick simulates a mouse click at the given point
// (spec L4026: human-like input).
type MouseClick struct {
	Point    MousePoint  `json:"point"`
	Button   string      `json:"button"`   // "left", "right", "middle"
	Duration time.Duration `json:"duration"` // click duration
}

// NewMouseClick creates a new MouseClick at the given point
// (spec L4026: human-like input).
func NewMouseClick(point MousePoint, button string) MouseClick {
	return MouseClick{
		Point:    point,
		Button:   button,
		Duration: 50 * time.Millisecond, // typical click duration
	}
}

// String returns a diagnostic summary.
func (p MousePoint) String() string {
	return fmt.Sprintf("MousePoint(%.1f, %.1f)", p.X, p.Y)
}

// Distance computes the Euclidean distance to another point.
func (p MousePoint) Distance(other MousePoint) float64 {
	dx := p.X - other.X
	dy := p.Y - other.Y
	return math.Sqrt(dx*dx + dy*dy)
}

// String returns a diagnostic summary.
func (path MousePath) String() string {
	return fmt.Sprintf("MousePath{points:%d duration:%v}", len(path.Points), path.Duration)
}

// Start returns the first point in the path.
func (path MousePath) Start() MousePoint {
	if len(path.Points) == 0 {
		return MousePoint{}
	}
	return path.Points[0]
}

// End returns the last point in the path.
func (path MousePath) End() MousePoint {
	if len(path.Points) == 0 {
		return MousePoint{}
	}
	return path.Points[len(path.Points)-1]
}
