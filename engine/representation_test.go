package engine

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/network"
)

func TestPlaintextMarkupStaysLiteralAndBinaryIsRejected(t *testing.T) {
	const literal = "<title>Not a title</title><script>window.ran = true</script><tag>"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/plain":
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			writeTestBody(t, w, literal)
		case "/json":
			w.Header().Set("Content-Type", "application/json")
			writeTestBody(t, w, `{"html":"<b>x</b>"}`)
		default:
			w.Header().Set("Content-Type", "image/png")
			writeTestBody(t, w, "\x89PNG\r\n\x1a\nbinary")
		}
	}))
	defer srv.Close()
	eng, err := New(testConfig(srv))
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "engine close", eng.Close)
	page, err := eng.Fetch(context.Background(), srv.URL+"/plain", FetchOpts{RunScripts: true})
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "page close", page.Close)
	if page.Text() != literal || page.Title() != "" || string(page.RawBody()) != literal {
		t.Fatalf("plaintext gained document semantics: text=%q title=%q", page.Text(), page.Title())
	}
	if ran, err := page.EvalString(context.Background(), "String(window.ran === true)"); err != nil || ran != "false" {
		t.Fatalf("script inside text/plain executed: %q %v", ran, err)
	}
	jsonPage, err := eng.Fetch(context.Background(), srv.URL+"/json", FetchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "json close", jsonPage.Close)
	if jsonPage.Text() != `{"html":"<b>x</b>"}` {
		t.Fatalf("JSON body was not kept literally: %q", jsonPage.Text())
	}
	if _, err := eng.Fetch(context.Background(), srv.URL+"/image", FetchOpts{}); !errors.Is(err, ErrUnsupportedRepresentation) {
		t.Fatalf("binary response produced a page: %v", err)
	}
}

func TestPageRecordsExternalScriptFailures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			writeTestBody(t, w, `<!doctype html><body><script src="/missing.js"></script><script src="/ok.js"></script><script src="http://127.0.0.1:1/down.js"></script></body>`)
		case "/ok.js":
			w.Header().Set("Content-Type", "application/javascript")
			writeTestBody(t, w, "window.ok = 1;")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	config := testConfig(srv)
	config.PolicyConfig.AllowedPorts = append(config.PolicyConfig.AllowedPorts, 1)
	eng, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "engine close", eng.Close)
	page, err := eng.Fetch(context.Background(), srv.URL, FetchOpts{RunScripts: true})
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "page close", page.Close)
	degradations := strings.Join(page.Degradations(), "\n")
	if len(page.Degradations()) != 2 || !strings.Contains(degradations, "external_script_status: "+srv.URL+"/missing.js: 404") || !strings.Contains(degradations, "external_script_failed: http://127.0.0.1:1/down.js") {
		t.Fatalf("script failures were not recorded: %v", page.Degradations())
	}
}

func TestTabCapRefusesOnlyTheNewPage(t *testing.T) {
	eng, err := New(Config{SessionBudget: SessionBudget{MaxTabs: 1}})
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "engine close", eng.Close)
	mock := FetchOpts{OnRequest: func(req *RequestInfo) (*ResponseInfo, error) {
		return &ResponseInfo{Status: http.StatusOK, Body: []byte("<html><p>x</p></html>"), FinalURL: req.URL}, nil
	}}
	first, err := eng.Fetch(network.WithSessionID(context.Background(), "a"), "https://example.test/", mock)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "first close", first.Close)
	if _, err := eng.Fetch(network.WithSessionID(context.Background(), "b"), "https://example.test/", mock); !IsBudgetExceeded(err) {
		t.Fatalf("tab cap not enforced: %v", err)
	}
	if _, err := first.Eval(context.Background(), "1+1"); err != nil {
		t.Fatalf("a refused tab cancelled an admitted page: %v", err)
	}
	if eng.SessionUsage().Cancelled || eng.SessionUsageFor("b").Cancelled {
		t.Fatal("tab cap refusal became a terminal breach")
	}
}
