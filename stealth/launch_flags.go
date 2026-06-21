package stealth

import (
	"strings"
)

// LaunchFlags carries Chromium launch arguments with harmful-flag blocking.
type LaunchFlags struct {
	Args []string
}

// HARMFUL_ARGS are launch flags that must always be blocked because they
// enable automation detection or cause browser instability.
// Ref: research/webstack/Scrapling-main/scrapling/engines/constants.py:L15-L21
var blockedLaunchFlags = []string{
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

// DEFAULT_ARGS are safe default launch flags that speed up Chromium.
// Ref: research/webstack/Scrapling-main/scrapling/engines/constants.py:L23-L34
var defaultArgs = []string{
	"--no-pings",
	"--no-first-run",
	"--disable-infobars",
	"--disable-breakpad",
	"--no-service-autorun",
	"--homepage=about:blank",
	"--password-store=basic",
	"--disable-hang-monitor",
	"--no-default-browser-check",
	"--disable-session-crashed-bubble",
	"--disable-search-engine-choice-screen",
}

// STEALTH_ARGS are anti-detection launch flags active in StealthStealth and
// StealthParanoid modes. Makes the browser faster and less detectable.
// Ref: research/webstack/Scrapling-main/scrapling/engines/constants.py:L36-L99
var stealthArgs = []string{
	"--test-type",
	"--lang=en-US",
	"--mute-audio",
	"--disable-sync",
	"--hide-scrollbars",
	"--disable-logging",
	"--start-maximized",
	"--enable-async-dns",
	"--accept-lang=en-US",
	"--use-mock-keychain",
	"--disable-translate",
	"--disable-voice-input",
	"--window-position=0,0",
	"--disable-wake-on-wifi",
	"--ignore-gpu-blocklist",
	"--enable-tcp-fast-open",
	"--enable-web-bluetooth",
	"--disable-cloud-import",
	"--disable-print-preview",
	"--disable-dev-shm-usage",
	"--metrics-recording-only",
	"--disable-crash-reporter",
	"--disable-partial-raster",
	"--disable-gesture-typing",
	"--disable-checker-imaging",
	"--disable-prompt-on-repost",
	"--force-color-profile=srgb",
	"--font-render-hinting=none",
	"--aggressive-cache-discard",
	"--disable-cookie-encryption",
	"--disable-domain-reliability",
	"--disable-threaded-animation",
	"--disable-threaded-scrolling",
	"--enable-simple-cache-backend",
	"--disable-background-networking",
	"--enable-surface-synchronization",
	"--disable-image-animation-resync",
	"--disable-renderer-backgrounding",
	"--disable-ipc-flooding-protection",
	"--prerender-from-omnibox=disabled",
	"--safebrowsing-disable-auto-update",
	"--disable-offer-upload-credit-cards",
	"--disable-background-timer-throttling",
	"--disable-new-content-rendering-timeout",
	"--run-all-compositor-stages-before-draw",
	"--disable-client-side-phishing-detection",
	"--disable-backgrounding-occluded-windows",
	"--disable-layer-tree-host-memory-pressure",
	"--autoplay-policy=user-gesture-required",
	"--disable-offer-store-unmasked-wallet-cards",
	"--disable-blink-features=AutomationControlled",
	"--disable-component-extensions-with-background-pages",
	"--enable-features=NetworkService,NetworkServiceInProcess,TrustTokens,TrustTokensAlwaysAllowIssuance",
	"--blink-settings=primaryHoverType=2,availableHoverTypes=2,primaryPointerType=4,availablePointerTypes=4",
	"--disable-features=AudioServiceOutOfProcess,TranslateUI,BlinkGenPropertyTrees",
}

// WebRTC leak prevention flags. Active in StealthStealth and StealthParanoid.
// Prevents WebRTC from leaking real IP address through STUN requests.
var webrtcLeakPreventionArgs = []string{
	"--webrtc-ip-handling-policy=disable_non_proxied_udp",
	"--force-webrtc-ip-handling-policy",
}

// CanvasNoiseArgs returns canvas fingerprint noise launch flags.
// Conditional on hideCanvas being true (spec: --fingerprinting-canvas-image-data-noise).
func CanvasNoiseArgs(hideCanvas bool) []string {
	if !hideCanvas {
		return nil
	}
	return []string{"--fingerprinting-canvas-image-data-noise"}
}

// DefaultLaunchFlags returns safe defaults for Artemis launches.
// Includes DEFAULT_ARGS plus the core anti-detection flag.
func DefaultLaunchFlags() LaunchFlags {
	args := make([]string, 0, len(defaultArgs)+1)
	args = append(args, defaultArgs...)
	args = append(args, "--disable-blink-features=AutomationControlled")
	return LaunchFlags{Args: args}
}

// StealthLaunchFlags returns launch flags appropriate for the given stealth level.
// StealthDefault: DEFAULT_ARGS + core anti-detection only.
// StealthStealth: DEFAULT_ARGS + full STEALTH_ARGS + WebRTC leak prevention.
// StealthParanoid: DEFAULT_ARGS + full STEALTH_ARGS + WebRTC + canvas noise.
func StealthLaunchFlags(level StealthLevel) LaunchFlags {
	args := make([]string, 0, len(defaultArgs)+len(stealthArgs)+len(webrtcLeakPreventionArgs)+1)
	args = append(args, defaultArgs...)

	switch level {
	case StealthStealth:
		args = append(args, stealthArgs...)
		args = append(args, webrtcLeakPreventionArgs...)
	case StealthParanoid:
		args = append(args, stealthArgs...)
		args = append(args, webrtcLeakPreventionArgs...)
		args = append(args, CanvasNoiseArgs(true)...)
	default:
		// StealthDefault: just core anti-detection
		args = append(args, "--disable-blink-features=AutomationControlled")
	}

	return LaunchFlags{Args: args}
}

// Allows returns false if the flag is in the HARMFUL_ARGS blocklist.
func (f LaunchFlags) Allows(flag string) bool {
	flag = strings.TrimSpace(flag)
	for _, b := range blockedLaunchFlags {
		if strings.EqualFold(flag, b) {
			return false
		}
	}
	return true
}

// FilterBlocked removes any harmful flags from the args list.
func (f LaunchFlags) FilterBlocked() []string {
	var out []string
	for _, arg := range f.Args {
		if f.Allows(arg) {
			out = append(out, arg)
		}
	}
	return out
}

// HasWebRTCProtection returns true if WebRTC leak prevention flags are present.
func (f LaunchFlags) HasWebRTCProtection() bool {
	for _, arg := range f.Args {
		if strings.HasPrefix(arg, "--webrtc-ip-handling-policy") {
			return true
		}
	}
	return false
}

// HasCanvasNoise returns true if canvas fingerprint noise flag is present.
func (f LaunchFlags) HasCanvasNoise() bool {
	for _, arg := range f.Args {
		if strings.Contains(arg, "fingerprinting-canvas-image-data-noise") {
			return true
		}
	}
	return false
}

// HasStealthArg returns true if the full STEALTH_ARGS set is present.
func (f LaunchFlags) HasStealthArg() bool {
	for _, arg := range f.Args {
		if strings.Contains(arg, "--blink-settings=primaryHoverType") {
			return true
		}
	}
	return false
}
