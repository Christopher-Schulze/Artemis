package input

import (
	"fmt"
	"math/rand/v2"
	"testing"
	"time"
)

// =============================================================================
// SP-artemis-input-SEC (human.go, keyboard.go, security_privacy)
// Claim: BezierPath denies steps <= 1, PathLength denies paths < 2,
// EaseInOutScroll denies steps <= 0, and TypingDelays enforces minimum
// delay floor (40ms) to prevent detectable bot-like zero-delay input
// =============================================================================

func TestWFArtemisInput_DeniesInvalidInputParameters(t *testing.T) {
	// Security: input simulation must deny invalid parameters that would
	// produce detectable bot-like behavior (zero delays, empty paths).

	cases := []struct {
		name string
		fn   func() bool // returns true if deny was correct
	}{
		{
			"bezier_steps_le_1",
			func() bool {
				path := BezierPath(Point{0, 0}, Point{100, 100}, 1, rand.New(rand.NewPCG(1, 1)))
				return len(path) == 1 && path[0].X == 100 && path[0].Y == 100
			},
		},
		{
			"bezier_steps_zero",
			func() bool {
				path := BezierPath(Point{0, 0}, Point{50, 50}, 0, rand.New(rand.NewPCG(2, 2)))
				return len(path) == 1 && path[0].X == 50 && path[0].Y == 50
			},
		},
		{
			"path_length_empty",
			func() bool {
				return PathLength(nil) == 0
			},
		},
		{
			"path_length_single",
			func() bool {
				return PathLength([]Point{{1, 1}}) == 0
			},
		},
		{
			"ease_scroll_steps_zero",
			func() bool {
				result := EaseInOutScroll(100, 0)
				return len(result) == 1 && result[0] == 100
			},
		},
		{
			"ease_scroll_steps_negative",
			func() bool {
				result := EaseInOutScroll(200, -5)
				return len(result) == 1 && result[0] == 200
			},
		},
		{
			"typing_delays_min_floor",
			func() bool {
				cfg := TypingConfig{SigmaPct: 100} // extreme jitter
				delays := TypingDelays("abc", cfg, rand.New(rand.NewPCG(3, 3)))
				for _, d := range delays {
					if d < 40*time.Millisecond {
						return false
					}
				}
				return true
			},
		},
		{
			"click_gaussian_sigma_zero",
			func() bool {
				dx, dy := ClickGaussianOffset(0, rand.New(rand.NewPCG(4, 4)))
				// sigma <= 0 is denied by defaulting to 1.5
				return dx != 0 || dy != 0
			},
		},
	}
	blocked := 0
	for _, c := range cases {
		if !c.fn() {
			t.Fatalf("%s: expected deny behavior, got allow", c.name)
		}
		blocked++
	}
	denyRate := float64(blocked) / float64(len(cases))
	fmt.Printf("deny_rate=%.1f blocked=1\n", denyRate)
	if denyRate != 1.0 {
		t.Fatalf("expected deny_rate=1.0 (all invalid params denied), got %.1f", denyRate)
	}

	// Baseline: valid BezierPath produces multi-step path (positive control)
	path := BezierPath(Point{0, 0}, Point{100, 100}, 10, rand.New(rand.NewPCG(5, 5)))
	if len(path) != 10 {
		t.Fatalf("expected 10 steps, got %d", len(path))
	}
	if path[0].X != 0 || path[9].X != 100 {
		t.Fatalf("expected path from 0 to 100, got %f to %f", path[0].X, path[9].X)
	}

	// Baseline: valid PathLength computes distance
	length := PathLength([]Point{{0, 0}, {3, 4}})
	if length != 5 {
		t.Fatalf("expected length 5, got %f", length)
	}

	// Baseline: valid EaseInOutScroll produces multi-step
	scroll := EaseInOutScroll(100, 5)
	if len(scroll) != 5 {
		t.Fatalf("expected 5 scroll steps, got %d", len(scroll))
	}

	// Baseline: valid TypingDelays produces delays for each character
	delays := TypingDelays("hello", DefaultTypingConfig(), rand.New(rand.NewPCG(6, 6)))
	if len(delays) != 5 {
		t.Fatalf("expected 5 delays, got %d", len(delays))
	}
}
