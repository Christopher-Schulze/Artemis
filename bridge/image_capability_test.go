package bridge

import (
	"bytes"
	"image"
	"image/jpeg"
	"testing"

	"github.com/Christopher-Schulze/Artemis/platform"
)

func TestJPEGAdmissionUsesFrozenHardwareCapability(t *testing.T) {
	var raw bytes.Buffer
	if err := jpeg.Encode(&raw, image.NewNRGBA(image.Rect(0, 0, 80, 40)), nil); err != nil {
		t.Fatal(err)
	}
	pipeline := NewJPEGPipeline()
	if pipeline.nativeAdmitted != pipeline.Caps.HasHWJPEGDecode {
		t.Fatal("measured hardware flag was not bound at construction")
	}
	admitted := pipeline.nativeAdmitted
	for _, forged := range []bool{false, true} {
		pipeline.Caps.HasHWJPEGDecode = forged
		result, err := pipeline.DecodeJPEGResult(t.Context(), raw.Bytes())
		if err != nil || result.Image == nil || result.Image.Bounds().Dx() != 80 || pipeline.nativeAdmitted != admitted {
			t.Fatalf("mutable public flags changed frozen admission or actual pixels: %+v %v", result, err)
		}
	}
	// An available-looking proof cannot activate a decoder on a pipeline
	// whose private boot capability was never admitted.
	unowned := &JPEGPipeline{Caps: platform.PlatformCapabilities{HasHWJPEGDecode: true}, nativeAdmission: platform.ProbeResult{State: platform.StateAvailable}}
	result, err := unowned.DecodeJPEGResult(t.Context(), raw.Bytes())
	if err != nil || result.Path != "sw_go_image" || result.Image == nil || result.Acceleration.State != platform.StateUnavailable {
		t.Fatalf("missing frozen hardware admission executed or labeled native work: %+v %v", result, err)
	}
}
