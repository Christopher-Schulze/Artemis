package js

import (
	"crypto/sha256"
	"crypto/sha512"
	"errors"
	"hash"
	"math"
	"strings"

	"github.com/pjbgf/sha1cd"
	v8 "github.com/tommie/v8go"
)

// The renderless WebCrypto subset is getRandomValues, randomUUID (crypto.go)
// and subtle.digest. Key handling, encryption, signing and derivation are
// deliberately absent: pages that need them escalate to Chromium, whose
// maintained WebCrypto implementation replaces any emulation here.

const maxCryptoInputBytes = 16 << 20

type cryptoSubtleTemplates struct {
	subtleObjTmpl *v8.ObjectTemplate
	digest        *v8.FunctionTemplate
}

func (r *Runtime) ensureCryptoSubtleTemplates() *cryptoSubtleTemplates {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cryptoSubtle == nil {
		r.cryptoSubtle = &cryptoSubtleTemplates{
			subtleObjTmpl: v8.NewObjectTemplate(r.iso),
			digest:        newSubtleDigestTmpl(r.iso, r),
		}
	}
	return r.cryptoSubtle
}

// installCryptoSubtle installs crypto.subtle with digest as its only member.
func installCryptoSubtle(iso *v8.Isolate, v8ctx *v8.Context, c *Context) error {
	global := v8ctx.Global()
	defer global.Release()
	cryptoObj, err := bindingObject(global, v8ctx, "crypto")
	if err != nil {
		return err
	}
	defer cryptoObj.Release()
	t := c.rt.ensureCryptoSubtleTemplates()
	subtle, err := t.subtleObjTmpl.NewInstance(v8ctx)
	if err != nil {
		return err
	}
	defer subtle.Release()
	digest := t.digest.GetFunction(v8ctx)
	err = subtle.Set("digest", digest)
	digest.Release()
	if err != nil {
		return err
	}
	if err := cryptoObj.Set("subtle", subtle); err != nil {
		return err
	}
	return global.Set("crypto", cryptoObj)
}

func newSubtleDigestTmpl(iso *v8.Isolate, runtime *Runtime) *v8.FunctionTemplate {
	return v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		defer info.Release()
		resolver, err := v8.NewPromiseResolver(info.Context())
		if err != nil {
			return runtime.responseError(info, err)
		}
		defer resolver.Release()
		args := info.Args()
		if len(args) < 2 {
			rejectErr(iso, resolver, errors.New("digest: needs 2 args"))
			return resolver.GetPromise().Value.ReturnFromCallback()
		}
		var h hash.Hash
		switch strings.ToUpper(strings.ReplaceAll(args[0].String(), "-", "")) {
		case "SHA1":
			// SHA-1 is retained only for explicitly requested WebCrypto
			// compatibility; sha1cd detects known collision attacks.
			h = sha1cd.New()
		case "SHA256":
			h = sha256.New()
		case "SHA384":
			h = sha512.New384()
		case "SHA512":
			h = sha512.New()
		default:
			rejectErr(iso, resolver, errors.New("digest: unsupported algorithm"))
			return resolver.GetPromise().Value.ReturnFromCallback()
		}
		dataObj, err := args[1].AsObject()
		if err != nil {
			rejectErr(iso, resolver, err)
			return resolver.GetPromise().Value.ReturnFromCallback()
		}
		buf, err := runtime.readCryptoBytes(info, dataObj)
		if err != nil {
			rejectErr(iso, resolver, err)
			return resolver.GetPromise().Value.ReturnFromCallback()
		}
		h.Write(buf)
		runtime.resolveCryptoBytes(info.Context(), resolver, h.Sum(nil))
		return resolver.GetPromise().Value.ReturnFromCallback()
	})
}

func cryptoArrayLength(obj *v8.Object, maximum int) (int, error) {
	if obj == nil {
		return 0, errors.New("crypto: object input required")
	}
	lenVal, err := obj.Get("length")
	if err != nil {
		return 0, err
	}
	defer lenVal.Release()
	if !lenVal.IsNumber() {
		return 0, errors.New("crypto: numeric input length required")
	}
	length := lenVal.Number()
	if length < 0 || length > float64(maximum) || math.Trunc(length) != length {
		return 0, errors.New("crypto: invalid or excessive input length")
	}
	return int(length), nil
}

func (r *Runtime) readCryptoBytes(info *v8.FunctionCallbackInfo, obj *v8.Object) ([]byte, error) {
	owner := r.contextFor(info.Context())
	if owner == nil || owner.closed.Load() || obj == nil {
		return nil, errors.New("crypto: active owner and byte input required")
	}
	if owner.evalCtx != nil && owner.evalCtx.Err() != nil {
		return nil, owner.evalCtx.Err()
	}
	if err := owner.async.ctx.Err(); err != nil {
		return nil, err
	}
	if obj.IsArrayBuffer() || obj.IsArrayBufferView() || obj.IsSharedArrayBuffer() {
		return obj.BufferBytes(maxCryptoInputBytes)
	}
	n, err := cryptoArrayLength(obj, maxCryptoInputBytes)
	if err != nil {
		return nil, err
	}
	out := make([]byte, n)
	for i := 0; i < n; i++ {
		if i%256 == 0 {
			if owner.evalCtx != nil && owner.evalCtx.Err() != nil {
				return nil, owner.evalCtx.Err()
			}
			if err := owner.async.ctx.Err(); err != nil {
				return nil, err
			}
		}
		v, err := obj.GetIdx(uint32(i))
		if err != nil {
			return nil, err
		}
		if !v.IsNumber() {
			v.Release()
			return nil, errors.New("crypto: byte input contains a non-byte value")
		}
		value := v.Number()
		valid := value >= 0 && value <= 255 && math.Trunc(value) == value
		v.Release()
		if !valid {
			return nil, errors.New("crypto: byte input contains a non-byte value")
		}
		out[i] = byte(value)
	}
	return out, nil
}

func (r *Runtime) resolveCryptoBytes(ctx *v8.Context, resolver *v8.PromiseResolver, data []byte) {
	value, err := ctx.NewArrayBuffer(data)
	if err != nil {
		rejectErr(r.iso, resolver, err)
		return
	}
	defer value.Release()
	if resolver.Resolve(value) {
		return
	}
	if span := r.execution; span != nil {
		span.recordFailure(errors.New("crypto: native result publication failed"))
		return
	}
	thrown := r.iso.ThrowException(v8.Null(r.iso))
	thrown.Release()
}
