package stealth

import (
	"strings"
	"testing"
)

func TestQuickContainsPatches(t *testing.T) {
	s := Quick()
	checks := []string{
		"'webdriver'",
		"'plugins'",
		"'languages'",
		"window.chrome",
		"navigator.permissions",
		"WebGLRenderingContext",
		"CanvasRenderingContext2D",
		"AudioContext",
		"Notification",
		"'mimeTypes'",
		"outerWidth",
		"devicePixelRatio",
		"'vendor'",
		"'platform'",
		"'deviceMemory'",
		"'hardwareConcurrency'",
		"'width'",
		"Intl.DateTimeFormat",
		"'maxTouchPoints'",
		"DeviceOrientationEvent",
		"matchMedia",
		"console.debug",
		"navigator.credentials",
		"speechSynthesis",
	}
	for _, c := range checks {
		if !strings.Contains(s, c) {
			t.Errorf("stealth script missing patch for %q", c)
		}
	}
}

func TestProfileDeterminism(t *testing.T) {
	p := Profile{Seed: "test-seed", ViewportWidth: 1280, ViewportHeight: 720}
	a := Script(p)
	b := Script(p)
	if a != b {
		t.Error("same profile produced different scripts")
	}
}

func TestProfileDifferentiation(t *testing.T) {
	a := Script(Profile{Seed: "a", ViewportWidth: 1920, ViewportHeight: 1080})
	b := Script(Profile{Seed: "b", ViewportWidth: 1920, ViewportHeight: 1080})
	if a == b {
		t.Error("different seeds produced identical scripts")
	}
}
