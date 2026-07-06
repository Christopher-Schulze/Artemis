package js

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"strings"

	v8 "rogchap.com/v8go"
)

// cryptoAESTemplates caches the 4 AES-related FunctionTemplates at
// Runtime level. Callbacks don't capture *Context (they use info.Context()
// for promise resolution and globalKeyStore for state) so the templates
// are trivially shareable across all Contexts in the Isolate.
type cryptoAESTemplates struct {
	encrypt, decrypt, genAES, importAES *v8.FunctionTemplate
}

func (r *Runtime) ensureCryptoAESTemplates() *cryptoAESTemplates {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cryptoAES != nil {
		return r.cryptoAES
	}
	iso := r.iso
	r.cryptoAES = &cryptoAESTemplates{
		encrypt:   newCryptoAESEncryptTmpl(iso),
		decrypt:   newCryptoAESDecryptTmpl(iso),
		genAES:    newCryptoAESGenTmpl(iso),
		importAES: newCryptoAESImportTmpl(iso),
	}
	return r.cryptoAES
}

// installCryptoAES extends crypto.subtle with AES-GCM encrypt/decrypt
// and AES-* generateKey/importKey/exportKey. Plugged onto the existing
// crypto.subtle object created by installCryptoSubtle.
func installCryptoAES(iso *v8.Isolate, v8ctx *v8.Context, c *Context) error {
	cryptoVal, err := v8ctx.Global().Get("crypto")
	if err != nil {
		return err
	}
	cryptoObj, err := cryptoVal.AsObject()
	if err != nil {
		return err
	}
	subtleVal, err := cryptoObj.Get("subtle")
	if err != nil {
		return err
	}
	subtle, err := subtleVal.AsObject()
	if err != nil {
		return err
	}
	c.registerBootstrap("artemis-crypto-aes", cryptoAESBootstrap)

	t := c.rt.ensureCryptoAESTemplates()
	if err := subtle.Set("encrypt", t.encrypt.GetFunction(v8ctx)); err != nil {
		return err
	}
	if err := subtle.Set("decrypt", t.decrypt.GetFunction(v8ctx)); err != nil {
		return err
	}
	if err := subtle.Set("__generateKey_aes", t.genAES.GetFunction(v8ctx)); err != nil {
		return err
	}
	return subtle.Set("__importKey_aes", t.importAES.GetFunction(v8ctx))
}

func newCryptoAESEncryptTmpl(iso *v8.Isolate) *v8.FunctionTemplate {
	return v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		args := info.Args()
		resolver, _ := v8.NewPromiseResolver(info.Context())
		if len(args) < 3 {
			rejectErr(iso, resolver, errors.New("encrypt: needs 3 args"))
			return resolver.GetPromise().Value
		}
		algoObj, err := args[0].AsObject()
		if err != nil {
			rejectErr(iso, resolver, err)
			return resolver.GetPromise().Value
		}
		name, _ := getStr(algoObj, "name")
		if strings.ToUpper(name) != "AES-GCM" {
			rejectErr(iso, resolver, errors.New("encrypt: only AES-GCM supported"))
			return resolver.GetPromise().Value
		}
		ivObj, err := algoObj.Get("iv")
		if err != nil || !ivObj.IsObject() {
			rejectErr(iso, resolver, errors.New("AES-GCM: iv required"))
			return resolver.GetPromise().Value
		}
		ivO, _ := ivObj.AsObject()
		iv := readByteArray(ivO)
		keyObj, err := args[1].AsObject()
		if err != nil {
			rejectErr(iso, resolver, err)
			return resolver.GetPromise().Value
		}
		idVal, _ := keyObj.Get("__id")
		key := globalKeyStore.get(uint32(idVal.Integer()))
		if key == nil || !strings.HasPrefix(key.algoName, "AES") {
			rejectErr(iso, resolver, errors.New("encrypt: not an AES key"))
			return resolver.GetPromise().Value
		}
		dataObj, err := args[2].AsObject()
		if err != nil {
			rejectErr(iso, resolver, err)
			return resolver.GetPromise().Value
		}
		plaintext := readByteArray(dataObj)

		block, err := aes.NewCipher(key.rawBytes)
		if err != nil {
			rejectErr(iso, resolver, err)
			return resolver.GetPromise().Value
		}
		gcm, err := cipher.NewGCM(block)
		if err != nil {
			rejectErr(iso, resolver, err)
			return resolver.GetPromise().Value
		}
		var aad []byte
		if v, err := algoObj.Get("additionalData"); err == nil && v.IsObject() {
			aadObj, _ := v.AsObject()
			aad = readByteArray(aadObj)
		}
		out := gcm.Seal(nil, iv, plaintext, aad)
		_ = resolver.Resolve(bytesToJSArr(iso, info.Context(), out))
		return resolver.GetPromise().Value
	})
}

func newCryptoAESDecryptTmpl(iso *v8.Isolate) *v8.FunctionTemplate {
	return v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		args := info.Args()
		resolver, _ := v8.NewPromiseResolver(info.Context())
		if len(args) < 3 {
			rejectErr(iso, resolver, errors.New("decrypt: needs 3 args"))
			return resolver.GetPromise().Value
		}
		algoObj, err := args[0].AsObject()
		if err != nil {
			rejectErr(iso, resolver, err)
			return resolver.GetPromise().Value
		}
		name, _ := getStr(algoObj, "name")
		if strings.ToUpper(name) != "AES-GCM" {
			rejectErr(iso, resolver, errors.New("decrypt: only AES-GCM supported"))
			return resolver.GetPromise().Value
		}
		ivObj, err := algoObj.Get("iv")
		if err != nil || !ivObj.IsObject() {
			rejectErr(iso, resolver, errors.New("AES-GCM: iv required"))
			return resolver.GetPromise().Value
		}
		ivO, _ := ivObj.AsObject()
		iv := readByteArray(ivO)
		keyObj, err := args[1].AsObject()
		if err != nil {
			rejectErr(iso, resolver, err)
			return resolver.GetPromise().Value
		}
		idVal, _ := keyObj.Get("__id")
		key := globalKeyStore.get(uint32(idVal.Integer()))
		if key == nil {
			rejectErr(iso, resolver, errors.New("decrypt: unknown key"))
			return resolver.GetPromise().Value
		}
		dataObj, err := args[2].AsObject()
		if err != nil {
			rejectErr(iso, resolver, err)
			return resolver.GetPromise().Value
		}
		ciphertext := readByteArray(dataObj)
		block, err := aes.NewCipher(key.rawBytes)
		if err != nil {
			rejectErr(iso, resolver, err)
			return resolver.GetPromise().Value
		}
		gcm, err := cipher.NewGCM(block)
		if err != nil {
			rejectErr(iso, resolver, err)
			return resolver.GetPromise().Value
		}
		var aad []byte
		if v, err := algoObj.Get("additionalData"); err == nil && v.IsObject() {
			aadObj, _ := v.AsObject()
			aad = readByteArray(aadObj)
		}
		plaintext, err := gcm.Open(nil, iv, ciphertext, aad)
		if err != nil {
			rejectErr(iso, resolver, err)
			return resolver.GetPromise().Value
		}
		_ = resolver.Resolve(bytesToJSArr(iso, info.Context(), plaintext))
		return resolver.GetPromise().Value
	})
}

// newCryptoAESGenTmpl builds the __generateKey_aes function template.
// Patches generateKey to handle AES-GCM/CBC/CTR/KW by length.
func newCryptoAESGenTmpl(iso *v8.Isolate) *v8.FunctionTemplate {
	return v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		args := info.Args()
		resolver, _ := v8.NewPromiseResolver(info.Context())
		if len(args) < 3 {
			rejectErr(iso, resolver, errors.New("generateKey AES: needs 3 args"))
			return resolver.GetPromise().Value
		}
		algoObj, err := args[0].AsObject()
		if err != nil {
			rejectErr(iso, resolver, err)
			return resolver.GetPromise().Value
		}
		name, _ := getStr(algoObj, "name")
		nameU := strings.ToUpper(name)
		nbits := 256
		if v, err := algoObj.Get("length"); err == nil && v.IsNumber() {
			nbits = int(v.Integer())
		}
		if nbits != 128 && nbits != 192 && nbits != 256 {
			rejectErr(iso, resolver, errors.New("AES key length must be 128/192/256"))
			return resolver.GetPromise().Value
		}
		raw := make([]byte, nbits/8)
		if _, err := rand.Read(raw); err != nil {
			rejectErr(iso, resolver, errors.New("AES generateKey: entropy source unavailable"))
			return resolver.GetPromise().Value
		}
		extractable := args[1].Boolean()
		usagesObj, _ := args[2].AsObject()
		usages := readStringArray(usagesObj)
		k := &cryptoKey{
			keyType: "secret", algoName: nameU, algoHash: "",
			usages: usages, extract: extractable, rawBytes: raw,
		}
		id := globalKeyStore.put(k)
		js, err := buildAESKey(iso, info.Context(), id, k, nbits)
		if err != nil {
			rejectErr(iso, resolver, err)
			return resolver.GetPromise().Value
		}
		_ = resolver.Resolve(js)
		return resolver.GetPromise().Value
	})
}

// newCryptoAESImportTmpl builds the __importKey_aes function template.
func newCryptoAESImportTmpl(iso *v8.Isolate) *v8.FunctionTemplate {
	return v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		args := info.Args()
		resolver, _ := v8.NewPromiseResolver(info.Context())
		if len(args) < 5 {
			rejectErr(iso, resolver, errors.New("importKey AES: needs 5 args"))
			return resolver.GetPromise().Value
		}
		dataObj, _ := args[1].AsObject()
		raw := readByteArray(dataObj)
		algoObj, _ := args[2].AsObject()
		name, _ := getStr(algoObj, "name")
		nbits := len(raw) * 8
		extractable := args[3].Boolean()
		usagesObj, _ := args[4].AsObject()
		usages := readStringArray(usagesObj)
		k := &cryptoKey{
			keyType: "secret", algoName: strings.ToUpper(name),
			usages: usages, extract: extractable, rawBytes: raw,
		}
		id := globalKeyStore.put(k)
		js, err := buildAESKey(iso, info.Context(), id, k, nbits)
		if err != nil {
			rejectErr(iso, resolver, err)
			return resolver.GetPromise().Value
		}
		_ = resolver.Resolve(js)
		return resolver.GetPromise().Value
	})
}

func buildAESKey(iso *v8.Isolate, ctx *v8.Context, id uint32, k *cryptoKey, nbits int) (*v8.Value, error) {
	obj, err := v8.NewObjectTemplate(iso).NewInstance(ctx)
	if err != nil {
		return nil, err
	}
	_ = obj.Set("__id", int32(id))
	_ = obj.Set("type", "secret")
	_ = obj.Set("extractable", k.extract)
	algoObj, _ := v8.NewObjectTemplate(iso).NewInstance(ctx)
	_ = algoObj.Set("name", k.algoName)
	_ = algoObj.Set("length", int32(nbits))
	_ = obj.Set("algorithm", algoObj)
	usages, _ := v8.NewObjectTemplate(iso).NewInstance(ctx)
	for i, u := range k.usages {
		_ = usages.SetIdx(uint32(i), u)
	}
	_ = usages.Set("length", int32(len(k.usages)))
	_ = obj.Set("usages", usages)
	return obj.Value, nil
}

const cryptoAESBootstrap = `
(() => {
  // Patch generateKey/importKey to dispatch by algorithm name.
  const _genHMAC = crypto.subtle.generateKey;
  const _impHMAC = crypto.subtle.importKey;
  crypto.subtle.generateKey = function(algo, ext, usages) {
    const name = String((algo && algo.name) || '').toUpperCase();
    if (name === 'AES-GCM' || name === 'AES-CBC' || name === 'AES-CTR' || name === 'AES-KW') {
      return crypto.subtle.__generateKey_aes(algo, ext, usages);
    }
    return _genHMAC.call(this, algo, ext, usages);
  };
  crypto.subtle.importKey = function(format, data, algo, ext, usages) {
    const name = String((algo && algo.name) || (algo && (algo.toUpperCase ? algo.toUpperCase() : '')) || '').toUpperCase();
    if (name.startsWith('AES-')) {
      return crypto.subtle.__importKey_aes(format, data, algo, ext, usages);
    }
    return _impHMAC.call(this, format, data, algo, ext, usages);
  };
})();
`
