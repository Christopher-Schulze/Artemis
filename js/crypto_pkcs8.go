package js

import (
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/x509"
	"errors"
	"strings"

	v8 "rogchap.com/v8go"
)

// installCryptoPKCS8 adds importKey for 'pkcs8' (DER-encoded private
// keys, PKCS#8) and 'spki' (DER-encoded public keys, SubjectPublicKeyInfo)
// formats. RSA + EC.
// cryptoPKCS8Templates caches the 2 PKCS8/SPKI import templates.
type cryptoPKCS8Templates struct {
	pkcs8, spki *v8.FunctionTemplate
}

func (r *Runtime) ensureCryptoPKCS8Templates() *cryptoPKCS8Templates {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cryptoPKCS8 != nil {
		return r.cryptoPKCS8
	}
	iso := r.iso
	r.cryptoPKCS8 = &cryptoPKCS8Templates{
		pkcs8: newPKCS8ImportTmpl(iso),
		spki:  newSPKIImportTmpl(iso),
	}
	return r.cryptoPKCS8
}

func installCryptoPKCS8(iso *v8.Isolate, v8ctx *v8.Context, c *Context) error {
	cryptoVal, _ := v8ctx.Global().Get("crypto")
	cryptoObj, _ := cryptoVal.AsObject()
	subtleVal, _ := cryptoObj.Get("subtle")
	subtle, _ := subtleVal.AsObject()
	c.registerBootstrap("artemis-crypto-pkcs8", cryptoPKCS8Bootstrap)

	t := c.rt.ensureCryptoPKCS8Templates()
	if err := subtle.Set("__importKey_pkcs8", t.pkcs8.GetFunction(v8ctx)); err != nil {
		return err
	}
	return subtle.Set("__importKey_spki", t.spki.GetFunction(v8ctx))
}

// newPKCS8ImportTmpl builds importKey for PKCS8 (DER) format.
func newPKCS8ImportTmpl(iso *v8.Isolate) *v8.FunctionTemplate {
	return v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		args := info.Args()
		resolver, _ := v8.NewPromiseResolver(info.Context())
		if len(args) < 5 {
			rejectErr(iso, resolver, errors.New("importKey pkcs8: needs 5 args"))
			return resolver.GetPromise().Value
		}
		dataObj, _ := args[1].AsObject()
		raw := readByteArray(dataObj)
		algoObj, _ := args[2].AsObject()
		algoName, _ := getStr(algoObj, "name")
		hashName := ""
		if v, err := algoObj.Get("hash"); err == nil && !v.IsNullOrUndefined() {
			if v.IsObject() {
				hObj, _ := v.AsObject()
				hashName, _ = getStr(hObj, "name")
			} else {
				hashName = v.String()
			}
		}
		extractable := args[3].Boolean()
		usagesObj, _ := args[4].AsObject()
		usages := readStringArray(usagesObj)

		priv, err := x509.ParsePKCS8PrivateKey(raw)
		if err != nil {
			rejectErr(iso, resolver, err)
			return resolver.GetPromise().Value
		}
		algoNameU := strings.ToUpper(algoName)
		hashU := strings.ToUpper(strings.ReplaceAll(hashName, "-", ""))

		k := &cryptoKey{keyType: "private", algoName: algoNameU, algoHash: hashU,
			usages: usages, extract: extractable}
		algoMap := map[string]any{"name": algoNameU}

		switch p := priv.(type) {
		case *rsa.PrivateKey:
			k.rsaPriv = p
			k.rsaPub = &p.PublicKey
			algoMap["modulusLength"] = int32(p.N.BitLen())
			if hashU != "" {
				algoMap["hash"] = map[string]string{"name": "SHA-" + strings.TrimPrefix(hashU, "SHA")}
			}
		case *ecdsa.PrivateKey:
			k.ecPriv = p
			k.ecPub = &p.PublicKey
			k.curve = curveJWKName(p.Curve.Params().Name)
			algoMap["namedCurve"] = k.curve
		default:
			rejectErr(iso, resolver, errors.New("pkcs8: unsupported key type"))
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

// newSPKIImportTmpl builds importKey for SPKI (DER) format.
func newSPKIImportTmpl(iso *v8.Isolate) *v8.FunctionTemplate {
	return v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		args := info.Args()
		resolver, _ := v8.NewPromiseResolver(info.Context())
		if len(args) < 5 {
			rejectErr(iso, resolver, errors.New("importKey spki: needs 5 args"))
			return resolver.GetPromise().Value
		}
		dataObj, _ := args[1].AsObject()
		raw := readByteArray(dataObj)
		algoObj, _ := args[2].AsObject()
		algoName, _ := getStr(algoObj, "name")
		hashName := ""
		if v, err := algoObj.Get("hash"); err == nil && !v.IsNullOrUndefined() {
			if v.IsObject() {
				hObj, _ := v.AsObject()
				hashName, _ = getStr(hObj, "name")
			} else {
				hashName = v.String()
			}
		}
		extractable := args[3].Boolean()
		usagesObj, _ := args[4].AsObject()
		usages := readStringArray(usagesObj)

		pub, err := x509.ParsePKIXPublicKey(raw)
		if err != nil {
			rejectErr(iso, resolver, err)
			return resolver.GetPromise().Value
		}
		algoNameU := strings.ToUpper(algoName)
		hashU := strings.ToUpper(strings.ReplaceAll(hashName, "-", ""))
		k := &cryptoKey{keyType: "public", algoName: algoNameU, algoHash: hashU,
			usages: usages, extract: extractable}
		algoMap := map[string]any{"name": algoNameU}

		switch p := pub.(type) {
		case *rsa.PublicKey:
			k.rsaPub = p
			algoMap["modulusLength"] = int32(p.N.BitLen())
			if hashU != "" {
				algoMap["hash"] = map[string]string{"name": "SHA-" + strings.TrimPrefix(hashU, "SHA")}
			}
		case *ecdsa.PublicKey:
			k.ecPub = p
			k.curve = curveJWKName(p.Curve.Params().Name)
			algoMap["namedCurve"] = k.curve
		default:
			rejectErr(iso, resolver, errors.New("spki: unsupported key type"))
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

// curveJWKName converts Go's curve name (P-256 -> "P-256") to JWK form.
func curveJWKName(name string) string {
	// Go's elliptic.P256().Params().Name returns "P-256" already.
	return name
}

const cryptoPKCS8Bootstrap = `
(() => {
  const _impPrev = crypto.subtle.importKey;
  crypto.subtle.importKey = function(format, data, algo, ext, usages) {
    if (format === 'pkcs8') {
      return crypto.subtle.__importKey_pkcs8(format, data, algo, ext, usages);
    }
    if (format === 'spki') {
      return crypto.subtle.__importKey_spki(format, data, algo, ext, usages);
    }
    return _impPrev.call(this, format, data, algo, ext, usages);
  };
})();
`
