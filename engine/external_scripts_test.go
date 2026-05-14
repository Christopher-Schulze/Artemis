package engine

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExternalScriptLoadingExecutesInOrder(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/lib.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, `globalThis.fromExt = 'A';`)
	})
	mux.HandleFunc("/render.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, `
			document.body.innerHTML = '<h1>External-rendered: ' + globalThis.fromExt + '</h1>';
		`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!doctype html><html><body>
			<script src="/lib.js"></script>
			<script>globalThis.fromInline = 'B';</script>
			<script src="/render.js"></script>
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

	html := page.HTML()
	if !strings.Contains(html, "External-rendered: A") {
		t.Errorf("html missing external content, got: %s", html)
	}

	v, _ := page.Eval(context.Background(), `globalThis.fromInline`)
	if v.String() != "B" {
		t.Errorf("inline script global = %q, want B", v.String())
	}
}

func TestExternalScriptCachedPerPage(t *testing.T) {
	hits := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/lib.js", func(w http.ResponseWriter, r *http.Request) {
		hits++
		fmt.Fprint(w, `globalThis.libLoaded = (globalThis.libLoaded||0) + 1;`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><body>
			<script src="/lib.js"></script>
			<script src="/lib.js"></script>
			<script src="/lib.js"></script>
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
	if hits != 1 {
		t.Errorf("server hits = %d, want 1 (cache must dedupe per page)", hits)
	}
	v, _ := page.Eval(context.Background(), `globalThis.libLoaded`)
	if v.Int64() != 3 {
		t.Errorf("libLoaded = %d, want 3 (cached body still re-eval'd in document order)", v.Int64())
	}
}

func TestExternalScript404DoesNotAbort(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><body>
			<script src="/missing.js"></script>
			<script>globalThis.afterMissing = true;</script>
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
	v, _ := page.Eval(context.Background(), `globalThis.afterMissing`)
	if !v.Bool() {
		t.Error("script after 404 did not run")
	}
}
