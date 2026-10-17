package engine

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDocumentTitleAssignmentThroughActualHTTPAndScriptLoading(t *testing.T) {
	const source = `<!doctype html><html><head><title>Original</title></head><body><p>Keep body</p>
<script>document.title = '\tLoaded\n from script\u00a0';</script></body></html>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTestBody(t, w, source)
	}))
	defer server.Close()
	owner, err := New(testConfig(server))
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "engine", owner.Close)
	for _, scripts := range []bool{false, true} {
		page, err := owner.Fetch(t.Context(), server.URL, FetchOpts{RunScripts: scripts})
		if err != nil {
			t.Fatal(err)
		}
		defer closeTestResource(t, "page", page.Close)
		want := "Original"
		if scripts {
			want = "Loaded from script\u00a0"
		}
		if page.Title() != want || page.Document().Title() != want || page.StatusCode() != 200 || string(page.RawBody()) != source || !strings.Contains(page.HTML(), "Keep body") {
			t.Fatalf("actual HTTP/script title mismatch: scripts=%t title=%q want=%q", scripts, page.Title(), want)
		}
		if scripts {
			value, err := page.Eval(t.Context(), "document.title")
			if err != nil || value.String() != want {
				t.Fatalf("actual script/Go title reflection diverged: %v", err)
			}
		}
	}
}
