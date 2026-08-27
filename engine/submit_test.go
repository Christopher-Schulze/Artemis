package engine

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/agent"
)

func TestPageSubmitPOSTRoundTrip(t *testing.T) {
	var (
		gotMethod string
		gotCT     string
		gotBody   url.Values
	)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotCT = r.Header.Get("Content-Type")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read submitted body: %v", err)
			return
		}
		gotBody, err = url.ParseQuery(string(body))
		if err != nil {
			t.Errorf("parse submitted body: %v", err)
			return
		}
		if _, err := fmt.Fprintf(w, `<html><body><h1>Welcome %s</h1></body></html>`, gotBody.Get("user")); err != nil {
			t.Errorf("write submit response: %v", err)
		}
	}))
	defer target.Close()

	form := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := fmt.Fprintf(w, `<html><body><form id="f" action="%s" method="POST">
			<input name="user" value="">
			<input name="pass" value="">
		</form></body></html>`, target.URL); err != nil {
			t.Errorf("write form response: %v", err)
		}
	}))
	defer form.Close()

	eng, err := New(testConfig(target, form))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer closeTestResource(t, "engine", eng.Close)

	page, err := eng.Fetch(context.Background(), form.URL, FetchOpts{})
	if err != nil {
		t.Fatalf("Fetch form: %v", err)
	}
	defer closeTestResource(t, "page", page.Close)

	f := agent.FindForm(page.Document(), "#f")
	if f == nil {
		t.Fatal("form not found")
	}
	if err := f.Set("user", "ada"); err != nil {
		t.Fatal(err)
	}
	if err := f.Set("pass", "lovelace"); err != nil {
		t.Fatal(err)
	}
	sub, err := f.Submit()
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	next, err := eng.Submit(context.Background(), sub, FetchOpts{})
	if err != nil {
		t.Fatalf("engine submit: %v", err)
	}
	defer closeTestResource(t, "submitted page", next.Close)

	if gotMethod != "POST" {
		t.Errorf("server method = %q", gotMethod)
	}
	if gotCT != "application/x-www-form-urlencoded" {
		t.Errorf("server CT = %q", gotCT)
	}
	if gotBody.Get("user") != "ada" || gotBody.Get("pass") != "lovelace" {
		t.Errorf("server body = %v", gotBody)
	}
	if !strings.Contains(next.HTML(), "Welcome ada") {
		t.Errorf("next page missing welcome: %s", next.HTML())
	}
}

func TestPageClickFiresJSListener(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTestBody(t, w, `<!doctype html><html><body><button id="b">Press</button>
<script>
document.getElementById('b').addEventListener('click', () => {
  document.getElementById('b').textContent = 'clicked';
});
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

	btn, ok := agent.ClickByText(page.Document(), "Press")
	if !ok {
		t.Fatal("button not found")
	}
	if clickErr := page.Click(context.Background(), btn); clickErr != nil {
		t.Fatalf("Click: %v", clickErr)
	}
	v, err := page.Eval(context.Background(), `document.getElementById('b').textContent`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != "clicked" {
		t.Errorf("button text = %q, want 'clicked'", v.String())
	}
}
