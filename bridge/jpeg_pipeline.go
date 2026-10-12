package bridge

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/jpeg"
	"runtime"

	"github.com/Christopher-Schulze/Artemis/platform"
)

// ScreenshotFormat is CDP capture format.
type ScreenshotFormat string

const (
	FormatJPEG ScreenshotFormat = "jpeg"
	FormatPNG  ScreenshotFormat = "png"
)

// JPEGPipeline decodes CDP JPEG screenshots for LLM resize path (spec P5.6).
type JPEGPipeline struct {
	Caps      platform.PlatformCapabilities
	Quality   int
	MaxWidth  int
	MaxHeight int
}

// NewJPEGPipeline creates a pipeline with platform-aware decode path selection.
func NewJPEGPipeline() *JPEGPipeline {
	return &JPEGPipeline{
		Caps:      platform.Detect(),
		Quality:   80,
		MaxWidth:  1024,
		MaxHeight: 1024,
	}
}

// DecodePath returns the decode path label for metrics.
func (p *JPEGPipeline) DecodePath() string {
	if !p.Caps.HasHWJPEGDecode {
		return "sw_go_image"
	}
	if runtime.GOOS == "darwin" {
		return "hw_videotoolbox"
	}
	return "hw_libjpeg_turbo"
}

// DecodeScreenshot decodes base64 JPEG bytes from CDP captureScreenshot.
func (p *JPEGPipeline) DecodeScreenshot(b64 string) (image.Image, string, error) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, "", fmt.Errorf("jpeg pipeline: decode b64: %w", err)
	}
	img, err := jpeg.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, "", fmt.Errorf("jpeg pipeline: decode jpeg: %w", err)
	}
	return img, p.DecodePath(), nil
}

// ResizeForModel scales img down to model input bounds.
func (p *JPEGPipeline) ResizeForModel(img image.Image) image.Image {
	if img == nil {
		return nil
	}
	b := img.Bounds()
	if b.Dx() <= p.MaxWidth && b.Dy() <= p.MaxHeight {
		return img
	}
	ratio := float64(p.MaxWidth) / float64(b.Dx())
	if rh := float64(p.MaxHeight) / float64(b.Dy()); rh < ratio {
		ratio = rh
	}
	w := int(float64(b.Dx()) * ratio)
	h := int(float64(b.Dy()) * ratio)
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			sx := b.Min.X + x*b.Dx()/w
			sy := b.Min.Y + y*b.Dy()/h
			dst.Set(x, y, img.At(sx, sy))
		}
	}
	return dst
}
