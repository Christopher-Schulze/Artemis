package v8go

import "testing"

func TestCoerceValuePreservesNativeValueErrors(t *testing.T) {
	for _, test := range []struct {
		name  string
		value interface{}
	}{{"string", "actual text"}, {"boolean", true}} {
		t.Run(test.name, func(t *testing.T) {
			value, err := coerceValue(nil, test.value)
			if value != nil || err == nil {
				t.Fatalf("native value-construction failure was lost: value=%v err=%v", value, err)
			}
		})
	}
}

func TestStringCheckedPreservesBytesAndConversionErrors(t *testing.T) {
	isolate := NewIsolate()
	defer isolate.Dispose()
	ctx := NewContext(isolate)
	defer ctx.Close()
	for _, test := range []struct {
		name, source, want string
		invalid            bool
	}{
		{"embedded NUL", `'actual\x00text'`, "actual\x00text", false},
		{"conversion", `({toString(){return 'converted'}})`, "converted", false},
		{"throwing conversion", `({toString(){throw Error('actual failure')}})`, "", true},
		{"empty throw", `({toString(){throw ''}})`, "", true},
		{"symbol", `Symbol('invalid DOMString')`, "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			value, err := ctx.RunScript(test.source, "<checked-string>")
			if err != nil {
				t.Fatal(err)
			}
			defer value.Release()
			got, err := value.StringChecked()
			if test.invalid {
				if err == nil || err.Error() == "" {
					t.Fatalf("conversion error discarded: %q %v", got, err)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("string bytes changed: %q %v", got, err)
			}
		})
	}
}

func TestObjectWritersRetireOnlyOwnedPrimitiveConversions(t *testing.T) {
	isolate := NewIsolate()
	defer isolate.Dispose()
	ctx := NewContext(isolate)
	defer ctx.Close()
	template := NewObjectTemplate(isolate)
	template.SetInternalFieldCount(1)
	object, err := template.NewInstance(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer object.Release()
	borrowed, err := NewValue(isolate, "caller-owned")
	if err != nil {
		t.Fatal(err)
	}
	defer borrowed.Release()
	for _, test := range []struct {
		name    string
		write   func() error
		invalid bool
	}{
		{"named", func() error { return object.Set("actual", "owned") }, false},
		{"indexed", func() error { return object.SetIdx(0, true) }, false},
		{"internal", func() error { return object.SetInternalField(0, int32(42)) }, false},
		{"batch", func() error {
			return object.SetMany([]string{"one", "two", "borrowed"}, []interface{}{"first", int32(2), borrowed})
		}, false},
		{"failed conversion batch", func() error { return object.SetMany([]string{"one", "bad"}, []interface{}{"owned", struct{}{}}) }, true},
		{"borrowed", func() error { return object.Set("borrowed", borrowed) }, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := isolate.RetainedValueCount()
			err := test.write()
			if (err != nil) != test.invalid {
				t.Fatalf("write error=%v invalid=%t", err, test.invalid)
			}
			if after := isolate.RetainedValueCount(); after != before {
				t.Fatalf("primitive conversion retained handles: before=%d after=%d", before, after)
			}
			if borrowed.String() != "caller-owned" {
				t.Fatal("borrowed caller value was retired")
			}
		})
	}
	stored, err := object.Get("actual")
	if err != nil {
		t.Fatal(err)
	}
	defer stored.Release()
	if stored.String() != "owned" {
		t.Fatal("retiring conversion destroyed stored JavaScript value")
	}
}

func TestNullPrototypeObjectOwnsValuesWithoutPageDispatch(t *testing.T) {
	var absent *Context
	if object, err := absent.NewNullPrototypeObject(); err == nil || object != nil {
		t.Fatal("nil context admitted an object")
	}
	isolate := NewIsolate()
	defer isolate.Dispose()
	ctx := NewContext(isolate)
	defer ctx.Close()
	global := ctx.Global()
	defer global.Release()
	value, err := ctx.RunScript(`Object.defineProperty(Object.prototype, 'actual', {set() { throw Error('page setter reached'); }});`, "<prototype-control>")
	if err != nil {
		t.Fatal(err)
	}
	value.Release()
	before := ctx.RetainedValueCount()
	for index := 0; index < 20; index++ {
		object, err := ctx.NewNullPrototypeObject()
		if err != nil {
			t.Fatal(err)
		}
		if err := object.Set("actual", "owned value"); err != nil {
			object.Release()
			t.Fatal(err)
		}
		if err := global.Set("actualTransport", object); err != nil {
			object.Release()
			t.Fatal(err)
		}
		object.Release()
	}
	if after := ctx.RetainedValueCount(); after != before {
		t.Fatalf("object handle leak: before=%d after=%d", before, after)
	}
	value, err = ctx.RunScript(`Object.getPrototypeOf(actualTransport) === null && actualTransport.actual === 'owned value'`, "<object-survival>")
	if err != nil {
		t.Fatal(err)
	}
	defer value.Release()
	if !value.Boolean() {
		t.Fatal("object lost its null prototype or stored value after handle retirement")
	}
}
