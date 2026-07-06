package stealth

import (
	"net/http"
	"strings"
)

// BotSignals captures page-level bot-detection indicators (spec ss28.6.1.1).
type BotSignals struct {
	Blocked        bool
	Challenge      bool
	HTTPStatus     int
	Title          string
	BodyLen        int
	Cloudflare     bool
	CAPTCHAPresent bool
	Reasons        []string
}

// DetectBot evaluates title, body snippet, status, and URL for bot walls.
func DetectBot(status int, title, body, pageURL string) BotSignals {
	out := BotSignals{
		HTTPStatus: status,
		Title:      strings.ToLower(strings.TrimSpace(title)),
		BodyLen:    len(body),
	}
	lowerBody := strings.ToLower(body)
	if status == http.StatusForbidden || status == http.StatusTooManyRequests {
		out.Blocked = true
		out.Reasons = append(out.Reasons, "http_status")
	}
	titlePatterns := []string{
		"access denied", "bot detected", "verification required",
		"just a moment", "attention required", "checking your browser",
		"are you a robot", "unusual traffic", "please verify",
	}
	for _, p := range titlePatterns {
		if strings.Contains(out.Title, p) {
			out.Blocked = true
			out.Reasons = append(out.Reasons, "title:"+p)
			break
		}
	}
	if out.BodyLen > 0 && out.BodyLen < 100 {
		out.Blocked = true
		out.Reasons = append(out.Reasons, "empty_body")
	}
	if strings.Contains(lowerBody, "challenges.cloudflare.com") {
		out.Cloudflare = true
		out.Challenge = true
		out.Reasons = append(out.Reasons, "cloudflare")
	}
	captchaMarkers := []string{
		"g-recaptcha", "hcaptcha.com", "challenges.cloudflare.com",
		"class=\"captcha\"", "class='captcha'", "turnstile",
	}
	for _, m := range captchaMarkers {
		if strings.Contains(lowerBody, m) {
			out.CAPTCHAPresent = true
			out.Challenge = true
			out.Reasons = append(out.Reasons, "captcha")
			break
		}
	}
	return out
}

// SuggestEscalation returns the next stealth tier when bot signals fire.
func SuggestEscalation(current StealthLevel, signals BotSignals) StealthLevel {
	if !signals.Blocked && !signals.Challenge {
		return current
	}
	switch current {
	case StealthDefault:
		return StealthStealth
	case StealthStealth:
		return StealthParanoid
	default:
		return current
	}
}
