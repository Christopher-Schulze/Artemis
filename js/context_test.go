package js

import (
	"context"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/parser"
)

func newCtxFromHTML(t *testing.T, src string, console Console) *Context {
	t.Helper()
	return newCtxFromHTMLOpts(t, src, ContextOpts{Console: console})
}

func newCtxFromHTMLOpts(t *testing.T, src string, opts ContextOpts) *Context {
	t.Helper()
	doc, err := parser.ParseHTML(strings.NewReader(src), "https://example.test/")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	rt := NewRuntime()
	t.Cleanup(rt.Close)
	c, err := rt.NewContext(doc, opts)
	if err != nil {
		t.Fatalf("NewContext: %v", err)
	}
	t.Cleanup(c.Close)
	return c
}

func TestEvalPrimitives(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	cases := []struct {
		expr string
		want string
	}{
		{`1+2`, "3"},
		{`"hi"`, "hi"},
		{`true`, "true"},
		{`null`, "null"},
		{`undefined`, "undefined"},
	}
	for _, tc := range cases {
		v, err := c.Eval(context.Background(), tc.expr)
		if err != nil {
			t.Errorf("%s: %v", tc.expr, err)
			continue
		}
		if got := v.String(); got != tc.want {
			t.Errorf("%s = %q, want %q", tc.expr, got, tc.want)
		}
	}
}

func TestEvalSyntaxError(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	_, err := c.Eval(context.Background(), `1+`)
	if err == nil {
		t.Fatal("expected syntax error")
	}
}

func TestEvalThrowsCarriesStack(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	_, err := c.Eval(context.Background(), `function inner(){ throw new Error("boom"); } inner();`)
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "boom") {
		t.Errorf("error missing message: %s", msg)
	}
}

func TestDocumentTitleAndURL(t *testing.T) {
	c := newCtxFromHTML(t, `<html><head><title>T</title></head></html>`, nil)
	v, err := c.Eval(context.Background(), `document.title`)
	if err != nil {
		t.Fatalf("eval title: %v", err)
	}
	if got := v.String(); got != "T" {
		t.Errorf("title = %q, want T", got)
	}
	v, err = c.Eval(context.Background(), `document.URL`)
	if err != nil {
		t.Fatalf("eval URL: %v", err)
	}
	if got := v.String(); got != "https://example.test/" {
		t.Errorf("URL = %q, want https://example.test/", got)
	}
}

func TestQuerySelectorTextContent(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><h1 id="x">Hello</h1></body></html>`, nil)
	v, err := c.Eval(context.Background(), `document.querySelector('#x').textContent`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if got := v.String(); got != "Hello" {
		t.Errorf("textContent = %q, want Hello", got)
	}
}

func TestQuerySelectorAllLength(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><p>a</p><p>b</p><p>c</p></body></html>`, nil)
	v, err := c.Eval(context.Background(), `document.querySelectorAll('p').length`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if got := v.Int64(); got != 3 {
		t.Errorf("length = %d, want 3", got)
	}
}

func TestQuerySelectorAllIndexAccess(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><p>a</p><p>b</p></body></html>`, nil)
	v, err := c.Eval(context.Background(), `document.querySelectorAll('p')[1].textContent`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if got := v.String(); got != "b" {
		t.Errorf("p[1].textContent = %q, want b", got)
	}
}

func TestQuerySelectorGetAttribute(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><a id="x" href="https://example.test/y">link</a></body></html>`, nil)
	v, err := c.Eval(context.Background(), `document.querySelector('#x').getAttribute('href')`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if got := v.String(); got != "https://example.test/y" {
		t.Errorf("href = %q", got)
	}
}

func TestConsoleCapture(t *testing.T) {
	cc := &CollectConsole{}
	c := newCtxFromHTML(t, `<html></html>`, cc)
	if _, err := c.Eval(context.Background(), `console.log('a','b'); console.warn('w');`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	got := cc.Snapshot()
	if len(got) != 2 {
		t.Fatalf("entries = %d, want 2: %v", len(got), got)
	}
	if got[0].Level != "log" || got[0].Msg != "a b" {
		t.Errorf("entry[0] = %+v", got[0])
	}
	if got[1].Level != "warn" || got[1].Msg != "w" {
		t.Errorf("entry[1] = %+v", got[1])
	}
}

func TestBodyInnerHTML(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><p>x</p></body></html>`, nil)
	v, err := c.Eval(context.Background(), `document.body.innerHTML`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	got := v.String()
	if !strings.Contains(got, "<p>x</p>") {
		t.Errorf("innerHTML = %q", got)
	}
}

func TestEvalAfterCloseFails(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	c.Close()
	if _, err := c.Eval(context.Background(), `1`); err == nil {
		t.Error("expected error after Close")
	}
}
