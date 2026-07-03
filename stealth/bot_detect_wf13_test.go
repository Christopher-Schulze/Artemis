package stealth

import (
	"fmt"
	"testing"
)

// =============================================================================
// SP-artemis-stealth-SEC (bot_detect.go, security_privacy)
// Claim: DetectBot denies bot-blocked pages (403/429 status, blocked titles,
// empty body, CAPTCHA markers), SuggestEscalation denies escalation when
// no signals are present
// =============================================================================

func TestWFArtemisStealth_BotDetectDeniesInvalidInput(t *testing.T) {
	// Security: bot detection must deny blocked/challenged pages to
	// prevent silent scraping failures and bot-detection bypass.

	cases := []struct {
		name   string
		signal BotSignals
		expect bool // true if Blocked or Challenge expected
	}{
		{
			"http_403_forbidden",
			DetectBot(403, "Access Denied", "<html></html>", "https://example.com"),
			true,
		},
		{
			"http_429_rate_limited",
			DetectBot(429, "Too Many Requests", "<html></html>", "https://example.com"),
			true,
		},
		{
			"title_access_denied",
			DetectBot(200, "Access Denied", "<html><body>content</body></html>", "https://example.com"),
			true,
		},
		{
			"title_bot_detected",
			DetectBot(200, "Bot Detected", "<html><body>content</body></html>", "https://example.com"),
			true,
		},
		{
			"title_just_a_moment",
			DetectBot(200, "Just a moment...", "<html><body>content</body></html>", "https://example.com"),
			true,
		},
		{
			"title_verify",
			DetectBot(200, "Are you a robot?", "<html><body>content</body></html>", "https://example.com"),
			true,
		},
		{
			"empty_body",
			DetectBot(200, "Normal", "<html></html>", "https://example.com"),
			true,
		},
		{
			"cloudflare_challenge",
			DetectBot(200, "Normal", `<html><script src="challenges.cloudflare.com"></script></html>`, "https://example.com"),
			true,
		},
		{
			"recaptcha_present",
			DetectBot(200, "Normal", `<html><div class="g-recaptcha"></div></html>`, "https://example.com"),
			true,
		},
		{
			"hcaptcha_present",
			DetectBot(200, "Normal", `<html><script src="https://hcaptcha.com/1/api.js"></script></html>`, "https://example.com"),
			true,
		},
	}
	blocked := 0
	for _, c := range cases {
		if !c.signal.Blocked && !c.signal.Challenge {
			t.Fatalf("%s: expected Blocked or Challenge, got neither", c.name)
		}
		if len(c.signal.Reasons) == 0 {
			t.Fatalf("%s: expected reasons, got none", c.name)
		}
		blocked++
	}
	denyRate := float64(blocked) / float64(len(cases))
	fmt.Printf("deny_rate=%.1f blocked=1\n", denyRate)
	if denyRate != 1.0 {
		t.Fatalf("expected deny_rate=1.0 (all blocked pages detected), got %.1f", denyRate)
	}

	// Baseline: normal page is not blocked (positive control)
	normal := DetectBot(200, "Normal Page", "<html><body>This is a normal page with enough content to pass the bot detection check and not trigger any false positives.</body></html>", "https://example.com")
	if normal.Blocked {
		t.Fatal("expected normal page to not be blocked")
	}
	if normal.Challenge {
		t.Fatal("expected normal page to not have challenge")
	}
	if normal.Cloudflare {
		t.Fatal("expected normal page to not have cloudflare")
	}
	if normal.CAPTCHAPresent {
		t.Fatal("expected normal page to not have CAPTCHA")
	}

	// Baseline: SuggestEscalation denies escalation when no signals
	noEscalation := SuggestEscalation(StealthDefault, BotSignals{})
	if noEscalation != StealthDefault {
		t.Fatalf("expected no escalation without signals, got %v", noEscalation)
	}

	// Baseline: SuggestEscalation escalates from Default to Stealth on signals
	escalation := SuggestEscalation(StealthDefault, BotSignals{Blocked: true})
	if escalation != StealthStealth {
		t.Fatalf("expected escalation to Stealth, got %v", escalation)
	}

	// Baseline: SuggestEscalation escalates from Stealth to Paranoid on challenge
	escalation2 := SuggestEscalation(StealthStealth, BotSignals{Challenge: true})
	if escalation2 != StealthParanoid {
		t.Fatalf("expected escalation to Paranoid, got %v", escalation2)
	}

	// Baseline: SuggestEscalation stays at Paranoid (max level)
	escalation3 := SuggestEscalation(StealthParanoid, BotSignals{Blocked: true})
	if escalation3 != StealthParanoid {
		t.Fatalf("expected to stay at Paranoid, got %v", escalation3)
	}
}
