package engine

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/js"
)

func TestPageEval(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!doctype html><html><head><title>X</title></head><body><h1 id="h">Hi</h1></body></html>`)
	}))
	defer srv.Close()

	eng, err := New(Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer eng.Close()

	page, err := eng.Fetch(context.Background(), srv.URL, FetchOpts{})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	defer page.Close()

	v, err := page.Eval(context.Background(), `document.title`)
	if err != nil {
		t.Fatalf("Eval title: %v", err)
	}
	if v.String() != "X" {
		t.Errorf("title eval = %q, want X", v.String())
	}

	v, err = page.Eval(context.Background(), `document.querySelector('#h').textContent`)
	if err != nil {
		t.Fatalf("Eval qs: %v", err)
	}
	if v.String() != "Hi" {
		t.Errorf("h text = %q, want Hi", v.String())
	}
}

func TestRunInlineScripts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!doctype html><html><body><p>before</p><script>globalThis.fromScript = "yes";</script><p>after</p></body></html>`)
	}))
	defer srv.Close()

	eng, err := New(Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer eng.Close()

	cc := &js.CollectConsole{}

	page, err := eng.Fetch(context.Background(), srv.URL, FetchOpts{
		RunInlineScripts: true,
		Console:          cc,
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	defer page.Close()

	v, err := page.Eval(context.Background(), `globalThis.fromScript`)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if !strings.Contains(v.String(), "yes") {
		t.Errorf("globalThis.fromScript = %q, want yes", v.String())
	}
}

func TestInlineScriptConsoleRouted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><body><script>console.log('hello',1+2);</script></body></html>`)
	}))
	defer srv.Close()

	eng, err := New(Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer eng.Close()

	cc := &js.CollectConsole{}
	page, err := eng.Fetch(context.Background(), srv.URL, FetchOpts{
		RunInlineScripts: true,
		Console:          cc,
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	defer page.Close()

	got := cc.Snapshot()
	if len(got) != 1 {
		t.Fatalf("entries = %d, want 1", len(got))
	}
	if got[0].Level != "log" || got[0].Msg != "hello 3" {
		t.Errorf("entry = %+v, want log: hello 3", got[0])
	}
}
