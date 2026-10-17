package js

import (
	"errors"

	v8 "github.com/tommie/v8go"
)

const maxCallbackReturnValues = 4096

func (r *Runtime) callbackPrimitive(info *v8.FunctionCallbackInfo, primitive any) *v8.Value {
	value, err := v8.NewValue(r.iso, primitive)
	if err != nil {
		return r.responseError(info, err)
	}
	return value.ReturnFromCallback()
}

// Returned callback handles remain live until native frames have copied their
// values. The owning execution retires them before releasing the isolate gate.
func (c *Context) callbackResult(value *v8.Value) *v8.Value {
	if len(c.callbackValues) >= maxCallbackReturnValues {
		value.Release()
		failure := errors.New("JavaScript callback result budget exceeded")
		if span := c.rt.execution; span != nil {
			span.recordFailure(failure)
			span.interrupted = true
			c.rt.iso.TerminateExecution()
		} else {
			thrown := c.rt.iso.ThrowException(v8.Null(c.rt.iso))
			thrown.Release()
		}
		return v8.Undefined(c.rt.iso)
	}
	c.callbackValues = append(c.callbackValues, value)
	return value
}

func (c *Context) releaseCallbackValues() {
	for _, value := range c.callbackValues {
		value.Release()
	}
	clear(c.callbackValues)
	c.callbackValues = c.callbackValues[:0]
}
