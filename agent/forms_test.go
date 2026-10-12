package agent

import (
	"net/url"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/parser"
)

func parseForms(t *testing.T, src, base string) []*Form {
	t.Helper()
	doc, err := parser.ParseHTML(strings.NewReader(src), base)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return Forms(doc)
}

func TestFormsMetadata(t *testing.T) {
	forms := parseForms(t, `<form action="/login" method="POST" enctype="application/x-www-form-urlencoded"></form>`, "https://e.test/")
	if len(forms) != 1 {
		t.Fatalf("forms = %d", len(forms))
	}
	f := forms[0]
	if f.Action != "/login" || f.Method != "POST" || f.EncType != "application/x-www-form-urlencoded" {
		t.Errorf("metadata = %+v", f)
	}
}

func TestFormFieldsScan(t *testing.T) {
	src := `<form>
		<input type="text" name="user" value="ada">
		<input type="hidden" name="csrf" value="t0k3n">
		<input type="checkbox" name="remember" value="1" checked>
		<input type="checkbox" name="news">
		<input type="radio" name="plan" value="free">
		<input type="radio" name="plan" value="pro" checked>
		<select name="lang"><option value="en" selected>EN</option><option value="de">DE</option></select>
		<textarea name="bio">Hi</textarea>
		<button type="submit">Go</button>
	</form>`
	forms := parseForms(t, src, "https://e.test/")
	if len(forms) != 1 {
		t.Fatalf("no form")
	}
	fields := forms[0].Fields()
	want := map[string]string{
		"user": "ada", "csrf": "t0k3n", "remember": "1",
		"plan": "pro", "lang": "en", "bio": "Hi",
	}
	got := map[string]string{}
	for _, f := range fields {
		got[f.Name] = f.Value
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("field %s = %q, want %q", k, got[k], v)
		}
	}
}

func TestFormSetAndSubmitGET(t *testing.T) {
	forms := parseForms(t, `<form action="/search" method="GET">
		<input type="text" name="q" value="">
		<input type="hidden" name="src" value="bar">
	</form>`, "https://e.test/")
	f := forms[0]
	if err := f.Set("q", "ada lovelace"); err != nil {
		t.Fatalf("set: %v", err)
	}
	sub, err := f.Submit()
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if sub.Method != "GET" {
		t.Errorf("method = %q", sub.Method)
	}
	u, _ := url.Parse(sub.URL)
	q := u.Query()
	if q.Get("q") != "ada lovelace" {
		t.Errorf("q = %q", q.Get("q"))
	}
	if q.Get("src") != "bar" {
		t.Errorf("src = %q", q.Get("src"))
	}
	if u.Path != "/search" {
		t.Errorf("path = %q", u.Path)
	}
	if len(sub.Body) != 0 {
		t.Errorf("GET should have no body, got %d", len(sub.Body))
	}
}

func TestFormSetAndSubmitPOST(t *testing.T) {
	forms := parseForms(t, `<form action="/login" method="POST">
		<input type="text" name="user" value="">
		<input type="password" name="pass" value="">
	</form>`, "https://e.test/p/")
	f := forms[0]
	_ = f.Set("user", "ada")
	_ = f.Set("pass", "secret&safe")
	sub, err := f.Submit()
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if sub.Method != "POST" {
		t.Errorf("method = %q", sub.Method)
	}
	if sub.URL != "https://e.test/login" {
		t.Errorf("url = %q", sub.URL)
	}
	if sub.ContentType != "application/x-www-form-urlencoded" {
		t.Errorf("content-type = %q", sub.ContentType)
	}
	v, _ := url.ParseQuery(string(sub.Body))
	if v.Get("user") != "ada" || v.Get("pass") != "secret&safe" {
		t.Errorf("body parsed wrong: %v", v)
	}
}

func TestFormToggleCheckbox(t *testing.T) {
	forms := parseForms(t, `<form><input type="checkbox" name="news" value="1"></form>`, "https://e.test/")
	f := forms[0]
	_ = f.Toggle("news", true)
	sub, _ := f.Submit()
	u, _ := url.Parse(sub.URL)
	if u.Query().Get("news") != "1" {
		t.Errorf("checkbox not checked: %q", sub.URL)
	}
}

func TestFindFormBySelector(t *testing.T) {
	src := `<form id="a"></form><form id="b"></form>`
	doc, _ := parser.ParseHTML(strings.NewReader(src), "")
	f := FindForm(doc, "#b")
	if f == nil {
		t.Fatal("FindForm returned nil")
	}
	if id, _ := f.node.Attr("id"); id != "b" {
		t.Errorf("matched form id = %q, want b", id)
	}
}
