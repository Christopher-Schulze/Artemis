package js

import (
	"context"
	"errors"
	"fmt"

	v8 "github.com/tommie/v8go"
)

// EventListenerError reports bounded listener failures after dispatch finishes.
// Other listeners still run; the owning operation cannot acknowledge success.
type EventListenerError struct{ Message string }

func (e *EventListenerError) Error() string { return "event listener: " + e.Message }

func (c *Context) documentReadyState() string {
	switch c.documentLoadingStep {
	case 0:
		return "loading"
	case 1, 2:
		return "interactive"
	default:
		return "complete"
	}
}

func (c *Context) bindDocumentEventDispatcher() error {
	global := c.v8ctx.Global()
	defer global.Release()
	value, err := global.Get("__artemisDispatchDocumentEvent")
	if err != nil {
		return err
	}
	function, err := value.AsFunction()
	if err != nil {
		value.Release()
		return fmt.Errorf("document event dispatcher: %w", err)
	}
	c.documentEventDispatch = function
	return nil
}

func (c *Context) dispatchDocumentEvent(operation int32) (*v8.Value, error) {
	argument := mustValue(c.rt.iso, operation)
	defer argument.Release()
	global := c.v8ctx.Global()
	defer global.Release()
	return c.documentEventDispatch.Call(global, argument)
}

func (c *Context) takeDocumentEventErrors() error {
	value, err := c.dispatchDocumentEvent(0)
	if err != nil {
		return formatJSError(err)
	}
	defer value.Release()
	if message := value.String(); message != "" {
		return &EventListenerError{Message: message}
	}
	return nil
}

// CompleteDocumentLoading completes the existing eager script loader's four
// loading stages once. It does not implement streaming/module/resource loading.
func (c *Context) CompleteDocumentLoading(ctx context.Context) error {
	_, err := c.execute(ctx, func() (*Value, error) { return nil, c.completeDocumentLoadingLocked(ctx) })
	return err
}

func (c *Context) completeDocumentLoadingLocked(ctx context.Context) error {
	if c.documentLoadingError != nil {
		return c.documentLoadingError
	}
	for c.documentLoadingStep < 4 {
		if c.closed.Load() {
			return errors.New("document loading: context closed")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		operation := [...]int32{1, 2, 1, 3}[c.documentLoadingStep]
		c.documentLoadingStep++
		value, err := c.dispatchDocumentEvent(operation)
		if value != nil {
			value.Release()
		}
		c.documentLoadingError = errors.Join(formatJSError(err), c.takeDocumentEventErrors(), c.styleMgr.takeFailure())
		if c.documentLoadingError == nil {
			c.documentLoadingError = c.pumpAsyncTasks()
		}
		c.documentLoadingError = errors.Join(c.documentLoadingError, c.rt.execution.takePromiseRejections())
		if c.documentLoadingError != nil {
			return c.documentLoadingError
		}
	}
	return ctx.Err()
}
