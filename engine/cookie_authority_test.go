package engine

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDocumentCookieProtectsActualHTTPOnlySessionState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/seed" {
			w.Header().Add("Set-Cookie", "session=actual-secret; Path=/; HttpOnly")
			w.Header().Add("Set-Cookie", "visible=server; Path=/")
			writeTestBody(t, w, "<html><body>actual cookie owner</body></html>")
			return
		}
		writeTestBody(t, w, r.Header.Get("Cookie"))
	}))
	defer server.Close()
	owner, err := New(testConfig(server))
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "engine", owner.Close)
	page, err := owner.Fetch(t.Context(), server.URL+"/seed", FetchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "page", page.Close)
	if got, err := page.EvalString(t.Context(), "document.cookie"); err != nil || got != "visible=server" {
		t.Fatalf("JavaScript exposed actual HttpOnly credentials: %q %v", got, err)
	}
	if got, err := page.EvalString(t.Context(), `document.cookie='session=forged; Path=/';document.cookie='session=; Path=/; Max-Age=0';document.cookie='created=forged; Path=/; HttpOnly';document.cookie='visible=script; Path=/';document.cookie`); err != nil || got != "visible=script" {
		t.Fatalf("document cookie write crossed HTTP authority: %q %v", got, err)
	}
	wire, err := owner.Fetch(t.Context(), server.URL+"/wire", FetchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "wire page", wire.Close)
	if got := wire.Text(); got != "session=actual-secret; visible=script" {
		t.Fatalf("script replaced/deleted/created HTTP-only state or broke real HTTP cookies: %q", got)
	}
}
