package js

import (
	"context"
	"strings"
	"testing"
)

func TestCryptoRandomUUID(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	v, err := c.Eval(context.Background(), `crypto.randomUUID()`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	uuid := v.String()
	// 8-4-4-4-12 hex with version 4 and variant 8/9/a/b
	if len(uuid) != 36 {
		t.Errorf("len = %d, want 36", len(uuid))
	}
	if uuid[14] != '4' {
		t.Errorf("version = %c, want 4", uuid[14])
	}
}

func TestCryptoGetRandomValues(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			const a = new Array(8);
			crypto.getRandomValues(a);
			return a.every(x => typeof x === 'number' && x >= 0 && x <= 255);
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if !v.Bool() {
		t.Error("getRandomValues did not fill with bytes")
	}
}

func TestHeadersAppendGetHas(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			const h = new Headers();
			h.append('Content-Type', 'text/plain');
			h.append('Accept', 'text/html');
			h.append('Accept', 'application/json');
			return h.get('content-type') + '|' + h.get('Accept') + '|' + h.has('x-missing');
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != "text/plain|text/html, application/json|false" {
		t.Errorf("got %q", v.String())
	}
}

func TestHeadersForEachAndDelete(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			const h = new Headers({A: '1', B: '2'});
			h.set('A', 'x');
			h.delete('B');
			let out = '';
			h.forEach((v, k) => { out += k + '=' + v + ';'; });
			return out;
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if !strings.Contains(v.String(), "a=x;") || strings.Contains(v.String(), "b=") {
		t.Errorf("forEach = %q", v.String())
	}
}

func TestHistoryPushReplace(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			history.pushState({n:1}, '', '/a');
			history.pushState({n:2}, '', '/b');
			return location.pathname + ':' + history.length;
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if !strings.Contains(v.String(), "/b") {
		t.Errorf("got %q, want pathname /b", v.String())
	}
}

func TestHistoryBackForward(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			history.pushState(null, '', '/a');
			history.pushState(null, '', '/b');
			history.back();
			const after = location.pathname;
			history.forward();
			return after + '|' + location.pathname;
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if !strings.Contains(v.String(), "/a|/b") {
		t.Errorf("got %q, want /a|/b", v.String())
	}
}
