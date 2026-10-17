package js

import (
	"fmt"

	v8 "github.com/tommie/v8go"
)

// Capture the owning realm's implementation before page scripts can replace it.
func (c *Context) bindDOMExceptionConstructor() error {
	global := c.v8ctx.Global()
	defer global.Release()
	value, err := global.Get("DOMException")
	if err != nil {
		return err
	}
	constructor, err := value.AsFunction()
	if err != nil {
		value.Release()
		return fmt.Errorf("DOMException constructor: %w", err)
	}
	c.domExceptionClass = constructor
	return nil
}

func (c *Context) throwDOMException(name, message string) *v8.Value {
	text, err := v8.NewValue(c.rt.iso, message)
	if err != nil {
		return domCallbackResult(c.rt.iso, domCallbackError(c.rt.iso, err))
	}
	defer text.Release()
	kind, err := v8.NewValue(c.rt.iso, name)
	if err != nil {
		return domCallbackResult(c.rt.iso, domCallbackError(c.rt.iso, err))
	}
	defer kind.Release()
	exception, err := c.domExceptionClass.NewInstance(text, kind)
	if err != nil {
		return domCallbackResult(c.rt.iso, domCallbackError(c.rt.iso, err))
	}
	defer exception.Release()
	return c.rt.iso.ThrowException(exception.Value).ReturnFromCallback()
}
