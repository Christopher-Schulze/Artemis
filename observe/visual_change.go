package observe

import (
	"image"
)

// visual_change.go measures how much of a screenshot changed (spec L4264,
// reference-bound visual grounding). The comparison samples a bounded grid,
// so its cost and output stay fixed regardless of image size. It complements
// and never replaces the semantic AX snapshot diff.

// MaxImageChangeGrid bounds the sampled cells per axis.
const MaxImageChangeGrid = 128

// imageChannelTolerance absorbs JPEG and anti-aliasing noise per 8-bit channel.
const imageChannelTolerance = 24

// ImageChange is the bounded change summary between two images. Bounds is
// the changed region in pixels of the after image, nil when nothing changed.
type ImageChange struct {
	Width        int              `json:"width"`
	Height       int              `json:"height"`
	Cells        int              `json:"cells"`
	ChangedCells int              `json:"changedCells"`
	Ratio        float64          `json:"ratio"`
	Bounds       *image.Rectangle `json:"bounds,omitempty"`
	// Resized reports different dimensions; the whole image counts changed.
	Resized bool `json:"resized,omitempty"`
}

// Changed reports whether any sampled cell differs.
func (c ImageChange) Changed() bool {
	return c.ChangedCells > 0
}

// CompareImages samples region (or the whole image when empty) of before and
// after on a grid of at most MaxImageChangeGrid cells per axis.
func CompareImages(before, after image.Image, region image.Rectangle) ImageChange {
	afterBounds := after.Bounds()
	if before.Bounds().Size() != afterBounds.Size() {
		bounds := afterBounds
		return ImageChange{Width: afterBounds.Dx(), Height: afterBounds.Dy(), Cells: 1, ChangedCells: 1, Ratio: 1, Bounds: &bounds, Resized: true}
	}
	if region.Empty() {
		region = image.Rect(0, 0, afterBounds.Dx(), afterBounds.Dy())
	}
	region = region.Intersect(image.Rect(0, 0, afterBounds.Dx(), afterBounds.Dy()))
	change := ImageChange{Width: afterBounds.Dx(), Height: afterBounds.Dy()}
	if region.Empty() {
		return change
	}
	columns, rows := min(region.Dx(), MaxImageChangeGrid), min(region.Dy(), MaxImageChangeGrid)
	var changed image.Rectangle
	for row := 0; row < rows; row++ {
		y := region.Min.Y + (2*row+1)*region.Dy()/(2*rows)
		for column := 0; column < columns; column++ {
			x := region.Min.X + (2*column+1)*region.Dx()/(2*columns)
			change.Cells++
			if samePixel(before, after, x, y) {
				continue
			}
			change.ChangedCells++
			cell := image.Rect(region.Min.X+column*region.Dx()/columns, region.Min.Y+row*region.Dy()/rows, region.Min.X+(column+1)*region.Dx()/columns, region.Min.Y+(row+1)*region.Dy()/rows)
			changed = changed.Union(cell)
		}
	}
	change.Ratio = float64(change.ChangedCells) / float64(change.Cells)
	if change.ChangedCells > 0 {
		change.Bounds = &changed
	}
	return change
}

func samePixel(before, after image.Image, x, y int) bool {
	beforeMin, afterMin := before.Bounds().Min, after.Bounds().Min
	r1, g1, b1, a1 := before.At(beforeMin.X+x, beforeMin.Y+y).RGBA()
	r2, g2, b2, a2 := after.At(afterMin.X+x, afterMin.Y+y).RGBA()
	return channelClose(r1, r2) && channelClose(g1, g2) && channelClose(b1, b2) && channelClose(a1, a2)
}

func channelClose(left, right uint32) bool {
	left, right = left>>8, right>>8
	if left > right {
		return left-right <= imageChannelTolerance
	}
	return right-left <= imageChannelTolerance
}
