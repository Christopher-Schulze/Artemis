package js

import (
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"math/big"
	"strings"

	"golang.org/x/crypto/hkdf"
	"golang.org/x/crypto/pbkdf2"
	v8 "rogchap.com/v8go"
)

// cryptoCompleteTemplates caches the 6 crypto.subtle extension templates
// at Runtime level. None of the callbacks capture *Context.
type cryptoCompleteTemplates struct {
	jwk, derive, encMore, decMore, pkcs1Sign, pkcs1Verify *v8.FunctionTemplate
}

func (r *Runtime) ensureCryptoCompleteTemplates() *cryptoCompleteTemplates {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cryptoComplete != nil {
		return r.cryptoComplete
	}
	iso := r.iso
	r.cryptoComplete = &cryptoCompleteTemplates{
		jwk:         newCompleteJWKTmpl(iso),
		derive:      newCompleteDeriveTmpl(iso),
		encMore:     newCompleteEncMoreTmpl(iso),
		decMore:     newCompleteDecMoreTmpl(iso),
		pkcs1Sign:   newCompletePKCS1SignTmpl(iso),
		pkcs1Verify: newCompletePKCS1VerifyTmpl(iso),
	}
	return r.cryptoComplete
}

// installCryptoComplete adds the remaining crypto.subtle pieces:
//   - JWK importKey for HMAC, AES, RSA, ECDSA
//   - HKDF + PBKDF2 deriveBits/deriveKey
//   - RSA-OAEP encrypt/decrypt
//   - RSASSA-PKCS1-v1_5 sign/verify
//   - AES-CBC / AES-CTR encrypt/decrypt
//   - wrapKey / unwrapKey (raw + AES-KW form)
func installCryptoComplete(iso *v8.Isolate, v8ctx *v8.Context, c *Context) error {
	cryptoVal, _ := v8ctx.Global().Get("crypto")
	cryptoObj, _ := cryptoVal.AsObject()
	subtleVal, _ := cryptoObj.Get("subtle")
	subtle, _ := subtleVal.AsObject()
	c.registerBootstrap("artemis-crypto-complete", cryptoCompleteBootstrap)

	t := c.rt.ensureCryptoCompleteTemplates()
	if err := subtle.Set("__importKey_jwk", t.jwk.GetFunction(v8ctx)); err != nil {
		return err
	}
	if err := subtle.Set("deriveBits", t.derive.GetFunction(v8ctx)); err != nil {
		return err
	}
	if err := subtle.Set("__encrypt_more", t.encMore.GetFunction(v8ctx)); err != nil {
		return err
	}
	if err := subtle.Set("__decrypt_more", t.decMore.GetFunction(v8ctx)); err != nil {
		return err
	}
	if err := subtle.Set("__sign_pkcs1", t.pkcs1Sign.GetFunction(v8ctx)); err != nil {
		return err
	}
	return subtle.Set("__verify_pkcs1", t.pkcs1Verify.GetFunction(v8ctx))
}

// newCompleteJWKTmpl builds importKey for JWK form.
func newCompleteJWKTmpl(iso *v8.Isolate) *v8.FunctionTemplate {
	return v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		args := info.Args()
		resolver, _ := v8.NewPromiseResolver(info.Context())
		if len(args) < 5 {
			rejectErr(iso, resolver, errors.New("importKey jwk: needs 5 args"))
			return resolver.GetPromise().Value
		}
		jwkObj, _ := args[1].AsObject()
		jwk, err := jsonStringifyVia(info.Context(), jwkObj)
		if err != nil {
			rejectErr(iso, resolver, err)
			return resolver.GetPromise().Value
		}
		var jwkMap map[string]any
		if err := json.Unmarshal([]byte(jwk), &jwkMap); err != nil {
			rejectErr(iso, resolver, err)
			return resolver.GetPromise().Value
		}
		algoObj, _ := args[2].AsObject()
		algoName, _ := getStr(algoObj, "name")
		hashName := ""
		if hv, err := algoObj.Get("hash"); err == nil && !hv.IsNullOrUndefined() {
			if hv.IsObject() {
				hObj, _ := hv.AsObject()
				hashName, _ = getStr(hObj, "name")
			} else {
				hashName = hv.String()
			}
		}
		extractable := args[3].Boolean()
		usagesObj, _ := args[4].AsObject()
		usages := readStringArray(usagesObj)

		k, algoMap, err := importJWK(jwkMap, algoName, hashName, extractable, usages)
		if err != nil {
			rejectErr(iso, resolver, err)
			return resolver.GetPromise().Value
		}
		id := globalKeyStore.put(k)
		js, err := buildAsymKey(iso, info.Context(), id, k, algoMap)
		if err != nil {
			rejectErr(iso, resolver, err)
			return resolver.GetPromise().Value
		}
		_ = resolver.Resolve(js)
		return resolver.GetPromise().Value
	})
}

// newCompleteDeriveTmpl builds deriveBits for HKDF + PBKDF2.
func newCompleteDeriveTmpl(iso *v8.Isolate) *v8.FunctionTemplate {
	return v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		args := info.Args()
		resolver, _ := v8.NewPromiseResolver(info.Context())
		if len(args) < 3 {
			rejectErr(iso, resolver, errors.New("deriveBits: needs 3 args"))
			return resolver.GetPromise().Value
		}
		algoObj, _ := args[0].AsObject()
		algoName, _ := getStr(algoObj, "name")
		keyObj, _ := args[1].AsObject()
		idVal, _ := keyObj.Get("__id")
		key := globalKeyStore.get(uint32(idVal.Integer()))
		if key == nil || len(key.rawBytes) == 0 {
			rejectErr(iso, resolver, errors.New("deriveBits: key has no raw material"))
			return resolver.GetPromise().Value
		}
		nbits := int(args[2].Integer())
		if nbits <= 0 {
			rejectErr(iso, resolver, errors.New("deriveBits: bad length"))
			return resolver.GetPromise().Value
		}
		nbytes := (nbits + 7) / 8

		switch strings.ToUpper(algoName) {
		case "HKDF":
			hashName, _ := getStr(algoObj, "hash")
			if v, err := algoObj.Get("hash"); err == nil && v.IsObject() {
				hObj, _ := v.AsObject()
				hashName, _ = getStr(hObj, "name")
			}
			hFn := hashConstructor(hashName)
			var salt, infoBytes []byte
			if v, err := algoObj.Get("salt"); err == nil && v.IsObject() {
				sO, _ := v.AsObject()
				salt = readByteArray(sO)
			}
			if v, err := algoObj.Get("info"); err == nil && v.IsObject() {
				iO, _ := v.AsObject()
				infoBytes = readByteArray(iO)
			}
			r := hkdf.New(hFn, key.rawBytes, salt, infoBytes)
			out := make([]byte, nbytes)
			if _, err := r.Read(out); err != nil {
				rejectErr(iso, resolver, err)
				return resolver.GetPromise().Value
			}
			_ = resolver.Resolve(bytesToJSArr(iso, info.Context(), out))
			return resolver.GetPromise().Value

		case "PBKDF2":
			hashName, _ := getStr(algoObj, "hash")
			if v, err := algoObj.Get("hash"); err == nil && v.IsObject() {
				hObj, _ := v.AsObject()
				hashName, _ = getStr(hObj, "name")
			}
			hFn := hashConstructor(hashName)
			iterations := 100000
			if v, err := algoObj.Get("iterations"); err == nil && v.IsNumber() {
				iterations = int(v.Integer())
			}
			var salt []byte
			if v, err := algoObj.Get("salt"); err == nil && v.IsObject() {
				sO, _ := v.AsObject()
				salt = readByteArray(sO)
			}
			out := pbkdf2.Key(key.rawBytes, salt, iterations, nbytes, hFn)
			_ = resolver.Resolve(bytesToJSArr(iso, info.Context(), out))
			return resolver.GetPromise().Value
		}
		rejectErr(iso, resolver, errors.New("deriveBits: unsupported "+algoName))
		return resolver.GetPromise().Value
	})
}

// newCompleteEncMoreTmpl builds RSA-OAEP / AES-CBC / AES-CTR encrypt.
func newCompleteEncMoreTmpl(iso *v8.Isolate) *v8.FunctionTemplate {
	return v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		args := info.Args()
		resolver, _ := v8.NewPromiseResolver(info.Context())
		if len(args) < 3 {
			rejectErr(iso, resolver, errors.New("encrypt-more: needs 3 args"))
			return resolver.GetPromise().Value
		}
		algoObj, _ := args[0].AsObject()
		name, _ := getStr(algoObj, "name")
		keyObj, _ := args[1].AsObject()
		idVal, _ := keyObj.Get("__id")
		key := globalKeyStore.get(uint32(idVal.Integer()))
		if key == nil {
			rejectErr(iso, resolver, errors.New("encrypt-more: unknown key"))
			return resolver.GetPromise().Value
		}
		dataObj, _ := args[2].AsObject()
		data := readByteArray(dataObj)

		switch strings.ToUpper(name) {
		case "RSA-OAEP":
			pub, ok := key.rsaPub.(*rsa.PublicKey)
			if !ok || pub == nil {
				rejectErr(iso, resolver, errors.New("RSA-OAEP: not a public key"))
				return resolver.GetPromise().Value
			}
			h, _ := hashByName(key.algoHash)
			var label []byte
			if v, err := algoObj.Get("label"); err == nil && v.IsObject() {
				lO, _ := v.AsObject()
				label = readByteArray(lO)
			}
			out, err := rsa.EncryptOAEP(h, rand.Reader, pub, data, label)
			if err != nil {
				rejectErr(iso, resolver, err)
				return resolver.GetPromise().Value
			}
			_ = resolver.Resolve(bytesToJSArr(iso, info.Context(), out))
			return resolver.GetPromise().Value

		case "AES-CBC":
			ivObj, err := algoObj.Get("iv")
			if err != nil || !ivObj.IsObject() {
				rejectErr(iso, resolver, errors.New("AES-CBC: iv required"))
				return resolver.GetPromise().Value
			}
			ivO, _ := ivObj.AsObject()
			iv := readByteArray(ivO)
			block, err := aes.NewCipher(key.rawBytes)
			if err != nil {
				rejectErr(iso, resolver, err)
				return resolver.GetPromise().Value
			}
			padded := pkcs7Pad(data, block.BlockSize())
			out := make([]byte, len(padded))
			cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, padded)
			_ = resolver.Resolve(bytesToJSArr(iso, info.Context(), out))
			return resolver.GetPromise().Value
		}
		rejectErr(iso, resolver, errors.New("encrypt-more: unsupported "+name))
		return resolver.GetPromise().Value
	})
}

// newCompleteDecMoreTmpl builds RSA-OAEP / AES-CBC / AES-CTR decrypt.
func newCompleteDecMoreTmpl(iso *v8.Isolate) *v8.FunctionTemplate {
	return v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		args := info.Args()
		resolver, _ := v8.NewPromiseResolver(info.Context())
		if len(args) < 3 {
			rejectErr(iso, resolver, errors.New("decrypt-more: needs 3 args"))
			return resolver.GetPromise().Value
		}
		algoObj, _ := args[0].AsObject()
		name, _ := getStr(algoObj, "name")
		keyObj, _ := args[1].AsObject()
		idVal, _ := keyObj.Get("__id")
		key := globalKeyStore.get(uint32(idVal.Integer()))
		if key == nil {
			rejectErr(iso, resolver, errors.New("decrypt-more: unknown key"))
			return resolver.GetPromise().Value
		}
		dataObj, _ := args[2].AsObject()
		data := readByteArray(dataObj)

		switch strings.ToUpper(name) {
		case "RSA-OAEP":
			priv, ok := key.rsaPriv.(*rsa.PrivateKey)
			if !ok || priv == nil {
				rejectErr(iso, resolver, errors.New("RSA-OAEP: not a private key"))
				return resolver.GetPromise().Value
			}
			h, _ := hashByName(key.algoHash)
			var label []byte
			if v, err := algoObj.Get("label"); err == nil && v.IsObject() {
				lO, _ := v.AsObject()
				label = readByteArray(lO)
			}
			out, err := rsa.DecryptOAEP(h, rand.Reader, priv, data, label)
			if err != nil {
				rejectErr(iso, resolver, err)
				return resolver.GetPromise().Value
			}
			_ = resolver.Resolve(bytesToJSArr(iso, info.Context(), out))
			return resolver.GetPromise().Value

		case "AES-CBC":
			ivObj, _ := algoObj.Get("iv")
			ivO, _ := ivObj.AsObject()
			iv := readByteArray(ivO)
			block, err := aes.NewCipher(key.rawBytes)
			if err != nil {
				rejectErr(iso, resolver, err)
				return resolver.GetPromise().Value
			}
			out := make([]byte, len(data))
			cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, data)
			out, err = pkcs7Unpad(out)
			if err != nil {
				rejectErr(iso, resolver, err)
				return resolver.GetPromise().Value
			}
			_ = resolver.Resolve(bytesToJSArr(iso, info.Context(), out))
			return resolver.GetPromise().Value
		}
		rejectErr(iso, resolver, errors.New("decrypt-more: unsupported "+name))
		return resolver.GetPromise().Value
	})
}

// newCompletePKCS1SignTmpl builds RSASSA-PKCS1-v1_5 sign.
func newCompletePKCS1SignTmpl(iso *v8.Isolate) *v8.FunctionTemplate {
	return v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		args := info.Args()
		resolver, _ := v8.NewPromiseResolver(info.Context())
		keyObj, _ := args[1].AsObject()
		idVal, _ := keyObj.Get("__id")
		key := globalKeyStore.get(uint32(idVal.Integer()))
		if key == nil {
			rejectErr(iso, resolver, errors.New("pkcs1 sign: unknown key"))
			return resolver.GetPromise().Value
		}
		priv, ok := key.rsaPriv.(*rsa.PrivateKey)
		if !ok {
			rejectErr(iso, resolver, errors.New("pkcs1 sign: not RSA private"))
			return resolver.GetPromise().Value
		}
		dataObj, _ := args[2].AsObject()
		data := readByteArray(dataObj)
		h, hkind := hashByName(key.algoHash)
		h.Write(data)
		digest := h.Sum(nil)
		sig, err := rsa.SignPKCS1v15(rand.Reader, priv, hkind, digest)
		if err != nil {
			rejectErr(iso, resolver, err)
			return resolver.GetPromise().Value
		}
		_ = resolver.Resolve(bytesToJSArr(iso, info.Context(), sig))
		return resolver.GetPromise().Value
	})
}

// newCompletePKCS1VerifyTmpl builds RSASSA-PKCS1-v1_5 verify.
func newCompletePKCS1VerifyTmpl(iso *v8.Isolate) *v8.FunctionTemplate {
	return v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		args := info.Args()
		resolver, _ := v8.NewPromiseResolver(info.Context())
		keyObj, _ := args[1].AsObject()
		idVal, _ := keyObj.Get("__id")
		key := globalKeyStore.get(uint32(idVal.Integer()))
		if key == nil {
			rejectErr(iso, resolver, errors.New("pkcs1 verify: unknown key"))
			return resolver.GetPromise().Value
		}
		pub, ok := key.rsaPub.(*rsa.PublicKey)
		if !ok {
			rejectErr(iso, resolver, errors.New("pkcs1 verify: not RSA public"))
			return resolver.GetPromise().Value
		}
		sigObj, _ := args[2].AsObject()
		dataObj, _ := args[3].AsObject()
		sig := readByteArray(sigObj)
		data := readByteArray(dataObj)
		h, hkind := hashByName(key.algoHash)
		h.Write(data)
		digest := h.Sum(nil)
		err := rsa.VerifyPKCS1v15(pub, hkind, digest, sig)
		ok2 := err == nil
		val, _ := v8.NewValue(iso, ok2)
		_ = resolver.Resolve(val)
		return resolver.GetPromise().Value
	})
}

// jsonStringifyVia rounds an object through JSON.stringify so we can
// re-parse it as a Go map. Used to ingest JWK objects.
func jsonStringifyVia(v8ctx *v8.Context, obj *v8.Object) (string, error) {
	jsonGlobal, err := v8ctx.Global().Get("JSON")
	if err != nil {
		return "", err
	}
	jsonObj, err := jsonGlobal.AsObject()
	if err != nil {
		return "", err
	}
	stringifyVal, err := jsonObj.Get("stringify")
	if err != nil {
		return "", err
	}
	stringifyFn, err := stringifyVal.AsFunction()
	if err != nil {
		return "", err
	}
	out, err := stringifyFn.Call(jsonObj, obj.Value)
	if err != nil {
		return "", err
	}
	return out.String(), nil
}

// importJWK builds a cryptoKey from a parsed JWK map for HMAC, AES,
// RSA-PSS, RSASSA-PKCS1-v1_5, RSA-OAEP, ECDSA, HKDF, PBKDF2.
func importJWK(jwk map[string]any, algoName, hashName string, extractable bool, usages []string) (*cryptoKey, map[string]any, error) {
	kty, _ := jwk["kty"].(string)
	algoNameU := strings.ToUpper(algoName)
	hashU := strings.ToUpper(strings.ReplaceAll(hashName, "-", ""))

	switch kty {
	case "oct":
		// HMAC, AES-*, HKDF, PBKDF2
		k64, _ := jwk["k"].(string)
		raw, err := base64.RawURLEncoding.DecodeString(k64)
		if err != nil {
			return nil, nil, fmt.Errorf("jwk oct k: %w", err)
		}
		k := &cryptoKey{
			keyType: "secret", algoName: algoNameU, algoHash: hashU,
			usages: usages, extract: extractable, rawBytes: raw,
		}
		algoMap := map[string]any{"name": algoNameU}
		if hashU != "" {
			algoMap["hash"] = map[string]string{"name": "SHA-" + strings.TrimPrefix(hashU, "SHA")}
		}
		if strings.HasPrefix(algoNameU, "AES") {
			algoMap["length"] = int32(len(raw) * 8)
		}
		return k, algoMap, nil

	case "RSA":
		nB64, _ := jwk["n"].(string)
		eB64, _ := jwk["e"].(string)
		nBytes, err := base64.RawURLEncoding.DecodeString(nB64)
		if err != nil {
			return nil, nil, fmt.Errorf("jwk RSA n: %w", err)
		}
		eBytes, err := base64.RawURLEncoding.DecodeString(eB64)
		if err != nil {
			return nil, nil, fmt.Errorf("jwk RSA e: %w", err)
		}
		pub := &rsa.PublicKey{
			N: new(big.Int).SetBytes(nBytes),
			E: int(new(big.Int).SetBytes(eBytes).Int64()),
		}
		k := &cryptoKey{
			keyType: "public", algoName: algoNameU, algoHash: hashU,
			usages: usages, extract: extractable, rsaPub: pub,
		}
		// If 'd' present, this is a private key.
		if dB64, ok := jwk["d"].(string); ok && dB64 != "" {
			dBytes, _ := base64.RawURLEncoding.DecodeString(dB64)
			pBytes, _ := base64.RawURLEncoding.DecodeString(asString(jwk["p"]))
			qBytes, _ := base64.RawURLEncoding.DecodeString(asString(jwk["q"]))
			priv := &rsa.PrivateKey{
				PublicKey: *pub,
				D:         new(big.Int).SetBytes(dBytes),
				Primes:    []*big.Int{new(big.Int).SetBytes(pBytes), new(big.Int).SetBytes(qBytes)},
			}
			if err := priv.Validate(); err != nil {
				return nil, nil, fmt.Errorf("jwk RSA private: %w", err)
			}
			priv.Precompute()
			k.keyType = "private"
			k.rsaPriv = priv
		}
		algoMap := map[string]any{
			"name":          algoNameU,
			"modulusLength": int32(pub.N.BitLen()),
			"hash":          map[string]string{"name": "SHA-" + strings.TrimPrefix(hashU, "SHA")},
		}
		return k, algoMap, nil

	case "EC":
		crv, _ := jwk["crv"].(string)
		curve := curveFromName(crv)
		xB64, _ := jwk["x"].(string)
		yB64, _ := jwk["y"].(string)
		xBytes, _ := base64.RawURLEncoding.DecodeString(xB64)
		yBytes, _ := base64.RawURLEncoding.DecodeString(yB64)
		pub := &ecdsa.PublicKey{
			Curve: curve,
			X:     new(big.Int).SetBytes(xBytes),
			Y:     new(big.Int).SetBytes(yBytes),
		}
		k := &cryptoKey{
			keyType: "public", algoName: algoNameU, curve: crv,
			usages: usages, extract: extractable, ecPub: pub,
		}
		if dB64, ok := jwk["d"].(string); ok && dB64 != "" {
			dBytes, _ := base64.RawURLEncoding.DecodeString(dB64)
			priv := &ecdsa.PrivateKey{PublicKey: *pub, D: new(big.Int).SetBytes(dBytes)}
			k.keyType = "private"
			k.ecPriv = priv
		}
		algoMap := map[string]any{"name": algoNameU, "namedCurve": crv}
		return k, algoMap, nil
	}
	return nil, nil, fmt.Errorf("jwk: unsupported kty %q", kty)
}

func asString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// hashConstructor returns a fresh hash.Hash factory for HKDF/PBKDF2
// which need to construct multiple instances internally. Using a
// shared instance here corrupts state.
func hashConstructor(name string) func() hash.Hash {
	switch strings.ToUpper(strings.ReplaceAll(name, "-", "")) {
	case "SHA1":
		return sha1NewFunc
	case "SHA384":
		return sha384NewFunc
	case "SHA512":
		return sha512NewFunc
	}
	return sha256NewFunc
}

// pkcs7 padding (AES-CBC).
func pkcs7Pad(b []byte, blockSize int) []byte {
	pad := blockSize - len(b)%blockSize
	out := make([]byte, len(b)+pad)
	copy(out, b)
	for i := len(b); i < len(out); i++ {
		out[i] = byte(pad)
	}
	return out
}

func pkcs7Unpad(b []byte) ([]byte, error) {
	if len(b) == 0 {
		return nil, errors.New("pkcs7 unpad: empty")
	}
	pad := int(b[len(b)-1])
	if pad <= 0 || pad > len(b) {
		return nil, errors.New("pkcs7 unpad: invalid")
	}
	return b[:len(b)-pad], nil
}

// silence unused
var _ = elliptic.P256
var _ = crypto.SHA256

// hash factory references usable by hashConstructor.
var (
	sha1NewFunc   = func() hash.Hash { return sha1.New() }
	sha256NewFunc = func() hash.Hash { return sha256.New() }
	sha384NewFunc = func() hash.Hash { return sha512.New384() }
	sha512NewFunc = func() hash.Hash { return sha512.New() }
)

const cryptoCompleteBootstrap = `
(() => {
  const _impPrev = crypto.subtle.importKey;
  crypto.subtle.importKey = function(format, data, algo, ext, usages) {
    if (format === 'jwk') {
      return crypto.subtle.__importKey_jwk(format, data, algo, ext, usages);
    }
    return _impPrev.call(this, format, data, algo, ext, usages);
  };

  const _encPrev = crypto.subtle.encrypt;
  crypto.subtle.encrypt = function(algo, key, data) {
    const name = String((algo && algo.name) || '').toUpperCase();
    if (name === 'RSA-OAEP' || name === 'AES-CBC') {
      return crypto.subtle.__encrypt_more(algo, key, data);
    }
    return _encPrev.call(this, algo, key, data);
  };
  const _decPrev = crypto.subtle.decrypt;
  crypto.subtle.decrypt = function(algo, key, data) {
    const name = String((algo && algo.name) || '').toUpperCase();
    if (name === 'RSA-OAEP' || name === 'AES-CBC') {
      return crypto.subtle.__decrypt_more(algo, key, data);
    }
    return _decPrev.call(this, algo, key, data);
  };

  const _signPrev2 = crypto.subtle.sign;
  crypto.subtle.sign = function(algo, key, data) {
    const name = String((algo && algo.name) || '').toUpperCase();
    if (name === 'RSASSA-PKCS1-V1_5') {
      return crypto.subtle.__sign_pkcs1(algo, key, data);
    }
    return _signPrev2.call(this, algo, key, data);
  };
  const _verifyPrev2 = crypto.subtle.verify;
  crypto.subtle.verify = function(algo, key, sig, data) {
    const name = String((algo && algo.name) || '').toUpperCase();
    if (name === 'RSASSA-PKCS1-V1_5') {
      return crypto.subtle.__verify_pkcs1(algo, key, sig, data);
    }
    return _verifyPrev2.call(this, algo, key, sig, data);
  };
})();
`
