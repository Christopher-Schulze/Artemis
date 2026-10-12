package scraper

import (
	"testing"
)

// TestFallbackRateMatrix measures the escalation/fallback rate across
// a representative page-type matrix. This is the baseline measurement
// for TASK-2337 Sub-Task 2: "measure fallback-rate delta".
//
// Page types:
//   - content: server-rendered article pages (should stay static)
//   - spa-shell: SPA with empty root div (should escalate)
//   - captcha: bot-challenge pages (should escalate)
//   - webcomponents: custom-elements pages (should escalate)
//   - serviceworker: SW-registered pages (should escalate)
//   - infinite-scroll: infinite-scroll pages (should escalate)
//   - login-wall: auth-redirect pages (should escalate)
//   - js-heavy-shell: many scripts, no content (should escalate)
//   - error: 4xx/5xx pages (should escalate)
//   - empty: zero-byte response (should escalate)
func TestFallbackRateMatrix(t *testing.T) {
	type pageCase struct {
		name       string
		statusCode int
		body       string
		wantMode   RenderMode
	}
	cases := []pageCase{
		{"content", 200, `<!doctype html><html><body><article><h1>T</h1><p>P1</p><p>P2</p><p>P3</p></article></body></html>`, RenderModeStatic},
		{"spa-shell", 200, `<!doctype html><html><head><script src="/app.js"></script></head><body><div id="root"></div></body></html>`, RenderModeEscalated},
		{"captcha", 200, `<!doctype html><html><body><div class="cf-challenge">Checking</div></body></html>`, RenderModeEscalated},
		{"webcomponents", 200, `<!doctype html><html><body><script>customElements.define('x-w', class extends HTMLElement {})</script></body></html>`, RenderModeEscalated},
		{"serviceworker", 200, `<!doctype html><html><body><script>navigator.serviceWorker.register('/sw.js')</script></body></html>`, RenderModeEscalated},
		{"infinite-scroll", 200, `<!doctype html><html><body><div data-infinite="true">C</div><script>new IntersectionObserver(()=>{})</script></body></html>`, RenderModeEscalated},
		{"login-wall", 200, `<!doctype html><html><head><meta http-equiv="refresh" content="0;url=/login"></head><body>Login required</body></html>`, RenderModeEscalated},
		{"js-heavy-shell", 200, `<!doctype html><html><head>` + repeatScript(20) + `</head><body><div id="x"></div></body></html>`, RenderModeEscalated},
		{"error-404", 404, `<!doctype html><html><body>404 Not Found</body></html>`, RenderModeEscalated},
		{"error-503", 503, `<!doctype html><html><body>503 Service Unavailable</body></html>`, RenderModeEscalated},
		{"empty", 200, ``, RenderModeEscalated},
		{"content-with-scripts", 200, `<!doctype html><html><body><article><h1>T</h1><p>P1</p><p>P2</p><p>P3</p></article><script>console.log('ok')</script></body></html>`, RenderModeStatic},
	}

	total := len(cases)
	escalated := 0
	staticCount := 0
	correct := 0

	for _, c := range cases {
		sig := DetectEscalationSignals(c.statusCode, []byte(c.body))
		got := ShouldEscalate(sig)
		if got == c.wantMode {
			correct++
		}
		if got == RenderModeEscalated {
			escalated++
		} else {
			staticCount++
		}
	}

	fallbackRate := float64(escalated) / float64(total)
	t.Logf("Fallback-rate matrix: %d/%d escalated (%.1f%%), %d static, %d/%d correct",
		escalated, total, fallbackRate*100, staticCount, correct, total)

	if correct != total {
		t.Errorf("classification accuracy: %d/%d (%.1f%%)", correct, total, float64(correct)/float64(total)*100)
	}

	// Expected distribution: 2 static (content, content-with-scripts),
	// 9 escalated. Fallback rate ~81.8% on this synthetic matrix.
	// Real-world fallback rate depends on the actual URL set.
	expectedStatic := 2
	if staticCount != expectedStatic {
		t.Errorf("static count: got %d, want %d", staticCount, expectedStatic)
	}
}

func repeatScript(n int) string {
	s := ""
	for i := 0; i < n; i++ {
		s += `<script src="/chunk` + string(rune('0'+i)) + `.js"></script>`
	}
	return s
}
