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
	Points   []MousePoint  `json:"points"`
	Duration time.Duration `json:"duration"`
}

// MouseMoveConfig configures mouse movement behavior
// (spec L4026: Bezier curve movement + jitter).
type MouseMoveConfig struct {
	Steps       int           // number of interpolation steps
	Jitter      float64       // max pixel jitter per point (0 = no jitter)
	CurveBias   float64       // control point bias (0-1, 0.5 = centered)
	MinDuration time.Duration // minimum total duration
	MaxDuration time.Duration // maximum total duration
}

// DefaultMouseMoveConfig returns the default mouse move config
// (spec L4189: cubic Bezier, 4 control points, 100ms base + 200ms
// per 2000px, 5-30 steps, +-1.0px jitter, 16-23ms frame timing,
// +-50px control point offsets).
func DefaultMouseMoveConfig() MouseMoveConfig {
	return MouseMoveConfig{
		Steps:       25,
		Jitter:      1.0, // spec: +-1.0px per-step jitter
		CurveBias:   0.5,
		MinDuration: 100 * time.Millisecond, // spec: 100ms base
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

// CubicBezierCurve computes a point on a cubic Bezier curve with 4
// control points (spec L4189: 4 control points, cubic interpolation).
// p0 is start, p1/p2 are control points, p3 is end.
// t is the parameter (0 <= t <= 1).
func CubicBezierCurve(p0, p1, p2, p3 MousePoint, t float64) MousePoint {
	u := 1 - t
	uu := u * u
	uuu := uu * u
	tt := t * t
	ttt := tt * t
	x := uuu*p0.X + 3*uu*t*p1.X + 3*u*tt*p2.X + ttt*p3.X
	y := uuu*p0.Y + 3*uu*t*p1.Y + 3*u*tt*p2.Y + ttt*p3.Y
	return MousePoint{X: x, Y: y}
}

// GenerateMousePath generates a cubic Bezier curve mouse path from
// start to end with jitter (spec L4189: 4 control points, cubic
// interpolation, 5-30 steps, +-1.0px jitter, +-50px control offsets).
func GenerateMousePath(start, end MousePoint, cfg MouseMoveConfig, rng *rand.Rand) MousePath {
	if cfg.Steps <= 0 {
		cfg.Steps = 25
	}
	// Clamp steps to spec range 5-30 (spec L4189).
	if cfg.Steps < 5 {
		cfg.Steps = 5
	}
	if cfg.Steps > 30 {
		cfg.Steps = 30
	}
	if cfg.Jitter < 0 {
		cfg.Jitter = 0
	}

	dx := end.X - start.X
	dy := end.Y - start.Y
	distance := math.Sqrt(dx*dx + dy*dy)

	// Compute two control points for cubic Bezier with +-50px
	// random offsets (spec L4189: random control point offsets +-50px).
	perpX := -dy * cfg.CurveBias
	perpY := dx * cfg.CurveBias

	var cp1RandX, cp1RandY, cp2RandX, cp2RandY float64
	if rng != nil {
		cp1RandX = (rng.Float64() - 0.5) * 100 // +-50px
		cp1RandY = (rng.Float64() - 0.5) * 100
		cp2RandX = (rng.Float64() - 0.5) * 100
		cp2RandY = (rng.Float64() - 0.5) * 100
	}

	// Control point 1: 1/3 along the path + perpendicular + random
	t1 := 1.0 / 3.0
	cp1 := MousePoint{
		X: start.X + dx*t1 + perpX*0.3 + cp1RandX,
		Y: start.Y + dy*t1 + perpY*0.3 + cp1RandY,
	}
	// Control point 2: 2/3 along the path + perpendicular + random
	t2 := 2.0 / 3.0
	cp2 := MousePoint{
		X: start.X + dx*t2 + perpX*0.3 + cp2RandX,
		Y: start.Y + dy*t2 + perpY*0.3 + cp2RandY,
	}

	// Generate points along the cubic Bezier curve
	points := make([]MousePoint, cfg.Steps)
	for i := 0; i < cfg.Steps; i++ {
		t := float64(i) / float64(cfg.Steps-1)
		pt := CubicBezierCurve(start, cp1, cp2, end, t)
		// Add per-step jitter (spec L4189: +-1.0px per-step jitter)
		if cfg.Jitter > 0 && rng != nil {
			pt.X += (rng.Float64() - 0.5) * 2 * cfg.Jitter
			pt.Y += (rng.Float64() - 0.5) * 2 * cfg.Jitter
		}
		points[i] = pt
	}
	// Ensure the last point is exactly the end point
	points[cfg.Steps-1] = end

	// Compute duration: 100ms base + 200ms per 2000px
	// (spec L4189: Duration 100ms base + 200ms per 2000px).
	duration := computeCubicDuration(distance, cfg)

	return MousePath{
		Points:   points,
		Duration: duration,
	}
}

// computeCubicDuration computes duration per spec L4189:
// 100ms base + 200ms per 2000px distance.
func computeCubicDuration(distance float64, cfg MouseMoveConfig) time.Duration {
	base := 100 * time.Millisecond
	per2000px := 200 * time.Millisecond
	extra := time.Duration(distance/2000.0*float64(per2000px)) * 1
	duration := base + extra
	if cfg.MaxDuration > 0 && duration > cfg.MaxDuration {
		duration = cfg.MaxDuration
	}
	if duration < cfg.MinDuration {
		duration = cfg.MinDuration
	}
	return duration
}

// FrameInterval returns the frame timing for mouse movement
// (spec L4189: 16-23ms frame timing).
func FrameInterval(rng *rand.Rand) time.Duration {
	if rng == nil {
		return 16 * time.Millisecond
	}
	// Random 16-23ms (spec L4189)
	ms := 16 + rng.Intn(8) // 16..23
	return time.Duration(ms) * time.Millisecond
}

// MoveMouse generates a mouse path from start to end
// (spec L4026: Bezier curve movement + jitter).
func MoveMouse(start, end MousePoint) MousePath {
	return GenerateMousePath(start, end, DefaultMouseMoveConfig(), rand.New(rand.NewSource(time.Now().UnixNano())))
}

// MouseClick simulates a mouse click at the given point
// (spec L4026: human-like input).
type MouseClick struct {
	Point    MousePoint    `json:"point"`
	Button   string        `json:"button"`   // "left", "right", "middle"
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

// ClickSequenceConfig configures the full human-like click sequence
// (spec L4190: Click Sequence).
type ClickSequenceConfig struct {
	// StartOffsetRange is the max random offset from target center
	// per axis (spec: -50..150px). The actual offset is random in
	// [-StartOffsetRangeMin, +StartOffsetRangeMax].
	StartOffsetMinX float64 // default -50
	StartOffsetMaxX float64 // default 150
	StartOffsetMinY float64 // default -50
	StartOffsetMaxY float64 // default 150
	// MoveThreshold: if distance > this, do a smooth mouse move first
	// (spec: if distance > 30px).
	MoveThreshold float64
	// PreClickDelayMin/Max: random delay before pressing
	// (spec: 50-199ms).
	PreClickDelayMin time.Duration
	PreClickDelayMax time.Duration
	// HoldDurationMin/Max: random hold time between press and release
	// (spec: 30-119ms).
	HoldDurationMin time.Duration
	HoldDurationMax time.Duration
	// ReleaseJitter: max pixel jitter on release (spec: +-1.0px).
	ReleaseJitter float64
	// TargetOffsetMax: max offset from element center
	// (spec: center + +-5px offset).
	TargetOffsetMax float64
}

// DefaultClickSequenceConfig returns the spec-mandated click sequence
// config (spec L4190).
func DefaultClickSequenceConfig() ClickSequenceConfig {
	return ClickSequenceConfig{
		StartOffsetMinX:  -50,
		StartOffsetMaxX:  150,
		StartOffsetMinY:  -50,
		StartOffsetMaxY:  150,
		MoveThreshold:    30,
		PreClickDelayMin: 50 * time.Millisecond,
		PreClickDelayMax: 199 * time.Millisecond,
		HoldDurationMin:  30 * time.Millisecond,
		HoldDurationMax:  119 * time.Millisecond,
		ReleaseJitter:    1.0,
		TargetOffsetMax:  5.0,
	}
}

// ClickSequence is the full human-like click sequence
// (spec L4190: random start offset -> smooth move -> pre-click delay
// -> MousePressed -> hold -> MouseReleased with jitter).
type ClickSequence struct {
	StartPoint    MousePoint    `json:"startPoint"`         // with random offset
	TargetPoint   MousePoint    `json:"targetPoint"`        // element center + offset
	MovePath      *MousePath    `json:"movePath,omitempty"` // nil if distance <= threshold
	PreClickDelay time.Duration `json:"preClickDelay"`
	HoldDuration  time.Duration `json:"holdDuration"`
	ReleasePoint  MousePoint    `json:"releasePoint"` // target + jitter
	Button        string        `json:"button"`
}

// GenerateClickSequence generates a full human-like click sequence
// from a current mouse position to a target element center
// (spec L4190: Click Sequence).
// boxCenter is the element center from dom.GetBoxModel.
func GenerateClickSequence(currentPos, boxCenter MousePoint, cfg ClickSequenceConfig, rng *rand.Rand) ClickSequence {
	// 1. Random start offset from target center (-50..150px per axis)
	// (spec L4190: Random start offset -50..150px per axis from target)
	startOffsetX := cfg.StartOffsetMinX
	startOffsetY := cfg.StartOffsetMinY
	if rng != nil {
		startOffsetX = cfg.StartOffsetMinX + rng.Float64()*(cfg.StartOffsetMaxX-cfg.StartOffsetMinX)
		startOffsetY = cfg.StartOffsetMinY + rng.Float64()*(cfg.StartOffsetMaxY-cfg.StartOffsetMinY)
	}
	startPoint := MousePoint{
		X: boxCenter.X + startOffsetX,
		Y: boxCenter.Y + startOffsetY,
	}

	// 2. Target point: element center + +-5px offset
	// (spec L4190: center + +-5px offset)
	var targetOffX, targetOffY float64
	if rng != nil {
		targetOffX = (rng.Float64() - 0.5) * 2 * cfg.TargetOffsetMax
		targetOffY = (rng.Float64() - 0.5) * 2 * cfg.TargetOffsetMax
	}
	targetPoint := MousePoint{
		X: boxCenter.X + targetOffX,
		Y: boxCenter.Y + targetOffY,
	}

	// 3. Smooth mouse move if distance > threshold (spec: >30px)
	var movePath *MousePath
	distance := currentPos.Distance(targetPoint)
	if distance > cfg.MoveThreshold {
		path := GenerateMousePath(currentPos, targetPoint, DefaultMouseMoveConfig(), rng)
		movePath = &path
	}

	// 4. Pre-click delay 50-199ms (spec L4190)
	preClickDelay := cfg.PreClickDelayMin
	if rng != nil && cfg.PreClickDelayMax > cfg.PreClickDelayMin {
		rangeMs := int((cfg.PreClickDelayMax - cfg.PreClickDelayMin) / time.Millisecond)
		preClickDelay = cfg.PreClickDelayMin + time.Duration(rng.Intn(rangeMs))*time.Millisecond
	}

	// 5. Hold duration 30-119ms (spec L4190)
	holdDuration := cfg.HoldDurationMin
	if rng != nil && cfg.HoldDurationMax > cfg.HoldDurationMin {
		rangeMs := int((cfg.HoldDurationMax - cfg.HoldDurationMin) / time.Millisecond)
		holdDuration = cfg.HoldDurationMin + time.Duration(rng.Intn(rangeMs))*time.Millisecond
	}

	// 6. Release point: target + +-1.0px jitter (spec L4190)
	releasePoint := targetPoint
	if rng != nil && cfg.ReleaseJitter > 0 {
		releasePoint.X += (rng.Float64() - 0.5) * 2 * cfg.ReleaseJitter
		releasePoint.Y += (rng.Float64() - 0.5) * 2 * cfg.ReleaseJitter
	}

	return ClickSequence{
		StartPoint:    startPoint,
		TargetPoint:   targetPoint,
		MovePath:      movePath,
		PreClickDelay: preClickDelay,
		HoldDuration:  holdDuration,
		ReleasePoint:  releasePoint,
		Button:        "left",
	}
}

// BoxModel represents the box model of a DOM element from
// dom.GetBoxModel (spec L4190: backend DOM node ID -> dom.GetBoxModel).
type BoxModel struct {
	BackendNodeID int64 `json:"backendNodeId"`
	// Quad is the content box quad [x1,y1, x2,y2, x3,y3, x4,y4]
	// (top-left, top-right, bottom-right, bottom-left).
	Quad []float64 `json:"quad"`
}

// BoxCenter computes the center point of a box model
// (spec L4190: center + +-5px offset).
func (b BoxModel) Center() MousePoint {
	if len(b.Quad) < 4 {
		return MousePoint{}
	}
	// Quad is [x1,y1, x2,y2, x3,y3, x4,y4]
	// Center = average of all 4 corners
	x := (b.Quad[0] + b.Quad[2] + b.Quad[4] + b.Quad[6]) / 4
	y := (b.Quad[1] + b.Quad[3] + b.Quad[5] + b.Quad[7]) / 4
	return MousePoint{X: x, Y: y}
}

// GenerateClickSequenceFromBox generates a click sequence from a
// box model (spec L4190: element-based: backend DOM node ID ->
// dom.GetBoxModel -> center + +-5px offset).
func GenerateClickSequenceFromBox(currentPos MousePoint, box BoxModel, cfg ClickSequenceConfig, rng *rand.Rand) ClickSequence {
	return GenerateClickSequence(currentPos, box.Center(), cfg, rng)
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
