package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExternalScriptLoadingExecutesInOrder(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/lib.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		writeTestBody(t, w, `globalThis.fromExt = 'A';`)
	})
	mux.HandleFunc("/render.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		writeTestBody(t, w, `
			document.body.innerHTML = '<h1>External-rendered: ' + globalThis.fromExt + '</h1>';
		`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeTestBody(t, w, `<!doctype html><html><body>
			<script src="/lib.js"></script>
			<script>globalThis.fromInline = 'B';</script>
			<script src="/render.js"></script>
		</body></html>`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	eng, err := New(testConfig(srv))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer closeTestResource(t, "engine close", eng.Close)

	page, err := eng.Fetch(context.Background(), srv.URL+"/", FetchOpts{RunScripts: true})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	defer closeTestResource(t, "page close", page.Close)

	html := page.HTML()
	if !strings.Contains(html, "External-rendered: A") {
		t.Errorf("html missing external content, got: %s", html)
	}

	v, err := page.Eval(context.Background(), `globalThis.fromInline`)
	if err != nil {
		t.Fatalf("Eval inline script: %v", err)
	}
	if v.String() != "B" {
		t.Errorf("inline script global = %q, want B", v.String())
	}
}

func TestExternalScriptCachedPerPage(t *testing.T) {
	hits := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/lib.js", func(w http.ResponseWriter, r *http.Request) {
		hits++
		writeTestBody(t, w, `globalThis.libLoaded = (globalThis.libLoaded||0) + 1;`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeTestBody(t, w, `<html><body>
			<script src="/lib.js"></script>
			<script src="/lib.js"></script>
			<script src="/lib.js"></script>
		</body></html>`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	eng, err := New(testConfig(srv))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer closeTestResource(t, "engine close", eng.Close)
	page, err := eng.Fetch(context.Background(), srv.URL+"/", FetchOpts{RunScripts: true})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	defer closeTestResource(t, "page close", page.Close)
	if hits != 1 {
		t.Errorf("server hits = %d, want 1 (cache must dedupe per page)", hits)
	}
	v, err := page.Eval(context.Background(), `globalThis.libLoaded`)
	if err != nil {
		t.Fatalf("Eval cached script: %v", err)
	}
	if v.Int64() != 3 {
		t.Errorf("libLoaded = %d, want 3 (cached body still re-eval'd in document order)", v.Int64())
	}
}

func TestExternalScript404DoesNotAbort(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeTestBody(t, w, `<html><body>
			<script src="/missing.js"></script>
			<script>globalThis.afterMissing = true;</script>
		</body></html>`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	eng, err := New(testConfig(srv))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer closeTestResource(t, "engine close", eng.Close)
	page, err := eng.Fetch(context.Background(), srv.URL+"/", FetchOpts{RunScripts: true})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	defer closeTestResource(t, "page close", page.Close)
	v, err := page.Eval(context.Background(), `globalThis.afterMissing`)
	if err != nil {
		t.Fatalf("Eval after missing script: %v", err)
	}
	if !v.Bool() {
		t.Error("script after 404 did not run")
	}
}
