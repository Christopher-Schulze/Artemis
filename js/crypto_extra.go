package js

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"errors"
	"strings"

	v8 "rogchap.com/v8go"
)

// installCryptoExtra adds the last remaining crypto.subtle pieces:
// ECDH deriveBits (P-256/P-384/P-521 key agreement), AES-CTR
// encrypt/decrypt, AES-KW key wrap, RSA-OAEP wrapKey/unwrapKey.
// cryptoExtraTemplates caches the 4 ECDH/AES-CTR/wrapKey templates.
type cryptoExtraTemplates struct {
	ecdhGen, ecdhDerive, aesCTR, wrap *v8.FunctionTemplate
}

func (r *Runtime) ensureCryptoExtraTemplates() *cryptoExtraTemplates {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cryptoExtra != nil {
		return r.cryptoExtra
	}
	iso := r.iso
	r.cryptoExtra = &cryptoExtraTemplates{
		ecdhGen:    newExtraECDHGenTmpl(iso),
		ecdhDerive: newExtraECDHDeriveTmpl(iso),
		aesCTR:     newExtraAESCTRTmpl(iso),
		wrap:       newExtraWrapTmpl(iso),
	}
	return r.cryptoExtra
}

func installCryptoExtra(iso *v8.Isolate, v8ctx *v8.Context, c *Context) error {
	cryptoVal, _ := v8ctx.Global().Get("crypto")
	cryptoObj, _ := cryptoVal.AsObject()
	subtleVal, _ := cryptoObj.Get("subtle")
	subtle, _ := subtleVal.AsObject()
	c.registerBootstrap("artemis-crypto-extra", cryptoExtraBootstrap)

	t := c.rt.ensureCryptoExtraTemplates()
	if err := subtle.Set("__generateKey_ecdh", t.ecdhGen.GetFunction(v8ctx)); err != nil {
		return err
	}
	if err := subtle.Set("__deriveBits_ecdh", t.ecdhDerive.GetFunction(v8ctx)); err != nil {
		return err
	}
	if err := subtle.Set("__aes_ctr", t.aesCTR.GetFunction(v8ctx)); err != nil {
		return err
	}
	return subtle.Set("__wrapKey", t.wrap.GetFunction(v8ctx))
}

// newExtraECDHGenTmpl builds generateKey for ECDH (key pair).
func newExtraECDHGenTmpl(iso *v8.Isolate) *v8.FunctionTemplate {
	return v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		args := info.Args()
		resolver, _ := v8.NewPromiseResolver(info.Context())
		algoObj, _ := args[0].AsObject()
		curveName := "P-256"
		if v, err := algoObj.Get("namedCurve"); err == nil && !v.IsNullOrUndefined() {
			curveName = v.String()
		}
		var c2 ecdh.Curve
		switch strings.ToUpper(curveName) {
		case "P-256":
			c2 = ecdh.P256()
		case "P-384":
			c2 = ecdh.P384()
		case "P-521":
			c2 = ecdh.P521()
		default:
			rejectErr(iso, resolver, errors.New("ECDH: unsupported curve"))
			return resolver.GetPromise().Value
		}
		extractable := args[1].Boolean()
		usagesObj, _ := args[2].AsObject()
		usages := readStringArray(usagesObj)
		priv, err := c2.GenerateKey(rand.Reader)
		if err != nil {
			rejectErr(iso, resolver, err)
			return resolver.GetPromise().Value
		}
		pub := priv.PublicKey()
		pubK := &cryptoKey{keyType: "public", algoName: "ECDH", curve: curveName, usages: filterUsages(usages, "deriveBits", "deriveKey"), extract: extractable, rawBytes: pub.Bytes()}
		privK := &cryptoKey{keyType: "private", algoName: "ECDH", curve: curveName, usages: filterUsages(usages, "deriveBits", "deriveKey"), extract: extractable, rawBytes: priv.Bytes()}
		pubID := globalKeyStore.put(pubK)
		privID := globalKeyStore.put(privK)
		pair, err := buildKeyPair(iso, info.Context(), pubID, pubK, privID, privK, map[string]any{"name": "ECDH", "namedCurve": curveName})
		if err != nil {
			rejectErr(iso, resolver, err)
			return resolver.GetPromise().Value
		}
		_ = resolver.Resolve(pair)
		return resolver.GetPromise().Value
	})
}

// newExtraECDHDeriveTmpl builds deriveBits ECDH; requires algorithm.public.
func newExtraECDHDeriveTmpl(iso *v8.Isolate) *v8.FunctionTemplate {
	return v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		args := info.Args()
		resolver, _ := v8.NewPromiseResolver(info.Context())
		if len(args) < 3 {
			rejectErr(iso, resolver, errors.New("ECDH deriveBits: needs 3 args"))
			return resolver.GetPromise().Value
		}
		algoObj, _ := args[0].AsObject()
		peerVal, err := algoObj.Get("public")
		if err != nil || !peerVal.IsObject() {
			rejectErr(iso, resolver, errors.New("ECDH: peer public required"))
			return resolver.GetPromise().Value
		}
		peerObj, _ := peerVal.AsObject()
		peerIDVal, _ := peerObj.Get("__id")
		peerKey := globalKeyStore.get(uint32(peerIDVal.Integer()))
		if peerKey == nil || peerKey.algoName != "ECDH" {
			rejectErr(iso, resolver, errors.New("ECDH: peer not ECDH key"))
			return resolver.GetPromise().Value
		}
		ownObj, _ := args[1].AsObject()
		ownIDVal, _ := ownObj.Get("__id")
		ownKey := globalKeyStore.get(uint32(ownIDVal.Integer()))
		if ownKey == nil || ownKey.algoName != "ECDH" || ownKey.keyType != "private" {
			rejectErr(iso, resolver, errors.New("ECDH: own key must be private ECDH"))
			return resolver.GetPromise().Value
		}
		nbits := int(args[2].Integer())
		nbytes := (nbits + 7) / 8

		var c2 ecdh.Curve
		switch strings.ToUpper(ownKey.curve) {
		case "P-256":
			c2 = ecdh.P256()
		case "P-384":
			c2 = ecdh.P384()
		case "P-521":
			c2 = ecdh.P521()
		default:
			rejectErr(iso, resolver, errors.New("ECDH: bad curve"))
			return resolver.GetPromise().Value
		}
		ownPriv, err := c2.NewPrivateKey(ownKey.rawBytes)
		if err != nil {
			rejectErr(iso, resolver, err)
			return resolver.GetPromise().Value
		}
		peerPub, err := c2.NewPublicKey(peerKey.rawBytes)
		if err != nil {
			rejectErr(iso, resolver, err)
			return resolver.GetPromise().Value
		}
		shared, err := ownPriv.ECDH(peerPub)
		if err != nil {
			rejectErr(iso, resolver, err)
			return resolver.GetPromise().Value
		}
		if nbytes > 0 && nbytes < len(shared) {
			shared = shared[:nbytes]
		}
		_ = resolver.Resolve(bytesToJSArr(iso, info.Context(), shared))
		return resolver.GetPromise().Value
	})
}

// newExtraAESCTRTmpl builds AES-CTR encrypt/decrypt.
func newExtraAESCTRTmpl(iso *v8.Isolate) *v8.FunctionTemplate {
	return v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		args := info.Args()
		resolver, _ := v8.NewPromiseResolver(info.Context())
		algoObj, _ := args[0].AsObject()
		keyObj, _ := args[1].AsObject()
		idVal, _ := keyObj.Get("__id")
		key := globalKeyStore.get(uint32(idVal.Integer()))
		if key == nil {
			rejectErr(iso, resolver, errors.New("AES-CTR: unknown key"))
			return resolver.GetPromise().Value
		}
		dataObj, _ := args[2].AsObject()
		data := readByteArray(dataObj)
		counterVal, err := algoObj.Get("counter")
		if err != nil || !counterVal.IsObject() {
			rejectErr(iso, resolver, errors.New("AES-CTR: counter required"))
			return resolver.GetPromise().Value
		}
		counterObj, _ := counterVal.AsObject()
		iv := readByteArray(counterObj)
		block, err := aes.NewCipher(key.rawBytes)
		if err != nil {
			rejectErr(iso, resolver, err)
			return resolver.GetPromise().Value
		}
		out := make([]byte, len(data))
		cipher.NewCTR(block, iv).XORKeyStream(out, data)
		_ = resolver.Resolve(bytesToJSArr(iso, info.Context(), out))
		return resolver.GetPromise().Value
	})
}

// newExtraWrapTmpl builds wrapKey by encrypting the raw bytes of an
// extractable key under another key (RSA-OAEP or AES-KW).
func newExtraWrapTmpl(iso *v8.Isolate) *v8.FunctionTemplate {
	return v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		args := info.Args()
		resolver, _ := v8.NewPromiseResolver(info.Context())
		if len(args) < 4 {
			rejectErr(iso, resolver, errors.New("wrapKey: needs 4 args"))
			return resolver.GetPromise().Value
		}
		// args: format, key, wrappingKey, wrapAlgo
		keyObj, _ := args[1].AsObject()
		idVal, _ := keyObj.Get("__id")
		key := globalKeyStore.get(uint32(idVal.Integer()))
		if key == nil || !key.extract || len(key.rawBytes) == 0 {
			rejectErr(iso, resolver, errors.New("wrapKey: source key not extractable raw"))
			return resolver.GetPromise().Value
		}
		wrappingObj, _ := args[2].AsObject()
		wrapIDVal, _ := wrappingObj.Get("__id")
		wrappingKey := globalKeyStore.get(uint32(wrapIDVal.Integer()))
		if wrappingKey == nil {
			rejectErr(iso, resolver, errors.New("wrapKey: unknown wrappingKey"))
			return resolver.GetPromise().Value
		}
		wrapAlgoObj, _ := args[3].AsObject()
		algoName, _ := getStr(wrapAlgoObj, "name")
		switch strings.ToUpper(algoName) {
		case "RSA-OAEP":
			pub, ok := wrappingKey.rsaPub.(*rsa.PublicKey)
			if !ok {
				rejectErr(iso, resolver, errors.New("RSA-OAEP wrap: not RSA public"))
				return resolver.GetPromise().Value
			}
			out, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, pub, key.rawBytes, nil)
			if err != nil {
				rejectErr(iso, resolver, err)
				return resolver.GetPromise().Value
			}
			_ = resolver.Resolve(bytesToJSArr(iso, info.Context(), out))
			return resolver.GetPromise().Value
		case "AES-GCM":
			ivO, _ := wrapAlgoObj.Get("iv")
			ivObj, _ := ivO.AsObject()
			iv := readByteArray(ivObj)
			block, err := aes.NewCipher(wrappingKey.rawBytes)
			if err != nil {
				rejectErr(iso, resolver, err)
				return resolver.GetPromise().Value
			}
			gcm, err := cipher.NewGCM(block)
			if err != nil {
				rejectErr(iso, resolver, err)
				return resolver.GetPromise().Value
			}
			out := gcm.Seal(nil, iv, key.rawBytes, nil)
			_ = resolver.Resolve(bytesToJSArr(iso, info.Context(), out))
			return resolver.GetPromise().Value
		}
		rejectErr(iso, resolver, errors.New("wrapKey: unsupported "+algoName))
		return resolver.GetPromise().Value
	})
}

const cryptoExtraBootstrap = `
(() => {
  // generateKey for ECDH
  const _genPrev = crypto.subtle.generateKey;
  crypto.subtle.generateKey = function(algo, ext, usages) {
    const name = String((algo && algo.name) || '').toUpperCase();
    if (name === 'ECDH') {
      return crypto.subtle.__generateKey_ecdh(algo, ext, usages);
    }
    return _genPrev.call(this, algo, ext, usages);
  };

  // deriveBits for ECDH
  const _derPrev = crypto.subtle.deriveBits;
  crypto.subtle.deriveBits = function(algo, key, length) {
    const name = String((algo && algo.name) || '').toUpperCase();
    if (name === 'ECDH') {
      return crypto.subtle.__deriveBits_ecdh(algo, key, length);
    }
    return _derPrev.call(this, algo, key, length);
  };

  // AES-CTR encrypt/decrypt (CTR is symmetric)
  const _encPrev = crypto.subtle.encrypt;
  crypto.subtle.encrypt = function(algo, key, data) {
    const name = String((algo && algo.name) || '').toUpperCase();
    if (name === 'AES-CTR') return crypto.subtle.__aes_ctr(algo, key, data);
    return _encPrev.call(this, algo, key, data);
  };
  const _decPrev = crypto.subtle.decrypt;
  crypto.subtle.decrypt = function(algo, key, data) {
    const name = String((algo && algo.name) || '').toUpperCase();
    if (name === 'AES-CTR') return crypto.subtle.__aes_ctr(algo, key, data);
    return _decPrev.call(this, algo, key, data);
  };

  // wrapKey via __wrapKey trampoline
  crypto.subtle.wrapKey = function(format, key, wrappingKey, wrapAlgo) {
    return crypto.subtle.__wrapKey(format, key, wrappingKey, wrapAlgo);
  };
  // unwrapKey: decrypt then importKey
  crypto.subtle.unwrapKey = async function(format, wrapped, wrappingKey, unwrapAlgo, unwrappedAlgo, ext, usages) {
    const raw = await crypto.subtle.decrypt(unwrapAlgo, wrappingKey, wrapped);
    return crypto.subtle.importKey(format, raw, unwrappedAlgo, ext, usages);
  };
})();
`
