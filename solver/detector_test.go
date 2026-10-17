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
		t.Fatalf("explicit response and element challenge was lost: %+v", first)
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

func TestChallengeDetectorSeparatesHTTPFailuresFromExplicitChallenges(t *testing.T) {
	cases := []struct {
		name string
		page PageSignals
		want ChallengeType
	}{
		{"forbidden", PageSignals{StatusCode: 403, HTML: "Forbidden"}, TypeNone},
		{"empty forbidden", PageSignals{StatusCode: 403}, TypeNone},
		{"rate limited", PageSignals{StatusCode: 429, HTML: "Too Many Requests", ResponseHeaders: map[string]string{"Retry-After": "5"}}, TypeNone},
		{"empty rate limited", PageSignals{StatusCode: 429}, TypeNone},
		{"empty challenge header", PageSignals{StatusCode: 403, ResponseHeaders: map[string]string{"X-Challenge": " "}}, TypeNone},
		{"unrelated captcha text", PageSignals{StatusCode: 403, ResponseHeaders: map[string]string{"Location": "/articles/captcha", "X-Documentation": "cf-mitigated"}}, TypeNone},
		{"empty mitigation header", PageSignals{StatusCode: 429, ResponseHeaders: map[string]string{"Cf-Mitigated": " "}}, TypeNone},
		{"explicit challenge header", PageSignals{StatusCode: 403, HTML: "Forbidden", ResponseHeaders: map[string]string{"x-cHaLlEnGe": "1+1"}}, TypeGeneric},
		{"successful challenge header", PageSignals{StatusCode: 200, ResponseHeaders: map[string]string{"X-Challenge": "1+1"}}, TypeGeneric},
		{"mitigation header", PageSignals{StatusCode: 429, ResponseHeaders: map[string]string{"Cf-Mitigated": "challenge"}}, TypeGeneric},
		{"captcha header", PageSignals{StatusCode: 200, ResponseHeaders: map[string]string{"X-Captcha": "required"}}, TypeGeneric},
		{"challenge title", PageSignals{StatusCode: 403, Title: "Just a moment..."}, TypeGeneric},
		{"challenge body", PageSignals{StatusCode: 429, HTML: `<div class="captcha"></div>`}, TypeGeneric},
		{"challenge network", PageSignals{StatusCode: 403, NetworkURLs: []string{"https://challenges.cloudflare.com/turnstile"}}, TypeCloudflare},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			observed, err := NewChallengeDetector().Detect(t.Context(), test.page)
			if err != nil || observed.Type != test.want {
				t.Fatalf("HTTP outcome or actual challenge was misclassified: %+v err=%v", observed, err)
			}
			if test.page.StatusCode != 403 && test.page.StatusCode != 429 {
				return
			}
			for _, signal := range observed.Signals {
				if signal.Source == SignalResponse && signal.Marker == "status code" && signal.Present {
					return
				}
			}
			t.Fatal("actual HTTP failure diagnostic was dropped")
		})
	}
}
