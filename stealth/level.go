package stealth

// StealthLevel is the configured anti-detection tier (spec ss28.6.1.1).
type StealthLevel string

const (
	StealthDefault  StealthLevel = "default"
	StealthStealth  StealthLevel = "stealth"
	StealthParanoid StealthLevel = "paranoid"
)

// PatchCountFor returns embedded patch count for the level.
func PatchCountFor(level StealthLevel) int {
	switch level {
	case StealthParanoid:
		return PatchCount() + 2 // 37 fingerprint + 2 paranoid (typing rhythm, referrer)
	case StealthStealth:
		return PatchCount()
	default:
		return 0
	}
}

// LaunchFlagCountFor returns launch-flag budget for the level.
func LaunchFlagCountFor(level StealthLevel) int {
	switch level {
	case StealthStealth, StealthParanoid:
		return LaunchFlagCount()
	default:
		return 0
	}
}

// LaunchFlagCount is the zero-cost launch flag set size for public stealth tiers.
func LaunchFlagCount() int {
	return len(stealthLaunchFlags())
}

func stealthLaunchFlags() []string {
	return []string{
		"--disable-blink-features=AutomationControlled",
		"--disable-automation",
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-background-networking",
		"--disable-background-timer-throttling",
		"--disable-backgrounding-occluded-windows",
		"--disable-breakpad",
		"--disable-client-side-phishing-detection",
		"--disable-component-update",
		"--disable-default-apps",
		"--disable-dev-shm-usage",
		"--disable-domain-reliability",
		"--disable-extensions",
		"--disable-features=TranslateUI",
		"--disable-hang-monitor",
		"--disable-ipc-flooding-protection",
		"--disable-popup-blocking",
		"--disable-prompt-on-repost",
		"--disable-renderer-backgrounding",
		"--disable-sync",
		"--enable-features=NetworkService,NetworkServiceInProcess",
		"--force-color-profile=srgb",
		"--metrics-recording-only",
		"--password-store=basic",
		"--use-mock-keychain",
		"--hide-scrollbars",
		"--mute-audio",
		"--no-sandbox",
		"--disable-setuid-sandbox",
		"--disable-gpu-sandbox",
		"--disable-software-rasterizer",
		"--disable-webgl",
		"--disable-webgl2",
		"--disable-accelerated-2d-canvas",
		"--disable-accelerated-video-decode",
		"--disable-accelerated-video-encode",
		"--disable-features=VizDisplayCompositor",
		"--disable-features=IsolateOrigins,site-per-process",
		"--disable-site-isolation-trials",
		"--disable-features=ImprovedCookieControls",
		"--disable-features=LazyFrameLoading",
		"--disable-features=GlobalMediaControls",
		"--disable-features=MediaRouter",
		"--disable-features=OptimizationHints",
		"--disable-features=InterestFeedContentSuggestions",
		"--disable-features=CertificateTransparencyComponentUpdater",
		"--disable-features=AutofillServerCommunication",
		"--disable-features=CalculateNativeWinOcclusion",
		"--disable-features=BackForwardCache",
		"--disable-features=AcceptCHFrame",
		"--disable-features=AvoidUnnecessaryBeforeUnloadCheckSync",
		"--disable-features=DestroyProfileOnBrowserClose",
		"--disable-features=DialMediaRouteProvider",
		"--disable-features=MediaSessionService",
		"--disable-features=PaintHolding",
		"--disable-features=PrivacySandboxSettings4",
		"--disable-features=SharedArrayBuffer",
		"--disable-features=WebOTP",
		"--disable-features=WebPayments",
		"--disable-features=WebUSB",
	}
}
