package js

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"errors"
	"hash"
	"strings"
	"sync"
	"sync/atomic"

	v8 "rogchap.com/v8go"
)

// cryptoKey holds the raw bytes for an opaque CryptoKey JS object. The
// JS-side never touches the bytes; it only carries an opaque handle to
// this Go-side store.
type cryptoKey struct {
	id       uint32
	keyType  string // 'secret' (HMAC/AES) | 'public' | 'private'
	algoName string // 'HMAC' | 'AES-GCM' | 'RSA-PSS' | 'ECDSA' | ...
	algoHash string // 'SHA1' | 'SHA256' | ...
	curve    string // for ECDSA: 'P-256' / 'P-384' / 'P-521'
	usages   []string
	extract  bool

	// Algorithm-specific material. Only one is populated.
	rawBytes []byte
	rsaPriv  any // *rsa.PrivateKey
	rsaPub   any // *rsa.PublicKey
	ecPriv   any // *ecdsa.PrivateKey
	ecPub    any // *ecdsa.PublicKey
}

type cryptoKeyStore struct {
	mu     sync.Mutex
	nextID uint32
	keys   map[uint32]*cryptoKey
}

func newCryptoKeyStore() *cryptoKeyStore {
	return &cryptoKeyStore{keys: make(map[uint32]*cryptoKey)}
}

func (s *cryptoKeyStore) put(k *cryptoKey) uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	k.id = s.nextID
	s.keys[k.id] = k
	return k.id
}

func (s *cryptoKeyStore) get(id uint32) *cryptoKey {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.keys[id]
}

// global store (keys are per-Runtime / process scope; we don't expose
// extractable raw bytes back to JS, so cross-context leak risk is low.)
var globalKeyStore = newCryptoKeyStore()
var globalKeyCounter atomic.Uint64

// installCryptoSubtle adds crypto.subtle.digest and exposes a thin
// native helper used by the JS-side wrapper. Sign/verify/encrypt/decrypt
// land in a future TASK.
// cryptoSubtleTemplates caches the 6 SubtleCrypto entry-point templates
// at Runtime level. Callbacks are stateless from a per-Context point of
// view (they go through globalKeyStore + info.Context()).
type cryptoSubtleTemplates struct {
	subtleObjTmpl                                           *v8.ObjectTemplate
	digest, importKey, generateKey, sign, verify, exportKey *v8.FunctionTemplate
}

func (r *Runtime) ensureCryptoSubtleTemplates() *cryptoSubtleTemplates {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cryptoSubtle != nil {
		return r.cryptoSubtle
	}
	iso := r.iso
	r.cryptoSubtle = &cryptoSubtleTemplates{
		subtleObjTmpl: v8.NewObjectTemplate(iso),
		digest:        newSubtleDigestTmpl(iso),
		importKey:     newSubtleImportTmpl(iso),
		generateKey:   newSubtleGenTmpl(iso),
		sign:          newSubtleSignTmpl(iso),
		verify:        newSubtleVerifyTmpl(iso),
		exportKey:     newSubtleExportTmpl(iso),
	}
	return r.cryptoSubtle
}

func installCryptoSubtle(iso *v8.Isolate, v8ctx *v8.Context, c *Context) error {
	cryptoVal, err := v8ctx.Global().Get("crypto")
	if err != nil {
		return err
	}
	cryptoObj, err := cryptoVal.AsObject()
	if err != nil {
		return err
	}
	t := c.rt.ensureCryptoSubtleTemplates()
	subtle, err := t.subtleObjTmpl.NewInstance(v8ctx)
	if err != nil {
		return err
	}
	if err := subtle.Set("digest", t.digest.GetFunction(v8ctx)); err != nil {
		return err
	}
	if err := subtle.Set("importKey", t.importKey.GetFunction(v8ctx)); err != nil {
		return err
	}
	if err := subtle.Set("generateKey", t.generateKey.GetFunction(v8ctx)); err != nil {
		return err
	}
	if err := subtle.Set("sign", t.sign.GetFunction(v8ctx)); err != nil {
		return err
	}
	if err := subtle.Set("verify", t.verify.GetFunction(v8ctx)); err != nil {
		return err
	}
	if err := subtle.Set("exportKey", t.exportKey.GetFunction(v8ctx)); err != nil {
		return err
	}
	return cryptoObj.Set("subtle", subtle)
}

func newSubtleDigestTmpl(iso *v8.Isolate) *v8.FunctionTemplate {
	return v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		args := info.Args()
		if len(args) < 2 {
			return v8.Null(iso)
		}
		algo := strings.ToUpper(strings.ReplaceAll(args[0].String(), "-", ""))
		var h hash.Hash
		switch algo {
		case "SHA1":
			h = sha1.New()
		case "SHA256":
			h = sha256.New()
		case "SHA384":
			h = sha512.New384()
		case "SHA512":
			h = sha512.New()
		default:
			return v8.Null(iso)
		}
		// data is a Uint8Array-shaped object: read length + numeric idx.
		dataObj, err := args[1].AsObject()
		if err != nil {
			return v8.Null(iso)
		}
		lenVal, _ := dataObj.Get("length")
		n := int(lenVal.Integer())
		buf := make([]byte, n)
		for i := 0; i < n; i++ {
			v, err := dataObj.GetIdx(uint32(i))
			if err != nil {
				continue
			}
			buf[i] = byte(v.Integer())
		}
		h.Write(buf)
		out := h.Sum(nil)

		// Build a Promise resolved with a Uint8Array-shaped Array.
		resolver, err := v8.NewPromiseResolver(info.Context())
		if err != nil {
			return v8.Null(iso)
		}
		arr, err := v8.NewObjectTemplate(iso).NewInstance(info.Context())
		if err != nil {
			return v8.Null(iso)
		}
		_ = arr.Set("length", int32(len(out)))
		_ = arr.Set("byteLength", int32(len(out)))
		for i, b := range out {
			_ = arr.SetIdx(uint32(i), int32(b))
		}
		_ = resolver.Resolve(arr.Value)
		return resolver.GetPromise().Value
	})
}

// newSubtleImportTmpl builds importKey('raw', keyBytes, {name,hash}, extractable, usages).
func newSubtleImportTmpl(iso *v8.Isolate) *v8.FunctionTemplate {
	return v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		args := info.Args()
		resolver, _ := v8.NewPromiseResolver(info.Context())
		if len(args) < 5 {
			rejectErr(iso, resolver, errStr("importKey: needs 5 args"))
			return resolver.GetPromise().Value
		}
		format := args[0].String()
		if format != "raw" {
			rejectErr(iso, resolver, errStr("importKey: only 'raw' supported"))
			return resolver.GetPromise().Value
		}
		dataObj, err := args[1].AsObject()
		if err != nil {
			rejectErr(iso, resolver, err)
			return resolver.GetPromise().Value
		}
		raw := readByteArray(dataObj)
		algoObj, err := args[2].AsObject()
		if err != nil {
			rejectErr(iso, resolver, err)
			return resolver.GetPromise().Value
		}
		algoName, _ := getStr(algoObj, "name")
		hashName, _ := getStr(algoObj, "hash")
		if hashName == "" {
			// hash can also be {name: 'SHA-256'}
			if h, err := algoObj.Get("hash"); err == nil && h.IsObject() {
				hObj, _ := h.AsObject()
				hashName, _ = getStr(hObj, "name")
			}
		}
		extractable := args[3].Boolean()
		usagesObj, _ := args[4].AsObject()
		usages := readStringArray(usagesObj)
		k := &cryptoKey{
			keyType:  "secret",
			algoName: strings.ToUpper(algoName),
			algoHash: strings.ToUpper(strings.ReplaceAll(hashName, "-", "")),
			usages:   usages,
			extract:  extractable,
			rawBytes: raw,
		}
		id := globalKeyStore.put(k)
		js, err := buildCryptoKey(iso, info.Context(), id, k)
		if err != nil {
			rejectErr(iso, resolver, err)
			return resolver.GetPromise().Value
		}
		_ = resolver.Resolve(js)
		return resolver.GetPromise().Value
	})
}

// newSubtleGenTmpl builds generateKey({name,hash,length}, extractable, usages).
func newSubtleGenTmpl(iso *v8.Isolate) *v8.FunctionTemplate {
	return v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		args := info.Args()
		resolver, _ := v8.NewPromiseResolver(info.Context())
		if len(args) < 3 {
			rejectErr(iso, resolver, errStr("generateKey: needs 3 args"))
			return resolver.GetPromise().Value
		}
		algoObj, err := args[0].AsObject()
		if err != nil {
			rejectErr(iso, resolver, err)
			return resolver.GetPromise().Value
		}
		algoName, _ := getStr(algoObj, "name")
		hashName, _ := getStr(algoObj, "hash")
		if hashName == "" {
			if h, err := algoObj.Get("hash"); err == nil && h.IsObject() {
				hObj, _ := h.AsObject()
				hashName, _ = getStr(hObj, "name")
			}
		}
		// derive default key length for HMAC from hash output size
		nbits := 256
		if v, err := algoObj.Get("length"); err == nil && v.IsNumber() {
			nbits = int(v.Integer())
		} else {
			switch strings.ToUpper(strings.ReplaceAll(hashName, "-", "")) {
			case "SHA1":
				nbits = 160
			case "SHA256":
				nbits = 256
			case "SHA384":
				nbits = 384
			case "SHA512":
				nbits = 512
			}
		}
		raw := make([]byte, (nbits+7)/8)
		if _, err := rand.Read(raw); err != nil {
			rejectErr(iso, resolver, errors.New("HMAC generateKey: entropy source unavailable"))
			return resolver.GetPromise().Value
		}
		extractable := args[1].Boolean()
		usagesObj, _ := args[2].AsObject()
		usages := readStringArray(usagesObj)
		k := &cryptoKey{
			keyType:  "secret",
			algoName: strings.ToUpper(algoName),
			algoHash: strings.ToUpper(strings.ReplaceAll(hashName, "-", "")),
			usages:   usages,
			extract:  extractable,
			rawBytes: raw,
		}
		id := globalKeyStore.put(k)
		js, err := buildCryptoKey(iso, info.Context(), id, k)
		if err != nil {
			rejectErr(iso, resolver, err)
			return resolver.GetPromise().Value
		}
		_ = resolver.Resolve(js)
		return resolver.GetPromise().Value
	})
}

// newSubtleSignTmpl builds sign(algo, key, data) -> Promise<bytes>.
func newSubtleSignTmpl(iso *v8.Isolate) *v8.FunctionTemplate {
	return v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		args := info.Args()
		resolver, _ := v8.NewPromiseResolver(info.Context())
		if len(args) < 3 {
			rejectErr(iso, resolver, errStr("sign: needs 3 args"))
			return resolver.GetPromise().Value
		}
		keyObj, err := args[1].AsObject()
		if err != nil {
			rejectErr(iso, resolver, err)
			return resolver.GetPromise().Value
		}
		idVal, _ := keyObj.Get("__id")
		key := globalKeyStore.get(uint32(idVal.Integer()))
		if key == nil {
			rejectErr(iso, resolver, errStr("sign: unknown key"))
			return resolver.GetPromise().Value
		}
		dataObj, err := args[2].AsObject()
		if err != nil {
			rejectErr(iso, resolver, err)
			return resolver.GetPromise().Value
		}
		data := readByteArray(dataObj)
		out := hmacSign(key, data)
		if out == nil {
			rejectErr(iso, resolver, errStr("sign: unsupported algorithm"))
			return resolver.GetPromise().Value
		}
		_ = resolver.Resolve(bytesToJSArr(iso, info.Context(), out))
		return resolver.GetPromise().Value
	})
}

// newSubtleVerifyTmpl builds verify(algo, key, signature, data) -> Promise<bool>.
func newSubtleVerifyTmpl(iso *v8.Isolate) *v8.FunctionTemplate {
	return v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		args := info.Args()
		resolver, _ := v8.NewPromiseResolver(info.Context())
		if len(args) < 4 {
			rejectErr(iso, resolver, errStr("verify: needs 4 args"))
			return resolver.GetPromise().Value
		}
		keyObj, err := args[1].AsObject()
		if err != nil {
			rejectErr(iso, resolver, err)
			return resolver.GetPromise().Value
		}
		idVal, _ := keyObj.Get("__id")
		key := globalKeyStore.get(uint32(idVal.Integer()))
		if key == nil {
			rejectErr(iso, resolver, errStr("verify: unknown key"))
			return resolver.GetPromise().Value
		}
		sigObj, err := args[2].AsObject()
		if err != nil {
			rejectErr(iso, resolver, err)
			return resolver.GetPromise().Value
		}
		dataObj, err := args[3].AsObject()
		if err != nil {
			rejectErr(iso, resolver, err)
			return resolver.GetPromise().Value
		}
		sig := readByteArray(sigObj)
		data := readByteArray(dataObj)
		expected := hmacSign(key, data)
		if expected == nil {
			rejectErr(iso, resolver, errStr("verify: unsupported algorithm"))
			return resolver.GetPromise().Value
		}
		ok := hmac.Equal(sig, expected)
		val, _ := v8.NewValue(iso, ok)
		_ = resolver.Resolve(val)
		return resolver.GetPromise().Value
	})
}

// newSubtleExportTmpl builds exportKey('raw', key) -> Promise<bytes>.
func newSubtleExportTmpl(iso *v8.Isolate) *v8.FunctionTemplate {
	return v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		args := info.Args()
		resolver, _ := v8.NewPromiseResolver(info.Context())
		if len(args) < 2 {
			rejectErr(iso, resolver, errStr("exportKey: needs 2 args"))
			return resolver.GetPromise().Value
		}
		format := args[0].String()
		if format != "raw" {
			rejectErr(iso, resolver, errStr("exportKey: only 'raw' supported"))
			return resolver.GetPromise().Value
		}
		keyObj, err := args[1].AsObject()
		if err != nil {
			rejectErr(iso, resolver, err)
			return resolver.GetPromise().Value
		}
		idVal, _ := keyObj.Get("__id")
		k := globalKeyStore.get(uint32(idVal.Integer()))
		if k == nil || !k.extract {
			rejectErr(iso, resolver, errStr("exportKey: not extractable"))
			return resolver.GetPromise().Value
		}
		_ = resolver.Resolve(bytesToJSArr(iso, info.Context(), k.rawBytes))
		return resolver.GetPromise().Value
	})
}

// helpers shared with crypto.subtle implementation

func errStr(s string) error { return &simpleErr{s} }

type simpleErr struct{ msg string }

func (e *simpleErr) Error() string { return e.msg }

func getStr(obj *v8.Object, k string) (string, bool) {
	v, err := obj.Get(k)
	if err != nil || v.IsNullOrUndefined() {
		return "", false
	}
	return v.String(), true
}

func readByteArray(obj *v8.Object) []byte {
	if obj == nil {
		return nil
	}
	lenVal, err := obj.Get("length")
	if err != nil {
		return nil
	}
	n := int(lenVal.Integer())
	if n <= 0 {
		return nil
	}
	out := make([]byte, n)
	for i := 0; i < n; i++ {
		v, err := obj.GetIdx(uint32(i))
		if err != nil {
			continue
		}
		out[i] = byte(v.Integer())
	}
	return out
}

func readStringArray(obj *v8.Object) []string {
	if obj == nil {
		return nil
	}
	lenVal, err := obj.Get("length")
	if err != nil {
		return nil
	}
	n := int(lenVal.Integer())
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		v, err := obj.GetIdx(uint32(i))
		if err != nil {
			continue
		}
		out = append(out, v.String())
	}
	return out
}

func bytesToJSArr(iso *v8.Isolate, ctx *v8.Context, b []byte) *v8.Value {
	arr, err := v8.NewObjectTemplate(iso).NewInstance(ctx)
	if err != nil {
		return v8.Null(iso)
	}
	_ = arr.Set("length", int32(len(b)))
	_ = arr.Set("byteLength", int32(len(b)))
	for i, x := range b {
		_ = arr.SetIdx(uint32(i), int32(x))
	}
	return arr.Value
}

func buildCryptoKey(iso *v8.Isolate, ctx *v8.Context, id uint32, k *cryptoKey) (*v8.Value, error) {
	obj, err := v8.NewObjectTemplate(iso).NewInstance(ctx)
	if err != nil {
		return nil, err
	}
	_ = obj.Set("__id", int32(id))
	_ = obj.Set("type", k.keyType)
	_ = obj.Set("extractable", k.extract)
	algoObj, err := v8.NewObjectTemplate(iso).NewInstance(ctx)
	if err != nil {
		return nil, err
	}
	_ = algoObj.Set("name", k.algoName)
	_ = algoObj.Set("hash", "SHA-"+strings.TrimPrefix(k.algoHash, "SHA"))
	_ = obj.Set("algorithm", algoObj)
	usagesArr, _ := v8.NewObjectTemplate(iso).NewInstance(ctx)
	for i, u := range k.usages {
		_ = usagesArr.SetIdx(uint32(i), u)
	}
	_ = usagesArr.Set("length", int32(len(k.usages)))
	_ = obj.Set("usages", usagesArr)
	return obj.Value, nil
}

func hmacSign(k *cryptoKey, data []byte) []byte {
	if k.algoName != "HMAC" {
		return nil
	}
	var h func() hash.Hash
	switch k.algoHash {
	case "SHA1":
		h = sha1.New
	case "SHA256":
		h = sha256.New
	case "SHA384":
		h = sha512.New384
	case "SHA512":
		h = sha512.New
	default:
		return nil
	}
	mac := hmac.New(h, k.rawBytes)
	mac.Write(data)
	return mac.Sum(nil)
}

// silence unused
var _ = atomic.Uint64{}
