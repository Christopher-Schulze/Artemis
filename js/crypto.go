package js

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	v8 "github.com/tommie/v8go"
)

const maxRandomValueBytes = 65536

type cryptoRandomTemplates struct{ uuid, values *v8.FunctionTemplate }

func (r *Runtime) ensureCryptoRandomTemplates() *cryptoRandomTemplates {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cryptoRandom == nil {
		r.cryptoRandom = &cryptoRandomTemplates{
			uuid:   v8.NewFunctionTemplate(r.iso, r.randomUUID),
			values: v8.NewFunctionTemplate(r.iso, r.getRandomValues),
		}
	}
	return r.cryptoRandom
}

// installCrypto installs window.crypto with randomUUID and
// getRandomValues. The implementation reads from crypto/rand on the Go
// side.
func installCrypto(c *Context) error {
	v8ctx := c.v8ctx
	global := v8ctx.Global()
	defer global.Release()
	// When a startup snapshot is loaded, globalThis.crypto already exists
	// (with the stub crypto.subtle object). Merge the Go-bound
	// randomUUID + getRandomValues onto that existing object instead of
	// replacing it, so installCryptoSubtle can extend the same object.
	obj, err := bindingObject(global, v8ctx, "crypto")
	if err != nil {
		return err
	}
	defer obj.Release()
	templates := c.rt.ensureCryptoRandomTemplates()
	for _, method := range []struct {
		name     string
		template *v8.FunctionTemplate
	}{
		{"randomUUID", templates.uuid}, {"getRandomValues", templates.values},
	} {
		function := method.template.GetFunction(v8ctx)
		err := obj.Set(method.name, function)
		function.Release()
		if err != nil {
			return err
		}
	}
	return global.Set("crypto", obj)
}

func bindingObject(global *v8.Object, ctx *v8.Context, name string) (*v8.Object, error) {
	existing, err := global.Get(name)
	if err != nil {
		return nil, err
	}
	if existing.IsObject() {
		object, err := existing.AsObject()
		if err != nil {
			existing.Release()
		}
		return object, err
	}
	existing.Release()
	return ctx.NewNullPrototypeObject()
}

func (r *Runtime) randomUUID(info *v8.FunctionCallbackInfo) *v8.Value {
	defer info.Release()
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return domCallbackResult(r.iso, domCallbackError(r.iso, errors.New("crypto: random source failed")))
	}
	// Set version (4) and variant (10).
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	s := fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]),
		hex.EncodeToString(b[4:6]),
		hex.EncodeToString(b[6:8]),
		hex.EncodeToString(b[8:10]),
		hex.EncodeToString(b[10:16]),
	)
	return r.callbackPrimitive(info, s)
}

func (r *Runtime) getRandomValues(info *v8.FunctionCallbackInfo) (result *v8.Value) {
	args := info.Args()
	defer info.This().Release()
	defer func() {
		for _, argument := range args {
			if argument != result {
				argument.Release()
			}
		}
	}()
	owner := r.contextFor(info.Context())
	if owner == nil {
		return v8.Undefined(r.iso)
	}
	if len(args) == 0 {
		return owner.throwRandomTypeError()
	}
	array := args[0]
	if !isRandomIntegerView(array) {
		return owner.throwDOMException("TypeMismatchError", "crypto: integer typed array required")
	}
	return owner.fillRandomArray(array)
}

func (owner *Context) fillRandomArray(array *v8.Value) *v8.Value {
	length, err := array.BufferByteLength()
	if err != nil {
		return owner.throwDOMException("TypeMismatchError", err.Error())
	}
	if length > maxRandomValueBytes {
		return owner.throwDOMException("QuotaExceededError", "crypto: random byte quota exceeded")
	}
	data := make([]byte, int(length))
	if _, err := rand.Read(data); err != nil {
		return owner.throwDOMException("OperationError", "crypto: random source failed")
	}
	if err := array.SetBufferBytes(data); err != nil {
		return owner.throwDOMException("OperationError", err.Error())
	}
	return array.ReturnFromCallback()
}

func isRandomIntegerView(value *v8.Value) bool {
	return value.IsInt8Array() || value.IsUint8Array() || value.IsUint8ClampedArray() ||
		value.IsInt16Array() || value.IsUint16Array() || value.IsInt32Array() ||
		value.IsUint32Array() || value.IsBigInt64Array() || value.IsBigUint64Array()
}

func (c *Context) throwRandomTypeError() *v8.Value {
	value, err := c.v8ctx.NewTypeError("crypto.getRandomValues requires an argument")
	if err != nil {
		return domCallbackResult(c.rt.iso, domCallbackError(c.rt.iso, err))
	}
	defer value.Release()
	return c.rt.iso.ThrowException(value).ReturnFromCallback()
}
