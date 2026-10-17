package v8go_test

import (
	"strings"
	"testing"

	v8 "github.com/tommie/v8go"
)

func TestPromiseRejectionsTrackHandlersAndRetirePendingReasons(t *testing.T) {
	iso := v8.NewIsolate()
	defer iso.Dispose()
	ctx := v8.NewContext(iso)
	defer ctx.Close()
	if err := ctx.EnablePromiseRejectionTracking(); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ source, want string }{
		{`Promise.reject('actual native rejection')`, "actual native rejection"},
		{`Promise.reject('handled').catch(()=>{})`, ""},
		{`Promise.reject('')`, "reason: "},
		{`Promise.reject(new Error('actual native error'))`, "actual native error"},
		{`Promise.resolve().then(()=>{throw Error('derived actual rejection')})`, "derived actual rejection"},
	} {
		value, err := ctx.RunScript(test.source, "<native-rejection-control>")
		if err != nil {
			t.Fatal(err)
		}
		value.Release()
		message, err := ctx.TakePromiseRejections()
		if err != nil {
			t.Fatal(err)
		}
		if (test.want == "" && message != "") || (test.want != "" && !strings.Contains(message, test.want)) {
			t.Fatalf("diagnostic=%q want=%q", message, test.want)
		}
		if again, err := ctx.TakePromiseRejections(); err != nil || again != "" {
			t.Fatalf("taken reasons retained: %q %v", again, err)
		}
	}
}

func TestPromiseRejectionDiagnosticsNeverDispatchReasonCodeAndBoundGrowth(t *testing.T) {
	iso := v8.NewIsolate()
	defer iso.Dispose()
	ctx := v8.NewContext(iso)
	defer ctx.Close()
	if err := ctx.EnablePromiseRejectionTracking(); err != nil {
		t.Fatal(err)
	}
	value, err := ctx.RunScript(`globalThis.reasonCalls=0;const reason={toString(){reasonCalls++;throw Error('must not dispatch')}};const error=new Error('actual');Object.defineProperty(error,'message',{get(){reasonCalls++;throw Error('must not read')}});Promise.reject(reason);Promise.reject(error);Promise.reject('x'.repeat(100000));for(let index=0;index<4100;index++)Promise.reject('bounded');`, "<native-rejection-budget>")
	if err != nil {
		t.Fatal(err)
	}
	value.Release()
	message, err := ctx.TakePromiseRejections()
	if err != nil || !strings.Contains(message, "tracking limit") || len(message) > 34000 {
		t.Fatalf("unbounded or missing rejection quota: length=%d err=%v", len(message), err)
	}
	value, err = ctx.RunScript(`reasonCalls`, "<native-reason-dispatch-check>")
	if err != nil {
		t.Fatal(err)
	}
	defer value.Release()
	if value.Integer() != 0 {
		t.Fatal("diagnostic dispatched page reason code")
	}
	if again, err := ctx.TakePromiseRejections(); err != nil || again != "" {
		t.Fatalf("quota did not retire: %q %v", again, err)
	}
}

func TestPromiseRejectionCloseRetiresCreationRealmBeforeForeignHandler(t *testing.T) {
	iso := v8.NewIsolate()
	defer iso.Dispose()
	first := v8.NewContext(iso)
	second := v8.NewContext(iso)
	defer second.Close()
	if err := first.EnablePromiseRejectionTracking(); err != nil {
		t.Fatal(err)
	}
	if err := second.EnablePromiseRejectionTracking(); err != nil {
		t.Fatal(err)
	}
	value, err := first.RunScript(`Promise.reject('closed realm')`, "<closed-rejection>")
	if err != nil {
		first.Close()
		t.Fatal(err)
	}
	global := second.Global()
	if err := global.Set("foreignPromise", value); err != nil {
		value.Release()
		global.Release()
		first.Close()
		t.Fatal(err)
	}
	global.Release()
	value.Release()
	first.Close()
	if !first.IsClosed() || second.IsClosed() {
		t.Fatal("physical context closure observation is wrong")
	}
	value, err = second.RunScript(`foreignPromise.catch(()=>{})`, "<foreign-handler>")
	if err != nil {
		t.Fatal(err)
	}
	value.Release()
	if message, err := second.TakePromiseRejections(); err != nil || message != "" {
		t.Fatalf("closed creation realm leaked into sibling: %q %v", message, err)
	}
}
