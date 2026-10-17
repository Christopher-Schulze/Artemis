package v8go_test

import (
	v8 "github.com/tommie/v8go"
	"testing"
)

func TestMapDeleteKeyUsesNativeStorageAndPreservesBorrowedValues(t *testing.T) {
	iso := v8.NewIsolate()
	defer iso.Dispose()
	ctx := v8.NewContext(iso)
	defer ctx.Close()
	value, err := ctx.RunScript(`globalThis.actualMap=new Map([[7,'owned']]); actualMap.delete=()=>{throw Error('page method must not run')}; actualMap`, "<native-map>")
	if err != nil {
		t.Fatal(err)
	}
	defer value.Release()
	key, err := v8.NewValue(iso, int32(7))
	if err != nil {
		t.Fatal(err)
	}
	defer key.Release()
	before := iso.RetainedValueCount()
	for iteration := 0; iteration < 2; iteration++ {
		if err := value.DeleteMapKey(key); err != nil {
			t.Fatal(err)
		}
	}
	if iso.RetainedValueCount() != before || key.Int32() != 7 {
		t.Fatal("native map deletion changed caller handle ownership")
	}
	result, err := ctx.RunScript("actualMap.size===0 && !actualMap.has(7)", "<native-map-proof>")
	if err != nil {
		t.Fatal(err)
	}
	defer result.Release()
	if !result.Boolean() {
		t.Fatal("native map still retains removed key")
	}
	other := v8.NewIsolate()
	defer other.Dispose()
	foreign, err := v8.NewValue(other, int32(7))
	if err != nil {
		t.Fatal(err)
	}
	defer foreign.Release()
	if err := value.DeleteMapKey(foreign); err == nil {
		t.Fatal("cross-isolate map key accepted")
	}
	fake, err := ctx.RunScript("({delete(){return true}})", "<fake-map>")
	if err != nil {
		t.Fatal(err)
	}
	defer fake.Release()
	if err := fake.DeleteMapKey(key); err == nil {
		t.Fatal("map-shaped object passed native brand admission")
	}
}

func TestMapDeleteKeyDoesNotPumpPendingMicrotasks(t *testing.T) {
	iso := v8.NewIsolate()
	defer iso.Dispose()
	ctx := v8.NewContext(iso)
	defer ctx.Close()
	value, err := ctx.RunScript("new Map([[7,'owned']])", "<microtask-safe-map>")
	if err != nil {
		t.Fatal(err)
	}
	defer value.Release()
	key, err := v8.NewValue(iso, int32(7))
	if err != nil {
		t.Fatal(err)
	}
	defer key.Release()
	resolver, err := v8.NewPromiseResolver(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer resolver.Release()
	promise := resolver.GetPromise()
	defer promise.Release()
	called := false
	chained := promise.Then(func(info *v8.FunctionCallbackInfo) *v8.Value { defer info.Release(); called = true; return nil })
	defer chained.Release()
	failure := ""
	cleanup := v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		defer info.Release()
		if !resolver.Resolve(key) {
			failure = "actual promise did not resolve"
			return nil
		}
		if called {
			failure = "control did not leave a pending actual microtask"
			return nil
		}
		if err := value.DeleteMapKey(key); err != nil {
			failure = err.Error()
			return nil
		}
		if called {
			failure = "native cleanup executed a pending page microtask"
		}
		return nil
	}).GetFunction(ctx)
	defer cleanup.Release()
	global := ctx.Global()
	defer global.Release()
	if err := global.Set("actualCleanup", cleanup); err != nil {
		t.Fatal(err)
	}
	result, err := ctx.RunScript("actualCleanup()", "<owned-native-map-cleanup>")
	if result != nil {
		result.Release()
	}
	if err != nil || failure != "" {
		t.Fatalf("native cleanup violated queued callback ownership: %s %v", failure, err)
	}
	ctx.PerformMicrotaskCheckpoint()
	if !called {
		t.Fatal("native cleanup lost the queued actual callback")
	}
}
