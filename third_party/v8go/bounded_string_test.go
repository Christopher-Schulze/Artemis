package v8go

import "testing"

func TestStringWithLimitPreservesExactUTF8AndRejectsExcess(t *testing.T) {
	isolate := NewIsolate()
	defer isolate.Dispose()
	ctx := NewContext(isolate)
	defer ctx.Close()
	for _, test := range []struct {
		name, source, want string
		limit              int
		invalid            bool
	}{
		{"empty", `''`, "", 0, false},
		{"NUL", `'a\x00b'`, "a\x00b", 3, false},
		{"UTF8 exact", `'é🙂'`, "é🙂", 6, false},
		{"UTF8 excess", `'é🙂'`, "", 5, true},
		{"excess units", `'x'.repeat(1024 * 1024)`, "", 16, true},
		{"converted", `({toString(){return 'actual'}})`, "actual", 6, false},
		{"conversion error", `({toString(){throw Error('actual cause')}})`, "", 16, true},
		{"empty error", `({toString(){throw ''}})`, "", 16, true},
		{"symbol", `Symbol('actual')`, "", 16, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			value, err := ctx.RunScript(test.source, "<bounded-string>")
			if err != nil {
				t.Fatal(err)
			}
			defer value.Release()
			before := ctx.RetainedValueCount()
			got, err := value.StringWithLimit(test.limit)
			if (err != nil) != test.invalid || got != test.want {
				t.Fatalf("bounded string=%q error=%v", got, err)
			}
			if ctx.RetainedValueCount() != before {
				t.Fatal("bounded string retained handles")
			}
		})
	}
	var absent *Value
	if _, err := absent.StringWithLimit(1); err == nil {
		t.Fatal("nil input accepted")
	}
	value, err := NewValue(isolate, "actual")
	if err != nil {
		t.Fatal(err)
	}
	defer value.Release()
	if _, err := value.StringWithLimit(-1); err == nil {
		t.Fatal("negative limit accepted")
	}
}
