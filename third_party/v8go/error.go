package v8go

// #include <stdlib.h>
// #include "v8go.h"
import "C"

import (
	"errors"
	"math"
	"unsafe"
)

// NewTypeError creates an owned TypeError in this context's realm without
// invoking a page constructor. The caller must own the live context/isolate.
func (c *Context) NewTypeError(message string) (*Value, error) {
	if c == nil || c.ptr == nil || len(message) > math.MaxInt32 {
		return nil, errors.New("TypeError: valid context and bounded message required")
	}
	text := C.CString(message)
	defer C.free(unsafe.Pointer(text))
	return valueResult(c, C.ContextNewTypeError(c.ptr, text, C.int(len(message))))
}
