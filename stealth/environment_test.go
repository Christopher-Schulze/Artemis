package stealth

import (
	"strings"
	"testing"
)

func testEnvironmentFacts() EnvironmentFacts {
	return EnvironmentFacts{
		UserAgent:           "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36",
		ChromeVersion:       "126.0.0.0",
		Platform:            "MacIntel",
		PlatformVersion:     "14.0",
		Architecture:        "arm",
		Locale:              "de-DE",
		Languages:           []string{"de-DE", "de", "en-US"},
		Timezone:            "Europe/Berlin",
		ViewportWidth:       1280,
		ViewportHeight:      720,
		DevicePixelRatio:    2,
		HardwareConcurrency: 10,
		DeviceMemoryGB:      16,
		WebGLVendor:         "measured-vendor",
		WebGLRenderer:       "measured-renderer",
		NetworkRTTMillis:    42,
		Measured:            true,
	}
}

func TestEnvironmentProfileStableAndConsistent(t *testing.T) {
	first, err := NewEnvironmentProfile("session-1", StealthStealth, testEnvironmentFacts(), true)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewEnvironmentProfile("session-1", StealthStealth, testEnvironmentFacts(), true)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := first.ScriptHash()
	if err != nil || first.ProfileID != second.ProfileID || hash == "" {
		t.Fatal("profile identity or script hash was not stable")
	}
	if err := first.ValidateConsistency(ConsistencyProbe{
		UserAgent: first.UserAgent, ClientHintsUA: `"Chromium";v="126"`, ClientHintsPlat: "macOS",
		Platform: first.Platform, Locale: first.Locale, Timezone: first.Timezone,
		ViewportWidth: first.ViewportWidth, ViewportHeight: first.ViewportHeight,
		DevicePixelRatio: first.DevicePixelRatio, WebGLVendor: first.WebGLVendor, WebGLRenderer: first.WebGLRenderer,
	}); err != nil {
		t.Fatal(err)
	}
	if err := first.ValidateConsistency(ConsistencyProbe{Platform: "Linux x86_64"}); err == nil {
		t.Fatal("mismatched platform was accepted")
	}
}

func TestEnvironmentScriptsAreEscapedAndContextSpecific(t *testing.T) {
	profile, err := NewEnvironmentProfile("session-script", StealthStealth, testEnvironmentFacts(), true)
	if err != nil {
		t.Fatal(err)
	}
	page, err := NewDocumentScript(profile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(page, "measured-renderer") || !strings.Contains(page, "[native code]") {
		t.Fatal("page script did not contain profile values and native mask")
	}
	if !strings.Contains(page, "parameter === 0x9291") || !strings.Contains(page, "parameter === 0x9292") || strings.Contains(page, "=== 0x1F00") {
		t.Fatal("WebGL patch must cover unmasked debug params (0x9291/0x9292), not masked VENDOR/RENDERER")
	}
	worker, err := NewWorkerScript(profile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(worker, "document") || strings.Contains(worker, "globalThis.window") {
		t.Fatal("worker script contains page-only globals")
	}
	if !strings.Contains(worker, "Chrome/126.0.0.0") || !strings.Contains(worker, "navigator") {
		t.Fatal("worker script lost shared identity")
	}
	if strings.Contains(page, "secret-value") {
		t.Fatal("unexpected secret in page script")
	}
}

func TestEnvironmentDefaultDoesNotInjectAdvancedOverrides(t *testing.T) {
	profile, err := NewEnvironmentProfile("session-default", StealthDefault, testEnvironmentFacts(), false)
	if err != nil {
		t.Fatal(err)
	}
	page, err := NewDocumentScript(profile)
	if err != nil {
		t.Fatal(err)
	}
	if page != "" {
		t.Fatal("default level unexpectedly injected stealth script")
	}
}

func TestEnvironmentAdvancedLevelRequiresLegalGate(t *testing.T) {
	if _, err := NewEnvironmentProfile("session-denied", StealthParanoid, testEnvironmentFacts(), false); err == nil {
		t.Fatal("advanced profile without legal gate was accepted")
	}
	facts := testEnvironmentFacts()
	facts.Measured = false
	if _, err := NewEnvironmentProfile("session-unmeasured", StealthStealth, facts, true); err == nil {
		t.Fatal("advanced profile without measured facts was accepted")
	}
}
