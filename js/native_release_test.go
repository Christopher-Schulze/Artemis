package js

import (
	"runtime"
	"testing"
)

func TestNativeValuesRetireAcrossOwnedOSThreadSwitches(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	page := newCtxFromHTML(t, "<body></body>", nil)
	jobs := make(chan *Value)
	retired := make(chan struct{})
	done := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(done)
		for value := range jobs {
			value.Release()
			retired <- struct{}{}
		}
	}()
	defer func() { close(jobs); <-done }()
	for index := range 256 {
		value, err := page.Eval(t.Context(), `({actual:new Uint8Array(256),answer:42})`)
		if err != nil {
			t.Fatal(err)
		}
		before := page.v8ctx.RetainedValueCount()
		jobs <- value
		<-retired
		if after := page.v8ctx.RetainedValueCount(); after != before-1 || value.Raw() != nil {
			t.Fatalf("thread switch %d did not retire its actual handle: before=%d after=%d raw=%v", index, before, after, value.Raw())
		}
	}
	if value, err := page.EvalString(t.Context(), `String(6*7)`); err != nil || value != "42" {
		t.Fatalf("execution after cross-thread handle retirement failed: %q %v", value, err)
	}
}
