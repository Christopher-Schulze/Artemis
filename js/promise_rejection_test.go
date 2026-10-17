package js

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestUnhandledPromiseRejectionsCannotAcknowledgeSuccess(t *testing.T) {
	for _, test := range []struct{ name, source string }{
		{"direct", `Promise.reject(new Error('actual-unhandled'))`},
		{"microtask", `Promise.resolve().then(()=>{throw new Error('actual-unhandled')})`},
		{"timer", `setTimeout(async()=>{throw new Error('actual-unhandled')},0)`},
		{"observer", `new MutationObserver(async()=>{throw new Error('actual-unhandled')}).observe(document.body,{childList:true});document.body.appendChild(document.createElement('div'))`},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := newCtxFromHTML(t, `<body></body>`, nil)
			value, err := c.Eval(t.Context(), test.source)
			if value != nil {
				value.Raw().Release()
			}
			if err == nil || !strings.Contains(err.Error(), "actual-unhandled") {
				t.Fatalf("unhandled rejection acknowledged success: %v", err)
			}
			var rejection *UnhandledPromiseRejectionError
			if !errors.As(err, &rejection) {
				t.Fatalf("native rejection lost its typed owner: %v", err)
			}
			assertExecutionRecovery(t, c)
		})
	}
}

func TestHandledPromiseRejectionsDoNotFailExecution(t *testing.T) {
	c := newCtxFromHTML(t, `<body></body>`, nil)
	for _, source := range []string{
		`Promise.reject(new Error('handled')).catch(()=>{})`,
		`Promise.resolve().then(()=>{throw new Error('handled')}).catch(()=>{})`,
		`async function handled(){try{await Promise.reject('handled')}catch{}};handled()`,
	} {
		if err := c.EvalDiscard(t.Context(), source); err != nil {
			t.Fatalf("handled rejection failed: %v", err)
		}
	}
}

func TestActualAsyncFetchRejectionSurfacesThroughWaitIdle(t *testing.T) {
	release := make(chan struct{})
	c := newCtxFromHTMLOpts(t, `<body></body>`, ContextOpts{AsyncFetch: true, Fetch: func(ctx context.Context, request FetchRequest) (*FetchResponse, error) {
		select {
		case <-release:
			return nil, errors.New("actual-unhandled-fetch")
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}})
	if err := c.EvalDiscard(t.Context(), `void fetch('/actual')`); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := c.WaitIdle(t.Context()); err == nil || !strings.Contains(err.Error(), "actual-unhandled-fetch") {
		t.Fatalf("unhandled transport rejection acknowledged success: %v", err)
	}
	assertExecutionRecovery(t, c)
}

func TestActualChildPromiseRejectionRemainsOwnedByParentExecution(t *testing.T) {
	c := newCtxFromHTMLOpts(t, `<iframe src='/actual'></iframe>`, ContextOpts{LoadIFrame: func(ctx context.Context, target string) (*IFrameResponse, error) {
		return &IFrameResponse{URL: target, Body: []byte(`<script>Promise.resolve().then(()=>{throw new Error('actual-unhandled-child')})</script>`)}, nil
	}})
	if err := c.EvalDiscard(t.Context(), `try{document.querySelector('iframe').contentDocument}catch{}`); err == nil || !strings.Contains(err.Error(), "actual-unhandled-child") {
		t.Fatalf("child rejection acknowledged parent success: %v", err)
	}
}

func TestEmptyAndOpaquePromiseReasonsRemainObservableWithoutDispatch(t *testing.T) {
	c := newCtxFromHTML(t, `<body></body>`, nil)
	for _, source := range []string{`Promise.reject('')`, `Promise.reject()`, `Promise.reject(null)`, `Promise.reject(false)`, `Promise.reject({toString(){throw Error('must not dispatch')}})`} {
		var rejection *UnhandledPromiseRejectionError
		if err := c.EvalDiscard(t.Context(), source); !errors.As(err, &rejection) {
			t.Fatalf("rejection reason disappeared: %v", err)
		}
	}
	assertExecutionRecovery(t, c)
}

func TestForeignRealmPromiseRejectionUsesTheActualExecutingOwner(t *testing.T) {
	first := newCtxFromHTML(t, `<body></body>`, nil)
	second, err := first.rt.NewContext(first.doc, ContextOpts{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(second.Close)
	function, err := first.Eval(t.Context(), `(()=>{Promise.reject(new Error('actual-foreign-rejection'))})`)
	if err != nil {
		t.Fatal(err)
	}
	global := second.V8Context().Global()
	err = global.Set("foreignOperation", function.Raw())
	global.Release()
	function.Raw().Release()
	if err != nil {
		t.Fatal(err)
	}
	if err := second.EvalDiscard(t.Context(), `foreignOperation()`); err == nil || !strings.Contains(err.Error(), "actual-foreign-rejection") {
		t.Fatalf("foreign creation realm escaped executing owner: %v", err)
	}
	assertExecutionRecovery(t, first)
	assertExecutionRecovery(t, second)
}

func TestUnhandledRejectionDoesNotContaminateUntouchedSiblingExecution(t *testing.T) {
	first := newCtxFromHTML(t, `<body></body>`, nil)
	second, err := first.rt.NewContext(first.doc, ContextOpts{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(second.Close)
	value, err := first.V8Context().RunScript(`Promise.reject('actual-sibling-rejection')`, "<unmanaged-sibling-control>")
	if err != nil {
		t.Fatal(err)
	}
	value.Release()
	assertExecutionRecovery(t, second)
	if err := first.EvalDiscard(t.Context(), `void 0`); err == nil || !strings.Contains(err.Error(), "actual-sibling-rejection") {
		t.Fatalf("actual creation owner lost pending rejection: %v", err)
	}
}

func TestClosingForeignCreationOwnerCannotEraseItsPendingRejection(t *testing.T) {
	var first *Context
	closed := make(chan struct{})
	first = newCtxFromHTMLOpts(t, `<body></body>`, ContextOpts{Console: FuncConsole(func(_ string, message string) {
		if message != "close-creation-owner" {
			return
		}
		go func() { first.Close(); close(closed) }()
		<-first.async.ctx.Done()
	})})
	second, err := first.rt.NewContext(first.doc, ContextOpts{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(second.Close)
	function, err := first.Eval(t.Context(), `(()=>{Promise.reject('actual-closing-rejection');console.log('close-creation-owner')})`)
	if err != nil {
		t.Fatal(err)
	}
	global := second.V8Context().Global()
	err = global.Set("foreignClosingOperation", function.Raw())
	global.Release()
	function.Raw().Release()
	if err != nil {
		t.Fatal(err)
	}
	err = second.EvalDiscard(t.Context(), `foreignClosingOperation()`)
	<-closed
	if err == nil || !strings.Contains(err.Error(), "actual-closing-rejection") {
		t.Fatalf("logical close erased rejection before physical disposal: %v", err)
	}
	assertExecutionRecovery(t, second)
}

func TestDocumentLoadingPromiseRejectionRemainsStickyAcrossRetry(t *testing.T) {
	c := newCtxFromHTML(t, `<body></body>`, nil)
	if err := c.EvalDiscard(t.Context(), `document.addEventListener('DOMContentLoaded',async()=>{throw Error('actual-unhandled-loading')})`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		var rejection *UnhandledPromiseRejectionError
		if err := c.CompleteDocumentLoading(t.Context()); !errors.As(err, &rejection) || !strings.Contains(err.Error(), "actual-unhandled-loading") {
			t.Fatalf("loading retry acknowledged a rejected listener: %v", err)
		}
	}
}
