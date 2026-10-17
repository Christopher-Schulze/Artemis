package v8go

// #include <stdlib.h>
// #include "v8go.h"
import "C"

import (
	"errors"
	"math"
	"unsafe"
)

// StringWithLimit converts to UTF-8 with checked JavaScript exceptions and an
// exact byte bound before allocating native or Go output buffers. The caller
// owns the live value/context/isolate throughout the operation.
func (v *Value) StringWithLimit(maxBytes int) (string, error) {
	if v == nil || v.ptr == nil || maxBytes < 0 || maxBytes > math.MaxInt32 {
		return "", errors.New("v8go: valid value and string byte limit required")
	}
	result := C.ValueToStringCheckedBounded(v.ptr, C.int(maxBytes))
	if result.error.msg != nil {
		return "", newJSError(result.error)
	}
	defer C.free(unsafe.Pointer(result.data))
	return C.GoStringN(result.data, C.int(result.length)), nil
}
