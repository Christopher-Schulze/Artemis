package actions

import (
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"testing"

	"github.com/Christopher-Schulze/Artemis/bridge"
	"github.com/Christopher-Schulze/Artemis/platform"
)

func TestImageWorkingMemoryHoldsActualDecodeAndCancellation(t *testing.T) {
	raw := screenshotPixels(t, "jpeg", 1280, 640)
	pipeline := bridge.NewJPEGPipeline()
	for _, cancelInside := range []bool{false, true} {
		owner := &screenshotInputOwner{}
		ctx, cancel := context.WithCancel(t.Context())
		err := pipeline.WithImageWorkingMemory(ctx, raw, bridge.FormatJPEG, false, owner, func(workCtx context.Context, observation platform.ProbeResult) error {
			if observation.State != platform.StateAvailable || observation.Value != uint64(owner.held) || owner.held < 1280*640*4 {
				return errors.New("decoder began without its admitted workspace")
			}
			if cancelInside {
				cancel()
			}
			decoded, err := pipeline.DecodeJPEGResult(workCtx, raw)
			if cancelInside {
				return err
			}
			if err != nil {
				return err
			}
			if decoded.Path == "" || decoded.Image.Bounds().Dx() != 1280 || decoded.Image.Bounds().Dy() != 640 {
				return errors.New("admitted decoder did not produce actual pixels")
			}
			return nil
		})
		cancel()
		if cancelInside && !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation lost: %v", err)
		}
		if !cancelInside && err != nil {
			t.Fatal(err)
		}
		if owner.held != 0 || owner.peak == 0 {
			t.Fatal("image workspace leaked or was not admitted")
		}
	}
}

func TestImageWorkingMemoryDenialAndCleanupPublishNoPixels(t *testing.T) {
	raw := screenshotPixels(t, "jpeg", 64, 32)
	for _, tc := range []struct {
		name       string
		limit      int64
		releaseErr error
	}{
		{name: "denied", limit: 1},
		{name: "cleanup failure", releaseErr: errors.New("working lease settlement failed")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			owner := &screenshotInputOwner{limit: tc.limit, releaseErr: tc.releaseErr}
			runtime := &Runtime{inputMemory: owner}
			outcome := runtime.screenshotOutcome(t.Context(), Request{Kind: KindScreenshot}, "jpeg", raw, Evidence{})
			if outcome.Success || len(outcome.Bytes) != 0 || outcome.DecodePath != "" || owner.held != 0 {
				t.Fatalf("rejected workspace published successful pixels: %+v", outcome)
			}
			if tc.limit == 1 && owner.peak != 0 {
				t.Fatal("denied admission allocated working capacity")
			}
		})
	}
}

func TestImageWorkingMemoryEstimateAndNativeAdmissionAreBounded(t *testing.T) {
	for _, format := range []string{"jpeg", "png"} {
		raw := screenshotPixels(t, format, 1280, 640)
		pipeline := bridge.NewJPEGPipeline()
		ordinary, err := pipeline.ImageWorkingMemoryBytes(raw, bridge.ScreenshotFormat(format), false)
		if err != nil || ordinary < 1280*640*4 {
			t.Fatalf("invalid pixel working reservation: %d %v", ordinary, err)
		}
		prepared, err := pipeline.ImageWorkingMemoryBytes(raw, bridge.ScreenshotFormat(format), true)
		if err != nil || prepared <= ordinary {
			t.Fatalf("resize omitted working buffers: %d %d %v", prepared, ordinary, err)
		}
		pipeline.Caps.HasHWJPEGDecode = !pipeline.Caps.HasHWJPEGDecode
		after, err := pipeline.ImageWorkingMemoryBytes(raw, bridge.ScreenshotFormat(format), false)
		if err != nil || after != ordinary {
			t.Fatal("mutable public flag forged native workspace admission")
		}
	}
	if _, err := bridge.NewJPEGPipeline().ImageWorkingMemoryBytes([]byte("damaged"), bridge.FormatJPEG, false); err == nil {
		t.Fatal("bad header admitted")
	}
}

func TestUnownedWidePNGWorkingMemoryRejectsBeforePixelConsumer(t *testing.T) {
	raw := screenshotPixels(t, "png", 64, 32)
	binary.BigEndian.PutUint32(raw[16:20], bridge.JPEGPixelsLimit)
	binary.BigEndian.PutUint32(raw[20:24], 1)
	binary.BigEndian.PutUint32(raw[29:33], crc32.ChecksumIEEE(raw[12:29]))
	pipeline := bridge.NewJPEGPipeline()
	estimate, err := pipeline.ImageWorkingMemoryBytes(raw, bridge.FormatPNG, false)
	if err != nil || estimate <= 512<<20 {
		t.Fatalf("fixture did not reach real valid-header working bound: %d %v", estimate, err)
	}
	called := false
	err = pipeline.WithImageWorkingMemory(t.Context(), raw, bridge.FormatPNG, false, nil, func(context.Context, platform.ProbeResult) error { called = true; return nil })
	if err == nil || called {
		t.Fatal("unowned oversized image reached pixel allocation")
	}
}
