package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/Christopher-Schulze/Artemis/css"
)

func TestIframeInlineScriptExecutes(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/inner", func(w http.ResponseWriter, r *http.Request) {
		writeTestBody(t, w, `<html><body>
			<div id="content"></div>
			<script>
				globalThis.iframeRan = true;
				document.getElementById('content').innerHTML = '<h1>From iframe</h1>';
			</script>
		</body></html>`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeTestBody(t, w, `<html><body><iframe id="f" src="/inner"></iframe></body></html>`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	eng, err := New(testConfig(srv))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer closeTestResource(t, "engine", eng.Close)
	page, err := eng.Fetch(context.Background(), srv.URL+"/", FetchOpts{})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	defer closeTestResource(t, "page", page.Close)

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
		writeTestBody(t, w, `<html><body><div id="m"></div><script>document.getElementById('m').setAttribute('data-ran', 'a');</script></body></html>`)
	})
	mux.HandleFunc("/b", func(w http.ResponseWriter, r *http.Request) {
		writeTestBody(t, w, `<html><body><div id="m"></div><script>document.getElementById('m').setAttribute('data-ran', 'b');</script></body></html>`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeTestBody(t, w, `<html><body><iframe id="fa" src="/a"></iframe><iframe id="fb" src="/b"></iframe></body></html>`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	eng := mustNewTest(t, srv)
	defer closeTestResource(t, "engine", eng.Close)
	page, err := eng.Fetch(context.Background(), srv.URL+"/", FetchOpts{})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	defer closeTestResource(t, "page", page.Close)

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

func TestIframeCSSFailureCannotExposePartialDocumentOrDisappearInCatch(t *testing.T) {
	for _, test := range []struct {
		name, body string
	}{
		{"stylesheet", `<style>@media all {p{color:red}}</style><p>partial</p>`},
		{"script", `<p id="x" style="color:red;broken"></p><script>try {document.getElementById('x').style.color} catch(e) {}</script>`},
	} {
		t.Run(test.name, func(t *testing.T) {
			var loads atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/frame" {
					loads.Add(1)
					writeTestBody(t, w, test.body)
					return
				}
				writeTestBody(t, w, `<iframe id="f" src="/frame"></iframe>`)
			}))
			defer server.Close()
			eng := mustNewTest(t, server)
			defer closeTestResource(t, "engine", eng.Close)
			page := mustFetch(t, eng, server.URL, FetchOpts{})
			defer closeTestResource(t, "page", page.Close)
			for attempt := 0; attempt < 2; attempt++ {
				value, err := page.Eval(t.Context(), `try { globalThis.partialFrame = document.getElementById('f').contentDocument } catch(e) {}; 'hidden'`)
				if value != nil || !css.IsDiagnostic(err) {
					t.Fatalf("child CSS failure disappeared: %v %v", value, err)
				}
			}
			value, err := page.Eval(t.Context(), "globalThis.partialFrame === null")
			if err != nil || !value.Bool() || loads.Load() != 1 {
				t.Fatalf("failed child became visible or was repeatedly allocated: loads=%d err=%v", loads.Load(), err)
			}
		})
	}
}

func TestIframeMessageCaughtCSSFailureReachesParentExecution(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/frame" {
			writeTestBody(t, w, `<p id="x" style="color:red;broken"></p><script>window.addEventListener('message', function() {try {document.getElementById('x').style.color} catch(e) {}})</script>`)
			return
		}
		writeTestBody(t, w, `<iframe id="f" src="/frame"></iframe>`)
	}))
	defer server.Close()
	eng := mustNewTest(t, server)
	defer closeTestResource(t, "engine", eng.Close)
	page := mustFetch(t, eng, server.URL, FetchOpts{})
	defer closeTestResource(t, "page", page.Close)
	value, err := page.Eval(t.Context(), `document.getElementById('f').contentDocument !== null`)
	if err != nil || !value.Bool() {
		t.Fatalf("valid child context failed: %v", err)
	}
	value, err = page.Eval(t.Context(), `try {document.getElementById('f').contentWindow.postMessage('message','*')} catch(e) {}; 'hidden'`)
	if value != nil || !css.IsDiagnostic(err) {
		t.Fatalf("child message erased typed CSS failure: %v %v", value, err)
	}
}

func TestIframeStylesheetUsesChildDocumentAndFirstBase(t *testing.T) {
	var correct atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/frame/index":
			writeTestBody(t, w, `<base href="assets/"><link rel="stylesheet" href="theme.css"><p id="x"></p><script>document.getElementById('x').setAttribute('data-color',getComputedStyle(document.getElementById('x')).color)</script>`)
		case "/frame/assets/theme.css":
			correct.Add(1)
			writeTestBody(t, w, `#x {color:blue}`)
		case "/":
			writeTestBody(t, w, `<iframe id="f" src="/frame/index"></iframe>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	eng := mustNewTest(t, server)
	defer closeTestResource(t, "engine", eng.Close)
	page := mustFetch(t, eng, server.URL, FetchOpts{})
	defer closeTestResource(t, "page", page.Close)
	value, err := page.Eval(t.Context(), `document.getElementById('f').contentDocument.getElementById('x').getAttribute('data-color')`)
	if err != nil || value.String() != "rgb(0, 0, 255)" || correct.Load() != 1 {
		t.Fatalf("child stylesheet resolved against parent or ignored first base: hits=%d value=%v err=%v", correct.Load(), value, err)
	}
}
