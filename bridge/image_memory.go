package bridge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"

	"github.com/Christopher-Schulze/Artemis/platform"
)

// ImageWorkingMemoryBytes estimates a conservative transient reservation from
// the header before allocating pixels. It is not measured RSS or peak usage.
func (p *JPEGPipeline) ImageWorkingMemoryBytes(raw []byte, format ScreenshotFormat, forModel bool) (int64, error) {
	if p == nil {
		return 0, fmt.Errorf("image working memory: bounded pipeline input required")
	}
	if forModel && (p.MaxWidth <= 0 || p.MaxHeight <= 0) {
		return 0, fmt.Errorf("image working memory: positive model dimensions required")
	}
	config, err := screenshotMemoryHeader(raw, format)
	if err != nil {
		return 0, err
	}
	width, height := config.Width, config.Height
	// Up to 31px padding for any 1..4 sampling-factor MCU, including 24px;
	// four progressive int32 coefficient planes plus
	// decoded/conversion pixels. Fixed allowance covers headers and row state.
	size := (int64(width)+31)*(int64(height)+31)*24 + (256 << 10)
	if format == FormatPNG {
		// Up to RGBA64 pixels, two full filter rows and zlib/decoder state.
		size = int64(width)*int64(height)*8 + int64(width)*16 + (256 << 10)
	}
	if format == FormatJPEG && p.nativeAdmitted && p.nativeAdmission.State == platform.StateAvailable {
		size += 128 << 20 // native workspace allowance; TurboJPEG's configured cap
	}
	if forModel && (width > p.MaxWidth || height > p.MaxHeight) {
		size += p.modelResizeMemoryBytes(width, height)
	}
	return size, nil
}

func screenshotMemoryHeader(raw []byte, format ScreenshotFormat) (image.Config, error) {
	if len(raw) == 0 || len(raw) > JPEGEncodedLimit/4*3 {
		return image.Config{}, fmt.Errorf("image working memory: bounded input required")
	}
	var config image.Config
	var err error
	switch format {
	case FormatJPEG:
		config, err = jpeg.DecodeConfig(bytes.NewReader(raw))
	case FormatPNG:
		config, err = png.DecodeConfig(bytes.NewReader(raw))
	default:
		return image.Config{}, fmt.Errorf("image working memory: unsupported format")
	}
	if err != nil {
		return image.Config{}, fmt.Errorf("image working memory header: %w", err)
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > JPEGPixelsLimit/config.Height {
		return image.Config{}, fmt.Errorf("image working memory: pixel budget exceeded")
	}
	return config, nil
}

func (p *JPEGPipeline) modelResizeMemoryBytes(width, height int) int64 {
	// The owned Lanczos path retains at most four scanline workers, two NRGBA
	// passes and the pinned index+float64 weights. No image-sized task queue.
	w, h := int64(width), int64(height)
	destinationWidth, destinationHeight := modelFitSize(width, height, max(1, p.MaxWidth), max(1, p.MaxHeight))
	dstW, dstH := int64(destinationWidth), int64(destinationHeight)
	workers := int64(4)
	if p.CPU.Workers > 0 {
		workers = int64(min(4, p.CPU.Workers))
	}
	weights := 96*(w+h) + 120*(dstW+dstH)
	pixels := 4*dstW*h + 4*dstW*dstH
	scanlines := 4*w*min(workers, h) + 4*h*min(workers, dstW)
	workerState := workers * (64 << 10)
	return weights + pixels + scanlines + workerState + 2*(JPEGEncodedLimit/4*3)
}

// WithImageWorkingMemory keeps the same session/shared reservation through
// synchronous decoding, native cleanup, resizing and output encoding.
func (p *JPEGPipeline) WithImageWorkingMemory(ctx context.Context, raw []byte, format ScreenshotFormat, forModel bool, owner ScreenshotMemoryAdmission, consume func(context.Context, platform.ProbeResult) error) (err error) {
	if ctx == nil || consume == nil {
		return fmt.Errorf("image working memory: context and consumer required")
	}
	if err := validateScreenshotMemoryOwner(owner); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	size, err := p.ImageWorkingMemoryBytes(raw, format, forModel)
	if err != nil {
		return err
	}
	_, err = p.withImageCPU(ctx, func(workCtx context.Context, _ *JPEGPipeline) error {
		return withImageWorkingMemorySize(workCtx, size, owner, func(proof platform.ProbeResult) error {
			return consume(workCtx, proof)
		})
	})
	return err
}

func withImageWorkingMemorySize(ctx context.Context, size int64, owner ScreenshotMemoryAdmission, consume func(platform.ProbeResult) error) (err error) {
	if ctx == nil || consume == nil || size <= 0 {
		return fmt.Errorf("image working memory: context, consumer and positive size required")
	}
	if err := validateScreenshotMemoryOwner(owner); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if owner == nil && size > 512<<20 {
		return fmt.Errorf("image working memory: unowned operation exceeds 512 MiB ceiling")
	}
	observation := platform.ProbeResult{State: platform.StateUnavailable, Source: "bounded image working set", Reason: "no shared capacity owner supplied; pixel limits still apply"}
	if owner != nil {
		release, admissionErr := owner.ReserveMemory(ctx, size)
		if admissionErr != nil {
			return &ImageAdmissionError{Cause: fmt.Errorf("admit image working memory: %w", admissionErr)}
		}
		if release == nil {
			return fmt.Errorf("image working memory: release owner required")
		}
		defer func() { err = errors.Join(err, release()) }()
		observation = platform.ProbeResult{State: platform.StateAvailable, Source: "owned image working set reservation", Reason: "conservative admitted capacity, not measured RSS or peak usage", Value: uint64(size)}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return errors.Join(consume(observation), ctx.Err())
}
