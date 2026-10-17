package bridge

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/internal/jpegcodec"
	"github.com/Christopher-Schulze/Artemis/platform"
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
	expectedPath := "sw_go_image"
	if codec := jpegcodec.Probe(); codec.Available {
		expectedPath = codec.Path
	}
	if path != expectedPath || dec.Bounds() != img.Bounds() {
		t.Fatalf("incorrect decoder or dimensions: %s %v", path, dec.Bounds())
	}
	p.Caps.HasHWJPEGDecode = true
	_, path, err = p.DecodeScreenshot(b64)
	if err != nil || path != expectedPath {
		t.Fatalf("caller capability forged actual decode path: %s %v", path, err)
	}
	small := p.ResizeForModel(dec)
	if small.Bounds().Dx() > p.MaxWidth+1 {
		t.Fatalf("resize failed: %v", small.Bounds())
	}
}

func TestJPEGDecodeCarriesOperationEvidenceAndCancellation(t *testing.T) {
	var raw bytes.Buffer
	if err := jpeg.Encode(&raw, image.NewRGBA(image.Rect(0, 0, 40, 30)), nil); err != nil {
		t.Fatal(err)
	}
	pipeline := NewJPEGPipeline()
	result, err := pipeline.DecodeJPEGResult(t.Context(), raw.Bytes())
	if err != nil || result.Image == nil || result.Acceleration.Source == "" || result.Acceleration.Reason == "" {
		t.Fatalf("actual decode lost operation evidence: %+v %v", result, err)
	}
	if result.Path == "sw_go_image" {
		if result.Acceleration.State != platform.StateUnavailable && result.Acceleration.State != platform.StateDegraded && result.Acceleration.State != platform.StateUnknown {
			t.Fatalf("software claimed accelerated execution: %+v", result.Acceleration)
		}
	} else if result.Acceleration.State != platform.StateAvailable || result.Acceleration.Source != result.Path || result.Acceleration.Value != 1 {
		t.Fatalf("native decode has no matching completed execution proof: %+v", result)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, ctx := range []context.Context{nil, ctx} {
		rejected, err := pipeline.DecodeJPEGResult(ctx, raw.Bytes())
		if err == nil || rejected.Image != nil || rejected.Path != "" {
			t.Fatalf("invalid context manufactured success: %+v %v", rejected, err)
		}
	}
}

func TestJPEGProgressiveDecodeAndScanLimitBeforeFallback(t *testing.T) {
	// Reuse the Go distribution's real progressive JPEG without exporting or
	// duplicating a fixture tree. The same file exercises Go's own JPEG tests.
	raw, err := os.ReadFile(filepath.Join(runtime.GOROOT(), "src", "image", "testdata", "video-001.progressive.jpeg"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := jpeg.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	result, err := NewJPEGPipeline().DecodeJPEGResult(t.Context(), raw)
	if err != nil || result.Image == nil || result.Image.Bounds() != want.Bounds() || result.Path == "" {
		t.Fatalf("real progressive JPEG rejected or mislabeled: %+v %v", result, err)
	}
	var baseline bytes.Buffer
	if err := jpeg.Encode(&baseline, image.NewRGBA(image.Rect(0, 0, 32, 24)), nil); err != nil {
		t.Fatal(err)
	}
	overBudget := append([]byte(nil), baseline.Bytes()[:baseline.Len()-2]...)
	for i := 0; i < 100; i++ {
		overBudget = append(overBudget, 0xff, 0xda, 0, 2, 0)
	}
	overBudget = append(overBudget, 0xff, 0xd9)
	if _, err := jpeg.DecodeConfig(bytes.NewReader(overBudget)); err != nil {
		t.Fatal(err)
	}
	rejected, err := NewJPEGPipeline().DecodeJPEGResult(t.Context(), overBudget)
	if err == nil || !strings.Contains(err.Error(), "100-scan") || rejected.Image != nil || rejected.Path != "" {
		t.Fatalf("fallback bypassed pre-decode scan budget: %+v %v", rejected, err)
	}
}

func TestJPEGNativeWarningFallsBackToActualGoPixels(t *testing.T) {
	var encoded bytes.Buffer
	source := image.NewNRGBA(image.Rect(0, 0, 64, 32))
	for y := range 32 {
		for x := range 64 {
			source.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 3), G: uint8(y * 7), B: 91, A: 255})
		}
	}
	if err := jpeg.Encode(&encoded, source, nil); err != nil {
		t.Fatal(err)
	}
	raw := append([]byte(nil), encoded.Bytes()[:encoded.Len()-2]...)
	raw = append(raw, make([]byte, 32)...)
	raw = append(raw, 0xff, 0xd9)
	want, err := jpeg.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("Go decoder did not admit the actual warning fixture: %v", err)
	}
	pipeline := NewJPEGPipeline()
	result, err := pipeline.DecodeJPEGResult(t.Context(), raw)
	if err != nil || result.Image == nil || result.Image.Bounds() != want.Bounds() {
		t.Fatalf("warning fixture lost real pixels: %+v %v", result, err)
	}
	if codec := jpegcodec.Probe(); codec.Available && codec.Path == "hw_libjpeg_turbo" {
		native, path, nativeErr := jpegcodec.Decode(t.Context(), raw)
		if nativeErr == nil || native != nil || path != "" || result.Path != "sw_go_image" || result.Acceleration.State != platform.StateDegraded || result.Acceleration.Value != 0 {
			t.Fatalf("failed admitted native decode forged success or lost Go fallback: native=%v/%q/%v result=%+v", native, path, nativeErr, result)
		}
		t.Logf("admitted native decoder rejected the real stream: %v; actual fallback=%s state=%s", nativeErr, result.Path, result.Acceleration.State)
	}
	for y := range 32 {
		for x := range 64 {
			got := color.NRGBAModel.Convert(result.Image.At(x, y)).(color.NRGBA)
			expected := color.NRGBAModel.Convert(want.At(x, y)).(color.NRGBA)
			tolerance := 0
			if result.Path != "sw_go_image" {
				tolerance = 3
			}
			for _, channel := range []struct{ got, want uint8 }{{got.R, expected.R}, {got.G, expected.G}, {got.B, expected.B}, {got.A, expected.A}} {
				delta := int(channel.got) - int(channel.want)
				if delta < -tolerance || delta > tolerance {
					t.Fatalf("actual decoded pixels differ at %d,%d: %+v != %+v", x, y, got, expected)
				}
			}
		}
	}
}

func TestJPEGResizeRejectsTypedNilAndEmptyImages(t *testing.T) {
	for _, tc := range []struct {
		name string
		img  image.Image
	}{
		{"nil interface", nil}, {"nil RGBA", (*image.RGBA)(nil)},
		{"nil NRGBA", (*image.NRGBA)(nil)}, {"nil YCbCr", (*image.YCbCr)(nil)},
		{"empty pixels", image.NewRGBA(image.Rect(1, 1, 1, 1))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := NewJPEGPipeline().ResizeForModel(tc.img); got != nil {
				t.Fatalf("invalid image admitted: %v", got)
			}
		})
	}
}

func TestJPEGRejectsInvalidAndOversizedPayloadWithoutDecodeLabel(t *testing.T) {
	var valid bytes.Buffer
	if err := jpeg.Encode(&valid, image.NewRGBA(image.Rect(0, 0, 40, 30)), nil); err != nil {
		t.Fatal(err)
	}
	huge := append([]byte(nil), valid.Bytes()...)
	sof := bytes.Index(huge, []byte{0xff, 0xc0})
	if sof < 0 {
		t.Fatal("JPEG fixture has no baseline frame header")
	}
	for index := sof + 5; index < sof+9; index++ {
		huge[index] = 0xff
	}
	config, err := jpeg.DecodeConfig(bytes.NewReader(huge))
	if err != nil || config.Width != 65535 || config.Height != 65535 {
		t.Fatalf("adversarial frame header is not valid: %+v %v", config, err)
	}
	for _, tc := range []struct{ name, payload string }{
		{"empty", ""}, {"malformed base64", "%%%"},
		{"non JPEG", base64.StdEncoding.EncodeToString([]byte("not an image"))},
		{"pixel budget", base64.StdEncoding.EncodeToString(huge)},
		{"encoded budget", strings.Repeat("A", JPEGEncodedLimit+1)},
		{"truncated scan", base64.StdEncoding.EncodeToString(valid.Bytes()[:len(valid.Bytes())-20])},
	} {
		t.Run(tc.name, func(t *testing.T) {
			img, label, err := NewJPEGPipeline().DecodeScreenshot(tc.payload)
			if err == nil || img != nil || label != "" {
				t.Fatalf("failed decode claimed success: %v %s %v", img, label, err)
			}
			if tc.name == "pixel budget" && !strings.Contains(err.Error(), "pixel budget") {
				t.Fatalf("oversized frame was not rejected before allocation: %v", err)
			}
		})
	}
}

func TestJPEGLanczosDownscalePreservesBoundsAndFiltersPixels(t *testing.T) {
	img := image.NewRGBA(image.Rect(13, 7, 45, 23))
	for y := img.Rect.Min.Y; y < img.Rect.Max.Y; y++ {
		for x := img.Rect.Min.X; x < img.Rect.Max.X; x++ {
			value := uint8(0)
			if x%2 != 0 {
				value = 255
			}
			img.SetRGBA(x, y, color.RGBA{R: value, G: value, B: value, A: 255})
		}
	}
	p := NewJPEGPipeline()
	p.MaxWidth, p.MaxHeight = 4, 4
	small := p.ResizeForModel(img)
	if small == nil || small.Bounds() != image.Rect(0, 0, 4, 2) {
		t.Fatalf("downscale distorted aspect ratio: %v", small)
	}
	for y := 0; y < 2; y++ {
		for x := 0; x < 4; x++ {
			r, g, b, a := small.At(x, y).RGBA()
			if r < 70*257 || r > 180*257 || r != g || g != b || a != 65535 {
				t.Fatalf("stripe anti-aliasing/color failed: %d %d %d %d", r, g, b, a)
			}
		}
	}
	if p.ResizeForModel(small) != small {
		t.Fatal("bounded image unnecessarily copied or upscaled")
	}
	p.MaxWidth = 0
	if p.ResizeForModel(img) != nil || p.ResizeForModel(nil) != nil {
		t.Fatal("invalid model bounds accepted")
	}
}

func BenchmarkJPEGDecodeGoAndPipeline(b *testing.B) {
	img := image.NewRGBA(image.Rect(0, 0, 1280, 640))
	for y := 0; y < 640; y++ {
		for x := 0; x < 1280; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 73, A: 255})
		}
	}
	var raw bytes.Buffer
	if err := jpeg.Encode(&raw, img, &jpeg.Options{Quality: 80}); err != nil {
		b.Fatal(err)
	}
	pipeline := NewJPEGPipeline()
	for _, tc := range []struct {
		name   string
		decode func() (image.Image, error)
	}{
		{"Go", func() (image.Image, error) { return jpeg.Decode(bytes.NewReader(raw.Bytes())) }},
		{"Pipeline", func() (image.Image, error) {
			decoded, err := pipeline.DecodeJPEGResult(b.Context(), raw.Bytes())
			return decoded.Image, err
		}},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(raw.Bytes())))
			for b.Loop() {
				decoded, err := tc.decode()
				if err != nil || decoded.Bounds() != img.Bounds() {
					b.Fatalf("decode failed or changed dimensions: %v", err)
				}
			}
		})
	}
}
