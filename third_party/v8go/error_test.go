package v8go_test

import (
	v8 "github.com/tommie/v8go"
	"testing"
)

func TestContextTypeErrorUsesOwnRealmWithoutPageConstructor(t *testing.T) {
	iso := v8.NewIsolate()
	defer iso.Dispose()
	ctx := v8.NewContext(iso)
	defer ctx.Close()
	result, err := ctx.RunScript(`globalThis.actualType=TypeError; TypeError=()=>{throw Error('page constructor must not run')}`, "<error-realm>")
	if err != nil {
		t.Fatal(err)
	}
	result.Release()
	value, err := ctx.NewTypeError("actual\x00\u00ff message")
	if err != nil {
		t.Fatal(err)
	}
	defer value.Release()
	global := ctx.Global()
	defer global.Release()
	if err := global.Set("actualError", value); err != nil {
		t.Fatal(err)
	}
	result, err = ctx.RunScript(`actualError instanceof actualType && actualError.name==='TypeError' && actualError.message==='actual\x00\u00ff message'`, "<error-realm-proof>")
	if err != nil {
		t.Fatal(err)
	}
	defer result.Release()
	if !result.Boolean() {
		t.Fatal("native TypeError used a foreign realm, page constructor or changed message bytes")
	}
	var absent *v8.Context
	if value, err := absent.NewTypeError("invalid"); value != nil || err == nil {
		t.Fatal("invalid context admitted native Error construction")
	}
}
