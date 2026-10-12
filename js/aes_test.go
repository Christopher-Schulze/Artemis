package js

import (
	"context"
	"testing"
)

func TestAESGCMRoundTrip(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	if _, err := c.Eval(context.Background(), `
		var captured = '';
		(async () => {
			const enc = new TextEncoder();
			const dec = new TextDecoder();
			const key = await crypto.subtle.generateKey({name:'AES-GCM', length:256}, true, ['encrypt','decrypt']);
			const iv = new Array(12).fill(0).map((_,i)=>i+1);
			iv.length = 12;
			const plain = enc.encode('hello world');
			const ct = await crypto.subtle.encrypt({name:'AES-GCM', iv: iv}, key, plain);
			const pt = await crypto.subtle.decrypt({name:'AES-GCM', iv: iv}, key, ct);
			captured = dec.decode(pt);
		})();
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	v, _ := c.Eval(context.Background(), `captured`)
	if v.String() != "hello world" {
		t.Errorf("got %q, want 'hello world'", v.String())
	}
}

func TestAESImportKeyAndExport(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	if _, err := c.Eval(context.Background(), `
		var info = '';
		(async () => {
			const raw = new Array(32).fill(0).map((_,i)=>i);
			raw.length = 32;
			const k = await crypto.subtle.importKey('raw', raw, {name:'AES-GCM'}, true, ['encrypt','decrypt']);
			info = k.type + ':' + k.algorithm.name + ':' + k.algorithm.length;
		})();
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	v, _ := c.Eval(context.Background(), `info`)
	if v.String() != "secret:AES-GCM:256" {
		t.Errorf("got %q", v.String())
	}
}

func TestNavigatorUAData(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			const ua = navigator.userAgentData;
			return ua.brands.length + ':' + (typeof ua.getHighEntropyValues);
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != "3:function" {
		t.Errorf("got %q", v.String())
	}
}

func TestClipboardWriteRead(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	if _, err := c.Eval(context.Background(), `
		var captured = '';
		(async () => {
			await navigator.clipboard.writeText('hi');
			captured = await navigator.clipboard.readText();
		})();
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	v, _ := c.Eval(context.Background(), `captured`)
	if v.String() != "hi" {
		t.Errorf("got %q", v.String())
	}
}
