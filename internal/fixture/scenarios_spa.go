package fixture

import (
	"fmt"
	"net/http"
)

func spaScenarios() []Scenario {
	return []Scenario{
		{
			ID:          "spa-001",
			Path:        "/spa-001",
			Kind:        KindSPA,
			Description: "Single-page app fixture that fetches JSON",
			HTML: `<!doctype html>
<html lang="en"><head>
<meta charset="utf-8"><title>SPA Fixture</title>
</head><body>
<div id="app">Loading...</div>
<script>
  fetch(location.origin + '/api/spa-data')
    .then(r => r.json())
    .then(data => { document.getElementById('app').textContent = data.text; })
    .catch(err => { document.getElementById('app').textContent = 'err: ' + err.message; });
</script>
</body></html>`,
			RunScripts:  true,
			AsyncFetch:  true,
			WaitForIdle: true,
			Expect: Expect{
				Status:   200,
				Title:    "SPA Fixture",
				Contains: []string{"Hello SPA"},
			},
		},
		{
			ID:          "spa-001-api",
			Path:        "/api/spa-data",
			Kind:        KindSPA,
			Description: "JSON endpoint consumed by spa-001",
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, `{"text":"Hello SPA"}`)
			}),
			Expect: Expect{
				Status:   200,
				Contains: []string{"Hello SPA"},
			},
		},
	}
}
