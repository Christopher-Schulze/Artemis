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

func TestNewPatchesPresent(t *testing.T) {
	s := Quick()
	checks := []string{
		"chrome.runtime",          // patch 3: chrome.runtime.connect/sendMessage
		"connect",                 // patch 3: callable stubs
		"sendMessage",             // patch 3: callable stubs
		"__artemisRTT",            // patch 16: navigator.connection LIVE RTT
		"setInterval",             // patch 16: 60s refresh
		"effectiveType",           // patch 16: effectiveType
		"downlinkMax",             // patch 17: downlinkMax = Infinity
		"userAgentData",           // patch 18: userAgentData brands
		"getHighEntropyValues",    // patch 18: getHighEntropyValues
		"history",                 // patch 19: history.length = 1
		"__webdriver",             // patch 20: CDP marker cleanup
		"__selenium",              // patch 20: CDP marker cleanup
		"$chrome_asyncScriptInfo", // patch 20: CDP marker cleanup
		"cdc_",                    // patch 20: CDP marker cleanup
		"_expectedUA",             // patch 22: UA version coherence
		"getBattery",              // patch 27: Battery API rejection
		"TypeError",               // patch 27: Battery API rejects with TypeError
	}
	for _, c := range checks {
		if !strings.Contains(s, c) {
			t.Errorf("stealth script missing new patch for %q", c)
		}
	}
}

func TestPatchCountMeetsSpec(t *testing.T) {
	if PatchCount() < 27 {
		t.Fatalf("PatchCount=%d, spec requires >=27", PatchCount())
	}
}
