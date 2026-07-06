// Copyright 2024 the v8go contributors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package v8go_test

import (
	"testing"

	v8 "rogchap.com/v8go"
)

// BenchmarkCallbackParallel stresses the JS->Go callback dispatch path
// (goFunctionCallback -> getContext) across parallel goroutines, each holding
// its own Isolate+Context. Every callback reads the process-global context
// registry; with a shared RWMutex that read is a contended reader-count cache
// line across all isolates, with a lock-free sync.Map it is contention-free.
// Run with -cpu=1,4,8 to see the contention curve.
func BenchmarkCallbackParallel(b *testing.B) {
	const callsPerIter = 100
	script := "for (let i = 0; i < 100; i++) cb();"
	b.RunParallel(func(pb *testing.PB) {
		iso := v8.NewIsolate()
		defer iso.Dispose()
		global := v8.NewObjectTemplate(iso)
		cb := v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			return nil
		})
		if err := global.Set("cb", cb); err != nil {
			b.Fatal(err)
		}
		ctx := v8.NewContext(iso, global)
		defer ctx.Close()
		for pb.Next() {
			if _, err := ctx.RunScript(script, "bench.js"); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.SetBytes(callsPerIter)
}
