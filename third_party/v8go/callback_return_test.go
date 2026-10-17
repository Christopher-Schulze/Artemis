package v8go_test

import (
	v8 "github.com/tommie/v8go"
	"testing"
)

func TestCallbackReturnTransfersOwnedHandleAfterNativeCopy(t *testing.T) {
	iso := v8.NewIsolate()
	defer iso.Dispose()
	ctx := v8.NewContext(iso)
	defer ctx.Close()
	sequence := int32(0)
	var callbackError error
	function := v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		defer info.Release()
		sequence++
		value, err := v8.NewValue(iso, sequence)
		if err != nil {
			callbackError = err
			return nil
		}
		return value.ReturnFromCallback()
	}).GetFunction(ctx)
	defer function.Release()
	global := ctx.Global()
	defer global.Release()
	if err := global.Set("ownedReturn", function); err != nil {
		t.Fatal(err)
	}
	baseline := iso.RetainedValueCount()
	result, err := ctx.RunScript(`(() => {for(let index=1;index<=1000;index++) if(ownedReturn()!==index) return false; return true;})()`, "<owned-callback-returns>")
	if err != nil {
		t.Fatal(err)
	}
	defer result.Release()
	if callbackError != nil || !result.Boolean() || sequence != 1000 {
		t.Fatalf("native copy lost returned values: sequence=%d callback=%v", sequence, callbackError)
	}
	if got := iso.RetainedValueCount(); got != baseline {
		t.Fatalf("callback-owned handles leaked: got=%d baseline=%d", got, baseline)
	}
}

func TestCallbackReturnPreservesBorrowedAndOwnedObjectValues(t *testing.T) {
	iso := v8.NewIsolate()
	defer iso.Dispose()
	ctx := v8.NewContext(iso)
	defer ctx.Close()
	borrowed, err := v8.NewValue(iso, "caller-owned")
	if err != nil {
		t.Fatal(err)
	}
	defer borrowed.Release()
	global := ctx.Global()
	defer global.Release()
	var callbackError error
	for _, test := range []struct {
		name     string
		callback v8.FunctionCallback
	}{
		{"borrowed", func(info *v8.FunctionCallbackInfo) *v8.Value { defer info.Release(); return borrowed }},
		{"ownedObject", func(info *v8.FunctionCallbackInfo) *v8.Value {
			defer info.Release()
			value, err := info.Context().RunScript("({actual:42})", "<owned-callback-object>")
			if err != nil {
				callbackError = err
				return nil
			}
			return value.ReturnFromCallback()
		}},
	} {
		function := v8.NewFunctionTemplate(iso, test.callback).GetFunction(ctx)
		if err := global.Set(test.name, function); err != nil {
			t.Fatal(err)
		}
		function.Release()
	}
	baseline := ctx.RetainedValueCount()
	result, err := ctx.RunScript(`globalThis.retainedObject=ownedObject(); borrowed()==='caller-owned' && retainedObject.actual===42`, "<callback-ownership-proof>")
	if err != nil {
		t.Fatal(err)
	}
	if callbackError != nil || !result.Boolean() || borrowed.String() != "caller-owned" {
		t.Fatalf("native callback changed borrowed/object ownership: %v", callbackError)
	}
	result.Release()
	if got := ctx.RetainedValueCount(); got != baseline {
		t.Fatalf("owned object return leaked a page handle: got=%d baseline=%d", got, baseline)
	}
}
