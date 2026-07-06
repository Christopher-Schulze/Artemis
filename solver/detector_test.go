package solver

import (
	"context"
	"testing"
)

func TestChallengeDetectorCloudflare(t *testing.T) {
	d := NewChallengeDetector()
	info, err := d.Detect(context.Background(), PageSignals{
		Title: "Just a moment...",
		HTML:  `<iframe src="https://challenges.cloudflare.com/turnstile"></iframe>`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if info.Type != TypeCloudflare {
		t.Fatalf("expected cloudflare, got %s", info.Type)
	}
	if info.Confidence < 0.9 {
		t.Fatalf("low confidence: %f", info.Confidence)
	}
}
