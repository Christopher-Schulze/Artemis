package bridge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/jpeg"
	"image/png"

	"github.com/Christopher-Schulze/Artemis/platform"
)

// ModelImageResult reports the actual prepared bytes and executed decoder.
// Bytes may borrow the input when it already fits the selected dimensions.
type ModelImageResult struct {
	Bytes        []byte
	Format       ScreenshotFormat
	Width        int
	Height       int
	DecodePath   string
	Acceleration platform.ProbeResult
	Memory       platform.ProbeResult
	CPUAffinity  platform.ProbeResult
}

// PrepareModelImage holds admitted working memory through joined decoding,
// Lanczos resizing and bounded encoding. Already-fitting images retain their
// bytes when the requested output format matches the input.
func (p *JPEGPipeline) PrepareModelImage(ctx context.Context, raw []byte, format, target ScreenshotFormat, owner ScreenshotMemoryAdmission) (output ModelImageResult, returnErr error) {
	if p != nil && p.ImageMetrics != nil && ctx != nil && ImageOperationFromContext(ctx) == nil {
		var operation *ImageOperation
		ctx, operation = BeginImageOperation(ctx, ImageOperationPrepare, format, p.ImageMetrics)
		defer func() {
			outcome := ImageOutcomeSuccess
			var denied *ImageAdmissionError
			if returnErr != nil {
				outcome = ImageOutcomeFailed
			}
			if errors.As(returnErr, &denied) {
				outcome = ImageOutcomeDenied
			}
			if ctx.Err() != nil || errors.Is(returnErr, context.Canceled) || errors.Is(returnErr, context.DeadlineExceeded) {
				outcome = ImageOutcomeCanceled
			}
			operation.Finish(ctx, outcome)
		}()
	}
	if p == nil || ctx == nil || p.Quality < 1 || p.Quality > 100 || (target != FormatJPEG && target != FormatPNG) {
		return ModelImageResult{}, fmt.Errorf("model image: pipeline, context, valid quality and output format required")
	}
	if err := ctx.Err(); err != nil {
		return ModelImageResult{}, err
	}
	size, err := p.ImageWorkingMemoryBytes(raw, format, true)
	if err != nil {
		return ModelImageResult{}, err
	}
	config, err := screenshotMemoryHeader(raw, format)
	if err != nil {
		return ModelImageResult{}, err
	}
	if format != target && config.Width <= p.MaxWidth && config.Height <= p.MaxHeight {
		size += 2 * (JPEGEncodedLimit / 4 * 3)
	}
	var result ModelImageResult
	proof, err := p.withImageCPU(ctx, func(workCtx context.Context, owned *JPEGPipeline) error {
		return withImageWorkingMemorySize(workCtx, size, owner, func(memory platform.ProbeResult) error {
			decoded, err := owned.decodeModelImage(workCtx, raw, format)
			if err != nil {
				return err
			}
			result, err = owned.prepareDecodedModelImage(workCtx, raw, format, target, decoded)
			result.Memory = memory
			return err
		})
	})
	if err != nil {
		return ModelImageResult{}, err
	}
	result.CPUAffinity = proof
	return result, nil
}

func (p *JPEGPipeline) decodeModelImage(ctx context.Context, raw []byte, format ScreenshotFormat) (JPEGDecodeResult, error) {
	if format == FormatJPEG {
		return p.DecodeJPEGResult(ctx, raw)
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return JPEGDecodeResult{}, fmt.Errorf("model image PNG decode: %w", err)
	}
	ImageOperationFromContext(ctx).Decoded("sw_go_image", false)
	if err := ctx.Err(); err != nil {
		return JPEGDecodeResult{}, err
	}
	return JPEGDecodeResult{Image: img, Path: "sw_go_image", Acceleration: platform.ProbeResult{State: platform.StateUnavailable, Source: "Go PNG decoder", Reason: "complete PNG decoded in software"}}, nil
}

func (p *JPEGPipeline) prepareDecodedModelImage(ctx context.Context, raw []byte, format, target ScreenshotFormat, decoded JPEGDecodeResult) (ModelImageResult, error) {
	img, err := p.resizeForModel(ctx, decoded.Image, 0)
	if err != nil {
		return ModelImageResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return ModelImageResult{}, err
	}
	if img != decoded.Image || format != target {
		buffer := boundedModelImageBuffer{limit: JPEGEncodedLimit / 4 * 3}
		var err error
		if target == FormatPNG {
			err = png.Encode(&buffer, img)
		} else {
			err = jpeg.Encode(&buffer, img, &jpeg.Options{Quality: p.Quality})
		}
		if err != nil {
			return ModelImageResult{}, fmt.Errorf("encode model image: %w", err)
		}
		raw, format = buffer.buffer.Bytes(), target
	}
	if err := ctx.Err(); err != nil {
		return ModelImageResult{}, err
	}
	return ModelImageResult{Bytes: raw, Format: format, Width: img.Bounds().Dx(), Height: img.Bounds().Dy(), DecodePath: decoded.Path, Acceleration: decoded.Acceleration}, nil
}

type boundedModelImageBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (b *boundedModelImageBuffer) Write(data []byte) (int, error) {
	if len(data) > b.limit-b.buffer.Len() {
		return 0, fmt.Errorf("encoded model screenshot exceeds byte budget")
	}
	return b.buffer.Write(data)
}
