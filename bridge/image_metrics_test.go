package bridge

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"sync"
	"testing"
)

type metricImageMemory struct {
	held    int64
	denied  error
	cleanup error
}

func (m *metricImageMemory) ReserveMemory(_ context.Context, size int64) (func() error, error) {
	if m.denied != nil {
		return nil, m.denied
	}
	m.held += size
	return func() error { m.held -= size; return m.cleanup }, nil
}

func TestImageMetricsDescribeRealDecodeAndJoinedSettlement(t *testing.T) {
	var raw bytes.Buffer
	if err := jpeg.Encode(&raw, image.NewNRGBA(image.Rect(0, 0, 80, 40)), nil); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		denied  bool
		cleanup bool
		cancel  bool
		want    ImageOperationOutcome
	}{
		{"success", false, false, false, ImageOutcomeSuccess},
		{"denied", true, false, false, ImageOutcomeDenied},
		{"settlement_failed", false, true, false, ImageOutcomeFailed},
		{"canceled", false, false, true, ImageOutcomeCanceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			owner := &metricImageMemory{}
			if test.denied {
				owner.denied = errors.New("private capacity-denial details")
			}
			if test.cleanup {
				owner.cleanup = errors.New("private settlement details")
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if test.cancel {
				cancel()
			}
			pipeline := NewJPEGPipeline()
			pipeline.MaxWidth, pipeline.MaxHeight = 40, 20
			var events []ImageMetricEvent
			pipeline.ImageMetrics = func(event ImageMetricEvent) {
				if owner.held != 0 {
					t.Error("metrics published before actual memory settlement")
				}
				events = append(events, event)
			}
			result, err := pipeline.PrepareModelImage(ctx, raw.Bytes(), FormatJPEG, FormatJPEG, owner)
			if len(events) != 1 || events[0].Outcome != test.want || events[0].Duration <= 0 || events[0].CaptureAttempts != 0 {
				t.Fatalf("actual lifecycle metric: %+v %v", events, err)
			}
			decoded := uint32(0)
			for _, count := range events[0].DecodeCounts {
				decoded += count
			}
			if (test.denied || test.cancel) && decoded != 0 || (!test.denied && !test.cancel) && decoded != 1 {
				t.Fatalf("decoder counter inferred unexecuted work: %+v", events[0])
			}
			if test.want == ImageOutcomeSuccess {
				if err != nil || result.Width != 40 || result.Height != 20 {
					t.Fatalf("metrics changed real model output: %+v %v", result, err)
				}
			} else if err == nil || len(result.Bytes) != 0 {
				t.Fatal("failed operation published image bytes")
			}
		})
	}
}

func TestImageMetricsBoundCountersAndFinishOnce(t *testing.T) {
	events := make(chan ImageMetricEvent, 2)
	ctx, operation := BeginImageOperation(t.Context(), ImageOperationCapture, FormatPNG, func(event ImageMetricEvent) { events <- event })
	if ImageOperationFromContext(ctx) != operation {
		t.Fatal("operation context lost its owner")
	}
	var workers sync.WaitGroup
	for range 8 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for range 256 {
				operation.CaptureAttempt()
				operation.Decoded("sw_go_image", true)
				operation.Decoded("private unrecognized path", false)
			}
		}()
	}
	workers.Wait()
	first := operation.Finish(ctx, ImageOutcomeSuccess)
	operation.CaptureAttempt()
	operation.Decoded("hw_videotoolbox", false)
	second := operation.Finish(ctx, ImageOutcomeFailed)
	if first != second || first.CaptureAttempts != 1024 || first.DecodeCounts != [3]uint32{1024, 0, 0} || !first.Fallback || len(events) != 1 {
		t.Fatalf("metric window was mutable or unbounded: %+v %+v emissions=%d", first, second, len(events))
	}
}
