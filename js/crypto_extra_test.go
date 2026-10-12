package js

import (
	"context"
	"testing"
)

func TestECDHDeriveBits(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	if _, err := c.Eval(context.Background(), `
		var captured = '';
		(async () => {
			const a = await crypto.subtle.generateKey({name:'ECDH', namedCurve:'P-256'}, false, ['deriveBits']);
			const b = await crypto.subtle.generateKey({name:'ECDH', namedCurve:'P-256'}, false, ['deriveBits']);
			const ab = await crypto.subtle.deriveBits({name:'ECDH', public: b.publicKey}, a.privateKey, 256);
			const ba = await crypto.subtle.deriveBits({name:'ECDH', public: a.publicKey}, b.privateKey, 256);
			let same = ab.length === ba.length;
			if (same) for (let i = 0; i < ab.length; i++) if (ab[i] !== ba[i]) { same = false; break; }
			captured = ab.length + ':' + same;
		})();
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	v, _ := c.Eval(context.Background(), `captured`)
	if v.String() != "32:true" {
		t.Errorf("got %q, want 32:true (32 bytes shared secret, both sides equal)", v.String())
	}
}

func TestAESCTRRoundTrip(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	if _, err := c.Eval(context.Background(), `
		var captured = '';
		(async () => {
			const enc = new TextEncoder();
			const dec = new TextDecoder();
			const key = await crypto.subtle.generateKey({name:'AES-CTR', length: 256}, true, ['encrypt','decrypt']);
			const counter = new Array(16).fill(0).map((_,i)=>i+1);
			counter.length = 16;
			const data = enc.encode('counter mode test');
			const ct = await crypto.subtle.encrypt({name:'AES-CTR', counter, length: 64}, key, data);
			const pt = await crypto.subtle.decrypt({name:'AES-CTR', counter, length: 64}, key, ct);
			captured = dec.decode(pt);
		})();
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	v, _ := c.Eval(context.Background(), `captured`)
	if v.String() != "counter mode test" {
		t.Errorf("got %q", v.String())
	}
}

func TestWrapKeyRSAOAEP(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	if _, err := c.Eval(context.Background(), `
		var captured = '';
		(async () => {
			// Generate an extractable AES key to wrap
			const aes = await crypto.subtle.generateKey({name:'AES-GCM', length: 256}, true, ['encrypt','decrypt']);
			// Generate an RSA-OAEP key pair to wrap with
			const rsa = await crypto.subtle.generateKey({name:'RSA-OAEP', modulusLength: 2048, hash: 'SHA-256'}, false, ['encrypt','decrypt','wrapKey','unwrapKey']);
			const wrapped = await crypto.subtle.wrapKey('raw', aes, rsa.publicKey, {name:'RSA-OAEP'});
			captured = (wrapped.length === 256) + ':' + (wrapped[0] !== undefined);
		})();
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	v, _ := c.Eval(context.Background(), `captured`)
	if v.String() != "true:true" {
		t.Errorf("got %q", v.String())
	}
}
