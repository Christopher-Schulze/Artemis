package fixture

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
)

func websocketScenarios() []Scenario {
	return []Scenario{
		{
			ID:          "websocket-001",
			Path:        "/websocket-001",
			Kind:        KindWebSocket,
			Description: "WebSocket client fixture",
			HTML: `<!doctype html>
<html lang="en"><head>
<meta charset="utf-8"><title>WebSocket</title>
</head><body>
<script>
  window.wsOK = window.WebSocket ? 'ok' : 'fail';
</script>
</body></html>`,
			RunScripts: true,
			Expect: Expect{
				Status:       200,
				Title:        "WebSocket",
				Eval:         "window.wsOK",
				EvalContains: "ok",
			},
		},
		{
			ID:          "websocket-001-ws",
			Path:        "/ws-001",
			Kind:        KindWebSocket,
			Description: "WebSocket echo endpoint for websocket-001",
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Upgrade") != "websocket" {
					http.Error(w, "Not a WebSocket request", http.StatusBadRequest)
					return
				}
				c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
				if err != nil {
					return
				}
				defer func() {
					if closeErr := c.CloseNow(); closeErr != nil {
						slog.Warn("fixture websocket close", slog.Any("error", closeErr))
					}
				}()
				for {
					typ, data, err := c.Read(r.Context())
					if err != nil {
						return
					}
					if err := c.Write(r.Context(), typ, data); err != nil {
						return
					}
				}
			}),
			Expect: Expect{
				Status:   400,
				Contains: []string{"Not a WebSocket request"},
			},
		},
	}
}

func serviceWorkerScenarios() []Scenario {
	return []Scenario{
		{
			ID:          "service-worker-001",
			Path:        "/service-worker-001",
			Kind:        KindServiceWorker,
			Description: "Service Worker registration fixture",
			HTML: `<!doctype html>
<html lang="en"><head>
<meta charset="utf-8"><title>Service Worker</title>
</head><body>
<script>
  window.swOK = 'ok';
  if (navigator.serviceWorker) {
    navigator.serviceWorker.register('/sw-001.js').catch(function() {});
  }
</script>
</body></html>`,
			RunScripts: true,
			Expect: Expect{
				Status:       200,
				Title:        "Service Worker",
				Eval:         "window.swOK",
				EvalContains: "ok",
			},
		},
		{
			ID:          "sw-001",
			Path:        "/sw-001.js",
			Kind:        KindServiceWorker,
			Description: "Service worker script for service-worker-001",
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/javascript")
				w.WriteHeader(http.StatusOK)
				if _, err := fmt.Fprint(w, "self.addEventListener('install', function() {});"); err != nil {
					return
				}
			}),
			Expect: Expect{
				Status:   200,
				Contains: []string{"addEventListener"},
			},
		},
	}
}

func authScenarios() []Scenario {
	return []Scenario{
		{
			ID:          "auth-basic-001",
			Path:        "/auth-basic-001",
			Kind:        KindAuth,
			Description: "HTTP Basic auth fixture",
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				user, pass, ok := r.BasicAuth()
				if !ok || user != "fixture" || pass != "secret" {
					w.Header().Set("WWW-Authenticate", `Basic realm="fixture"`)
					http.Error(w, "Unauthorized", http.StatusUnauthorized)
					return
				}
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(http.StatusOK)
				if _, err := fmt.Fprint(w, "<!doctype html><html><head><title>Authorized</title></head><body>Authorized</body></html>"); err != nil {
					return
				}
			}),
			Expect: Expect{
				Status:   401,
				Contains: []string{"Unauthorized"},
			},
		},
	}
}

func challengeScenarios() []Scenario {
	return []Scenario{
		{
			ID:          "challenge-001",
			Path:        "/challenge-001",
			Kind:        KindChallenge,
			Description: "Challenge-response token fixture",
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-Answer") != "2" {
					w.Header().Set("X-Challenge", "1+1")
					http.Error(w, "Forbidden", http.StatusForbidden)
					return
				}
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(http.StatusOK)
				if _, err := fmt.Fprint(w, "<!doctype html><html><head><title>Challenge OK</title></head><body>Challenge passed</body></html>"); err != nil {
					return
				}
			}),
			Expect: Expect{
				Status:   403,
				Contains: []string{"Forbidden"},
			},
		},
	}
}

func largeScenarios() []Scenario {
	return []Scenario{
		{
			ID:          "large-001",
			Path:        "/large-001",
			Kind:        KindLarge,
			Description: "Large page with many paragraphs",
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(http.StatusOK)
				var b strings.Builder
				b.WriteString("<!doctype html><html><head><meta charset=\"utf-8\"><title>Large</title></head><body>")
				for i := 0; i < 1000; i++ {
					fmt.Fprintf(&b, "<p>Line %d</p>\n", i+1)
				}
				b.WriteString("<p id=\"end\">End</p></body></html>")
				if _, err := w.Write([]byte(b.String())); err != nil {
					return
				}
			}),
			BodyBytes: 1000*len("<p>Line 0000</p>\n") + 100,
			Expect: Expect{
				Status:   200,
				Title:    "Large",
				Contains: []string{"Line 1", "Line 1000", "End"},
			},
		},
	}
}

func malformedScenarios() []Scenario {
	return []Scenario{
		{
			ID:          "malformed-001",
			Path:        "/malformed-001",
			Kind:        KindMalformed,
			Description: "Malformed HTML with unclosed tags",
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(http.StatusOK)
				if _, err := fmt.Fprint(w, "<!doctype html><html><head><title>Malformed</title></head><body><p>Start <div>not closed</body></html>"); err != nil {
					return
				}
			}),
			Expect: Expect{
				Status:   200,
				Title:    "Malformed",
				Contains: []string{"Start", "not closed"},
			},
		},
	}
}

func slowScenarios() []Scenario {
	return []Scenario{
		{
			ID:          "slow-001",
			Path:        "/slow-001",
			Kind:        KindSlow,
			Description: "Slow response fixture",
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				time.Sleep(500 * time.Millisecond)
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(http.StatusOK)
				if _, err := fmt.Fprint(w, "<!doctype html><html><head><title>Slow</title></head><body>slow response</body></html>"); err != nil {
					return
				}
			}),
			Expect: Expect{
				Status:   200,
				Title:    "Slow",
				Contains: []string{"slow response"},
			},
		},
	}
}

func crashScenarios() []Scenario {
	return []Scenario{
		{
			ID:          "crash-001",
			Path:        "/crash-001",
			Kind:        KindCrash,
			Description: "Internal server error fixture",
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}),
			Expect: Expect{
				Status:   500,
				Contains: []string{"Internal Server Error"},
			},
		},
	}
}
