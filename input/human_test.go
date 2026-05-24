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
