package actions

import (
	"bytes"
	"context"
	"errors"
	"image"
	"testing"

	"github.com/Christopher-Schulze/Artemis/bridge"
)

func TestScreenshotMetricsClassifyActualFailureClasses(t *testing.T) {
	for _, test := range []struct {
		result Outcome
		want   bridge.ImageOperationOutcome
	}{
		{Outcome{Success: true}, bridge.ImageOutcomeSuccess},
		{Outcome{Failure: FailurePolicy}, bridge.ImageOutcomeDenied},
		{Outcome{Failure: FailureCancelled}, bridge.ImageOutcomeCanceled},
		{Outcome{Failure: FailureTimeout}, bridge.ImageOutcomeCanceled},
		{Outcome{Failure: FailureValidation}, bridge.ImageOutcomeInvalid},
		{Outcome{Failure: FailurePostcondition}, bridge.ImageOutcomeFailed},
	} {
		if got := screenshotMetricOutcome(test.result); got != test.want {
			t.Fatalf("actual outcome %s classified as %s, want %s", test.result.Failure, got, test.want)
		}
	}
}

func TestScreenshotMetricsRealChromiumAndPreEffectDenials(t *testing.T) {
	fixture := newActionFixture(t)
	var events []bridge.ImageMetricEvent
	fixture.runtime.imageMetrics = func(event bridge.ImageMetricEvent) { events = append(events, event) }
	for _, request := range []Request{{Kind: KindScreenshot, Format: "png"}, {Kind: KindScreenshot, ForModel: true}} {
		result := fixture.runtime.Execute(t.Context(), request)
		if !result.Success {
			t.Fatalf("actual native screenshot: %+v", result)
		}
		pixels, format, err := image.Decode(bytes.NewReader(result.Bytes))
		if err != nil || pixels.Bounds().Dx() != result.Width || pixels.Bounds().Dy() != result.Height {
			t.Fatalf("actual captured pixels differ from result: %s %v", format, err)
		}
		event := events[len(events)-1]
		if event.Kind != bridge.ImageOperationCapture || event.Outcome != bridge.ImageOutcomeSuccess || event.CaptureAttempts != 1 || event.Duration <= 0 || event.DecodeCounts[0]+event.DecodeCounts[1]+event.DecodeCounts[2] != 1 {
			t.Fatalf("actual capture metric: %+v", event)
		}
	}
	fixture.runtime.policy = func(context.Context, Request) error { return errors.New("private action rejection details") }
	denied := fixture.runtime.Execute(t.Context(), Request{Kind: KindScreenshot})
	last := events[len(events)-1]
	if denied.Success || last.Outcome != bridge.ImageOutcomeDenied || last.CaptureAttempts != 0 || last.DecodeCounts != [3]uint32{} {
		t.Fatalf("denial fabricated a capture or decoder: %+v %+v", denied, last)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	fixture.runtime.Execute(ctx, Request{Kind: KindScreenshot})
	last = events[len(events)-1]
	if last.Outcome != bridge.ImageOutcomeCanceled || last.CaptureAttempts != 0 || len(events) != 4 {
		t.Fatalf("cancellation or nested preparation fabricated metrics: %+v events=%d", last, len(events))
	}
}
