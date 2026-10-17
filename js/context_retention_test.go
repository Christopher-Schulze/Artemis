package js

import (
	"runtime"
	"strings"
	"testing"
	"weak"

	"github.com/Christopher-Schulze/Artemis/parser"
)

func TestClosedPageContextsAreCollectableWhileRuntimeRemainsOpen(t *testing.T) {
	for _, test := range []struct {
		name     string
		poolSize int
	}{{"unpooled", 0}, {"pooled", 2}} {
		t.Run(test.name, func(t *testing.T) {
			owner := NewRuntimeWithPool(test.poolSize)
			t.Cleanup(owner.Close)
			var retired []weak.Pointer[Context]
			for range 4 {
				retired = append(retired, createAndCloseCookieContext(t, owner))
			}
			runtime.GC()
			runtime.GC()
			for index, pointer := range retired {
				if pointer.Value() != nil {
					t.Errorf("closed page %d remains reachable through the live Runtime", index)
				}
			}
			runtime.KeepAlive(owner)
		})
	}
}

func createAndCloseCookieContext(t *testing.T, owner *Runtime) weak.Pointer[Context] {
	t.Helper()
	document, err := parser.ParseHTML(strings.NewReader(`<div>actual retired page</div>`), "https://example.test/")
	if err != nil {
		t.Fatal(err)
	}
	cookie := ""
	writes := 0
	page, err := owner.NewContext(document, ContextOpts{GetCookie: func() string { return cookie }, SetCookie: func(value string) { cookie = value; writes++ }})
	if err != nil {
		t.Fatal(err)
	}
	if err := page.EvalDiscard(t.Context(), `let rejected=false;try{__doc_set_cookie({toString(){throw Error('actual cookie conversion failure')}})}catch{rejected=true}if(!rejected)throw Error('failed cookie conversion accepted')`); err != nil {
		page.Close()
		t.Fatal(err)
	}
	if writes != 0 {
		page.Close()
		t.Fatal("failed cookie conversion reached owner")
	}
	value, err := page.EvalString(t.Context(), `document.cookie='actual=retired';document.cookie`)
	if err != nil || value != "actual=retired" || writes != 1 {
		page.Close()
		t.Fatalf("cookie callback did not use actual page owner: %q %d %v", value, writes, err)
	}
	pointer := weak.Make(page)
	page.Close()
	return pointer
}

func TestPristineContextReuseRetiresNativeRebindingHandles(t *testing.T) {
	document, err := parser.ParseHTML(strings.NewReader(`<div></div>`), "https://example.test/")
	if err != nil {
		t.Fatal(err)
	}
	owner := NewRuntimeWithPool(1)
	t.Cleanup(owner.Close)
	first, err := owner.NewContext(document, ContextOpts{})
	if err != nil {
		t.Fatal(err)
	}
	realm := first.v8ctx
	first.Close()
	pageBefore, internalBefore := realm.RetainedValueCount(), owner.iso.RetainedValueCount()
	for range 8 {
		page, err := owner.NewContext(document, ContextOpts{})
		if err != nil {
			t.Fatal(err)
		}
		if page.v8ctx != realm {
			page.Close()
			t.Fatal("control did not actually reuse pristine realm")
		}
		page.Close()
	}
	if page, internal := realm.RetainedValueCount(), owner.iso.RetainedValueCount(); page != pageBefore || internal != internalBefore {
		t.Fatalf("pristine reuse retained handles: page=%d baseline=%d internal=%d baseline=%d", page, pageBefore, internal, internalBefore)
	}
}

func TestEvalValuesReleaseAndInvalidateWithTheirContext(t *testing.T) {
	c := newCtxFromHTML(t, `<body></body>`, nil)
	baseline := c.v8ctx.RetainedValueCount()
	released, err := c.Eval(t.Context(), `({actual: true})`)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.v8ctx.RetainedValueCount(); got != baseline+1 {
		t.Fatalf("Eval result handle not retained under its owner: got %d want %d", got, baseline+1)
	}
	released.Release()
	if released.Raw() != nil || c.v8ctx.RetainedValueCount() != baseline {
		t.Fatal("early result release did not retire the wrapper and native handle")
	}

	remaining, err := c.Eval(t.Context(), `({actual: 'closed'})`)
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	if remaining.Raw() != nil || remaining.String() != "" || remaining.IsObject() {
		t.Fatal("Context.Close left an executable Eval result wrapper")
	}
}

func TestRuntimeCloseInvalidatesEvalResults(t *testing.T) {
	document, err := parser.ParseHTML(strings.NewReader(`<body></body>`), "https://example.test/")
	if err != nil {
		t.Fatal(err)
	}
	owner := NewRuntime()
	t.Cleanup(owner.Close)
	page, err := owner.NewContext(document, ContextOpts{})
	if err != nil {
		t.Fatal(err)
	}
	value, err := page.Eval(t.Context(), `({actual: 'runtime-closed'})`)
	if err != nil {
		t.Fatal(err)
	}
	owner.Close()
	if value.Raw() != nil || value.String() != "" || value.IsObject() {
		t.Fatal("Runtime.Close left an executable Eval result wrapper")
	}
}
