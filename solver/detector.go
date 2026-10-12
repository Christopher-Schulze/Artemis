package solver

import (
	"context"
	"net/url"
	"sort"
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
	visual := strings.ToLower(page.VisualText)
	signals := make([]ChallengeSignal, 0, 8)
	add := func(source ChallengeSignalSource, marker string, weight int, present bool) {
		if present {
			signals = append(signals, ChallengeSignal{Source: source, Marker: marker, Weight: weight, Present: true})
		}
	}
	for _, marker := range []struct {
		text   string
		weight int
	}{
		{"just a moment", 4}, {"checking your browser", 4}, {"access denied", 4},
		{"bot detected", 4}, {"verification required", 3}, {"attention required", 3},
		{"unusual traffic", 3}, {"are you a robot", 3}, {"please verify", 3}, {"verify", 2},
	} {
		add(SignalTitle, marker.text, marker.weight, strings.Contains(title, marker.text))
	}
	add(SignalDOM, "challenges.cloudflare.com", 6, strings.Contains(html, "challenges.cloudflare.com"))
	add(SignalDOM, "g-recaptcha", 6, strings.Contains(html, "g-recaptcha") || strings.Contains(html, "recaptcha"))
	add(SignalDOM, "hcaptcha.com", 6, strings.Contains(html, "hcaptcha.com"))
	add(SignalDOM, "challenge marker", 3, strings.Contains(html, "captcha") || strings.Contains(html, "challenge"))
	add(SignalVisual, "challenge text", 3, strings.Contains(visual, "verify you are human") || strings.Contains(visual, "checking your browser"))
	for _, marker := range sortedStrings(page.ElementMarkers) {
		lower := strings.ToLower(strings.TrimSpace(marker))
		if lower == "" {
			continue
		}
		add(SignalDOM, lower, 4, strings.Contains(lower, "captcha") || strings.Contains(lower, "challenge") || strings.Contains(lower, "verify"))
	}
	for _, networkURL := range sortedStrings(page.NetworkURLs) {
		lower := strings.ToLower(networkURL)
		add(SignalNetwork, "challenges.cloudflare.com", 5, strings.Contains(lower, "challenges.cloudflare.com"))
		add(SignalNetwork, "recaptcha", 5, strings.Contains(lower, "recaptcha"))
		add(SignalNetwork, "hcaptcha", 5, strings.Contains(lower, "hcaptcha"))
	}
	networkCloudflare := false
	networkHCaptcha := false
	networkRecaptcha := false
	for _, networkURL := range page.NetworkURLs {
		lower := strings.ToLower(networkURL)
		networkCloudflare = networkCloudflare || strings.Contains(lower, "challenges.cloudflare.com")
		networkHCaptcha = networkHCaptcha || strings.Contains(lower, "hcaptcha")
		networkRecaptcha = networkRecaptcha || strings.Contains(lower, "recaptcha")
	}
	headerNames := make([]string, 0, len(page.ResponseHeaders))
	for name := range page.ResponseHeaders {
		headerNames = append(headerNames, name)
	}
	sort.Strings(headerNames)
	for _, name := range headerNames {
		value := page.ResponseHeaders[name]
		marker := strings.ToLower(name + ":" + value)
		add(SignalResponse, "challenge response", 3, strings.Contains(marker, "cf-mitigated") || strings.Contains(marker, "captcha"))
	}
	add(SignalResponse, "status code", 4, page.StatusCode == 403 || page.StatusCode == 429)
	add(SignalResponse, "empty body", 2, len(strings.TrimSpace(page.HTML)) < 100 && (page.StatusCode == 403 || page.StatusCode == 429))
	domain := ""
	if parsed, err := url.Parse(page.URL); err == nil {
		domain = strings.ToLower(parsed.Hostname())
	}
	if strings.Contains(html, `iframe[src*="challenges.cloudflare.com"]`) ||
		strings.Contains(html, "challenges.cloudflare.com") {
		return &ChallengeInfo{
			Type: TypeCloudflare, Confidence: 0.95,
			ElementRef: "iframe.cloudflare", PageTitle: page.Title, Domain: domain, Signals: signals,
		}, nil
	}
	if strings.Contains(html, "hcaptcha.com") {
		return &ChallengeInfo{Type: TypeHCaptcha, Confidence: 0.9, PageTitle: page.Title, Domain: domain, Signals: signals}, nil
	}
	if strings.Contains(html, "g-recaptcha") || strings.Contains(html, "recaptcha") {
		return &ChallengeInfo{Type: TypeRecaptcha, Confidence: 0.9, PageTitle: page.Title, Domain: domain, Signals: signals}, nil
	}
	if networkCloudflare {
		return &ChallengeInfo{Type: TypeCloudflare, Confidence: 0.85, PageTitle: page.Title, Domain: domain, Signals: signals}, nil
	}
	if networkHCaptcha {
		return &ChallengeInfo{Type: TypeHCaptcha, Confidence: 0.85, PageTitle: page.Title, Domain: domain, Signals: signals}, nil
	}
	if networkRecaptcha {
		return &ChallengeInfo{Type: TypeRecaptcha, Confidence: 0.85, PageTitle: page.Title, Domain: domain, Signals: signals}, nil
	}
	titleMarkers := []string{"just a moment", "verify", "checking your browser", "access denied", "bot detected", "verification required", "attention required", "unusual traffic", "are you a robot", "please verify"}
	for _, m := range titleMarkers {
		if strings.Contains(title, m) {
			return &ChallengeInfo{Type: TypeGeneric, Confidence: 0.75, PageTitle: page.Title, Domain: domain, Signals: signals}, nil
		}
	}
	if strings.Contains(html, `class="captcha"`) || strings.Contains(html, `class='captcha'`) ||
		strings.Contains(html, `class*="challenge"`) || strings.Contains(html, "checking your browser") ||
		strings.Contains(html, "verify you are human") || page.StatusCode == 403 || page.StatusCode == 429 {
		return &ChallengeInfo{Type: TypeGeneric, Confidence: 0.7, PageTitle: page.Title, Domain: domain, Signals: signals}, nil
	}
	return &ChallengeInfo{Type: TypeNone, Confidence: 1.0, PageTitle: page.Title, Domain: domain, Signals: signals}, nil
}

func sortedStrings(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}
