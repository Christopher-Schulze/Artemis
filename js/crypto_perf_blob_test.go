package js

import (
	"context"
	"testing"
)

func TestCryptoSubtleDigestSHA256(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	if _, err := c.Eval(context.Background(), `
		var captured = '';
		(async () => {
			const data = new TextEncoder().encode('abc');
			const buf = await crypto.subtle.digest('SHA-256', data);
			let hex = '';
			for (let i = 0; i < buf.length; i++) {
				hex += (buf[i] < 16 ? '0' : '') + buf[i].toString(16);
			}
			captured = hex;
		})();
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	v, _ := c.Eval(context.Background(), `captured`)
	want := "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if v.String() != want {
		t.Errorf("sha256(abc) = %q, want %q", v.String(), want)
	}
}

func TestPerformanceNow(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	v, err := c.Eval(context.Background(), `typeof performance.now()`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != "number" {
		t.Errorf("typeof performance.now() = %q", v.String())
	}
	v2, _ := c.Eval(context.Background(), `performance.now() >= 0`)
	if !v2.Bool() {
		t.Error("performance.now() should be >= 0")
	}
}

func TestBlobText(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	if _, err := c.Eval(context.Background(), `
		var captured = '';
		new Blob(['hello', ' world']).text().then(t => { captured = t; });
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	v, _ := c.Eval(context.Background(), `captured`)
	if v.String() != "hello world" {
		t.Errorf("blob.text = %q", v.String())
	}
}

func TestFileExtendsBlob(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			const f = new File(['hi'], 'h.txt', {type: 'text/plain'});
			return f.name + ':' + f.size + ':' + f.type + ':' + (f instanceof Blob);
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != "h.txt:2:text/plain:true" {
		t.Errorf("got %q", v.String())
	}
}

func TestMouseEvent(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			const ev = new MouseEvent('click', {clientX: 10, clientY: 20, ctrlKey: true});
			return ev.type + ':' + ev.clientX + ':' + ev.ctrlKey + ':' + (ev instanceof Event);
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != "click:10:true:true" {
		t.Errorf("got %q", v.String())
	}
}

func TestKeyboardAndInputAndFocusAndCustomEvent(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			const k = new KeyboardEvent('keydown', {key: 'Enter', code: 'Enter'});
			const i = new InputEvent('input', {data: 'x'});
			const f = new FocusEvent('focus');
			const cc = new CustomEvent('hello', {detail: {x: 1}});
			return k.key + ':' + i.data + ':' + f.type + ':' + cc.detail.x;
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != "Enter:x:focus:1" {
		t.Errorf("got %q", v.String())
	}
}
