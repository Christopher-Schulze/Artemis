package input

import (
	"math"
	"math/rand/v2"
)

// Point is a 2D coordinate for pointer paths.
type Point struct {
	X float64
	Y float64
}

// BezierPath builds a cubic-bezier mouse path between start and end with jitter control points.
func BezierPath(start, end Point, steps int, rng *rand.Rand) []Point {
	if steps <= 1 {
		return []Point{end}
	}
	if rng == nil {
		rng = rand.New(rand.NewPCG(1, 2))
	}
	dx := end.X - start.X
	dy := end.Y - start.Y
	c1 := Point{X: start.X + dx*0.25 + rng.Float64()*20 - 10, Y: start.Y + dy*0.25 + rng.Float64()*20 - 10}
	c2 := Point{X: start.X + dx*0.75 + rng.Float64()*20 - 10, Y: start.Y + dy*0.75 + rng.Float64()*20 - 10}
	out := make([]Point, 0, steps)
	for i := 0; i < steps; i++ {
		t := float64(i) / float64(steps-1)
		out = append(out, cubicBezier(start, c1, c2, end, t))
	}
	return out
}

func cubicBezier(p0, p1, p2, p3 Point, t float64) Point {
	u := 1 - t
	a := u * u * u
	b := 3 * u * u * t
	c := 3 * u * t * t
	d := t * t * t
	return Point{
		X: a*p0.X + b*p1.X + c*p2.X + d*p3.X,
		Y: a*p0.Y + b*p1.Y + c*p2.Y + d*p3.Y,
	}
}

// ClickGaussianOffset returns a click coordinate offset using gaussian noise (sigma in px).
func ClickGaussianOffset(sigma float64, rng *rand.Rand) (dx, dy float64) {
	if sigma <= 0 {
		sigma = 1.5
	}
	if rng == nil {
		rng = rand.New(rand.NewPCG(3, 4))
	}
	return rng.NormFloat64() * sigma, rng.NormFloat64() * sigma
}

// PathLength returns total Euclidean length of a path.
func PathLength(path []Point) float64 {
	if len(path) < 2 {
		return 0
	}
	var sum float64
	for i := 1; i < len(path); i++ {
		sum += math.Hypot(path[i].X-path[i-1].X, path[i].Y-path[i-1].Y)
	}
	return sum
}

// HoverDwell returns a Gaussian-distributed hover delay before click (mu=200ms, sigma=100ms).
// Humans hover before clicking; linear fixed delays are detectable.
func HoverDwell(rng *rand.Rand) float64 {
	if rng == nil {
		rng = rand.New(rand.NewPCG(5, 6))
	}
	d := 200 + rng.NormFloat64()*100
	if d < 50 {
		d = 50
	}
	return d
}

// EaseInOutScroll returns scroll offsets using easeInOut cubic easing.
// easeInOut(t) = t<0.5 ? 2t^2 : -1+(4-2t)*t. Linear scroll is bot-detectable.
// totalDistance is the full scroll distance; steps is the number of scroll increments.
func EaseInOutScroll(totalDistance float64, steps int) []float64 {
	if steps <= 0 {
		return []float64{totalDistance}
	}
	out := make([]float64, steps)
	var prev float64
	for i := 0; i < steps; i++ {
		t := float64(i) / float64(steps-1)
		var eased float64
		if t < 0.5 {
			eased = 2 * t * t
		} else {
			eased = -1 + (4-2*t)*t
		}
		pos := eased * totalDistance
		out[i] = pos - prev
		prev = pos
	}
	return out
}
