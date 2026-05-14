package js

import (
	"context"
	"testing"
)

func TestHMACSignAndVerifyRoundTrip(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	if _, err := c.Eval(context.Background(), `
		var captured = '';
		(async () => {
			const enc = new TextEncoder();
			const k = await crypto.subtle.importKey(
				'raw',
				enc.encode('secret-key-32bytes-padding-okk'),
				{name: 'HMAC', hash: 'SHA-256'},
				false,
				['sign', 'verify']
			);
			const data = enc.encode('hello world');
			const sig = await crypto.subtle.sign({name:'HMAC'}, k, data);
			const ok = await crypto.subtle.verify({name:'HMAC'}, k, sig, data);
			captured = sig.length + ':' + ok;
		})();
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	v, _ := c.Eval(context.Background(), `captured`)
	if v.String() != "32:true" {
		t.Errorf("got %q, want 32:true (SHA-256 = 32 bytes)", v.String())
	}
}

func TestHMACVerifyRejectsWrongSignature(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	if _, err := c.Eval(context.Background(), `
		var captured = '';
		(async () => {
			const k = await crypto.subtle.importKey('raw',
				new TextEncoder().encode('k'),
				{name:'HMAC', hash:'SHA-1'}, false, ['sign','verify']);
			const data = new TextEncoder().encode('hi');
			const sig = await crypto.subtle.sign({name:'HMAC'}, k, data);
			sig[0] = (sig[0] + 1) & 0xFF; // tamper
			captured = String(await crypto.subtle.verify({name:'HMAC'}, k, sig, data));
		})();
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	v, _ := c.Eval(context.Background(), `captured`)
	if v.String() != "false" {
		t.Errorf("got %q, want false", v.String())
	}
}

func TestGenerateKeyHMAC(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	if _, err := c.Eval(context.Background(), `
		var info = '';
		(async () => {
			const k = await crypto.subtle.generateKey(
				{name:'HMAC', hash:'SHA-512'},
				true,
				['sign']
			);
			info = k.type + ':' + k.algorithm.name + ':' + k.extractable;
		})();
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	v, _ := c.Eval(context.Background(), `info`)
	if v.String() != "secret:HMAC:true" {
		t.Errorf("got %q", v.String())
	}
}

func TestExportKeyExtractable(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	if _, err := c.Eval(context.Background(), `
		var len = -1;
		(async () => {
			const k = await crypto.subtle.generateKey(
				{name:'HMAC', hash:'SHA-256'}, true, ['sign']);
			const raw = await crypto.subtle.exportKey('raw', k);
			len = raw.length;
		})();
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	v, _ := c.Eval(context.Background(), `len`)
	if v.Int64() != 32 {
		t.Errorf("got %d, want 32", v.Int64())
	}
}
