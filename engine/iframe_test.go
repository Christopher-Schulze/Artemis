package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIFrameContentDocumentAccessible(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/inner", func(w http.ResponseWriter, r *http.Request) {
		writeTestBody(t, w, `<html><body><h1 id="inner-h">Inner heading</h1><p>Inner paragraph</p></body></html>`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeTestBody(t, w, `<!doctype html><html><body>
			<h1>Outer</h1>
			<iframe id="f" src="/inner"></iframe>
		</body></html>`)
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

	v, err := page.Eval(context.Background(), `
		(() => {
			const f = document.getElementById('f');
			const d = f.contentDocument;
			if (!d) return 'no doc';
			const h = d.getElementById('inner-h');
			if (!h) return 'no inner-h';
			return d.title + '|' + h.textContent;
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if !strings.Contains(v.String(), "Inner heading") {
		t.Errorf("contentDocument query returned %q", v.String())
	}
}

func TestIFrameContentWindowPostMessage(t *testing.T) {
	// Per-frame V8 realm: parent sends postMessage to iframe;
	// iframe-internal script with window.addEventListener('message')
	// receives it and writes to its own DOM. Parent reads back via
	// contentDocument.
	mux := http.NewServeMux()
	mux.HandleFunc("/inner", func(w http.ResponseWriter, r *http.Request) {
		writeTestBody(t, w, `<html><body><div id="rec"></div><script>
			window.addEventListener('message', (ev) => {
				document.getElementById('rec').setAttribute('data-msg', String(ev.data));
			});
		</script></body></html>`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeTestBody(t, w, `<html><body><iframe id="f" src="/inner"></iframe>
		<script>
			const f = document.getElementById('f');
			f.contentDocument; // force iframe load + iframe scripts
			f.contentWindow.postMessage('hello-iframe');
		</script></body></html>`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	eng := mustNewTest(t, srv)
	defer closeTestResource(t, "engine", eng.Close)
	page, err := eng.Fetch(context.Background(), srv.URL+"/", FetchOpts{RunScripts: true})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	defer closeTestResource(t, "page", page.Close)
	v, err := page.Eval(context.Background(), `document.getElementById('f').contentDocument.getElementById('rec').getAttribute('data-msg')`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != "hello-iframe" {
		t.Errorf("iframe data-msg = %q, want hello-iframe", v.String())
	}
}
