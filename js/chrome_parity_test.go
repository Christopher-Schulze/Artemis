package js

import (
	"context"
	"encoding/json"
	"testing"
)

// The renderless context must look like desktop Chrome on the surfaces
// fingerprinting scripts actually check.
func TestChromeParitySurface(t *testing.T) {
	c := newCtxFromHTMLOpts(t, `<html></html>`, ContextOpts{
		Navigator: NavigatorConfig{UserAgent: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/152.0.0.0 Safari/537.36", Platform: "MacIntel"},
	})
	v, err := c.Eval(context.Background(), `JSON.stringify({
		uad_platform: navigator.userAgentData.platform,
		uad_chrome: navigator.userAgentData.brands.some(b => b.brand === 'Google Chrome' && b.version === '152'),
		uad_noArtemis: !navigator.userAgentData.brands.some(b => b.brand === 'Artemis'),
		vendor: navigator.vendor,
		hc: navigator.hardwareConcurrency,
		dm: navigator.deviceMemory,
		pdf: navigator.pdfViewerEnabled,
		dnt: navigator.doNotTrack,
		wd: navigator.webdriver,
		plugins: navigator.plugins.length,
		mimes: navigator.mimeTypes.length,
		screen_w: screen.width, screen_avail: screen.availHeight,
		chrome: typeof window.chrome === 'object' && typeof window.chrome.runtime === 'object',
		notif: Notification.permission,
		voices: speechSynthesis.getVoices().length,
	})`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(v.String()), &m); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := map[string]interface{}{
		"uad_platform": "macOS", "uad_chrome": true, "uad_noArtemis": true,
		"vendor": "Google Inc.", "hc": float64(8), "dm": float64(8),
		"pdf": true, "dnt": nil, "wd": false,
		"plugins": float64(5), "mimes": float64(2),
		"screen_w": float64(1920), "screen_avail": float64(1040),
		"chrome": true, "notif": "default", "voices": float64(1),
	}
	for k, w := range want {
		if m[k] != w {
			t.Errorf("%s = %v, want %v", k, m[k], w)
		}
	}
}
