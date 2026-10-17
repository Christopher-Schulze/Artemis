package bridge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"

	"github.com/Christopher-Schulze/Artemis/internal/jpegcodec"
	"github.com/Christopher-Schulze/Artemis/platform"
)

// ScreenshotFormat is CDP capture format.
type ScreenshotFormat string

const (
	FormatJPEG ScreenshotFormat = "jpeg"
	FormatPNG  ScreenshotFormat = "png"
	// JPEGEncodedLimit and JPEGPixelsLimit bound hostile CDP image payloads.
	JPEGEncodedLimit = 32 * 1024 * 1024
	JPEGPixelsLimit  = 32 * 1024 * 1024
)

// JPEGPipeline decodes CDP JPEG screenshots for LLM resize path (spec P5.6).
type JPEGPipeline struct {
	Caps            platform.PlatformCapabilities
	Quality         int
	MaxWidth        int
	MaxHeight       int
	CPU             ImageCPUConfig
	ImageMetrics    func(ImageMetricEvent)
	nativeAdmission platform.ProbeResult
	nativeAdmitted  bool
	mmapAdmission   platform.ProbeResult
}

// NewJPEGPipeline creates an admitted decoder with a frozen capability snapshot.
func NewJPEGPipeline() *JPEGPipeline {
	caps := platform.Detect()
	return &JPEGPipeline{
		Caps:            caps,
		Quality:         80,
		MaxWidth:        1024,
		MaxHeight:       1024,
		nativeAdmission: caps.Evidence.HardwareJPEG,
		nativeAdmitted:  caps.HasHWJPEGDecode,
		mmapAdmission:   caps.Evidence.Mmap,
	}
}

// DecodePath names the guaranteed software fallback. DecodeJPEGResult reports
// the decoder that actually executed, independently of caller-supplied caps.
func (p *JPEGPipeline) DecodePath() string {
	return "sw_go_image"
}

// DecodeScreenshot decodes base64 JPEG bytes from CDP captureScreenshot.
func (p *JPEGPipeline) DecodeScreenshot(b64 string) (image.Image, string, error) {
	if p == nil {
		return nil, "", fmt.Errorf("jpeg pipeline required")
	}
	if len(b64) == 0 || len(b64) > JPEGEncodedLimit {
		return nil, "", fmt.Errorf("jpeg pipeline: encoded image exceeds bounds")
	}
	raw, err := DecodeScreenshotData(b64)
	if err != nil {
		return nil, "", fmt.Errorf("jpeg pipeline: decode b64: %w", err)
	}
	return p.DecodeJPEG(raw)
}

// DecodeJPEG validates and decodes caller-owned bytes without an input copy.
func (p *JPEGPipeline) DecodeJPEG(raw []byte) (image.Image, string, error) {
	result, err := p.DecodeJPEGResult(context.Background(), raw)
	return result.Image, result.Path, err
}

// JPEGDecodeResult carries operation-local decoder/fallback evidence.
type JPEGDecodeResult struct {
	Image        image.Image
	Path         string
	Acceleration platform.ProbeResult
}

// DecodeJPEGResult admits native decoding and preserves truthful Go fallback.
// Neither input nor native callbacks may outlive this synchronous operation.
func (p *JPEGPipeline) DecodeJPEGResult(ctx context.Context, raw []byte) (JPEGDecodeResult, error) {
	if p == nil || len(raw) == 0 || len(raw) > JPEGEncodedLimit/4*3 {
		return JPEGDecodeResult{}, fmt.Errorf("jpeg pipeline: raw image exceeds bounds")
	}
	if ctx == nil {
		return JPEGDecodeResult{}, fmt.Errorf("jpeg pipeline: context required")
	}
	if err := ctx.Err(); err != nil {
		return JPEGDecodeResult{}, err
	}
	config, err := jpeg.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return JPEGDecodeResult{}, fmt.Errorf("jpeg pipeline: decode header: %w", err)
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > JPEGPixelsLimit/config.Height {
		return JPEGDecodeResult{}, fmt.Errorf("jpeg pipeline: decoded image exceeds pixel budget")
	}
	if err := jpegcodec.ValidateScanBudget(raw); err != nil {
		return JPEGDecodeResult{}, err
	}
	evidence := p.nativeAdmission
	if evidence.State == "" {
		evidence = platform.ProbeResult{State: platform.StateUnavailable, Source: "pipeline construction", Reason: "no native boot admission supplied"}
	}
	if !p.nativeAdmitted && evidence.State == platform.StateAvailable {
		evidence = platform.ProbeResult{State: platform.StateUnavailable, Source: "pipeline construction", Reason: "hardware capability was not admitted at boot"}
	}
	var nativeErr error
	if p.nativeAdmitted && evidence.State == platform.StateAvailable {
		img, path, decodeErr := jpegcodec.Decode(ctx, raw)
		nativeErr = decodeErr
		if nativeErr == nil {
			ImageOperationFromContext(ctx).Decoded(path, false)
			return JPEGDecodeResult{Image: img, Path: path, Acceleration: platform.ProbeResult{State: platform.StateAvailable, Source: path, Reason: "native decoder completed with validated pixels", Value: 1}}, nil
		}
		if err := ctx.Err(); err != nil {
			return JPEGDecodeResult{}, err
		}
		evidence.State, evidence.Reason, evidence.Value = platform.StateDegraded, nativeErr.Error(), 0
	}
	img, err := jpeg.Decode(bytes.NewReader(raw))
	if err != nil {
		return JPEGDecodeResult{}, fmt.Errorf("jpeg pipeline: decode jpeg: %w", errors.Join(nativeErr, err))
	}
	ImageOperationFromContext(ctx).Decoded("sw_go_image", true)
	if err := ctx.Err(); err != nil {
		return JPEGDecodeResult{}, err
	}
	return JPEGDecodeResult{Image: img, Path: "sw_go_image", Acceleration: evidence}, nil
}

// ResizeForModel uses Lanczos to downscale within model bounds, without upscaling.
// Invalid or oversized images and invalid model bounds return nil.
func (p *JPEGPipeline) ResizeForModel(img image.Image) image.Image {
	resized, err := p.ResizeForModelContext(context.Background(), img)
	if err != nil {
		return nil
	}
	return resized
}
