package js

import (
	"context"
	"strings"
	"testing"
)

func TestTreeWalkerNextNode(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><div id="r"><p>1</p><span>2</span></div></body></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			const w = document.createTreeWalker(document.getElementById('r'), NodeFilter.SHOW_ELEMENT);
			const out = [];
			let n; while ((n = w.nextNode())) out.push(n.tagName);
			return out.join(',');
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if !strings.Contains(v.String(), "P") || !strings.Contains(v.String(), "SPAN") {
		t.Errorf("walker visited %q", v.String())
	}
}

func TestQueueMicrotask(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	if _, err := c.Eval(context.Background(), `
		var captured = '';
		queueMicrotask(() => { captured = 'a'; });
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	v, _ := c.Eval(context.Background(), `captured`)
	if v.String() != "a" {
		t.Errorf("microtask not fired: %q", v.String())
	}
}

func TestRequestIdleCallback(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	if _, err := c.Eval(context.Background(), `
		var fired = false;
		requestIdleCallback(() => { fired = true; });
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	v, _ := c.Eval(context.Background(), `fired`)
	if !v.Bool() {
		t.Error("requestIdleCallback did not fire")
	}
}

func TestRequestAnimationFrame(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	if _, err := c.Eval(context.Background(), `
		var fired = false;
		requestAnimationFrame(() => { fired = true; });
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	v, _ := c.Eval(context.Background(), `fired`)
	if !v.Bool() {
		t.Error("rAF did not fire")
	}
}

func TestIntersectionObserverStub(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			const obs = new IntersectionObserver(() => {});
			obs.observe(document.body);
			obs.disconnect();
			return obs.takeRecords().length;
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.Int64() != 0 {
		t.Errorf("got %d, want 0", v.Int64())
	}
}

func TestClassListAndDataset(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><div id="d" class="a b" data-foo="bar"></div></body></html>`, nil)
	cases := []struct {
		expr string
		want string
	}{
		{`document.getElementById('d').classList.contains('a')`, "true"},
		{`document.getElementById('d').classList.contains('z')`, "false"},
		{`document.getElementById('d').dataset.foo`, "bar"},
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
	if _, err := c.Eval(context.Background(), `
		document.getElementById('d').classList.add('c');
		document.getElementById('d').dataset.bar = 'baz';
	`); err != nil {
		t.Fatalf("set: %v", err)
	}
	v, _ := c.Eval(context.Background(), `document.getElementById('d').getAttribute('class')`)
	if !strings.Contains(v.String(), "c") {
		t.Errorf("class = %q", v.String())
	}
	v, _ = c.Eval(context.Background(), `document.getElementById('d').dataset.bar`)
	if v.String() != "baz" {
		t.Errorf("dataset.bar = %q", v.String())
	}
}

func TestMatchesAndClosest(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><div class="card"><p id="t">x</p></div></body></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			const t = document.getElementById('t');
			return t.matches('p') + ':' + (t.closest('.card') !== null);
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != "true:true" {
		t.Errorf("got %q", v.String())
	}
}

func TestDocumentFormsLive(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><form></form><form></form><img src="x"></body></html>`, nil)
	v, err := c.Eval(context.Background(), `document.forms.length + ':' + document.images.length`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != "2:1" {
		t.Errorf("got %q, want 2:1", v.String())
	}
}

func TestFormDataFromForm(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><form id="f"><input name="user" value="ada"><input name="x" type="hidden" value="y"></form></body></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			const fd = new FormData(document.getElementById('f'));
			return fd.get('user') + ':' + fd.get('x');
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != "ada:y" {
		t.Errorf("got %q", v.String())
	}
}

func TestProgressEventAndMessageEvent(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			const p = new ProgressEvent('progress', {loaded: 3, total: 10});
			const m = new MessageEvent('message', {data: 'hi', origin: 'https://x'});
			return p.loaded + '/' + p.total + ':' + m.data + '@' + m.origin;
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != "3/10:hi@https://x" {
		t.Errorf("got %q", v.String())
	}
}
