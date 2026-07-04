package input

import (
	"math/rand"
	"testing"
	"time"
)

func TestCubicBezierCurve(t *testing.T) {
	p0 := MousePoint{X: 0, Y: 0}
	p1 := MousePoint{X: 33, Y: 10}
	p2 := MousePoint{X: 66, Y: 10}
	p3 := MousePoint{X: 100, Y: 0}

	// At t=0, should be p0
	pt := CubicBezierCurve(p0, p1, p2, p3, 0)
	if pt.X != 0 || pt.Y != 0 {
		t.Errorf("t=0: got (%f,%f), want (0,0)", pt.X, pt.Y)
	}

	// At t=1, should be p3
	pt = CubicBezierCurve(p0, p1, p2, p3, 1)
	if pt.X != 100 || pt.Y != 0 {
		t.Errorf("t=1: got (%f,%f), want (100,0)", pt.X, pt.Y)
	}

	// At t=0.5, should be somewhere in the middle
	pt = CubicBezierCurve(p0, p1, p2, p3, 0.5)
	if pt.X <= 0 || pt.X >= 100 {
		t.Errorf("t=0.5: X=%f should be between 0 and 100", pt.X)
	}
}

func TestGenerateMousePathCubic(t *testing.T) {
	start := MousePoint{X: 0, Y: 0}
	end := MousePoint{X: 500, Y: 300}
	cfg := DefaultMouseMoveConfig()
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	path := GenerateMousePath(start, end, cfg, rng)

	if len(path.Points) != cfg.Steps {
		t.Errorf("points len = %d, want %d", len(path.Points), cfg.Steps)
	}
	// First point should be near start
	if path.Points[0].X > 10 || path.Points[0].Y > 10 {
		t.Errorf("first point (%f,%f) should be near start (0,0)", path.Points[0].X, path.Points[0].Y)
	}
	// Last point should be exactly end
	last := path.Points[len(path.Points)-1]
	if last.X != end.X || last.Y != end.Y {
		t.Errorf("last point (%f,%f), want (%f,%f)", last.X, last.Y, end.X, end.Y)
	}
	// Duration should be >= 100ms (spec: 100ms base)
	if path.Duration < 100*time.Millisecond {
		t.Errorf("duration = %v, want >= 100ms", path.Duration)
	}
}

func TestGenerateMousePathStepsClamped(t *testing.T) {
	start := MousePoint{X: 0, Y: 0}
	end := MousePoint{X: 100, Y: 100}
	rng := rand.New(rand.NewSource(42))

	// Too few steps: should clamp to 5
	cfg := MouseMoveConfig{Steps: 2, Jitter: 1.0}
	path := GenerateMousePath(start, end, cfg, rng)
	if len(path.Points) != 5 {
		t.Errorf("steps=2: got %d points, want 5 (clamped)", len(path.Points))
	}

	// Too many steps: should clamp to 30
	cfg = MouseMoveConfig{Steps: 100, Jitter: 1.0}
	path = GenerateMousePath(start, end, cfg, rng)
	if len(path.Points) != 30 {
		t.Errorf("steps=100: got %d points, want 30 (clamped)", len(path.Points))
	}
}

func TestComputeCubicDuration(t *testing.T) {
	cfg := DefaultMouseMoveConfig()

	// 0 distance: should be 100ms base
	dur := computeCubicDuration(0, cfg)
	if dur < 100*time.Millisecond {
		t.Errorf("0 distance: duration = %v, want >= 100ms", dur)
	}

	// 2000px distance: should be 100ms + 200ms = 300ms
	dur = computeCubicDuration(2000, cfg)
	if dur < 300*time.Millisecond {
		t.Errorf("2000px: duration = %v, want >= 300ms", dur)
	}
}

func TestFrameInterval(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	interval := FrameInterval(rng)
	if interval < 16*time.Millisecond || interval > 23*time.Millisecond {
		t.Errorf("FrameInterval = %v, want 16-23ms", interval)
	}

	// nil rng should return 16ms
	interval = FrameInterval(nil)
	if interval != 16*time.Millisecond {
		t.Errorf("nil rng: FrameInterval = %v, want 16ms", interval)
	}
}

func TestDefaultClickSequenceConfig(t *testing.T) {
	cfg := DefaultClickSequenceConfig()
	if cfg.StartOffsetMinX != -50 {
		t.Errorf("StartOffsetMinX = %f, want -50", cfg.StartOffsetMinX)
	}
	if cfg.StartOffsetMaxX != 150 {
		t.Errorf("StartOffsetMaxX = %f, want 150", cfg.StartOffsetMaxX)
	}
	if cfg.MoveThreshold != 30 {
		t.Errorf("MoveThreshold = %f, want 30", cfg.MoveThreshold)
	}
	if cfg.PreClickDelayMin != 50*time.Millisecond {
		t.Errorf("PreClickDelayMin = %v, want 50ms", cfg.PreClickDelayMin)
	}
	if cfg.PreClickDelayMax != 199*time.Millisecond {
		t.Errorf("PreClickDelayMax = %v, want 199ms", cfg.PreClickDelayMax)
	}
	if cfg.HoldDurationMin != 30*time.Millisecond {
		t.Errorf("HoldDurationMin = %v, want 30ms", cfg.HoldDurationMin)
	}
	if cfg.HoldDurationMax != 119*time.Millisecond {
		t.Errorf("HoldDurationMax = %v, want 119ms", cfg.HoldDurationMax)
	}
	if cfg.ReleaseJitter != 1.0 {
		t.Errorf("ReleaseJitter = %f, want 1.0", cfg.ReleaseJitter)
	}
	if cfg.TargetOffsetMax != 5.0 {
		t.Errorf("TargetOffsetMax = %f, want 5.0", cfg.TargetOffsetMax)
	}
}

func TestGenerateClickSequence(t *testing.T) {
	currentPos := MousePoint{X: 100, Y: 100}
	boxCenter := MousePoint{X: 500, Y: 300}
	cfg := DefaultClickSequenceConfig()
	rng := rand.New(rand.NewSource(42))

	seq := GenerateClickSequence(currentPos, boxCenter, cfg, rng)

	// Start point should be offset from box center
	if seq.StartPoint.X < boxCenter.X-50 || seq.StartPoint.X > boxCenter.X+150 {
		t.Errorf("StartPoint.X = %f, should be in [-50, +150] from center", seq.StartPoint.X)
	}

	// Target point should be near box center +-5px
	if absf(seq.TargetPoint.X-boxCenter.X) > 5 {
		t.Errorf("TargetPoint.X = %f, should be within +-5px of center %f", seq.TargetPoint.X, boxCenter.X)
	}

	// Should have a move path since distance > 30px
	if seq.MovePath == nil {
		t.Error("MovePath should not be nil (distance > 30px)")
	}

	// Pre-click delay should be in 50-199ms range
	if seq.PreClickDelay < 50*time.Millisecond || seq.PreClickDelay > 199*time.Millisecond {
		t.Errorf("PreClickDelay = %v, should be 50-199ms", seq.PreClickDelay)
	}

	// Hold duration should be in 30-119ms range
	if seq.HoldDuration < 30*time.Millisecond || seq.HoldDuration > 119*time.Millisecond {
		t.Errorf("HoldDuration = %v, should be 30-119ms", seq.HoldDuration)
	}

	// Release point should be near target +-1px
	if absf(seq.ReleasePoint.X-seq.TargetPoint.X) > 1.0 {
		t.Errorf("ReleasePoint.X = %f, should be within +-1px of target %f", seq.ReleasePoint.X, seq.TargetPoint.X)
	}

	// Button should be left
	if seq.Button != "left" {
		t.Errorf("Button = %s, want left", seq.Button)
	}
}

func TestGenerateClickSequenceNoMoveWhenClose(t *testing.T) {
	currentPos := MousePoint{X: 100, Y: 100}
	boxCenter := MousePoint{X: 105, Y: 105} // only ~7px away
	cfg := DefaultClickSequenceConfig()
	rng := rand.New(rand.NewSource(42))

	seq := GenerateClickSequence(currentPos, boxCenter, cfg, rng)
	if seq.MovePath != nil {
		t.Error("MovePath should be nil when distance <= 30px")
	}
}

func TestBoxModelCenter(t *testing.T) {
	box := BoxModel{
		BackendNodeID: 42,
		Quad:          []float64{0, 0, 100, 0, 100, 100, 0, 100}, // 100x100 box
	}
	center := box.Center()
	if center.X != 50 || center.Y != 50 {
		t.Errorf("Center = (%f,%f), want (50,50)", center.X, center.Y)
	}
}

func TestBoxModelCenterEmpty(t *testing.T) {
	box := BoxModel{BackendNodeID: 1}
	center := box.Center()
	if center.X != 0 || center.Y != 0 {
		t.Errorf("empty quad Center = (%f,%f), want (0,0)", center.X, center.Y)
	}
}

func TestGenerateClickSequenceFromBox(t *testing.T) {
	currentPos := MousePoint{X: 0, Y: 0}
	box := BoxModel{
		BackendNodeID: 42,
		Quad:          []float64{200, 200, 300, 200, 300, 300, 200, 300},
	}
	cfg := DefaultClickSequenceConfig()
	rng := rand.New(rand.NewSource(42))

	seq := GenerateClickSequenceFromBox(currentPos, box, cfg, rng)
	// Target should be near box center (250, 250) +-5px
	if absf(seq.TargetPoint.X-250) > 10 {
		t.Errorf("TargetPoint.X = %f, should be near 250", seq.TargetPoint.X)
	}
}

func absf(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
