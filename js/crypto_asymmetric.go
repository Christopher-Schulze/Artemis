package js

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"errors"
	"hash"
	"math/big"
	"strings"

	v8 "rogchap.com/v8go"
)

// installCryptoAsymmetric extends crypto.subtle with RSA-PSS and ECDSA
// sign/verify plus generateKey for both. Patches the dispatcher
// installed by installCryptoAES so generateKey/sign/verify route by
// algorithm name across all four families (HMAC, AES-*, RSA-PSS, ECDSA).
// cryptoAsymmetricTemplates caches the 3 asymmetric-crypto templates.
type cryptoAsymmetricTemplates struct {
	genAsym, signAsym, verifyAsym *v8.FunctionTemplate
}

func (r *Runtime) ensureCryptoAsymmetricTemplates() *cryptoAsymmetricTemplates {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cryptoAsymmetric != nil {
		return r.cryptoAsymmetric
	}
	iso := r.iso
	r.cryptoAsymmetric = &cryptoAsymmetricTemplates{
		genAsym:    newAsymGenTmpl(iso),
		signAsym:   newAsymSignTmpl(iso),
		verifyAsym: newAsymVerifyTmpl(iso),
	}
	return r.cryptoAsymmetric
}

func installCryptoAsymmetric(iso *v8.Isolate, v8ctx *v8.Context, c *Context) error {
	cryptoVal, _ := v8ctx.Global().Get("crypto")
	cryptoObj, _ := cryptoVal.AsObject()
	subtleVal, _ := cryptoObj.Get("subtle")
	subtle, _ := subtleVal.AsObject()
	c.registerBootstrap("artemis-crypto-asym", cryptoAsymBootstrap)

	t := c.rt.ensureCryptoAsymmetricTemplates()
	if err := subtle.Set("__generateKey_asym", t.genAsym.GetFunction(v8ctx)); err != nil {
		return err
	}
	if err := subtle.Set("__sign_asym", t.signAsym.GetFunction(v8ctx)); err != nil {
		return err
	}
	return subtle.Set("__verify_asym", t.verifyAsym.GetFunction(v8ctx))
}

// newAsymGenTmpl builds generateKey for RSA-PSS / ECDSA -> {publicKey, privateKey}.
func newAsymGenTmpl(iso *v8.Isolate) *v8.FunctionTemplate {
	return v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		args := info.Args()
		resolver, _ := v8.NewPromiseResolver(info.Context())
		if len(args) < 3 {
			rejectErr(iso, resolver, errors.New("generateKey: needs 3 args"))
			return resolver.GetPromise().Value
		}
		algoObj, _ := args[0].AsObject()
		name, _ := getStr(algoObj, "name")
		extractable := args[1].Boolean()
		usagesObj, _ := args[2].AsObject()
		usages := readStringArray(usagesObj)

		nameU := strings.ToUpper(name)
		switch nameU {
		case "RSA-PSS", "RSA-OAEP", "RSASSA-PKCS1-V1_5":
			bits := 2048
			if v, err := algoObj.Get("modulusLength"); err == nil && v.IsNumber() {
				bits = int(v.Integer())
			}
			hashName := "SHA256"
			if v, err := algoObj.Get("hash"); err == nil && !v.IsNullOrUndefined() {
				hashName = strings.ToUpper(strings.ReplaceAll(v.String(), "-", ""))
				if v.IsObject() {
					hObj, _ := v.AsObject()
					hn, _ := getStr(hObj, "name")
					hashName = strings.ToUpper(strings.ReplaceAll(hn, "-", ""))
				}
			}
			priv, err := rsa.GenerateKey(rand.Reader, bits)
			if err != nil {
				rejectErr(iso, resolver, err)
				return resolver.GetPromise().Value
			}
			pubUsages := filterUsages(usages, "verify", "encrypt", "wrapKey")
			privUsages := filterUsages(usages, "sign", "decrypt", "unwrapKey")
			pubK := &cryptoKey{keyType: "public", algoName: nameU, algoHash: hashName, usages: pubUsages, extract: extractable, rsaPub: &priv.PublicKey}
			privK := &cryptoKey{keyType: "private", algoName: nameU, algoHash: hashName, usages: privUsages, extract: extractable, rsaPriv: priv, rsaPub: &priv.PublicKey}
			pubID := globalKeyStore.put(pubK)
			privID := globalKeyStore.put(privK)
			pair, err := buildKeyPair(iso, info.Context(), pubID, pubK, privID, privK, map[string]any{"name": nameU, "modulusLength": int32(bits), "hash": map[string]string{"name": "SHA-" + strings.TrimPrefix(hashName, "SHA")}})
			if err != nil {
				rejectErr(iso, resolver, err)
				return resolver.GetPromise().Value
			}
			_ = resolver.Resolve(pair)
			return resolver.GetPromise().Value

		case "ECDSA":
			curve := "P-256"
			if v, err := algoObj.Get("namedCurve"); err == nil && !v.IsNullOrUndefined() {
				curve = v.String()
			}
			ec, err := ecdsa.GenerateKey(curveFromName(curve), rand.Reader)
			if err != nil {
				rejectErr(iso, resolver, err)
				return resolver.GetPromise().Value
			}
			pubK := &cryptoKey{keyType: "public", algoName: "ECDSA", curve: curve, usages: filterUsages(usages, "verify"), extract: extractable, ecPub: &ec.PublicKey}
			privK := &cryptoKey{keyType: "private", algoName: "ECDSA", curve: curve, usages: filterUsages(usages, "sign"), extract: extractable, ecPriv: ec, ecPub: &ec.PublicKey}
			pubID := globalKeyStore.put(pubK)
			privID := globalKeyStore.put(privK)
			pair, err := buildKeyPair(iso, info.Context(), pubID, pubK, privID, privK, map[string]any{"name": "ECDSA", "namedCurve": curve})
			if err != nil {
				rejectErr(iso, resolver, err)
				return resolver.GetPromise().Value
			}
			_ = resolver.Resolve(pair)
			return resolver.GetPromise().Value
		}
		rejectErr(iso, resolver, errors.New("generateKey: unsupported algorithm "+name))
		return resolver.GetPromise().Value
	})
}

// newAsymSignTmpl builds sign for RSA-PSS / ECDSA.
func newAsymSignTmpl(iso *v8.Isolate) *v8.FunctionTemplate {
	return v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		args := info.Args()
		resolver, _ := v8.NewPromiseResolver(info.Context())
		if len(args) < 3 {
			rejectErr(iso, resolver, errors.New("sign asym: needs 3 args"))
			return resolver.GetPromise().Value
		}
		algoObj, _ := args[0].AsObject()
		name, _ := getStr(algoObj, "name")
		keyObj, _ := args[1].AsObject()
		idVal, _ := keyObj.Get("__id")
		key := globalKeyStore.get(uint32(idVal.Integer()))
		if key == nil {
			rejectErr(iso, resolver, errors.New("sign asym: unknown key"))
			return resolver.GetPromise().Value
		}
		dataObj, _ := args[2].AsObject()
		data := readByteArray(dataObj)

		switch strings.ToUpper(name) {
		case "RSA-PSS":
			priv, ok := key.rsaPriv.(*rsa.PrivateKey)
			if !ok || priv == nil {
				rejectErr(iso, resolver, errors.New("RSA-PSS: not a private key"))
				return resolver.GetPromise().Value
			}
			h, hkind := hashByName(key.algoHash)
			if h == nil {
				rejectErr(iso, resolver, errors.New("RSA-PSS: unsupported hash"))
				return resolver.GetPromise().Value
			}
			h.Write(data)
			digest := h.Sum(nil)
			sig, err := rsa.SignPSS(rand.Reader, priv, hkind, digest, &rsa.PSSOptions{SaltLength: rsa.PSSSaltLengthEqualsHash})
			if err != nil {
				rejectErr(iso, resolver, err)
				return resolver.GetPromise().Value
			}
			_ = resolver.Resolve(bytesToJSArr(iso, info.Context(), sig))
			return resolver.GetPromise().Value

		case "ECDSA":
			priv, ok := key.ecPriv.(*ecdsa.PrivateKey)
			if !ok || priv == nil {
				rejectErr(iso, resolver, errors.New("ECDSA: not a private key"))
				return resolver.GetPromise().Value
			}
			hashName := "SHA256"
			if v, err := algoObj.Get("hash"); err == nil && !v.IsNullOrUndefined() {
				hashName = strings.ToUpper(strings.ReplaceAll(v.String(), "-", ""))
			}
			h, _ := hashByName(hashName)
			h.Write(data)
			digest := h.Sum(nil)
			r, s, err := ecdsa.Sign(rand.Reader, priv, digest)
			if err != nil {
				rejectErr(iso, resolver, err)
				return resolver.GetPromise().Value
			}
			// JOSE-style raw IEEE-P1363 encoding: r||s padded to curve byte size.
			byteSize := (priv.Curve.Params().BitSize + 7) / 8
			sig := make([]byte, byteSize*2)
			rBytes := r.Bytes()
			sBytes := s.Bytes()
			copy(sig[byteSize-len(rBytes):byteSize], rBytes)
			copy(sig[2*byteSize-len(sBytes):], sBytes)
			_ = resolver.Resolve(bytesToJSArr(iso, info.Context(), sig))
			return resolver.GetPromise().Value
		}
		rejectErr(iso, resolver, errors.New("sign asym: unsupported algorithm "+name))
		return resolver.GetPromise().Value
	})
}

// newAsymVerifyTmpl builds verify for RSA-PSS / ECDSA.
func newAsymVerifyTmpl(iso *v8.Isolate) *v8.FunctionTemplate {
	return v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		args := info.Args()
		resolver, _ := v8.NewPromiseResolver(info.Context())
		if len(args) < 4 {
			rejectErr(iso, resolver, errors.New("verify asym: needs 4 args"))
			return resolver.GetPromise().Value
		}
		algoObj, _ := args[0].AsObject()
		name, _ := getStr(algoObj, "name")
		keyObj, _ := args[1].AsObject()
		idVal, _ := keyObj.Get("__id")
		key := globalKeyStore.get(uint32(idVal.Integer()))
		if key == nil {
			rejectErr(iso, resolver, errors.New("verify asym: unknown key"))
			return resolver.GetPromise().Value
		}
		sigObj, _ := args[2].AsObject()
		dataObj, _ := args[3].AsObject()
		sig := readByteArray(sigObj)
		data := readByteArray(dataObj)

		switch strings.ToUpper(name) {
		case "RSA-PSS":
			pub, ok := key.rsaPub.(*rsa.PublicKey)
			if !ok || pub == nil {
				rejectErr(iso, resolver, errors.New("RSA-PSS: not a public key"))
				return resolver.GetPromise().Value
			}
			h, hkind := hashByName(key.algoHash)
			if h == nil {
				rejectErr(iso, resolver, errors.New("RSA-PSS: unsupported hash"))
				return resolver.GetPromise().Value
			}
			h.Write(data)
			digest := h.Sum(nil)
			err := rsa.VerifyPSS(pub, hkind, digest, sig, &rsa.PSSOptions{SaltLength: rsa.PSSSaltLengthEqualsHash})
			ok2 := err == nil
			val, _ := v8.NewValue(iso, ok2)
			_ = resolver.Resolve(val)
			return resolver.GetPromise().Value

		case "ECDSA":
			pub, ok := key.ecPub.(*ecdsa.PublicKey)
			if !ok || pub == nil {
				rejectErr(iso, resolver, errors.New("ECDSA: not a public key"))
				return resolver.GetPromise().Value
			}
			hashName := "SHA256"
			if v, err := algoObj.Get("hash"); err == nil && !v.IsNullOrUndefined() {
				hashName = strings.ToUpper(strings.ReplaceAll(v.String(), "-", ""))
			}
			h, _ := hashByName(hashName)
			h.Write(data)
			digest := h.Sum(nil)
			byteSize := (pub.Curve.Params().BitSize + 7) / 8
			if len(sig) != 2*byteSize {
				val, _ := v8.NewValue(iso, false)
				_ = resolver.Resolve(val)
				return resolver.GetPromise().Value
			}
			r := new(big.Int).SetBytes(sig[:byteSize])
			s := new(big.Int).SetBytes(sig[byteSize:])
			ok2 := ecdsa.Verify(pub, digest, r, s)
			val, _ := v8.NewValue(iso, ok2)
			_ = resolver.Resolve(val)
			return resolver.GetPromise().Value
		}
		rejectErr(iso, resolver, errors.New("verify asym: unsupported algorithm "+name))
		return resolver.GetPromise().Value
	})
}

func filterUsages(in []string, allow ...string) []string {
	out := []string{}
	for _, u := range in {
		for _, a := range allow {
			if u == a {
				out = append(out, u)
			}
		}
	}
	return out
}

func curveFromName(n string) elliptic.Curve {
	switch strings.ToUpper(n) {
	case "P-256":
		return elliptic.P256()
	case "P-384":
		return elliptic.P384()
	case "P-521":
		return elliptic.P521()
	}
	return elliptic.P256()
}

func hashByName(n string) (hash.Hash, crypto.Hash) {
	switch strings.ToUpper(n) {
	case "SHA1":
		return sha1.New(), crypto.SHA1
	case "SHA256":
		return sha256.New(), crypto.SHA256
	case "SHA384":
		return sha512.New384(), crypto.SHA384
	case "SHA512":
		return sha512.New(), crypto.SHA512
	}
	return sha256.New(), crypto.SHA256
}

func buildKeyPair(iso *v8.Isolate, ctx *v8.Context, pubID uint32, pub *cryptoKey, privID uint32, priv *cryptoKey, algoMap map[string]any) (*v8.Value, error) {
	pubObj, err := buildAsymKey(iso, ctx, pubID, pub, algoMap)
	if err != nil {
		return nil, err
	}
	privObj, err := buildAsymKey(iso, ctx, privID, priv, algoMap)
	if err != nil {
		return nil, err
	}
	pair, err := v8.NewObjectTemplate(iso).NewInstance(ctx)
	if err != nil {
		return nil, err
	}
	_ = pair.Set("publicKey", pubObj)
	_ = pair.Set("privateKey", privObj)
	return pair.Value, nil
}

func buildAsymKey(iso *v8.Isolate, ctx *v8.Context, id uint32, k *cryptoKey, algoMap map[string]any) (*v8.Value, error) {
	obj, err := v8.NewObjectTemplate(iso).NewInstance(ctx)
	if err != nil {
		return nil, err
	}
	_ = obj.Set("__id", int32(id))
	_ = obj.Set("type", k.keyType)
	_ = obj.Set("extractable", k.extract)
	algoObj, _ := v8.NewObjectTemplate(iso).NewInstance(ctx)
	for ak, av := range algoMap {
		switch v := av.(type) {
		case string:
			_ = algoObj.Set(ak, v)
		case int32:
			_ = algoObj.Set(ak, v)
		case map[string]string:
			sub, _ := v8.NewObjectTemplate(iso).NewInstance(ctx)
			for sk, sv := range v {
				_ = sub.Set(sk, sv)
			}
			_ = algoObj.Set(ak, sub)
		}
	}
	_ = obj.Set("algorithm", algoObj)
	usages, _ := v8.NewObjectTemplate(iso).NewInstance(ctx)
	for i, u := range k.usages {
		_ = usages.SetIdx(uint32(i), u)
	}
	_ = usages.Set("length", int32(len(k.usages)))
	_ = obj.Set("usages", usages)
	return obj.Value, nil
}

const cryptoAsymBootstrap = `
(() => {
  const _genKeyPrev = crypto.subtle.generateKey;
  const _signPrev = crypto.subtle.sign;
  const _verifyPrev = crypto.subtle.verify;
  crypto.subtle.generateKey = function(algo, ext, usages) {
    const name = String((algo && algo.name) || '').toUpperCase();
    if (name === 'RSA-PSS' || name === 'ECDSA' || name === 'RSA-OAEP' || name === 'RSASSA-PKCS1-V1_5') {
      return crypto.subtle.__generateKey_asym(algo, ext, usages);
    }
    return _genKeyPrev.call(this, algo, ext, usages);
  };
  crypto.subtle.sign = function(algo, key, data) {
    const name = String((algo && algo.name) || '').toUpperCase();
    if (name === 'RSA-PSS' || name === 'ECDSA' || name === 'RSASSA-PKCS1-V1_5') {
      return crypto.subtle.__sign_asym(algo, key, data);
    }
    return _signPrev.call(this, algo, key, data);
  };
  crypto.subtle.verify = function(algo, key, sig, data) {
    const name = String((algo && algo.name) || '').toUpperCase();
    if (name === 'RSA-PSS' || name === 'ECDSA' || name === 'RSASSA-PKCS1-V1_5') {
      return crypto.subtle.__verify_asym(algo, key, sig, data);
    }
    return _verifyPrev.call(this, algo, key, sig, data);
  };
})();
`
