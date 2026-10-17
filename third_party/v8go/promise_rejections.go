package v8go

// #include <stdlib.h>
// #include "v8go.h"
import "C"

import (
	"errors"
	"unsafe"
)

const MaxTrackedPromiseRejections = C.V8GO_MAX_PROMISE_REJECTIONS

// SetPromiseRejectionObserver observes the creation realm of newly unhandled
// rejections. It runs synchronously under isolate ownership and must not enter JS.
func (i *Isolate) SetPromiseRejectionObserver(observer func(*Context)) {
	i.cbMutex.Lock()
	i.promiseRejectionObserver = observer
	i.cbMutex.Unlock()
}

//export goPromiseRejection
func goPromiseRejection(reference C.int) {
	ctx := getContext(int(reference))
	if ctx == nil {
		return
	}
	ctx.iso.cbMutex.RLock()
	observer := ctx.iso.promiseRejectionObserver
	ctx.iso.cbMutex.RUnlock()
	if observer != nil {
		observer(ctx)
	}
}

// EnablePromiseRejectionTracking opts this realm into the native isolate hook.
// Pending identities retire on handler admission, explicit take or Context close.
func (c *Context) EnablePromiseRejectionTracking() error {
	if c == nil || c.ptr == nil {
		return errors.New("promise tracking: live context required")
	}
	result := C.ContextEnablePromiseRejectionTracking(c.ptr)
	if result.msg != nil {
		return newJSError(result)
	}
	return nil
}

// TakePromiseRejections retires pending ownership and returns up to sixteen
// bounded diagnostics without invoking reason getters, toString or page code.
// Quota overflow is sticky until take, even if subsequent handlers are attached.
func (c *Context) TakePromiseRejections() (string, error) {
	if c == nil || c.ptr == nil {
		return "", errors.New("promise tracking: live context required")
	}
	result := C.ContextTakePromiseRejections(c.ptr)
	if result.error.msg != nil {
		return "", newJSError(result.error)
	}
	defer C.free(unsafe.Pointer(result.data))
	return C.GoStringN(result.data, C.int(result.length)), nil
}
