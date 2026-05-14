package engine

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestObeyRobotsBlocksDisallowed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/robots.txt":
			fmt.Fprint(w, "User-agent: *\nDisallow: /secret/\n")
		case "/secret/page":
			fmt.Fprint(w, "<html>secret</html>")
		default:
			fmt.Fprint(w, "<html>ok</html>")
		}
	}))
	defer srv.Close()

	eng, err := New(Config{ObeyRobots: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer eng.Close()

	// Allowed
	p, err := eng.Fetch(context.Background(), srv.URL+"/", FetchOpts{})
	if err != nil {
		t.Fatalf("fetch /: %v", err)
	}
	p.Close()

	// Disallowed
	_, err = eng.Fetch(context.Background(), srv.URL+"/secret/page", FetchOpts{})
	if !errors.Is(err, ErrRobotsDisallowed) {
		t.Errorf("expected ErrRobotsDisallowed, got %v", err)
	}
}

func TestBlockPrivateIPs(t *testing.T) {
	eng, err := New(Config{BlockPrivateIPs: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer eng.Close()
	_, err = eng.Fetch(context.Background(), "http://127.0.0.1:1/", FetchOpts{})
	if err == nil {
		t.Error("expected error for loopback")
	}
}

func TestOnRequestInterceptionMocks(t *testing.T) {
	eng, err := New(Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer eng.Close()

	intercepted := false
	page, err := eng.Fetch(context.Background(), "https://example.test/", FetchOpts{
		OnRequest: func(req *RequestInfo) (*ResponseInfo, error) {
			intercepted = true
			return &ResponseInfo{
				Status:   200,
				Headers:  http.Header{"Content-Type": []string{"text/html"}},
				Body:     []byte(`<html><body><h1>Mocked</h1></body></html>`),
				FinalURL: req.URL,
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	defer page.Close()
	if !intercepted {
		t.Error("OnRequest not invoked")
	}
	if !strings.Contains(page.HTML(), "Mocked") {
		t.Errorf("mock body not used: %s", page.HTML())
	}
}

func TestDocumentCookieGetSet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "k", Value: "v", Path: "/"})
		fmt.Fprint(w, `<!doctype html><html><body><script>
			globalThis.captured = document.cookie;
			document.cookie = "extra=1; Path=/";
		</script></body></html>`)
	}))
	defer srv.Close()

	eng, err := New(Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer eng.Close()

	page, err := eng.Fetch(context.Background(), srv.URL, FetchOpts{RunInlineScripts: true})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	defer page.Close()

	v, err := page.Eval(context.Background(), `globalThis.captured`)
	if err != nil {
		t.Fatalf("eval captured: %v", err)
	}
	if !strings.Contains(v.String(), "k=v") {
		t.Errorf("captured cookie = %q", v.String())
	}

	v, err = page.Eval(context.Background(), `document.cookie`)
	if err != nil {
		t.Fatalf("eval cookie: %v", err)
	}
	if !strings.Contains(v.String(), "extra=1") {
		t.Errorf("cookie after JS set = %q", v.String())
	}
}
