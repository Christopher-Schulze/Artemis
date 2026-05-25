package solver

import (
	"context"
	"strings"
)

// ChallengeDetector detects CAPTCHA and bot-wall pages.
type ChallengeDetector struct{}

// NewChallengeDetector creates a detector instance.
func NewChallengeDetector() *ChallengeDetector {
	return &ChallengeDetector{}
}

// Detect implements intent detection from page signals (spec Stage 1 heuristics).
func (d *ChallengeDetector) Detect(ctx context.Context, page PageSignals) (*ChallengeInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	title := strings.ToLower(strings.TrimSpace(page.Title))
	html := strings.ToLower(page.HTML)
	if strings.Contains(html, `iframe[src*="challenges.cloudflare.com"]`) ||
		strings.Contains(html, "challenges.cloudflare.com") {
		return &ChallengeInfo{
			Type: TypeCloudflare, Confidence: 0.95,
			ElementRef: "iframe.cloudflare", PageTitle: page.Title,
		}, nil
	}
	if strings.Contains(html, "hcaptcha.com") {
		return &ChallengeInfo{Type: TypeHCaptcha, Confidence: 0.9, PageTitle: page.Title}, nil
	}
	if strings.Contains(html, "g-recaptcha") || strings.Contains(html, "recaptcha") {
		return &ChallengeInfo{Type: TypeRecaptcha, Confidence: 0.9, PageTitle: page.Title}, nil
	}
	titleMarkers := []string{"just a moment", "verify", "checking your browser"}
	for _, m := range titleMarkers {
		if strings.Contains(title, m) {
			return &ChallengeInfo{Type: TypeGeneric, Confidence: 0.75, PageTitle: page.Title}, nil
		}
	}
	if strings.Contains(html, `class="captcha"`) || strings.Contains(html, `class='captcha'`) ||
		strings.Contains(html, `class*="challenge"`) {
		return &ChallengeInfo{Type: TypeGeneric, Confidence: 0.7, PageTitle: page.Title}, nil
	}
	return &ChallengeInfo{Type: TypeNone, Confidence: 1.0, PageTitle: page.Title}, nil
}
