package js

import (
	"sync/atomic"

	v8 "github.com/tommie/v8go"
)

// Value wraps a v8go.Value with type-safe accessors.
type Value struct {
	raw   atomic.Pointer[v8.Value]
	owner atomic.Pointer[Context]
}

func newValue(raw *v8.Value) *Value {
	value := &Value{}
	value.raw.Store(raw)
	return value
}

func (v *Value) lockRaw() (*v8.Value, *Context) {
	if v == nil {
		return nil, nil
	}
	owner := v.owner.Load()
	if owner == nil {
		return v.raw.Load(), nil
	}
	owner.rt.ctxMu.Lock()
	raw := v.raw.Load()
	if owner.closed.Load() || owner.rt.closed.Load() || raw == nil {
		owner.rt.ctxMu.Unlock()
		return nil, nil
	}
	return raw, owner
}

func unlockValueOwner(owner *Context) {
	if owner != nil {
		owner.rt.ctxMu.Unlock()
	}
}

// Raw returns a borrowed v8go value. Do not release or retain the pointer;
// call Value.Release instead. It is valid only while its owning Context
// remains open and must not be used concurrently with Release or Context.Close.
// Prefer the synchronized accessors below.
func (v *Value) Raw() *v8.Value {
	if v == nil {
		return nil
	}
	return v.raw.Load()
}

// Release retires this result early. Context.Close also invalidates every
// unreleased result returned by Eval.
func (v *Value) Release() {
	if v == nil {
		return
	}
	if owner := v.owner.Load(); owner != nil {
		owner.rt.ctxMu.Lock()
		owner.releaseEvalValueLocked(v)
		owner.rt.ctxMu.Unlock()
		return
	}
	if raw := v.raw.Swap(nil); raw != nil {
		raw.Release()
	}
}

// String returns the string representation of the value.
func (v *Value) String() string {
	raw, owner := v.lockRaw()
	defer unlockValueOwner(owner)
	if raw == nil {
		return ""
	}
	return raw.String()
}

// Bool returns the boolean coercion of the value.
func (v *Value) Bool() bool {
	raw, owner := v.lockRaw()
	defer unlockValueOwner(owner)
	if raw == nil {
		return false
	}
	return raw.Boolean()
}

// Int64 returns the integer coercion of the value.
func (v *Value) Int64() int64 {
	raw, owner := v.lockRaw()
	defer unlockValueOwner(owner)
	if raw == nil {
		return 0
	}
	return raw.Integer()
}

// Float64 returns the floating-point coercion of the value.
func (v *Value) Float64() float64 {
	raw, owner := v.lockRaw()
	defer unlockValueOwner(owner)
	if raw == nil {
		return 0
	}
	return raw.Number()
}

// IsUndefined reports whether the value is JS undefined.
func (v *Value) IsUndefined() bool {
	raw, owner := v.lockRaw()
	defer unlockValueOwner(owner)
	return raw != nil && raw.IsUndefined()
}

// IsNull reports whether the value is JS null.
func (v *Value) IsNull() bool {
	raw, owner := v.lockRaw()
	defer unlockValueOwner(owner)
	return raw != nil && raw.IsNull()
}

// IsObject reports whether the value is a JS object.
func (v *Value) IsObject() bool {
	raw, owner := v.lockRaw()
	defer unlockValueOwner(owner)
	return raw != nil && raw.IsObject()
}

// IsString reports whether the value is a JS string.
func (v *Value) IsString() bool {
	raw, owner := v.lockRaw()
	defer unlockValueOwner(owner)
	return raw != nil && raw.IsString()
}

// IsNumber reports whether the value is a JS number.
func (v *Value) IsNumber() bool {
	raw, owner := v.lockRaw()
	defer unlockValueOwner(owner)
	return raw != nil && raw.IsNumber()
}

// IsBool reports whether the value is a JS boolean.
func (v *Value) IsBool() bool {
	raw, owner := v.lockRaw()
	defer unlockValueOwner(owner)
	return raw != nil && raw.IsBoolean()
}
