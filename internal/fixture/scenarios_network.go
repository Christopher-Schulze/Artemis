package fixture

import (
	"fmt"
	"html"
	"net/http"
)

func redirectScenarios() []Scenario {
	return []Scenario{
		{
			ID:          "redirect-001",
			Path:        "/redirect-001",
			Kind:        KindRedirect,
			Description: "First redirect in a short chain",
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, "/redirect-002", http.StatusFound)
			}),
			Expect: Expect{
				Status:   200,
				URL:      "/redirect-final",
				Contains: []string{"Redirect Final"},
			},
		},
		{
			ID:          "redirect-002",
			Path:        "/redirect-002",
			Kind:        KindRedirect,
			Description: "Second redirect in a short chain",
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, "/redirect-final", http.StatusFound)
			}),
			Expect: Expect{
				Status:   200,
				URL:      "/redirect-final",
				Contains: []string{"Redirect Final"},
			},
		},
		{
			ID:          "redirect-final",
			Path:        "/redirect-final",
			Kind:        KindRedirect,
			Description: "Final page after redirect chain",
			HTML: `<!doctype html>
<html lang="en"><head>
<meta charset="utf-8"><title>Redirect Final</title>
</head><body>
<h1>Redirect Final</h1>
<p>You reached the end of the chain.</p>
</body></html>`,
			Expect: Expect{
				Status:   200,
				Title:    "Redirect Final",
				Contains: []string{"end of the chain"},
			},
		},
	}
}

func cookieScenarios() []Scenario {
	return []Scenario{
		{
			ID:          "cookie-001",
			Path:        "/cookie-001",
			Kind:        KindCookie,
			Description: "Set a deterministic cookie",
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Add("Set-Cookie", "fixture-test=1; Path=/; Max-Age=3600; HttpOnly; SameSite=Lax")
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(http.StatusOK)
				if _, err := fmt.Fprint(w, "<!doctype html><html><head><title>Cookie Set</title></head><body>cookie-set</body></html>"); err != nil {
					return
				}
			}),
			Expect: Expect{
				Status:   200,
				Title:    "Cookie Set",
				Contains: []string{"cookie-set"},
			},
		},
		{
			ID:          "cookie-002",
			Path:        "/cookie-002",
			Kind:        KindCookie,
			Description: "Read the cookie set by cookie-001",
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c, err := r.Cookie("fixture-test")
				pair := ""
				if err == nil {
					pair = c.Name + "=" + c.Value
				}
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(http.StatusOK)
				if _, err := fmt.Fprintf(w, "<!doctype html><html><head><title>Cookie Read</title></head><body>cookie:%s</body></html>", html.EscapeString(pair)); err != nil {
					return
				}
			}),
			Expect: Expect{
				Status:   200,
				Title:    "Cookie Read",
				Contains: []string{"cookie:"},
			},
		},
	}
}

func storageScenarios() []Scenario {
	return []Scenario{
		{
			ID:          "storage-001",
			Path:        "/storage-001",
			Kind:        KindStorage,
			Description: "localStorage and sessionStorage roundtrip",
			HTML: `<!doctype html>
<html lang="en"><head>
<meta charset="utf-8"><title>Storage Fixture</title>
</head><body>
<div id="out"></div>
<script>
  localStorage.setItem('fixture', 'value');
  sessionStorage.setItem('fixture', 'session-value');
  document.getElementById('out').textContent = localStorage.getItem('fixture') + '|' + sessionStorage.getItem('fixture');
</script>
</body></html>`,
			RunScripts: true,
			Expect: Expect{
				Status:   200,
				Title:    "Storage Fixture",
				Contains: []string{"value|session-value"},
			},
		},
	}
}
