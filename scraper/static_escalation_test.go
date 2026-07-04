package scraper

import "testing"

func TestStaticRenderlessEscalation(t *testing.T) {
	if got := StaticRenderlessEscalation(200, 1024, false); got != RenderModeStatic {
		t.Fatalf("got %s", got)
	}
	if got := StaticRenderlessEscalation(404, 1024, false); got != RenderModeEscalated {
		t.Fatalf("got %s", got)
	}
	if got := StaticRenderlessEscalation(200, 1024, true); got != RenderModeEscalated {
		t.Fatalf("got %s", got)
	}
}

func TestShouldEscalate(t *testing.T) {
	tests := []struct {
		name string
		sig  EscalationSignals
		want RenderMode
	}{
		{"happy static", EscalationSignals{StatusCode: 200, BodyLen: 4096, BodyHasContent: true}, RenderModeStatic},
		{"error status", EscalationSignals{StatusCode: 503, BodyLen: 4096}, RenderModeEscalated},
		{"tiny body", EscalationSignals{StatusCode: 200, BodyLen: 32}, RenderModeEscalated},
		{"infinite scroll", EscalationSignals{StatusCode: 200, BodyLen: 4096, InfiniteScroll: true}, RenderModeEscalated},
		{"captcha", EscalationSignals{StatusCode: 200, BodyLen: 4096, HasCAPTCHA: true}, RenderModeEscalated},
		{"SPA shell", EscalationSignals{StatusCode: 200, BodyLen: 4096, IsSPA: true}, RenderModeEscalated},
		{"web components", EscalationSignals{StatusCode: 200, BodyLen: 4096, HasWebComponents: true}, RenderModeEscalated},
		{"service worker", EscalationSignals{StatusCode: 200, BodyLen: 4096, HasServiceWorker: true}, RenderModeEscalated},
		{"login wall", EscalationSignals{StatusCode: 200, BodyLen: 4096, HasLoginWall: true}, RenderModeEscalated},
		{"JS-heavy shell no content", EscalationSignals{StatusCode: 200, BodyLen: 4096, ScriptCount: 20, BodyHasContent: false}, RenderModeEscalated},
		{"JS-heavy with content stays static", EscalationSignals{StatusCode: 200, BodyLen: 4096, ScriptCount: 20, BodyHasContent: true}, RenderModeStatic},
		{"moderate scripts no content", EscalationSignals{StatusCode: 200, BodyLen: 4096, ScriptCount: 5, BodyHasContent: false}, RenderModeStatic},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ShouldEscalate(tt.sig); got != tt.want {
				t.Fatalf("ShouldEscalate(%+v) = %s, want %s", tt.sig, got, tt.want)
			}
		})
	}
}

func TestDetectEscalationSignals(t *testing.T) {
	t.Run("content page", func(t *testing.T) {
		body := []byte(`<!doctype html><html><body><article><h1>Title</h1><p>Para 1</p><p>Para 2</p><p>Para 3</p></article></body></html>`)
		sig := DetectEscalationSignals(200, body)
		if sig.HasCAPTCHA || sig.IsSPA || sig.HasWebComponents || sig.HasServiceWorker || sig.HasLoginWall || sig.InfiniteScroll {
			t.Fatalf("content page should have no escalation signals: %+v", sig)
		}
		if !sig.BodyHasContent {
			t.Fatal("content page should have BodyHasContent")
		}
		if ShouldEscalate(sig) != RenderModeStatic {
			t.Fatal("content page should stay static")
		}
	})

	t.Run("SPA shell", func(t *testing.T) {
		body := []byte(`<!doctype html><html><head><script src="/app.js"></script></head><body><div id="root"></div></body></html>`)
		sig := DetectEscalationSignals(200, body)
		if !sig.IsSPA {
			t.Fatal("SPA shell should be detected")
		}
		if ShouldEscalate(sig) != RenderModeEscalated {
			t.Fatal("SPA shell should escalate")
		}
	})

	t.Run("captcha page", func(t *testing.T) {
		body := []byte(`<!doctype html><html><body><div class="cf-challenge">Checking...</div></body></html>`)
		sig := DetectEscalationSignals(200, body)
		if !sig.HasCAPTCHA {
			t.Fatal("captcha should be detected")
		}
		if ShouldEscalate(sig) != RenderModeEscalated {
			t.Fatal("captcha page should escalate")
		}
	})

	t.Run("web components", func(t *testing.T) {
		body := []byte(`<!doctype html><html><body><script>customElements.define('my-widget', class extends HTMLElement {})</script></body></html>`)
		sig := DetectEscalationSignals(200, body)
		if !sig.HasWebComponents {
			t.Fatal("web components should be detected")
		}
		if ShouldEscalate(sig) != RenderModeEscalated {
			t.Fatal("web components page should escalate")
		}
	})

	t.Run("service worker", func(t *testing.T) {
		body := []byte(`<!doctype html><html><body><script>navigator.serviceWorker.register('/sw.js')</script></body></html>`)
		sig := DetectEscalationSignals(200, body)
		if !sig.HasServiceWorker {
			t.Fatal("service worker should be detected")
		}
		if ShouldEscalate(sig) != RenderModeEscalated {
			t.Fatal("service worker page should escalate")
		}
	})

	t.Run("infinite scroll", func(t *testing.T) {
		body := []byte(`<!doctype html><html><body><div data-infinite="true">Content</div><script>new IntersectionObserver(()=>{})</script></body></html>`)
		sig := DetectEscalationSignals(200, body)
		if !sig.InfiniteScroll {
			t.Fatal("infinite scroll should be detected")
		}
		if ShouldEscalate(sig) != RenderModeEscalated {
			t.Fatal("infinite scroll page should escalate")
		}
	})

	t.Run("login wall", func(t *testing.T) {
		body := []byte(`<!doctype html><html><head><meta http-equiv="refresh" content="0;url=/login"></head><body>Login required</body></html>`)
		sig := DetectEscalationSignals(200, body)
		if !sig.HasLoginWall {
			t.Fatal("login wall should be detected")
		}
		if ShouldEscalate(sig) != RenderModeEscalated {
			t.Fatal("login wall page should escalate")
		}
	})

	t.Run("error status", func(t *testing.T) {
		body := []byte(`<!doctype html><html><body>404 Not Found</body></html>`)
		sig := DetectEscalationSignals(404, body)
		if ShouldEscalate(sig) != RenderModeEscalated {
			t.Fatal("404 should escalate")
		}
	})

	t.Run("empty body", func(t *testing.T) {
		body := []byte(``)
		sig := DetectEscalationSignals(200, body)
		if ShouldEscalate(sig) != RenderModeEscalated {
			t.Fatal("empty body should escalate")
		}
	})
}
