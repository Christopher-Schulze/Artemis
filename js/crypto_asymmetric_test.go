package js

import (
	"context"
	"testing"
)

func TestRSAPSSGenerateAndSignVerify(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	if _, err := c.Eval(context.Background(), `
		var captured = '';
		(async () => {
			const enc = new TextEncoder();
			const kp = await crypto.subtle.generateKey(
				{name:'RSA-PSS', modulusLength: 2048, hash: 'SHA-256'},
				false,
				['sign','verify']
			);
			const data = enc.encode('the quick brown fox');
			const sig = await crypto.subtle.sign({name:'RSA-PSS'}, kp.privateKey, data);
			const ok = await crypto.subtle.verify({name:'RSA-PSS'}, kp.publicKey, sig, data);
			captured = (sig.length >= 256) + ':' + ok;
		})();
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	v, _ := c.Eval(context.Background(), `captured`)
	if v.String() != "true:true" {
		t.Errorf("got %q, want true:true", v.String())
	}
}

func TestRSAPSSVerifyRejectsTampered(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	if _, err := c.Eval(context.Background(), `
		var captured = '';
		(async () => {
			const enc = new TextEncoder();
			const kp = await crypto.subtle.generateKey(
				{name:'RSA-PSS', modulusLength: 2048, hash: 'SHA-256'},
				false, ['sign','verify']);
			const data = enc.encode('msg');
			const sig = await crypto.subtle.sign({name:'RSA-PSS'}, kp.privateKey, data);
			sig[0] = (sig[0] + 1) & 0xFF;
			captured = String(await crypto.subtle.verify({name:'RSA-PSS'}, kp.publicKey, sig, data));
		})();
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	v, _ := c.Eval(context.Background(), `captured`)
	if v.String() != "false" {
		t.Errorf("got %q, want false", v.String())
	}
}

func TestECDSAP256SignVerify(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	if _, err := c.Eval(context.Background(), `
		var captured = '';
		(async () => {
			const enc = new TextEncoder();
			const kp = await crypto.subtle.generateKey(
				{name:'ECDSA', namedCurve: 'P-256'},
				false, ['sign','verify']);
			const data = enc.encode('payload');
			const sig = await crypto.subtle.sign({name:'ECDSA', hash:'SHA-256'}, kp.privateKey, data);
			const ok = await crypto.subtle.verify({name:'ECDSA', hash:'SHA-256'}, kp.publicKey, sig, data);
			// P-256 r||s is 64 bytes
			captured = sig.length + ':' + ok;
		})();
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	v, _ := c.Eval(context.Background(), `captured`)
	if v.String() != "64:true" {
		t.Errorf("got %q, want 64:true", v.String())
	}
}

func TestECDSAVerifyRejectsTampered(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	if _, err := c.Eval(context.Background(), `
		var captured = '';
		(async () => {
			const enc = new TextEncoder();
			const kp = await crypto.subtle.generateKey({name:'ECDSA', namedCurve:'P-256'}, false, ['sign','verify']);
			const data = enc.encode('hi');
			const sig = await crypto.subtle.sign({name:'ECDSA', hash:'SHA-256'}, kp.privateKey, data);
			sig[0] = (sig[0] + 1) & 0xFF;
			captured = String(await crypto.subtle.verify({name:'ECDSA', hash:'SHA-256'}, kp.publicKey, sig, data));
		})();
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	v, _ := c.Eval(context.Background(), `captured`)
	if v.String() != "false" {
		t.Errorf("got %q, want false", v.String())
	}
}

func TestKeyPairTypeAndUsages(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(async () => {
			const kp = await crypto.subtle.generateKey({name:'ECDSA', namedCurve:'P-256'}, false, ['sign','verify']);
			return kp.publicKey.type + ':' + kp.privateKey.type + ':' + kp.publicKey.usages.length + ':' + kp.privateKey.usages.length;
		})();
		var captured;
		(async () => { captured = await arguments[0]; })();
	`)
	_ = err
	_ = v
	// quick sanity test via two-eval
	if _, err := c.Eval(context.Background(), `
		var captured = '';
		(async () => {
			const kp = await crypto.subtle.generateKey({name:'ECDSA', namedCurve:'P-256'}, false, ['sign','verify']);
			captured = kp.publicKey.type + ':' + kp.privateKey.type + ':' + kp.publicKey.usages[0] + ':' + kp.privateKey.usages[0];
		})();
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	v, _ = c.Eval(context.Background(), `captured`)
	if v.String() != "public:private:verify:sign" {
		t.Errorf("got %q", v.String())
	}
}
