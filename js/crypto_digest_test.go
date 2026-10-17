package js

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestCryptoDigestCopiesActualBufferRegions(t *testing.T) {
	expected := sha256.Sum256([]byte{0, 255, 128, 0})
	for _, expression := range []string{
		`new Uint8Array([0,255,128,0]).buffer`,
		`new DataView(new Uint8Array([99,0,255,128,0,77]).buffer,1,4)`,
		`new Uint8Array([99,0,255,128,0,77]).subarray(1,5)`,
	} {
		t.Run(expression, func(t *testing.T) {
			c := newCtxFromHTML(t, "<html></html>", nil)
			if err := c.EvalDiscard(t.Context(), `globalThis.digestTrace='pending';crypto.subtle.digest('SHA-256',`+expression+`).then(value=>{digestTrace=Array.from(new Uint8Array(value)).map(byte=>byte.toString(16).padStart(2,'0')).join('')},error=>{digestTrace='failure:'+String(error)});`); err != nil {
				t.Fatal(err)
			}
			if got, err := c.EvalString(t.Context(), "digestTrace"); err != nil || got != hex.EncodeToString(expected[:]) {
				t.Fatalf("actual BufferSource region changed: %q %v", got, err)
			}
		})
	}
}

func TestCryptoDigestRetiresActualNativeCallbackHandles(t *testing.T) {
	c := newCtxFromHTML(t, "<html></html>", nil)
	const work = `globalThis.digestCount=0;for(let index=0;index<100;index++)crypto.subtle.digest('SHA-256',new Uint8Array([42])).then(value=>{if(!(value instanceof ArrayBuffer)||value.byteLength!==32)throw Error('actual digest length lost');digestCount++});`
	if err := c.EvalDiscard(t.Context(), work); err != nil {
		t.Fatal(err)
	}
	pageBefore, internalBefore := c.v8ctx.RetainedValueCount(), c.rt.iso.RetainedValueCount()
	for range 3 {
		if err := c.EvalDiscard(t.Context(), work); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := c.EvalString(t.Context(), "String(digestCount)"); err != nil || got != "100" {
		t.Fatalf("actual digests did not complete: %q %v", got, err)
	}
	if page, internal := c.v8ctx.RetainedValueCount(), c.rt.iso.RetainedValueCount(); page != pageBefore || internal != internalBefore {
		t.Fatalf("digest callbacks retained native handles: page=%d baseline=%d internal=%d baseline=%d", page, pageBefore, internal, internalBefore)
	}
}

func TestCryptoSubtleExposesOnlyDigest(t *testing.T) {
	c := newCtxFromHTML(t, "<html></html>", nil)
	got, err := c.EvalString(t.Context(), `Object.getOwnPropertyNames(crypto.subtle).sort().join(',')+'|'+typeof crypto.getRandomValues+'|'+typeof crypto.randomUUID`)
	if err != nil || got != "digest|function|function" {
		t.Fatalf("renderless WebCrypto must stay limited to random+digest so key operations escalate to Chromium: %q %v", got, err)
	}
}
