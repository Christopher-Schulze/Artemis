package input

import (
	"math/rand/v2"
	"testing"
)

func TestBezierPathAndGaussianOffset(t *testing.T) {
	path := BezierPath(Point{X: 0, Y: 0}, Point{X: 100, Y: 50}, 10, rand.New(rand.NewPCG(1, 2)))
	if len(path) != 10 || PathLength(path) <= 0 {
		t.Fatalf("path=%d", len(path))
	}
	dx, dy := ClickGaussianOffset(2, rand.New(rand.NewPCG(3, 4)))
	if dx == 0 && dy == 0 {
		t.Fatal("expected offset")
	}
}

func TestHoverDwellGaussian(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 8))
	var sum float64
	const n = 1000
	for i := 0; i < n; i++ {
		d := HoverDwell(rng)
		if d < 50 {
			t.Fatalf("dwell=%f below floor 50ms", d)
		}
		sum += d
	}
	mean := sum / n
	if mean < 150 || mean > 250 {
		t.Fatalf("mean dwell=%f, expected ~200ms", mean)
	}
}

func TestEaseInOutScroll(t *testing.T) {
	offsets := EaseInOutScroll(1000, 10)
	if len(offsets) != 10 {
		t.Fatalf("offsets=%d", len(offsets))
	}
	var total float64
	for _, o := range offsets {
		total += o
	}
	if total < 999 || total > 1001 {
		t.Fatalf("total scroll=%f, expected ~1000", total)
	}
	// First half should have smaller increments (ease-in), second half larger
	firstHalf := offsets[0] + offsets[1] + offsets[2]
	lastHalf := offsets[7] + offsets[8] + offsets[9]
	if firstHalf >= lastHalf {
		t.Fatalf("easeInOut not working: first=%f last=%f", firstHalf, lastHalf)
	}
}
