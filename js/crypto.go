package js

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"

	v8 "rogchap.com/v8go"
)

// installCrypto installs window.crypto with randomUUID and
// getRandomValues. The implementation reads from crypto/rand on the Go
// side.
func installCrypto(iso *v8.Isolate, v8ctx *v8.Context) error {
	// When a startup snapshot is loaded, globalThis.crypto already exists
	// with subtle.importKey/encrypt/etc baked in. Merge the Go-bound
	// randomUUID + getRandomValues onto that existing object instead of
	// replacing it (which would wipe out the snapshot's wrappers).
	var obj *v8.Object
	if existing, err := v8ctx.Global().Get("crypto"); err == nil && existing.IsObject() {
		obj, _ = existing.AsObject()
	}
	if obj == nil {
		tmpl := v8.NewObjectTemplate(iso)
		fresh, err := tmpl.NewInstance(v8ctx)
		if err != nil {
			return err
		}
		obj = fresh
	}

	uuidFn := v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		var b [16]byte
		if _, err := rand.Read(b[:]); err != nil {
			thrown, _ := v8.NewValue(iso, "crypto: rand failed")
			iso.ThrowException(thrown)
			return nil
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
		out, _ := v8.NewValue(iso, s)
		return out
	})
	if err := obj.Set("randomUUID", uuidFn.GetFunction(v8ctx)); err != nil {
		return err
	}

	getRandFn := v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		args := info.Args()
		if len(args) < 1 {
			return v8.Null(iso)
		}
		arr, err := args[0].AsObject()
		if err != nil || arr == nil {
			return v8.Null(iso)
		}
		// Read length, fill numeric indices with random bytes (0-255).
		lenVal, err := arr.Get("length")
		if err != nil {
			return v8.Null(iso)
		}
		n := int(lenVal.Integer())
		if n <= 0 || n > 65536 {
			return v8.Null(iso)
		}
		buf := make([]byte, n)
		if _, err := rand.Read(buf); err != nil {
			thrown, _ := v8.NewValue(iso, "crypto: rand failed")
			iso.ThrowException(thrown)
			return nil
		}
		for i, b := range buf {
			_ = arr.SetIdx(uint32(i), int32(b))
		}
		return arr.Value
	})
	if err := obj.Set("getRandomValues", getRandFn.GetFunction(v8ctx)); err != nil {
		return err
	}

	// In the merge branch, obj IS the existing global.crypto, so the
	// Set is effectively a no-op identity assignment. In the fresh
	// branch, we install the new object.
	return v8ctx.Global().Set("crypto", obj)
}
