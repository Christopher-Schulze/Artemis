package engine

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExternalStylesheetLoaded(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/styles.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		fmt.Fprint(w, `.card { background: navy; padding: 12px; } #title { color: orange; }`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!doctype html><html><head>
<link rel="stylesheet" href="/styles.css">
<style>p { font-size: 16px; }</style>
</head><body>
<h1 id="title" class="card">Hello</h1>
<p>text</p>
</body></html>`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	eng, err := New(Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer eng.Close()
	page, err := eng.Fetch(context.Background(), srv.URL+"/", FetchOpts{RunScripts: true})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	defer page.Close()

	cases := []struct {
		expr, want string
	}{
		{`getComputedStyle(document.getElementById('title')).color`, "orange"},
		{`getComputedStyle(document.getElementById('title')).background`, "navy"},
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

func TestExternalStylesheet404Tolerated(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><head><link rel="stylesheet" href="/missing.css"><style>p { color: red; }</style></head><body><p id="t">x</p></body></html>`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	eng, _ := New(Config{})
	defer eng.Close()
	page, err := eng.Fetch(context.Background(), srv.URL+"/", FetchOpts{})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	defer page.Close()

	v, _ := page.Eval(context.Background(), `getComputedStyle(document.getElementById('t')).color`)
	if v.String() != "red" {
		t.Errorf("inline style still applies despite 404 css: got %q", v.String())
	}
}
