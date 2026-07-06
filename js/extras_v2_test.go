package js

import (
	"context"
	"strings"
	"testing"
)

func TestDOMException(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			const e = new DOMException('boom', 'AbortError');
			return e.name + ':' + e.code + ':' + (e instanceof Error);
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != "AbortError:20:true" {
		t.Errorf("got %q, want AbortError:20:true", v.String())
	}
}

func TestAbortControllerAbort(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			const ac = new AbortController();
			let fired = false;
			ac.signal.addEventListener('abort', () => { fired = true; });
			ac.abort();
			return ac.signal.aborted + ':' + fired;
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != "true:true" {
		t.Errorf("got %q", v.String())
	}
}

func TestOnAttributeCompiledFromHTML(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><button id="b" onclick="globalThis.clicked = true">go</button></body></html>`, nil)
	if _, err := c.Eval(context.Background(), `document.getElementById('b').click()`); err != nil {
		t.Fatalf("click: %v", err)
	}
	v, _ := c.Eval(context.Background(), `globalThis.clicked`)
	if !v.Bool() {
		t.Error("onclick HTML attribute did not fire")
	}
}

func TestDOMParser(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			const dp = new DOMParser();
			const d = dp.parseFromString('<html><body><h1>Hi</h1></body></html>', 'text/html');
			return d.querySelector('h1').textContent;
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != "Hi" {
		t.Errorf("got %q", v.String())
	}
}

func TestTextEncoderDecoder(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			const enc = new TextEncoder();
			const dec = new TextDecoder();
			const bytes = enc.encode('héllo');
			return dec.decode(bytes);
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != "héllo" {
		t.Errorf("got %q", v.String())
	}
}

func TestURLConstructor(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			const u = new URL('/path?a=1', 'https://e.test/');
			return u.href + '|' + u.pathname + '|' + u.searchParams.get('a');
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if !strings.Contains(v.String(), "https://e.test/path?a=1") || !strings.Contains(v.String(), "|/path|1") {
		t.Errorf("got %q", v.String())
	}
}

func TestURLSearchParams(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			const sp = new URLSearchParams('a=1&b=2&a=3');
			return sp.getAll('a').join(',') + ':' + sp.get('b') + ':' + sp.toString();
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if !strings.Contains(v.String(), "1,3:2:") {
		t.Errorf("got %q", v.String())
	}
}

func TestCustomElementsDefine(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><my-el></my-el></body></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			let connected = 0;
			class MyEl {
				connectedCallback() { connected++; }
			}
			customElements.define('my-el', MyEl);
			return connected + ':' + (customElements.get('my-el') === MyEl);
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != "1:true" {
		t.Errorf("got %q", v.String())
	}
}
