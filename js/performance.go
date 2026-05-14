package js

import (
	"time"

	v8 "rogchap.com/v8go"
)

// installPerformance installs `performance.now()`, `performance.timeOrigin`,
// `performance.mark`, `performance.measure` (entries are snapshot-only
// until full PerformanceObserver lands).
func installPerformance(iso *v8.Isolate, v8ctx *v8.Context) error {
	origin := time.Now()

	// When a startup snapshot is loaded, performance already exists with
	// the JS-side mark/measure/getEntries* implementations from the parity
	// bootstrap. We only need to (re)bind the Go-backed now() with the
	// real time origin and timeOrigin. Otherwise build the full object.
	var obj *v8.Object
	if existing, err := v8ctx.Global().Get("performance"); err == nil && existing.IsObject() {
		obj, _ = existing.AsObject()
	}
	freshBuild := obj == nil
	if freshBuild {
		tmpl := v8.NewObjectTemplate(iso)
		fresh, err := tmpl.NewInstance(v8ctx)
		if err != nil {
			return err
		}
		obj = fresh
	}

	nowFn := v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		ms := float64(time.Since(origin).Microseconds()) / 1000.0
		out, _ := v8.NewValue(iso, ms)
		return out
	})
	_ = obj.Set("now", nowFn.GetFunction(v8ctx))
	_ = obj.Set("timeOrigin", float64(origin.UnixMilli()))

	if freshBuild {
		// Without a snapshot the JS-side mark/measure aren't installed,
		// so provide no-op stubs to keep feature detection passing.
		noop := v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			return v8.Null(iso)
		})
		_ = obj.Set("mark", noop.GetFunction(v8ctx))
		_ = obj.Set("measure", noop.GetFunction(v8ctx))
		_ = obj.Set("clearMarks", noop.GetFunction(v8ctx))
		_ = obj.Set("clearMeasures", noop.GetFunction(v8ctx))
		emptyArr := v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			arr, err := v8.NewObjectTemplate(iso).NewInstance(info.Context())
			if err != nil {
				return v8.Null(iso)
			}
			_ = arr.Set("length", int32(0))
			return arr.Value
		})
		_ = obj.Set("getEntries", emptyArr.GetFunction(v8ctx))
		_ = obj.Set("getEntriesByName", emptyArr.GetFunction(v8ctx))
		_ = obj.Set("getEntriesByType", emptyArr.GetFunction(v8ctx))
		return v8ctx.Global().Set("performance", obj)
	}
	return nil
}
