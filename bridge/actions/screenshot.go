package actions

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"math"

	"github.com/Christopher-Schulze/Artemis/bridge"
	bridgeobserve "github.com/Christopher-Schulze/Artemis/bridge/observe"
	"github.com/Christopher-Schulze/Artemis/platform"
)

func screenshotParameters(q Request) captureScreenshotParams {
	format := q.Format
	if format == "" {
		format = "png"
		if q.ForModel {
			format = "jpeg"
		}
	}
	params := captureScreenshotParams{Format: format, FromSurface: true, CaptureBeyondViewport: true}
	if format == "jpeg" {
		quality := q.Quality
		if quality == 0 {
			quality = 80
		}
		params.Quality = &quality
	}
	return params
}

func (r *Runtime) elementScreenshotClip(ctx context.Context, ref string, evidence Evidence) (*screenshotClip, Outcome) {
	resolution, err := r.observer.Resolve(ctx, ref)
	if err != nil {
		return nil, failedNow(evidence, classifyContext(ctx, FailureProtocol), err.Error())
	}
	if resolution.Status != bridgeobserve.ResolutionExact || resolution.Node == nil {
		return nil, failedNow(evidence, FailureTarget, "screenshot requires an exact current reference: "+string(resolution.Status))
	}
	node := *resolution.Node
	evidence.BackendDOMID, evidence.FrameID = node.BackendNodeID, node.FrameID
	if !node.Visible || node.Box == nil {
		return nil, failedNow(evidence, FailureActionability, "screenshot target has no visible layout")
	}
	rect, err := r.observer.ScreenshotRect(ctx, node)
	if err != nil {
		return nil, failedNow(evidence, classifyContext(ctx, FailureActionability), err.Error())
	}
	if math.Ceil(rect.Width)*math.Ceil(rect.Height) > float64(bridge.JPEGPixelsLimit) {
		return nil, failedNow(evidence, FailureValidation, "element screenshot exceeds pixel budget")
	}
	return &screenshotClip{X: rect.X, Y: rect.Y, Width: rect.Width, Height: rect.Height, Scale: 1}, Outcome{Success: true, Evidence: evidence}
}

func (r *Runtime) screenshotInputOutcome(ctx context.Context, q Request, format, encoded string, e Evidence) Outcome {
	if ctx == nil {
		return failedNow(e, FailureValidation, "screenshot context required")
	}
	r.jpegOnce.Do(func() { r.jpegPipeline = bridge.NewJPEGPipeline() })
	var outcome Outcome
	var releaseCopy func() error
	err := r.jpegPipeline.WithScreenshotInput(ctx, encoded, r.inputMemory, func(raw []byte, observation platform.ProbeResult) error {
		outcome = r.screenshotOutcome(ctx, q, format, raw, e)
		outcome.InputMemory = &observation
		release, transferErr := r.transferMappedScreenshot(ctx, &outcome, raw)
		releaseCopy = release
		return transferErr
	})
	if releaseCopy != nil {
		err = errors.Join(err, releaseCopy())
	}
	if err != nil {
		var invalid *bridge.ScreenshotDataError
		if !q.ForModel && ctx.Err() == nil && errors.As(err, &invalid) {
			return failedNow(e, FailureProtocol, err.Error())
		}
		return screenshotImageFailure(ctx, e, err)
	}
	return outcome
}

func (r *Runtime) transferMappedScreenshot(ctx context.Context, outcome *Outcome, raw []byte) (func() error, error) {
	if !outcome.Success || outcome.InputMemory.State != platform.StateAvailable || len(outcome.Bytes) == 0 || &outcome.Bytes[0] != &raw[0] {
		return nil, nil
	}
	release, err := r.inputMemory.ReserveMemory(ctx, int64(len(outcome.Bytes)))
	if err != nil {
		return release, &bridge.ImageAdmissionError{Cause: fmt.Errorf("admit screenshot output transfer: %w", err)}
	}
	if release == nil {
		return nil, fmt.Errorf("screenshot output transfer requires a release owner")
	}
	if err := ctx.Err(); err != nil {
		return release, err
	}
	outcome.Bytes = bytes.Clone(outcome.Bytes)
	return release, nil
}

func (r *Runtime) screenshotOutcome(ctx context.Context, q Request, format string, raw []byte, e Evidence) Outcome {
	if ctx == nil {
		return failedNow(e, FailureValidation, "screenshot context required")
	}
	if err := ctx.Err(); err != nil {
		return failedNow(e, classifyContext(ctx, FailureCancelled), err.Error())
	}
	if format != "jpeg" && format != "png" {
		return failedNow(e, FailureValidation, "unsupported screenshot format")
	}
	if q.ForModel && format != "jpeg" {
		return failedNow(e, FailureValidation, "model screenshots require JPEG")
	}
	r.jpegOnce.Do(func() { r.jpegPipeline = bridge.NewJPEGPipeline() })
	pipeline := *r.jpegPipeline
	if r.imageCPU != nil {
		cpu, err := r.imageCPU(ctx)
		if err != nil {
			return screenshotImageFailure(ctx, e, &bridge.ImageAdmissionError{Cause: err})
		}
		pipeline.CPU = cpu
	}
	if q.ForModel {
		if q.Quality != 0 {
			pipeline.Quality = q.Quality
		}
		result, err := pipeline.PrepareModelImage(ctx, raw, bridge.ScreenshotFormat(format), bridge.FormatJPEG, r.inputMemory)
		if err != nil {
			return screenshotImageFailure(ctx, e, err)
		}
		e.Postcondition = Postcondition{Type: "image_signature", Expected: format, Actual: string(result.Format), Passed: true}
		return Outcome{Success: true, Bytes: result.Bytes, MIME: "image/" + string(result.Format), Width: result.Width, Height: result.Height, DecodePath: result.DecodePath, DecodeAcceleration: &result.Acceleration, ImageMemory: &result.Memory, ModelPrepared: true, Evidence: e}
	}
	var outcome Outcome
	err := pipeline.WithImageWorkingMemory(ctx, raw, bridge.ScreenshotFormat(format), q.ForModel, r.inputMemory, func(workCtx context.Context, observation platform.ProbeResult) error {
		outcome = r.decodeScreenshotOutcome(workCtx, q, format, raw, e)
		outcome.ImageMemory = &observation
		return nil
	})
	if err != nil {
		return screenshotImageFailure(ctx, e, err)
	}
	return outcome
}

func screenshotImageFailure(ctx context.Context, evidence Evidence, err error) Outcome {
	if ctx != nil && ctx.Err() != nil {
		return failedNow(evidence, classifyContext(ctx, FailureCancelled), err.Error())
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return failedNow(evidence, FailureTimeout, err.Error())
	}
	if errors.Is(err, context.Canceled) {
		return failedNow(evidence, FailureCancelled, err.Error())
	}
	var denied *bridge.ImageAdmissionError
	if errors.As(err, &denied) {
		return failedNow(evidence, FailurePolicy, err.Error())
	}
	return failedNow(evidence, FailurePostcondition, err.Error())
}

func (r *Runtime) decodeScreenshotOutcome(ctx context.Context, q Request, format string, raw []byte, e Evidence) Outcome {
	img, path, acceleration, err := r.decodeScreenshotPixels(ctx, format, raw)
	if err != nil {
		if ctx.Err() != nil {
			return failedNow(e, classifyContext(ctx, FailureCancelled), ctx.Err().Error())
		}
		return failedNow(e, FailurePostcondition, "invalid image: "+err.Error())
	}
	if err := ctx.Err(); err != nil {
		return failedNow(e, classifyContext(ctx, FailureCancelled), err.Error())
	}
	e.Postcondition = Postcondition{Type: "image_signature", Expected: format, Actual: format, Passed: true}
	return Outcome{Success: true, Bytes: raw, MIME: "image/" + format, Width: img.Bounds().Dx(), Height: img.Bounds().Dy(), DecodePath: path, DecodeAcceleration: acceleration, ModelPrepared: q.ForModel, Evidence: e}
}

func (r *Runtime) decodeScreenshotPixels(ctx context.Context, format string, raw []byte) (image.Image, string, *platform.ProbeResult, error) {
	if format == "jpeg" {
		decoded, err := r.jpegPipeline.DecodeJPEGResult(ctx, raw)
		return decoded.Image, decoded.Path, &decoded.Acceleration, err
	}
	img, err := decodePNGScreenshot(raw)
	if err == nil {
		bridge.ImageOperationFromContext(ctx).Decoded("sw_go_image", false)
	}
	return img, "sw_go_image", nil, err
}

func screenshotMetricOutcome(result Outcome) bridge.ImageOperationOutcome {
	switch {
	case result.Success:
		return bridge.ImageOutcomeSuccess
	case result.Failure == FailurePolicy:
		return bridge.ImageOutcomeDenied
	case result.Failure == FailureCancelled || result.Failure == FailureTimeout:
		return bridge.ImageOutcomeCanceled
	case result.Failure == FailureValidation:
		return bridge.ImageOutcomeInvalid
	default:
		return bridge.ImageOutcomeFailed
	}
}

func decodePNGScreenshot(raw []byte) (image.Image, error) {
	if len(raw) == 0 || len(raw) > bridge.JPEGEncodedLimit/4*3 {
		return nil, fmt.Errorf("raw screenshot exceeds bounds")
	}
	config, err := png.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("decode PNG header: %w", err)
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > bridge.JPEGPixelsLimit/config.Height {
		return nil, fmt.Errorf("PNG screenshot exceeds pixel budget")
	}
	return png.Decode(bytes.NewReader(raw))
}
