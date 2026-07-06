package engine

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIframeInlineScriptExecutes(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/inner", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><body>
			<div id="content"></div>
			<script>
				globalThis.iframeRan = true;
				document.getElementById('content').innerHTML = '<h1>From iframe</h1>';
			</script>
		</body></html>`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><body><iframe id="f" src="/inner"></iframe></body></html>`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	eng, err := New(Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer eng.Close()
	page, err := eng.Fetch(context.Background(), srv.URL+"/", FetchOpts{})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	defer page.Close()

	// Trigger iframe load by accessing contentDocument
	// Per-frame realm: globalThis is isolated per iframe. We verify
	// iframe scripts ran by reading the DOM mutations they performed.
	v, err := page.Eval(context.Background(), `
		(() => {
			const f = document.getElementById('f');
			const d = f.contentDocument;
			if (!d) return 'no-doc';
			const h = d.querySelector('h1');
			return h ? h.textContent : 'no-h1';
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != "From iframe" {
		t.Errorf("got %q, want 'From iframe'", v.String())
	}
}

func TestIframeMultipleEachRuns(t *testing.T) {
	// Per-frame V8 realm: each iframe has its own globals. We verify
	// scripts ran via DOM markers visible through contentDocument
	// (the underlying *html.Node is shared between parent and iframe
	// realms even though the JS contexts are distinct).
	mux := http.NewServeMux()
	mux.HandleFunc("/a", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><body><div id="m"></div><script>document.getElementById('m').setAttribute('data-ran', 'a');</script></body></html>`)
	})
	mux.HandleFunc("/b", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><body><div id="m"></div><script>document.getElementById('m').setAttribute('data-ran', 'b');</script></body></html>`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><body><iframe id="fa" src="/a"></iframe><iframe id="fb" src="/b"></iframe></body></html>`)
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

	v, err := page.Eval(context.Background(), `
		document.getElementById('fa').contentDocument.getElementById('m').getAttribute('data-ran') + ':' +
		document.getElementById('fb').contentDocument.getElementById('m').getAttribute('data-ran')
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != "a:b" {
		t.Errorf("got %q, want a:b", v.String())
	}
}
