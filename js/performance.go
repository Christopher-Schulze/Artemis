package js

import (
	"time"

	v8 "github.com/tommie/v8go"
)

// installPerformance binds the owning Context's clock. Entry methods are owned
// by the existing parity bootstrap in both fresh and snapshot-created realms.
func installPerformance(c *Context) error {
	global := c.v8ctx.Global()
	defer global.Release()

	// When a startup snapshot is loaded, performance already exists with
	// the JS-side mark/measure/getEntries* implementations from the parity
	// bootstrap. We only need to (re)bind the Go-backed now() with the
	// real time origin and timeOrigin. Otherwise build the full object.
	obj, err := bindingObject(global, c.v8ctx, "performance")
	if err != nil {
		return err
	}
	defer obj.Release()
	function := c.rt.ensurePerformanceTemplate().GetFunction(c.v8ctx)
	defer function.Release()
	if err := obj.Set("now", function); err != nil {
		return err
	}
	if err := obj.Set("timeOrigin", float64(c.performanceOrigin.UnixMilli())); err != nil {
		return err
	}
	return global.Set("performance", obj)
}

func (r *Runtime) ensurePerformanceTemplate() *v8.FunctionTemplate {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.performanceTemplate == nil {
		r.performanceTemplate = v8.NewFunctionTemplate(r.iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			defer info.Release()
			c := r.contextFor(info.Context())
			if c == nil {
				return v8.Undefined(r.iso)
			}
			milliseconds := float64(time.Since(c.performanceOrigin).Microseconds()) / 1000
			return r.callbackPrimitive(info, milliseconds)
		})
	}
	return r.performanceTemplate
}
