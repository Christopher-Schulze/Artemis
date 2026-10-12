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

func TestChallengeDetectorUsesResponseAndElementSignalsDeterministically(t *testing.T) {
	d := NewChallengeDetector()
	page := PageSignals{
		URL:             "https://example.com/account",
		StatusCode:      429,
		HTML:            "<html><body>blocked</body></html>",
		NetworkURLs:     []string{"https://z.example/challenge", "https://a.example/verify"},
		ResponseHeaders: map[string]string{"X-Challenge": "captcha", "Retry-After": "5"},
		ElementMarkers:  []string{"z-captcha", "a-challenge"},
	}
	first, err := d.Detect(context.Background(), page)
	if err != nil {
		t.Fatal(err)
	}
	second, err := d.Detect(context.Background(), page)
	if err != nil {
		t.Fatal(err)
	}
	if first.Type != TypeGeneric || len(first.Signals) == 0 {
		t.Fatalf("unexpected status-only challenge: %+v", first)
	}
	if len(first.Signals) != len(second.Signals) {
		t.Fatalf("signal count changed: %d vs %d", len(first.Signals), len(second.Signals))
	}
	for i := range first.Signals {
		if first.Signals[i] != second.Signals[i] {
			t.Fatalf("signal order changed at %d: %+v vs %+v", i, first.Signals[i], second.Signals[i])
		}
	}
}
