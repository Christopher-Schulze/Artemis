package observe

import (
	"image"
	"image/color"
	"image/draw"
	"testing"
)

func filledImage(width, height int, fill color.Color) *image.RGBA {
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(fill), image.Point{}, draw.Src)
	return canvas
}

func TestCompareImagesIsBoundedAndLocatesChanges(t *testing.T) {
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	before := filledImage(1000, 600, white)
	after := filledImage(1000, 600, white)
	if change := CompareImages(before, after, image.Rectangle{}); change.Changed() || change.Bounds != nil || change.Cells != MaxImageChangeGrid*MaxImageChangeGrid {
		t.Fatalf("identical images reported change or an unbounded grid: %+v", change)
	}
	// Noise below the channel tolerance is not a change.
	draw.Draw(after, image.Rect(0, 0, 1000, 600), image.NewUniform(color.RGBA{R: 240, G: 245, B: 250, A: 255}), image.Point{}, draw.Src)
	if change := CompareImages(before, after, image.Rectangle{}); change.Changed() {
		t.Fatalf("compression noise counted as change: %+v", change)
	}
	draw.Draw(after, image.Rect(0, 0, 1000, 600), image.NewUniform(white), image.Point{}, draw.Src)
	draw.Draw(after, image.Rect(400, 200, 500, 300), image.NewUniform(color.Black), image.Point{}, draw.Src)
	change := CompareImages(before, after, image.Rectangle{})
	if !change.Changed() || change.Bounds == nil || !change.Bounds.Overlaps(image.Rect(400, 200, 500, 300)) || !image.Rect(390, 190, 510, 310).Intersect(*change.Bounds).Eq(*change.Bounds) {
		t.Fatalf("change not located at the painted square: %+v", change)
	}
	if change.Ratio <= 0.01 || change.Ratio >= 0.03 {
		t.Fatalf("change ratio %.4f does not reflect a 1.7%% region", change.Ratio)
	}
	if region := CompareImages(before, after, image.Rect(0, 0, 100, 100)); region.Changed() || region.Cells != 100*100 {
		t.Fatalf("unchanged region reported change: %+v", region)
	}
	if resized := CompareImages(before, filledImage(999, 600, white), image.Rectangle{}); !resized.Resized || resized.Ratio != 1 {
		t.Fatalf("different dimensions were compared pixelwise: %+v", resized)
	}
	offset := after.SubImage(image.Rect(400, 200, 450, 250))
	if sub := CompareImages(before.SubImage(image.Rect(400, 200, 450, 250)), offset, image.Rectangle{}); sub.Ratio != 1 {
		t.Fatalf("sub-images were not compared at their own origins: %+v", sub)
	}
}
