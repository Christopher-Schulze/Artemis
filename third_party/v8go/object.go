// Copyright 2021 Roger Chapman and the v8go contributors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package v8go

// #include <stdlib.h>
// #include "v8go.h"
import "C"
import (
	"fmt"
	"math/big"
	"unsafe"
)

// Object is a JavaScript object (ECMA-262, 4.3.3)
type Object struct {
	*Value
}

func (o *Object) MethodCall(methodName string, args ...Valuer) (*Value, error) {
	ckey := C.CString(methodName)
	defer C.free(unsafe.Pointer(ckey))

	getRtn := C.ObjectGet(o.ptr, ckey)
	prop, err := valueResult(o.ctx, getRtn)
	if err != nil {
		return nil, err
	}
	fn, err := prop.AsFunction()
	if err != nil {
		return nil, err
	}
	return fn.Call(o, args...)
}

func coerceValue(iso *Isolate, val interface{}) (*Value, error) {
	switch v := val.(type) {
	case string, int32, uint32, int64, uint64, float64, bool, *big.Int:
		// ignoring error as code cannot reach the error state as we are already
		// validating the new value types in this case statement
		value, _ := NewValue(iso, v)
		return value, nil
	case Valuer:
		return v.value(), nil
	default:
		return nil, fmt.Errorf("v8go: unsupported object property type `%T`", v)
	}
}

// Set will set a property on the Object to a given value.
// Supports all value types, eg: Object, Array, Date, Set, Map etc
// If the value passed is a Go supported primitive (string, int32, uint32, int64, uint64, float64, big.Int)
// then a *Value will be created and set as the value property.
func (o *Object) Set(key string, val interface{}) error {
	value, err := coerceValue(o.ctx.iso, val)
	if err != nil {
		return err
	}

	ckey := C.CString(key)
	defer C.free(unsafe.Pointer(ckey))
	C.ObjectSet(o.ptr, ckey, value.ptr)
	return nil
}

// PreparedKeys is a pre-CString'd, cgo-safe array of property names.
// Build once at package init via PrepareKeys; reuse across many SetMany
// calls to skip the C.CString + free per key on the hot path.
type PreparedKeys struct {
	cKeys []*C.char
	count int
}

// PrepareKeys allocates C strings for each key. The returned PreparedKeys
// is safe to share across goroutines (read-only). Free is rarely needed
// since these are intended to live as package-level singletons.
func PrepareKeys(keys []string) *PreparedKeys {
	pk := &PreparedKeys{cKeys: make([]*C.char, len(keys)), count: len(keys)}
	for i, k := range keys {
		pk.cKeys[i] = C.CString(k)
	}
	return pk
}

// Free releases the C strings owned by pk. Only call when the keys will
// never be used again (typically not called for package-level keys).
func (pk *PreparedKeys) Free() {
	for _, k := range pk.cKeys {
		C.free(unsafe.Pointer(k))
	}
	pk.cKeys = nil
	pk.count = 0
}

// Len returns the number of keys.
func (pk *PreparedKeys) Len() int { return pk.count }

// SetManyPrepared sets each (keys[i], vals[i]) pair on the object in a
// single cgo crossing. The number of values must equal the prepared key
// count. Compared to repeated Set calls this saves N-1 cgo crossings
// plus N C.CString allocations.
func (o *Object) SetManyPrepared(keys *PreparedKeys, vals []interface{}) error {
	if keys == nil || keys.count == 0 {
		return nil
	}
	if len(vals) != keys.count {
		return fmt.Errorf("v8go: SetManyPrepared expects %d vals, got %d", keys.count, len(vals))
	}
	cVals := make([]C.ValuePtr, keys.count)
	for i, val := range vals {
		v, err := coerceValue(o.ctx.iso, val)
		if err != nil {
			return err
		}
		cVals[i] = v.ptr
	}
	C.ObjectSetMany(o.ptr, C.int(keys.count),
		(**C.char)(unsafe.Pointer(&keys.cKeys[0])),
		(*C.ValuePtr)(unsafe.Pointer(&cVals[0])))
	return nil
}

// SetMany is the convenience form: builds a fresh PreparedKeys, sets,
// then frees. For hot paths use PrepareKeys + SetManyPrepared.
func (o *Object) SetMany(keys []string, vals []interface{}) error {
	pk := PrepareKeys(keys)
	defer pk.Free()
	return o.SetManyPrepared(pk, vals)
}

// Set will set a given index on the Object to a given value.
// Supports all value types, eg: Object, Array, Date, Set, Map etc
// If the value passed is a Go supported primitive (string, int32, uint32, int64, uint64, float64, big.Int)
// then a *Value will be created and set as the value property.
func (o *Object) SetIdx(idx uint32, val interface{}) error {
	value, err := coerceValue(o.ctx.iso, val)
	if err != nil {
		return err
	}

	C.ObjectSetIdx(o.ptr, C.uint32_t(idx), value.ptr)

	return nil
}

// SetInternalField sets the value of an internal field for an ObjectTemplate instance.
// Panics if the index isn't in the range set by (*ObjectTemplate).SetInternalFieldCount.
func (o *Object) SetInternalField(idx uint32, val interface{}) error {
	value, err := coerceValue(o.ctx.iso, val)

	if err != nil {
		return err
	}

	inserted := C.ObjectSetInternalField(o.ptr, C.int(idx), value.ptr)

	if inserted == 0 {
		panic(fmt.Errorf("index out of range [%v] with length %v", idx, o.InternalFieldCount()))
	}

	return nil
}

// InternalFieldCount returns the number of internal fields this Object has.
func (o *Object) InternalFieldCount() uint32 {
	count := C.ObjectInternalFieldCount(o.ptr)
	return uint32(count)
}

// Get tries to get a Value for a given Object property key.
func (o *Object) Get(key string) (*Value, error) {
	ckey := C.CString(key)
	defer C.free(unsafe.Pointer(ckey))

	rtn := C.ObjectGet(o.ptr, ckey)
	return valueResult(o.ctx, rtn)
}

// GetInternalField gets the Value set by SetInternalField for the given index
// or the JS undefined value if the index hadn't been set.
// Panics if given an out of range index.
func (o *Object) GetInternalField(idx uint32) *Value {
	rtn := C.ObjectGetInternalField(o.ptr, C.int(idx))
	if rtn == nil {
		panic(fmt.Errorf("index out of range [%v] with length %v", idx, o.InternalFieldCount()))
	}
	return &Value{rtn, o.ctx}

}

// GetIdx tries to get a Value at a give Object index.
func (o *Object) GetIdx(idx uint32) (*Value, error) {
	rtn := C.ObjectGetIdx(o.ptr, C.uint32_t(idx))
	return valueResult(o.ctx, rtn)
}

// Has calls the abstract operation HasProperty(O, P) described in ECMA-262, 7.3.10.
// Returns true, if the object has the property, either own or on the prototype chain.
func (o *Object) Has(key string) bool {
	ckey := C.CString(key)
	defer C.free(unsafe.Pointer(ckey))
	return C.ObjectHas(o.ptr, ckey) != 0
}

// HasIdx returns true if the object has a value at the given index.
func (o *Object) HasIdx(idx uint32) bool {
	return C.ObjectHasIdx(o.ptr, C.uint32_t(idx)) != 0
}

// Delete returns true if successful in deleting a named property on the object.
func (o *Object) Delete(key string) bool {
	ckey := C.CString(key)
	defer C.free(unsafe.Pointer(ckey))
	return C.ObjectDelete(o.ptr, ckey) != 0
}

// DeleteIdx returns true if successful in deleting a value at a given index of the object.
func (o *Object) DeleteIdx(idx uint32) bool {
	return C.ObjectDeleteIdx(o.ptr, C.uint32_t(idx)) != 0
}
