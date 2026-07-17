package engine

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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

	cfg := testConfig(srv)
	cfg.ObeyRobots = true
	eng, err := New(cfg)
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
	eng, err := New(Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer eng.Close()
	_, err = eng.Fetch(context.Background(), "http://127.0.0.1/", FetchOpts{})
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

	eng, err := New(testConfig(srv))
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

func TestEnginePropagatesNetworkPolicyToJavaScriptWebSocket(t *testing.T) {
	eng, err := New(Config{SessionID: "engine-ws-policy"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer eng.Close()
	page, err := eng.Fetch(context.Background(), "https://example.test/", FetchOpts{
		OnRequest: func(req *RequestInfo) (*ResponseInfo, error) {
			return &ResponseInfo{
				Status:   http.StatusOK,
				Headers:  http.Header{"Content-Type": []string{"text/html"}},
				Body:     []byte(`<html></html>`),
				FinalURL: req.URL,
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	defer page.Close()
	if _, err := page.Eval(context.Background(), `
		var enginePolicyTrace = '';
		const ws = new WebSocket('ws://127.0.0.1:80/socket');
		ws.onerror = () => { enginePolicyTrace += 'error;'; };
		ws.onclose = () => { enginePolicyTrace += 'close;'; };
	`); err != nil {
		t.Fatalf("Eval: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		value, err := page.Eval(context.Background(), `enginePolicyTrace`)
		if err != nil {
			t.Fatalf("Eval trace: %v", err)
		}
		if strings.Contains(value.String(), "close;") {
			if !strings.Contains(value.String(), "error;") {
				t.Fatalf("policy denial did not emit error: %q", value.String())
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for WebSocket denial: %q", value.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
}
