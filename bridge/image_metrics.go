package bridge

import (
	"context"
	"sync"
	"time"

	"github.com/Christopher-Schulze/Artemis/platform"
)

type ImageOperationKind string

const (
	ImageOperationCapture ImageOperationKind = "capture"
	ImageOperationPrepare ImageOperationKind = "model_prepare"
	ImageOperationRoute   ImageOperationKind = "route"
)

type ImageOperationOutcome string

const (
	ImageOutcomeSuccess  ImageOperationOutcome = "success"
	ImageOutcomeFailed   ImageOperationOutcome = "failed"
	ImageOutcomeDenied   ImageOperationOutcome = "denied"
	ImageOutcomeCanceled ImageOperationOutcome = "canceled"
	ImageOutcomeInvalid  ImageOperationOutcome = "invalid"
)

// ImageMetricEvent contains bounded operational values, never pixels, URLs,
// actor IDs, prompt content, raw error text or reservation-as-peak estimates.
type ImageMetricEvent struct {
	Kind            ImageOperationKind
	Format          ScreenshotFormat
	Outcome         ImageOperationOutcome
	Duration        time.Duration
	CaptureAttempts uint32
	// DecodeCounts order: Go image, VideoToolbox, libjpeg-turbo.
	DecodeCounts [3]uint32
	Fallback     bool
	PeakMemory   platform.ProbeResult
}

// ImageOperation owns one synchronous metrics window. Native decoders and
// action callbacks may record on their joined worker; the snapshot is isolated.
type ImageOperation struct {
	mu       sync.Mutex
	started  time.Time
	event    ImageMetricEvent
	observer func(ImageMetricEvent)
	finished bool
}

type imageOperationKey struct{}

// BeginImageOperation creates no sampler goroutine or independent registry.
// The caller finishes after all image producers and cleanup have joined.
func BeginImageOperation(ctx context.Context, kind ImageOperationKind, format ScreenshotFormat, observer func(ImageMetricEvent)) (context.Context, *ImageOperation) {
	if ctx == nil || observer == nil {
		return ctx, nil
	}
	operation := &ImageOperation{started: time.Now(), observer: observer, event: ImageMetricEvent{Kind: kind, Format: format}}
	return context.WithValue(ctx, imageOperationKey{}, operation), operation
}

func ImageOperationFromContext(ctx context.Context) *ImageOperation {
	if ctx == nil {
		return nil
	}
	operation, _ := ctx.Value(imageOperationKey{}).(*ImageOperation)
	return operation
}

func (o *ImageOperation) CaptureAttempt() {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.finished && o.event.CaptureAttempts < 1024 {
		o.event.CaptureAttempts++
	}
}

func (o *ImageOperation) Decoded(path string, fallback bool) {
	if o == nil {
		return
	}
	var index int
	switch path {
	case "sw_go_image":
		index = 0
	case "hw_videotoolbox":
		index = 1
	case "hw_libjpeg_turbo":
		index = 2
	default:
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.finished && o.event.DecodeCounts[index] < 1024 {
		o.event.DecodeCounts[index]++
		o.event.Fallback = o.event.Fallback || fallback
	}
}

// Finish observes the real process high-water after cleanup, even when the
// operation's work context was canceled. This is observation, not a new effect.
func (o *ImageOperation) Finish(ctx context.Context, outcome ImageOperationOutcome) ImageMetricEvent {
	if o == nil || ctx == nil {
		return ImageMetricEvent{}
	}
	o.mu.Lock()
	if o.finished {
		event := o.event
		o.mu.Unlock()
		return event
	}
	o.finished = true
	o.event.Outcome, o.event.Duration = outcome, time.Since(o.started)
	o.event.PeakMemory = platform.ProcessPeakMemory(context.WithoutCancel(ctx))
	event, observer := o.event, o.observer
	o.mu.Unlock()
	observer(event)
	return event
}
