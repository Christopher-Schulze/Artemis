package bridge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/disintegration/imaging"
)

type genericResizeImage struct{ image.Image }

type proceduralResizeImage struct {
	bounds image.Rectangle
	reads  atomic.Int64
}

func (i *proceduralResizeImage) ColorModel() color.Model { return color.NRGBAModel }
func (i *proceduralResizeImage) Bounds() image.Rectangle { return i.bounds }
func (i *proceduralResizeImage) At(x, y int) color.Color {
	i.reads.Add(1)
	return color.NRGBA{uint8(x * 37), uint8(y * 53), uint8((x + y) * 79), 255}
}

func TestModelResizeRejectsUnownedWorkingMemoryBeforePixels(t *testing.T) {
	img := &proceduralResizeImage{bounds: image.Rect(0, 0, 5_000_000, 1)}
	resized, err := NewJPEGPipeline().ResizeForModelContext(t.Context(), img)
	if err == nil || !strings.Contains(err.Error(), "working memory exceeds 512 MiB") || resized != nil || img.reads.Load() != 0 {
		t.Fatalf("unowned resize allocated/scanned beyond working bound: %v reads=%d", err, img.reads.Load())
	}
}

func TestModelResizeDefaultReservationCoversEveryPermittedWorkerCount(t *testing.T) {
	pipeline := NewJPEGPipeline()
	pipeline.MaxWidth, pipeline.MaxHeight = 512, 256
	reserved := pipeline.modelResizeMemoryBytes(4096, 2048)
	for _, workers := range []int{1, 2, 3, 4} {
		pipeline.CPU.Workers = workers
		if needed := pipeline.modelResizeMemoryBytes(4096, 2048); needed > reserved {
			t.Fatalf("default reservation %d misses %d-worker envelope %d", reserved, workers, needed)
		}
	}
}

func resizePixelSources(t *testing.T) []struct {
	name string
	img  image.Image
} {
	t.Helper()
	rect := image.Rect(-3, 5, 34, 28)
	drawable := []struct {
		name string
		img  draw.Image
	}{
		{"nrgba", image.NewNRGBA(rect)}, {"nrgba64", image.NewNRGBA64(rect)},
		{"rgba", image.NewRGBA(rect)}, {"rgba64", image.NewRGBA64(rect)},
		{"gray", image.NewGray(rect)}, {"gray16", image.NewGray16(rect)},
		{"paletted", image.NewPaletted(rect, color.Palette{color.NRGBA{255, 7, 3, 0}, color.NRGBA{31, 219, 87, 128}, color.NRGBA64{10987, 49793, 12997, 65383}})},
	}
	sources := make([]struct {
		name string
		img  image.Image
	}, 0, len(drawable)+8)
	for _, source := range drawable {
		for y := rect.Min.Y; y < rect.Max.Y; y++ {
			for x := rect.Min.X; x < rect.Max.X; x++ {
				seed := (x-rect.Min.X)*43 + (y-rect.Min.Y)*137
				source.img.Set(x, y, color.NRGBA64{uint16(seed * 103), uint16(seed * 271), uint16(seed * 31), uint16(seed * 509)})
			}
		}
		sources = append(sources, struct {
			name string
			img  image.Image
		}{source.name, source.img})
	}
	for _, ratio := range []image.YCbCrSubsampleRatio{image.YCbCrSubsampleRatio444, image.YCbCrSubsampleRatio422, image.YCbCrSubsampleRatio420, image.YCbCrSubsampleRatio440, image.YCbCrSubsampleRatio411, image.YCbCrSubsampleRatio410} {
		source := image.NewYCbCr(rect, ratio)
		for index := range source.Y {
			source.Y[index] = uint8(index * 37)
		}
		for index := range source.Cb {
			source.Cb[index], source.Cr[index] = uint8(index*53), uint8(index*79)
		}
		sources = append(sources, struct {
			name string
			img  image.Image
		}{ratio.String(), source})
	}
	sources = append(sources, struct {
		name string
		img  image.Image
	}{"generic", genericResizeImage{drawable[1].img}})
	for _, format := range []ScreenshotFormat{FormatJPEG, FormatPNG} {
		var encoded bytes.Buffer
		var err error
		if format == FormatJPEG {
			err = jpeg.Encode(&encoded, drawable[0].img, nil)
		} else {
			err = png.Encode(&encoded, drawable[0].img)
		}
		if err != nil {
			t.Fatal(err)
		}
		decoded, _, err := image.Decode(&encoded)
		if err != nil {
			t.Fatal(err)
		}
		sources = append(sources, struct {
			name string
			img  image.Image
		}{"decoded_" + string(format), decoded})
	}
	for _, size := range []image.Point{{1, 257}, {257, 1}} {
		pixels := image.NewNRGBA(image.Rectangle{Min: rect.Min, Max: rect.Min.Add(size)})
		for index := range pixels.Pix {
			pixels.Pix[index] = uint8(index * 37)
		}
		sources = append(sources, struct {
			name string
			img  image.Image
		}{fmt.Sprintf("thin_%dx%d", size.X, size.Y), pixels})
	}
	parent := drawable[0].img.(*image.NRGBA)
	sources = append(sources, struct {
		name string
		img  image.Image
	}{"subimage_stride", parent.SubImage(image.Rect(1, 8, 30, 26))})
	return sources
}

func TestModelResizeMatchesPinnedLanczosPixels(t *testing.T) {
	for _, source := range resizePixelSources(t) {
		for _, size := range []image.Point{{19, 19}, {37, 7}, {7, 23}, {1, 1}, {1, 1024}, {1024, 1}} {
			want := imaging.Fit(source.img, size.X, size.Y, imaging.Lanczos)
			for _, workers := range []int{1, 2, 4} {
				t.Run(fmt.Sprintf("%s_%dx%d_workers%d", source.name, size.X, size.Y, workers), func(t *testing.T) {
					pipeline := NewJPEGPipeline()
					pipeline.MaxWidth, pipeline.MaxHeight, pipeline.CPU.Workers = size.X, size.Y, workers
					got, err := pipeline.ResizeForModelContext(t.Context(), source.img)
					if err != nil {
						t.Fatal(err)
					}
					if source.img.Bounds().Dx() <= size.X && source.img.Bounds().Dy() <= size.Y {
						if got != source.img {
							t.Fatal("fitting image was replaced or copied")
						}
						return
					}
					pixels, ok := got.(*image.NRGBA)
					if !ok || pixels.Bounds() != want.Bounds() || !bytes.Equal(pixels.Pix, want.Pix) {
						t.Fatalf("owned resize differs from pinned filter: type=%T bounds=%v want=%v", got, got.Bounds(), want.Bounds())
					}
				})
			}
		}
	}
}

func BenchmarkModelResizeLanczos(b *testing.B) {
	source := image.NewYCbCr(image.Rect(0, 0, 1280, 640), image.YCbCrSubsampleRatio420)
	for index := range source.Y {
		source.Y[index] = uint8(index * 37)
	}
	for index := range source.Cb {
		source.Cb[index], source.Cr[index] = uint8(index*53), uint8(index*79)
	}
	for _, workers := range []int{0, 1, 2, 4} {
		name := fmt.Sprintf("owned_workers_%d", workers)
		if workers == 0 {
			name = "upstream"
		}
		b.Run(name, func(b *testing.B) {
			pipeline := NewJPEGPipeline()
			pipeline.MaxWidth, pipeline.MaxHeight, pipeline.CPU.Workers = 512, 256, workers
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if workers == 0 {
					runtime.KeepAlive(imaging.Fit(source, 512, 256, imaging.Lanczos))
					continue
				}
				resized, err := pipeline.ResizeForModelContext(b.Context(), source)
				if err != nil {
					b.Fatal(err)
				}
				runtime.KeepAlive(resized)
			}
		})
	}
}

func TestImageWorkerPartitionAndJoin(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	entered, finish := make(chan struct{}, 4), make(chan struct{})
	done := make(chan error, 1)
	var active, peak atomic.Int32
	go func() {
		done <- runImageWorkers(ctx, 4, 10000, nil, func(workCtx context.Context, first, step int) error {
			count := active.Add(1)
			for current := peak.Load(); count > current && !peak.CompareAndSwap(current, count); current = peak.Load() {
			}
			defer active.Add(-1)
			entered <- struct{}{}
			<-finish
			return workCtx.Err()
		})
	}()
	for range 4 {
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			close(finish)
			t.Fatal("actual worker did not enter")
		}
	}
	cancel()
	select {
	case err := <-done:
		close(finish)
		t.Fatalf("returned before active workers finished: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(finish)
	if err := <-done; !errors.Is(err, context.Canceled) || active.Load() != 0 || peak.Load() != 4 {
		t.Fatalf("join or worker bound failed: %v active=%d peak=%d", err, active.Load(), peak.Load())
	}
	counts := make([]atomic.Int32, 37)
	if err := runImageWorkers(t.Context(), 4, len(counts), nil, func(ctx context.Context, first, step int) error {
		for index := first; index < len(counts); index += step {
			counts[index].Add(1)
		}
		return ctx.Err()
	}); err != nil {
		t.Fatal(err)
	}
	for index := range counts {
		if counts[index].Load() != 1 {
			t.Fatalf("line %d processed %d times", index, counts[index].Load())
		}
	}
}

func TestImageWorkerFailureCancelsPeers(t *testing.T) {
	for _, failure := range []string{"error", "panic", "exit"} {
		t.Run(failure, func(t *testing.T) {
			var joined atomic.Int32
			err := runImageWorkers(t.Context(), 2, 2, nil, func(ctx context.Context, first, step int) error {
				defer joined.Add(1)
				if first == 0 {
					switch failure {
					case "error":
						return errors.New("actual worker error")
					case "panic":
						panic("actual worker failure")
					case "exit":
						runtime.Goexit()
					}
				}
				<-ctx.Done()
				return ctx.Err()
			})
			if err == nil || joined.Load() < 1 {
				t.Fatalf("abnormal worker published success: %v joined=%d", err, joined.Load())
			}
		})
	}
}
