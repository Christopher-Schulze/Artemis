package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPageFetchRendersAPIData(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]string{
			{"name": "Alpha", "id": "1"},
			{"name": "Beta", "id": "2"},
			{"name": "Gamma", "id": "3"},
		})
	}))
	defer api.Close()

	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<!doctype html><html><body><div id="list"></div><script>
			var ready = false;
			fetch(%q).then(r => r.json()).then(items => {
				const ul = document.createElement('ul');
				items.forEach(it => {
					const li = document.createElement('li');
					li.textContent = it.name + ' (#' + it.id + ')';
					ul.appendChild(li);
				});
				document.getElementById('list').appendChild(ul);
				ready = true;
			});
		</script></body></html>`, api.URL)
	}))
	defer page.Close()

	eng, err := New(Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer eng.Close()

	p, err := eng.Fetch(context.Background(), page.URL, FetchOpts{RunInlineScripts: true})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	defer p.Close()

	md := p.Markdown()
	for _, want := range []string{"Alpha (#1)", "Beta (#2)", "Gamma (#3)"} {
		if !strings.Contains(md, want) {
			t.Errorf("Markdown missing %q, got:\n%s", want, md)
		}
	}
}
