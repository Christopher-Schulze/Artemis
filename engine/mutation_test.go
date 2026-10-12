package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPageMarkdownReflectsJSMutation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTestBody(t, w, `<!doctype html><html><body><div id="root"></div><script>
			const root = document.getElementById('root');
			root.innerHTML = '<h1>Mutated Heading</h1><p>Hello, <b>world</b>.</p>';
		</script></body></html>`)
	}))
	defer srv.Close()

	eng, err := New(testConfig(srv))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer closeTestResource(t, "engine", eng.Close)

	page, err := eng.Fetch(context.Background(), srv.URL, FetchOpts{RunInlineScripts: true})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	defer closeTestResource(t, "page", page.Close)

	md := page.Markdown()
	if !strings.Contains(md, "# Mutated Heading") {
		t.Errorf("Markdown missing mutated H1, got:\n%s", md)
	}
	if !strings.Contains(md, "**world**") {
		t.Errorf("Markdown missing inserted bold, got:\n%s", md)
	}

	htmlOut := page.HTML()
	if !strings.Contains(htmlOut, "Mutated Heading") {
		t.Errorf("HTML missing mutated content")
	}
}

func TestPageHTMLReflectsJSAppendChild(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTestBody(t, w, `<!doctype html><html><body><ul id="list"></ul><script>
			const ul = document.getElementById('list');
			for (const t of ['a','b','c']) {
				const li = document.createElement('li');
				li.textContent = t;
				ul.appendChild(li);
			}
		</script></body></html>`)
	}))
	defer srv.Close()

	eng, err := New(testConfig(srv))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer closeTestResource(t, "engine", eng.Close)

	page, err := eng.Fetch(context.Background(), srv.URL, FetchOpts{RunInlineScripts: true})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	defer closeTestResource(t, "page", page.Close)

	md := page.Markdown()
	for _, want := range []string{"- a", "- b", "- c"} {
		if !strings.Contains(md, want) {
			t.Errorf("Markdown missing %q, got:\n%s", want, md)
		}
	}
}
