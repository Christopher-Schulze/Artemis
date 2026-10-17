package observe

import (
	"math"
	"testing"
)

func TestScreenshotGeometryRejectsInvalidAndClipsActualBounds(t *testing.T) {
	for _, test := range []struct {
		name  string
		quad  []float64
		valid bool
	}{
		{"border", []float64{10, 20, 110, 20, 110, 70, 10, 70}, true},
		{"rotated_element_bounds", []float64{10, 20, 90, 10, 110, 60, 30, 70}, true},
		{"incomplete", []float64{10, 20}, false},
		{"nonfinite", []float64{math.NaN(), 20, 110, 20, 110, 70, 10, 70}, false},
		{"empty", []float64{10, 20, 10, 20, 10, 70, 10, 70}, false},
		{"diagonal_zero_area", []float64{10, 20, 30, 40, 50, 60, 70, 80}, false},
		{"overflow", []float64{-math.MaxFloat64, 20, math.MaxFloat64, 20, math.MaxFloat64, 70, -math.MaxFloat64, 70}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			rect, err := screenshotQuadBounds(test.quad)
			if (err == nil) != test.valid || test.valid && (rect.X != 10 || rect.Width != 100) {
				t.Fatalf("border geometry changed or invalid input admitted: %+v %v", rect, err)
			}
		})
	}
	for _, test := range []struct {
		name  string
		rect  Rect
		want  Rect
		valid bool
	}{
		{"intersect_frame", Rect{X: 90, Y: 60, Width: 50, Height: 40}, Rect{X: 90, Y: 60, Width: 20, Height: 10}, true},
		{"outside_frame", Rect{X: 120, Y: 80, Width: 50, Height: 40}, Rect{}, false},
		{"nan_position", Rect{X: math.NaN(), Width: 10, Height: 10}, Rect{}, false},
		{"infinite_extent", Rect{Width: math.Inf(1), Height: 10}, Rect{}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := intersectScreenshotRects(test.rect, Rect{X: 10, Y: 20, Width: 100, Height: 50})
			if (err == nil) != test.valid || got != test.want {
				t.Fatalf("frame clipping changed: %+v %v want %+v", got, err, test.want)
			}
		})
	}
}
