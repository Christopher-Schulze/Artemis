package engine

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Christopher-Schulze/Artemis/css"
)

func TestExternalStylesheetLoaded(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/styles.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		writeTestBody(t, w, `.card { background: navy; padding: 12px; } #title { color: orange; }`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeTestBody(t, w, `<!doctype html><html><head>
<link rel="stylesheet" href="/styles.css">
<style>p { font-size: 16px; }</style>
</head><body>
<h1 id="title" class="card">Hello</h1>
<p>text</p>
</body></html>`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	eng, err := New(testConfig(srv))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer closeTestResource(t, "engine", eng.Close)
	page, err := eng.Fetch(context.Background(), srv.URL+"/", FetchOpts{RunScripts: true})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	defer closeTestResource(t, "page", page.Close)

	cases := []struct {
		expr, want string
	}{
		{`getComputedStyle(document.getElementById('title')).color`, "rgb(255, 165, 0)"},
		{`getComputedStyle(document.getElementById('title')).background`, "rgb(0, 0, 128) none repeat scroll 0% 0% / auto padding-box border-box"},
		{`getComputedStyle(document.getElementById('title')).padding`, "12px"},
		{`getComputedStyle(document.querySelector('p'))['font-size']`, "16px"}, // inline style block still works
	}
	for _, tc := range cases {
		v, err := page.Eval(context.Background(), tc.expr)
		if err != nil {
			t.Errorf("%s: %v", tc.expr, err)
			continue
		}
		if v.String() != tc.want {
			t.Errorf("%s = %q, want %q", tc.expr, v.String(), tc.want)
		}
	}
}

func TestExternalStylesheet404RejectsPartialStyleEvidence(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/missing.css", http.NotFound)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeTestBody(t, w, `<html><head><link rel="stylesheet" href="/missing.css"><style>p { color: red; }</style></head><body><p id="t">x</p></body></html>`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	eng := mustNewTest(t, srv)
	defer closeTestResource(t, "engine", eng.Close)
	page, err := eng.Fetch(context.Background(), srv.URL+"/", FetchOpts{})
	if page != nil {
		closeTestResource(t, "unexpected page", page.Close)
		t.Fatal("missing external CSS produced a partial page")
	}
	var diagnostic *css.Diagnostic
	if !errors.As(err, &diagnostic) || diagnostic.Code != css.DiagnosticLoad {
		t.Fatalf("stylesheet failure lost typed escalation evidence: %v", err)
	}
}
