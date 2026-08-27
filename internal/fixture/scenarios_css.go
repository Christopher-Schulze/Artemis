package fixture

import (
	"fmt"
	"net/http"
)

func cssScenarios() []Scenario {
	return []Scenario{
		{
			ID:          "css-001",
			Path:        "/css-001",
			Kind:        KindCSS,
			Description: "Page with an external stylesheet",
			HTML: `<!doctype html>
<html lang="en"><head>
<meta charset="utf-8"><title>CSS Fixture</title>
<link rel="stylesheet" href="/css-001-style">
</head><body>
<h1 id="styled">CSS fixture</h1>
<p class="muted">Muted paragraph</p>
</body></html>`,
			Expect: Expect{
				Status:   200,
				Title:    "CSS Fixture",
				Contains: []string{"CSS fixture", "Muted paragraph"},
			},
		},
		{
			ID:          "css-001-style",
			Path:        "/css-001-style",
			Kind:        KindCSS,
			Description: "External stylesheet for css-001",
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/css; charset=utf-8")
				w.WriteHeader(http.StatusOK)
				if _, err := fmt.Fprint(w, "#styled { color: red; } .muted { color: gray; } body { font-family: sans-serif; }"); err != nil {
					return
				}
			}),
			Expect: Expect{
				Status:   200,
				Contains: []string{"body", "#styled"},
			},
		},
	}
}
