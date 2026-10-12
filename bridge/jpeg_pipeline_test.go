package bridge

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/jpeg"
	"testing"
)

func TestJPEGScreenshotPipeline(t *testing.T) {
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 40, 30))
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	b64 := base64.StdEncoding.EncodeToString(buf.Bytes())
	p := NewJPEGPipeline()
	dec, path, err := p.DecodeScreenshot(b64)
	if err != nil {
		t.Fatal(err)
	}
	if path == "" {
		t.Fatal("expected decode path label")
	}
	small := p.ResizeForModel(dec)
	if small.Bounds().Dx() > p.MaxWidth+1 {
		t.Fatalf("resize failed: %v", small.Bounds())
	}
}
