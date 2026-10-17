package v8go

// #include "v8go.h"
import "C"

import "errors"

// DeleteMapKey removes key from an actual native Map without invoking a page
// method or pumping microtasks. Removing an absent key succeeds. Values remain
// caller-owned; the caller must own both handles and the live isolate.
func (v *Value) DeleteMapKey(key Valuer) error {
	if v == nil || v.ptr == nil || key == nil || key.value() == nil || key.value().ptr == nil {
		return errors.New("Map deletion requires valid Map and key handles")
	}
	result := C.ValueDeleteMapKey(v.ptr, key.value().ptr)
	if result.msg != nil {
		return newJSError(result)
	}
	return nil
}
