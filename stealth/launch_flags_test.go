package stealth

import (
	"strings"
	"testing"
)

func TestDefaultLaunchFlags(t *testing.T) {
	flags := DefaultLaunchFlags()
	if len(flags.Args) == 0 {
		t.Fatal("expected non-empty default flags")
	}
	found := false
	for _, arg := range flags.Args {
		if arg == "--disable-blink-features=AutomationControlled" {
			found = true
		}
	}
	if !found {
		t.Fatal("default flags must include --disable-blink-features=AutomationControlled")
	}
}

func TestStealthLaunchFlagsDefault(t *testing.T) {
	flags := StealthLaunchFlags(StealthDefault)
	if flags.HasStealthArg() {
		t.Fatal("StealthDefault should not have full stealth args")
	}
	if flags.HasWebRTCProtection() {
		t.Fatal("StealthDefault should not have WebRTC protection")
	}
}

func TestStealthLaunchFlagsStealth(t *testing.T) {
	flags := StealthLaunchFlags(StealthStealth)
	if !flags.HasStealthArg() {
		t.Fatal("StealthStealth should have full stealth args")
	}
	if !flags.HasWebRTCProtection() {
		t.Fatal("StealthStealth should have WebRTC protection")
	}
	if flags.HasCanvasNoise() {
		t.Fatal("StealthStealth should not have canvas noise")
	}
}

func TestStealthLaunchFlagsParanoid(t *testing.T) {
	flags := StealthLaunchFlags(StealthParanoid)
	if !flags.HasStealthArg() {
		t.Fatal("StealthParanoid should have full stealth args")
	}
	if !flags.HasWebRTCProtection() {
		t.Fatal("StealthParanoid should have WebRTC protection")
	}
	if !flags.HasCanvasNoise() {
		t.Fatal("StealthParanoid should have canvas noise")
	}
}

func TestHarmfulArgsBlocked(t *testing.T) {
	harmful := []string{
		"--enable-automation",
		"--disable-popup-blocking",
		"--disable-component-update",
		"--disable-default-apps",
		"--disable-extensions",
		"--disable-web-security",
		"--disable-features=IsolateOrigins",
		"--no-sandbox",
		"--disable-site-isolation-trials",
	}
	for _, h := range harmful {
		if DefaultLaunchFlags().Allows(h) {
			t.Errorf("harmful flag %q should be blocked", h)
		}
	}
}

func TestAllowsSafeFlag(t *testing.T) {
	if !DefaultLaunchFlags().Allows("--no-first-run") {
		t.Fatal("safe flag --no-first-run should be allowed")
	}
}

func TestFilterBlocked(t *testing.T) {
	flags := LaunchFlags{Args: []string{
		"--no-first-run",
		"--enable-automation",
		"--disable-blink-features=AutomationControlled",
		"--no-sandbox",
	}}
	filtered := flags.FilterBlocked()
	if len(filtered) != 2 {
		t.Fatalf("expected 2 flags after filtering, got %d", len(filtered))
	}
	for _, f := range filtered {
		if strings.Contains(f, "enable-automation") || strings.Contains(f, "no-sandbox") {
			t.Errorf("harmful flag not filtered: %s", f)
		}
	}
}

func TestCanvasNoiseArgs(t *testing.T) {
	args := CanvasNoiseArgs(true)
	if len(args) != 1 {
		t.Fatalf("expected 1 canvas noise arg, got %d", len(args))
	}
	if !strings.Contains(args[0], "fingerprinting-canvas-image-data-noise") {
		t.Fatalf("expected canvas noise flag, got %s", args[0])
	}
}

func TestCanvasNoiseArgsDisabled(t *testing.T) {
	args := CanvasNoiseArgs(false)
	if len(args) != 0 {
		t.Fatalf("expected 0 args when hideCanvas=false, got %d", len(args))
	}
}

func TestHasWebRTCProtection(t *testing.T) {
	flags := LaunchFlags{Args: []string{"--webrtc-ip-handling-policy=disable_non_proxied_udp"}}
	if !flags.HasWebRTCProtection() {
		t.Fatal("expected WebRTC protection")
	}
	flags = LaunchFlags{Args: []string{"--no-first-run"}}
	if flags.HasWebRTCProtection() {
		t.Fatal("did not expect WebRTC protection")
	}
}

func TestHasCanvasNoise(t *testing.T) {
	flags := LaunchFlags{Args: []string{"--fingerprinting-canvas-image-data-noise"}}
	if !flags.HasCanvasNoise() {
		t.Fatal("expected canvas noise")
	}
	flags = LaunchFlags{Args: []string{"--no-first-run"}}
	if flags.HasCanvasNoise() {
		t.Fatal("did not expect canvas noise")
	}
}

func TestHasStealthArg(t *testing.T) {
	flags := LaunchFlags{Args: []string{"--blink-settings=primaryHoverType=2,availableHoverTypes=2"}}
	if !flags.HasStealthArg() {
		t.Fatal("expected stealth arg")
	}
	flags = LaunchFlags{Args: []string{"--no-first-run"}}
	if flags.HasStealthArg() {
		t.Fatal("did not expect stealth arg")
	}
}

func TestStealthArgsContainsKeyFlags(t *testing.T) {
	flags := StealthLaunchFlags(StealthStealth)
	expectedSubstrings := []string{
		"--disable-blink-features=AutomationControlled",
		"--webrtc-ip-handling-policy=disable_non_proxied_udp",
		"--force-webrtc-ip-handling-policy",
		"--force-color-profile=srgb",
		"--font-render-hinting=none",
		"--mute-audio",
		"--start-maximized",
		"--blink-settings=primaryHoverType",
	}
	allArgs := strings.Join(flags.Args, " ")
	for _, sub := range expectedSubstrings {
		if !strings.Contains(allArgs, sub) {
			t.Errorf("stealth flags missing %q", sub)
		}
	}
}

func TestDefaultArgsPresent(t *testing.T) {
	flags := DefaultLaunchFlags()
	allArgs := strings.Join(flags.Args, " ")
	expectedDefaults := []string{
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-infobars",
	}
	for _, d := range expectedDefaults {
		if !strings.Contains(allArgs, d) {
			t.Errorf("default flags missing %q", d)
		}
	}
}
