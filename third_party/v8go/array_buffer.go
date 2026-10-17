package v8go

// #include "v8go.h"
import "C"

import (
	"errors"
	"math"
	"unsafe"
)

// NewArrayBuffer copies data into a context-owned V8 ArrayBuffer. No Go pointer
// is retained. The caller owns the context/isolate throughout this operation.
func (c *Context) NewArrayBuffer(data []byte) (*Value, error) {
	if c == nil || c.ptr == nil || len(data) > math.MaxInt32 {
		return nil, errors.New("ArrayBuffer: valid context and bounded bytes required")
	}
	var pointer *C.uint8_t
	if len(data) != 0 {
		pointer = (*C.uint8_t)(unsafe.Pointer(&data[0]))
	}
	return valueResult(c, C.ContextNewArrayBuffer(c.ptr, pointer, C.int(len(data))))
}

// BufferBytes copies an ArrayBuffer or view's exact region into owned Go bytes.
// Shared/detached buffers and excess bounds fail before destination allocation.
// The caller owns the context/isolate until both native calls have completed.
func (v *Value) BufferBytes(maxBytes int) ([]byte, error) {
	if v == nil || v.ptr == nil || maxBytes < 0 || maxBytes > math.MaxInt32 {
		return nil, errors.New("ArrayBuffer: valid value and byte limit required")
	}
	size := C.ValueBufferSize(v.ptr, C.int(maxBytes))
	if size.error.msg != nil {
		return nil, newJSError(size.error)
	}
	data := make([]byte, int(size.length))
	var pointer *C.uint8_t
	if len(data) != 0 {
		pointer = (*C.uint8_t)(unsafe.Pointer(&data[0]))
	}
	result := C.ValueCopyBuffer(v.ptr, pointer, C.int(size.length))
	if result.msg != nil {
		return nil, newJSError(result)
	}
	return data, nil
}

// BufferByteLength returns the actual ArrayBuffer/view region size without
// allocating or reading page properties. Shared/detached storage is rejected.
func (v *Value) BufferByteLength() (uint64, error) {
	if v == nil || v.ptr == nil {
		return 0, errors.New("ArrayBuffer: valid value required")
	}
	result := C.ValueBufferByteLength(v.ptr)
	if result.error.msg != nil {
		return 0, newJSError(result.error)
	}
	return uint64(result.length), nil
}

// SetBufferBytes copies exactly one ArrayBuffer/view region from owned Go
// bytes. It never dispatches page code or retains the source pointer.
func (v *Value) SetBufferBytes(data []byte) error {
	if v == nil || v.ptr == nil || len(data) > math.MaxInt32 {
		return errors.New("ArrayBuffer: valid value and bounded bytes required")
	}
	var pointer *C.uint8_t
	if len(data) != 0 {
		pointer = (*C.uint8_t)(unsafe.Pointer(&data[0]))
	}
	result := C.ValueWriteBuffer(v.ptr, pointer, C.int(len(data)))
	if result.msg != nil {
		return newJSError(result)
	}
	return nil
}
