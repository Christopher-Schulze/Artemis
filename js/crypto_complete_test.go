package js

import (
	"context"
	"testing"
)

func TestRSAOAEPRoundTrip(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	if _, err := c.Eval(context.Background(), `
		var captured = '';
		(async () => {
			// Generate via RSA-PSS to get a key, then we need RSA-OAEP key.
			// Use generateKey directly with RSA-OAEP via the dispatcher;
			// it's routed through __generateKey_asym which only handles
			// RSA-PSS / ECDSA. So generate as RSA-PSS, ABUSE: import
			// public via raw rsaPub. Skip: just generate via asym path.
			// Actually we need an RSA-OAEP key. Patch: route by kty.
			// Simplest: generate RSA-PSS keys, then use them with OAEP
			// (Go can encrypt-decrypt with same RSA keypair regardless
			// of label).
			const kp = await crypto.subtle.generateKey(
				{name:'RSA-OAEP', modulusLength: 2048, hash:'SHA-256'},
				false, ['encrypt','decrypt']);
			const enc = new TextEncoder();
			const dec = new TextDecoder();
			const data = enc.encode('top secret');
			const ct = await crypto.subtle.encrypt({name:'RSA-OAEP'}, kp.publicKey, data);
			const pt = await crypto.subtle.decrypt({name:'RSA-OAEP'}, kp.privateKey, ct);
			captured = dec.decode(pt);
		})();
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	v, _ := c.Eval(context.Background(), `captured`)
	if v.String() != "top secret" {
		t.Errorf("got %q", v.String())
	}
}

func TestAESCBCRoundTrip(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	if _, err := c.Eval(context.Background(), `
		var captured = '';
		(async () => {
			const enc = new TextEncoder();
			const dec = new TextDecoder();
			const key = await crypto.subtle.generateKey(
				{name:'AES-CBC', length: 256}, true, ['encrypt','decrypt']);
			const iv = new Array(16).fill(0).map((_,i)=>i+1);
			iv.length = 16;
			const plain = enc.encode('block cipher test - more than 16 bytes here');
			const ct = await crypto.subtle.encrypt({name:'AES-CBC', iv}, key, plain);
			const pt = await crypto.subtle.decrypt({name:'AES-CBC', iv}, key, ct);
			captured = dec.decode(pt);
		})();
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	v, _ := c.Eval(context.Background(), `captured`)
	if v.String() != "block cipher test - more than 16 bytes here" {
		t.Errorf("got %q", v.String())
	}
}

func TestRSASSAPKCS1Sign(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	if _, err := c.Eval(context.Background(), `
		var captured = '';
		(async () => {
			const kp = await crypto.subtle.generateKey(
				{name:'RSASSA-PKCS1-v1_5', modulusLength: 2048, hash: 'SHA-256'},
				false, ['sign','verify']);
			const enc = new TextEncoder();
			const data = enc.encode('jws-payload');
			const sig = await crypto.subtle.sign({name:'RSASSA-PKCS1-v1_5'}, kp.privateKey, data);
			const ok = await crypto.subtle.verify({name:'RSASSA-PKCS1-v1_5'}, kp.publicKey, sig, data);
			captured = sig.length + ':' + ok;
		})();
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	v, _ := c.Eval(context.Background(), `captured`)
	// 2048-bit RSA sig is 256 bytes
	if v.String() != "256:true" {
		t.Errorf("got %q, want 256:true", v.String())
	}
}

func TestHKDFDerive(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	if _, err := c.Eval(context.Background(), `
		var captured = '';
		(async () => {
			const enc = new TextEncoder();
			const ikm = await crypto.subtle.importKey(
				'raw', enc.encode('input keying material'),
				{name:'HKDF'}, false, ['deriveBits']);
			const salt = enc.encode('salt');
			const info = enc.encode('hkdf-info');
			const out = await crypto.subtle.deriveBits(
				{name:'HKDF', hash:'SHA-256', salt, info},
				ikm, 256
			);
			captured = out.length;
		})();
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	v, _ := c.Eval(context.Background(), `captured`)
	if v.Int64() != 32 {
		t.Errorf("got %d, want 32 (256 bits)", v.Int64())
	}
}

func TestPBKDF2Derive(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	if _, err := c.Eval(context.Background(), `
		var captured = '';
		(async () => {
			const enc = new TextEncoder();
			const k = await crypto.subtle.importKey(
				'raw', enc.encode('password123'),
				{name:'PBKDF2'}, false, ['deriveBits']);
			const out = await crypto.subtle.deriveBits(
				{name:'PBKDF2', hash:'SHA-256', salt: enc.encode('salt'), iterations: 1000},
				k, 128
			);
			captured = out.length;
		})();
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	v, _ := c.Eval(context.Background(), `captured`)
	if v.Int64() != 16 {
		t.Errorf("got %d, want 16 (128 bits)", v.Int64())
	}
}

func TestJWKImportRSA(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	if _, err := c.Eval(context.Background(), `
		var captured = '';
		(async () => {
			// minimal JWK RSA public key (raw bytes won't be valid for sign,
			// but importKey shape must succeed).
			const jwk = {
				kty: 'RSA',
				n: 'sXchD3UxRwj0e3pXaXXOyQE_2gnLKxUbXtoZqvFx0qj7g5_VgdiH8j8H1G6N9JK7vR8VmS3UoX9R4l8mI3hO9k1m6P5N7eO2L9I8',
				e: 'AQAB'
			};
			try {
				const k = await crypto.subtle.importKey(
					'jwk', jwk,
					{name:'RSA-PSS', hash:'SHA-256'},
					false, ['verify']
				);
				captured = k.type + ':' + k.algorithm.name;
			} catch (e) {
				captured = 'err:' + e.message;
			}
		})();
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	v, _ := c.Eval(context.Background(), `captured`)
	if v.String() != "public:RSA-PSS" {
		t.Errorf("got %q", v.String())
	}
}

func TestJWKImportEC(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	if _, err := c.Eval(context.Background(), `
		var captured = '';
		(async () => {
			const jwk = {
				kty: 'EC', crv: 'P-256',
				x: 'f83OJ3D2xF1Bg8vub9tLe1gHMzV76e8Tus9uPHvRVEU',
				y: 'x_FEzRu9m36HLN_tue659LNpXW6pCyStikYjKIWI5a0'
			};
			const k = await crypto.subtle.importKey(
				'jwk', jwk, {name:'ECDSA', namedCurve:'P-256'},
				false, ['verify']);
			captured = k.type + ':' + k.algorithm.name + ':' + k.algorithm.namedCurve;
		})();
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	v, _ := c.Eval(context.Background(), `captured`)
	if v.String() != "public:ECDSA:P-256" {
		t.Errorf("got %q", v.String())
	}
}
