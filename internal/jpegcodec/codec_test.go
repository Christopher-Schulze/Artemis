package jpegcodec

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

func TestNativeJPEGActualPixelsOrExplicitUnavailable(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 1280, 640))
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 61, G: 113, B: 179, A: 255})
		}
	}
	var raw bytes.Buffer
	if err := jpeg.Encode(&raw, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	backend := Probe()
	decoded, path, err := Decode(context.Background(), raw.Bytes())
	if !backend.Available {
		if !errors.Is(err, ErrUnavailable) || decoded != nil || path != "" {
			t.Fatalf("unlinked/rejected decoder manufactured pixels: %v %q %v", decoded, path, err)
		}
		t.Logf("native decoder explicitly unavailable: %s", backend.Reason)
		return
	}
	if err != nil || path != backend.Path || decoded.Bounds() != img.Bounds() {
		t.Fatalf("admitted native decoder did not decode: %q %v; probe=%+v", path, err, backend)
	}
	want, err := jpeg.Decode(bytes.NewReader(raw.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			got := color.NRGBAModel.Convert(decoded.At(x, y)).(color.NRGBA)
			expected := color.NRGBAModel.Convert(want.At(x, y)).(color.NRGBA)
			for _, channel := range []struct{ got, want uint8 }{{got.R, expected.R}, {got.G, expected.G}, {got.B, expected.B}, {got.A, expected.A}} {
				delta := int(channel.got) - int(channel.want)
				if delta < -3 || delta > 3 {
					t.Fatalf("native pixels disagree with Go JPEG at %d,%d: %+v != %+v", x, y, got, expected)
				}
			}
		}
	}
}

func TestNativeJPEGRejectsMissingAndCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, tc := range []struct {
		name string
		ctx  context.Context
	}{{"missing", nil}, {"canceled", ctx}} {
		t.Run(tc.name, func(t *testing.T) {
			img, path, err := Decode(tc.ctx, []byte("jpeg"))
			if err == nil || img != nil || path != "" {
				t.Fatalf("invalid operation claimed decoded pixels: %v %q %v", img, path, err)
			}
		})
	}
}
