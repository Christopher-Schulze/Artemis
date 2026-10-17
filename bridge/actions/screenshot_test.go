package actions

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/Christopher-Schulze/Artemis/internal/jpegcodec"
)

func screenshotPixels(t *testing.T, format string, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 73, A: 255})
		}
	}
	var buffer bytes.Buffer
	var err error
	if format == "jpeg" {
		err = jpeg.Encode(&buffer, img, &jpeg.Options{Quality: 90})
	} else {
		err = png.Encode(&buffer, img)
	}
	if err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestScreenshotRequestValidationAndActualWireDefaults(t *testing.T) {
	for _, tc := range []struct {
		name    string
		request Request
		invalid bool
		format  string
		quality int
	}{
		{"ordinary default", Request{Kind: KindScreenshot}, false, "png", 0},
		{"JPEG default", Request{Kind: KindScreenshot, Format: "jpeg"}, false, "jpeg", 80},
		{"model default", Request{Kind: KindScreenshot, ForModel: true}, false, "jpeg", 80},
		{"model quality", Request{Kind: KindScreenshot, ForModel: true, Quality: 93}, false, "jpeg", 93},
		{"unknown format", Request{Kind: KindScreenshot, Format: "gif"}, true, "", 0},
		{"negative quality", Request{Kind: KindScreenshot, Format: "jpeg", Quality: -1}, true, "", 0},
		{"excessive quality", Request{Kind: KindScreenshot, Format: "jpeg", Quality: 101}, true, "", 0},
		{"PNG quality", Request{Kind: KindScreenshot, Format: "png", Quality: 80}, true, "", 0},
		{"implicit PNG quality", Request{Kind: KindScreenshot, Quality: 80}, true, "", 0},
		{"PNG model", Request{Kind: KindScreenshot, Format: "png", ForModel: true}, true, "", 0},
		{"non-image model", Request{Kind: KindNavigate, URL: "https://example.test", ForModel: true}, true, "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateRequest(tc.request); (err != nil) != tc.invalid {
				t.Fatalf("validation error = %v", err)
			}
			if tc.invalid {
				return
			}
			wire, err := json.Marshal(screenshotParameters(tc.request))
			if err != nil {
				t.Fatal(err)
			}
			var decoded captureScreenshotParams
			if err := json.Unmarshal(wire, &decoded); err != nil {
				t.Fatal(err)
			}
			if decoded.Format != tc.format || !decoded.FromSurface || !decoded.CaptureBeyondViewport {
				t.Fatalf("actual wire flags lost: %s", wire)
			}
			if tc.quality == 0 && decoded.Quality != nil || tc.quality > 0 && (decoded.Quality == nil || *decoded.Quality != tc.quality) {
				t.Fatalf("actual wire quality lost: %s", wire)
			}
		})
	}
}

func TestScreenshotResultUsesCompletePixelsAndPreservesOrdinaryBytes(t *testing.T) {
	for _, format := range []string{"jpeg", "png"} {
		t.Run(format, func(t *testing.T) {
			input := screenshotPixels(t, format, 40, 30)
			runtime := &Runtime{}
			outcome := runtime.screenshotOutcome(context.Background(), Request{Kind: KindScreenshot}, format, input, Evidence{Action: KindScreenshot})
			expectedPath := "sw_go_image"
			if codec := jpegcodec.Probe(); format == "jpeg" && codec.Available {
				expectedPath = codec.Path
			}
			if !outcome.Success || !outcome.Evidence.Postcondition.Passed || outcome.Width != 40 || outcome.Height != 30 || outcome.MIME != "image/"+format || outcome.DecodePath != expectedPath || outcome.ModelPrepared {
				t.Fatalf("incorrect pixel outcome: %+v", outcome)
			}
			if !bytes.Equal(outcome.Bytes, input) || &outcome.Bytes[0] != &input[0] {
				t.Fatal("ordinary image copied or transformed")
			}
			truncated := input[:len(input)-20]
			if _, _, err := image.DecodeConfig(bytes.NewReader(truncated)); err != nil {
				t.Fatalf("damaged fixture has no valid header: %v", err)
			}
			rejected := runtime.screenshotOutcome(context.Background(), Request{Kind: KindScreenshot}, format, truncated, Evidence{})
			if rejected.Success || rejected.Failure != FailurePostcondition || rejected.DecodePath != "" || len(rejected.Bytes) != 0 {
				t.Fatalf("header-only success admitted: %+v", rejected)
			}
		})
	}
}

func TestModelScreenshotActuallyResizesAndRetainsOneProbeSnapshot(t *testing.T) {
	input := screenshotPixels(t, "jpeg", 1280, 640)
	fingerprint := sha256.Sum256(input)
	runtime := &Runtime{}
	ordinary := runtime.screenshotOutcome(context.Background(), Request{Kind: KindScreenshot}, "jpeg", input, Evidence{})
	if !ordinary.Success || ordinary.Width != 1280 || ordinary.Height != 640 {
		t.Fatal("ordinary screenshot lost its full dimensions")
	}
	pipeline := runtime.jpegPipeline
	observedAt := pipeline.Caps.Evidence.ObservedAt
	pipeline.Caps.HasHWJPEGDecode = true
	model := runtime.screenshotOutcome(context.Background(), Request{Kind: KindScreenshot, ForModel: true, Quality: 91}, "jpeg", input, Evidence{})
	if !model.Success || !model.ModelPrepared || model.DecodePath != ordinary.DecodePath || model.Width != 1024 || model.Height != 512 || bytes.Equal(model.Bytes, input) {
		t.Fatalf("model input was not actually transformed: %+v", model)
	}
	decoded, err := jpeg.Decode(bytes.NewReader(model.Bytes))
	if err != nil || decoded.Bounds().Dx() != model.Width || decoded.Bounds().Dy() != model.Height {
		t.Fatalf("encoded dimensions disagree with result: %v", err)
	}
	if runtime.jpegPipeline != pipeline || !pipeline.Caps.Evidence.ObservedAt.Equal(observedAt) {
		t.Fatal("one runtime repeated its boot probes")
	}
	if sha256.Sum256(ordinary.Bytes) != fingerprint || sha256.Sum256(input) != fingerprint {
		t.Fatal("model preparation modified the previous caller's bytes")
	}
}

func TestScreenshotCancellationPublishesNoPixelsOrDecodeLabel(t *testing.T) {
	raw := screenshotPixels(t, "jpeg", 40, 30)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, tc := range []struct {
		name    string
		ctx     context.Context
		failure FailureClass
	}{{"missing", nil, FailureValidation}, {"canceled", ctx, FailureCancelled}} {
		t.Run(tc.name, func(t *testing.T) {
			out := (&Runtime{}).screenshotOutcome(tc.ctx, Request{Kind: KindScreenshot}, "jpeg", raw, Evidence{})
			if out.Success || len(out.Bytes) != 0 || out.DecodePath != "" || out.DecodeAcceleration != nil || out.Failure != tc.failure {
				t.Fatalf("invalid context published pixels or mislabeled failure: %+v", out)
			}
		})
	}
}
