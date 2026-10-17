package bridge

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestModelImageWriterBoundsRealEncoderOutput(t *testing.T) {
	buffer := boundedModelImageBuffer{limit: 128}
	err := jpeg.Encode(&buffer, image.NewRGBA(image.Rect(0, 0, 64, 32)), &jpeg.Options{Quality: 90})
	if err == nil || buffer.buffer.Len() > 128 {
		t.Fatal("real encoder bypassed byte budget")
	}
	ordinary := boundedModelImageBuffer{limit: JPEGEncodedLimit / 4 * 3}
	if err := jpeg.Encode(&ordinary, image.NewRGBA(image.Rect(0, 0, 64, 32)), &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	if _, err := jpeg.Decode(bytes.NewReader(ordinary.buffer.Bytes())); err != nil {
		t.Fatal(err)
	}
}

func TestModelImageFormatConversionAndRetainedInput(t *testing.T) {
	for _, source := range []ScreenshotFormat{FormatJPEG, FormatPNG} {
		var original bytes.Buffer
		img := image.NewRGBA(image.Rect(0, 0, 64, 32))
		var err error
		if source == FormatJPEG {
			err = jpeg.Encode(&original, img, &jpeg.Options{Quality: 90})
		} else {
			err = png.Encode(&original, img)
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, target := range []ScreenshotFormat{FormatJPEG, FormatPNG} {
			t.Run(string(source)+"_to_"+string(target), func(t *testing.T) {
				result, err := NewJPEGPipeline().PrepareModelImage(t.Context(), original.Bytes(), source, target, nil)
				if err != nil || result.Format != target || result.Width != 64 || result.Height != 32 || result.DecodePath == "" {
					t.Fatalf("actual format conversion: %+v %v", result, err)
				}
				decoded, actual, err := image.Decode(bytes.NewReader(result.Bytes))
				if err != nil || actual != string(target) || decoded.Bounds() != img.Bounds() {
					t.Fatalf("encoded pixels do not match receipt: %s %v", actual, err)
				}
				if source == target && (&result.Bytes[0] != &original.Bytes()[0] || !bytes.Equal(result.Bytes, original.Bytes())) {
					t.Fatal("fitting unchanged input was copied or transformed")
				}
			})
		}
	}
}

func TestModelImageFailurePublishesNoOutput(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, test := range []struct {
		name   string
		ctx    context.Context
		format ScreenshotFormat
		target ScreenshotFormat
	}{
		{"nil_context", nil, FormatJPEG, FormatJPEG},
		{"canceled", ctx, FormatJPEG, FormatJPEG},
		{"damaged", t.Context(), FormatJPEG, FormatJPEG},
		{"unknown_source", t.Context(), "unknown", FormatJPEG},
		{"unknown_target", t.Context(), FormatJPEG, "unknown"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := NewJPEGPipeline().PrepareModelImage(test.ctx, []byte("damaged image"), test.format, test.target, nil)
			if err == nil || len(result.Bytes) != 0 || result.DecodePath != "" || result.Width != 0 || result.Height != 0 {
				t.Fatal("failed preparation published successful pixels or receipt")
			}
		})
	}
}
