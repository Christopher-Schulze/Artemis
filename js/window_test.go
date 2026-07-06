package js

import (
	"context"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/parser"
	"github.com/Christopher-Schulze/Artemis/webapi"
)

func TestWindowIsGlobalThis(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	v, err := c.Eval(context.Background(), `window === globalThis`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if !v.Bool() {
		t.Error("window !== globalThis")
	}
}

func TestLocationParts(t *testing.T) {
	doc := mustParse(t, `<html></html>`, "https://user.example.test:8080/foo/bar?x=1#sec")
	c := mustNewCtx(t, doc, ContextOpts{})
	cases := []struct {
		expr string
		want string
	}{
		{`location.href`, "https://user.example.test:8080/foo/bar?x=1#sec"},
		{`location.protocol`, "https:"},
		{`location.host`, "user.example.test:8080"},
		{`location.hostname`, "user.example.test"},
		{`location.port`, "8080"},
		{`location.pathname`, "/foo/bar"},
		{`location.search`, "?x=1"},
		{`location.hash`, "#sec"},
		{`location.origin`, "https://user.example.test:8080"},
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

func TestNavigatorDefaults(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	v, err := c.Eval(context.Background(), `navigator.userAgent`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if got := v.String(); got == "" {
		t.Error("default UserAgent empty")
	}
	v, _ = c.Eval(context.Background(), `navigator.language`)
	if v.String() != "en-US" {
		t.Errorf("language = %q, want en-US", v.String())
	}
	v, _ = c.Eval(context.Background(), `navigator.languages.length`)
	if v.Int64() < 1 {
		t.Errorf("languages.length = %d", v.Int64())
	}
}

func TestNavigatorOverride(t *testing.T) {
	doc := mustParse(t, `<html></html>`, "https://example.test/")
	c := mustNewCtx(t, doc, ContextOpts{
		Navigator: NavigatorConfig{
			UserAgent: "Custom/1.0",
			Language:  "de-DE",
			Languages: []string{"de-DE", "de", "en"},
			Platform:  "MacIntel",
		},
	})
	v, _ := c.Eval(context.Background(), `navigator.userAgent`)
	if v.String() != "Custom/1.0" {
		t.Errorf("UA = %q", v.String())
	}
	v, _ = c.Eval(context.Background(), `navigator.language`)
	if v.String() != "de-DE" {
		t.Errorf("lang = %q", v.String())
	}
	v, _ = c.Eval(context.Background(), `navigator.languages.length`)
	if v.Int64() != 3 {
		t.Errorf("languages.length = %d", v.Int64())
	}
	v, _ = c.Eval(context.Background(), `navigator.platform`)
	if v.String() != "MacIntel" {
		t.Errorf("platform = %q", v.String())
	}
}

func TestLocalStorageRoundtrip(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	if _, err := c.Eval(context.Background(), `
		localStorage.setItem('k1', 'v1');
		localStorage.setItem('k2', 'v2');
	`); err != nil {
		t.Fatalf("setItem: %v", err)
	}
	cases := []struct {
		expr string
		want string
	}{
		{`localStorage.getItem('k1')`, "v1"},
		{`localStorage.getItem('k2')`, "v2"},
		{`localStorage.lengthOf()`, "2"},
		{`localStorage.key(0)`, "k1"},
		{`localStorage.key(1)`, "k2"},
	}
	for _, tc := range cases {
		v, err := c.Eval(context.Background(), tc.expr)
		if err != nil {
			t.Errorf("%s: %v", tc.expr, err)
			continue
		}
		if v.String() != tc.want {
			t.Errorf("%s = %q, want %q", tc.expr, v.String(), tc.want)
		}
	}

	if _, err := c.Eval(context.Background(), `localStorage.removeItem('k1')`); err != nil {
		t.Fatalf("removeItem: %v", err)
	}
	v, _ := c.Eval(context.Background(), `localStorage.getItem('k1')`)
	if !v.IsNull() {
		t.Errorf("after remove getItem('k1') = %q, want null", v.String())
	}

	if _, err := c.Eval(context.Background(), `localStorage.clear()`); err != nil {
		t.Fatalf("clear: %v", err)
	}
	v, _ = c.Eval(context.Background(), `localStorage.lengthOf()`)
	if v.Int64() != 0 {
		t.Errorf("after clear length = %d", v.Int64())
	}
}

func TestLocalAndSessionStorageAreIndependent(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	if _, err := c.Eval(context.Background(), `
		localStorage.setItem('k', 'L');
		sessionStorage.setItem('k', 'S');
	`); err != nil {
		t.Fatalf("set: %v", err)
	}
	v, _ := c.Eval(context.Background(), `localStorage.getItem('k')`)
	if v.String() != "L" {
		t.Errorf("local = %q", v.String())
	}
	v, _ = c.Eval(context.Background(), `sessionStorage.getItem('k')`)
	if v.String() != "S" {
		t.Errorf("session = %q", v.String())
	}
}

func TestSetTimeoutFiresAfterScript(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	// Schedule. The fn will run when fireTimers drains after this Eval.
	if _, err := c.Eval(context.Background(), `
		var fired = 0;
		setTimeout(() => { fired++; }, 0);
		setTimeout(() => { fired += 10; }, 100);
	`); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	v, _ := c.Eval(context.Background(), `fired`)
	if v.Int64() != 11 {
		t.Errorf("fired = %d, want 11", v.Int64())
	}
}

func TestClearTimeoutCancels(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	if _, err := c.Eval(context.Background(), `
		var fired = 0;
		const id = setTimeout(() => { fired++; }, 0);
		clearTimeout(id);
	`); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	v, _ := c.Eval(context.Background(), `fired`)
	if v.Int64() != 0 {
		t.Errorf("fired = %d, want 0 (cancelled)", v.Int64())
	}
}

func TestSetTimeoutChained(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	if _, err := c.Eval(context.Background(), `
		var trace = '';
		setTimeout(() => {
			trace += 'a';
			setTimeout(() => { trace += 'b'; }, 0);
		}, 0);
	`); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	v, _ := c.Eval(context.Background(), `trace`)
	if v.String() != "ab" {
		t.Errorf("trace = %q, want ab (chained timer ran)", v.String())
	}
}

func mustParse(t *testing.T, src, urlStr string) *webapi.Document {
	t.Helper()
	doc, err := parser.ParseHTML(strings.NewReader(src), urlStr)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return doc
}

func mustNewCtx(t *testing.T, doc *webapi.Document, opts ContextOpts) *Context {
	t.Helper()
	rt := NewRuntime()
	t.Cleanup(rt.Close)
	c, err := rt.NewContext(doc, opts)
	if err != nil {
		t.Fatalf("NewContext: %v", err)
	}
	t.Cleanup(c.Close)
	return c
}
