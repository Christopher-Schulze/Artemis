package js

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	v8 "github.com/tommie/v8go"
)

const executionChildEnvironment = "ARTEMIS_TEST_EXECUTION_CHILD"

func TestExecutionCancellationIsOwned(t *testing.T) {
	if mode := os.Getenv(executionChildEnvironment); mode != "" {
		switch mode {
		case "deadline", "timer", "microtask", "loading", "observer":
			assertActualExecutionDeadline(t, mode)
		case "context-close", "runtime-close":
			assertActualExecutionClose(t, mode)
		case "queued":
			assertActualQueuedCancellation(t)
		case "borrow-cancel":
			assertBorrowedCancellation(t)
		case "iframe-close", "iframe-deadline":
			assertActualFrameCancellation(t, mode)
		case "setter-errors":
			assertNativeSetterErrors(t)
		case "setter-deadline":
			assertNativeSetterDeadline(t)
		case "promise-resolve-termination", "promise-reject-termination":
			assertNativePromiseTermination(t, mode)
		case "async-fetch-cancel":
			assertAsyncFetchOperationCancellation(t)
		default:
			t.Fatalf("unknown owned execution case: %s", mode)
		}
		return
	}
	for _, mode := range []string{"deadline", "timer", "microtask", "loading", "observer", "context-close", "runtime-close", "queued", "borrow-cancel", "iframe-close", "iframe-deadline", "setter-errors", "setter-deadline", "promise-resolve-termination", "promise-reject-termination", "async-fetch-cancel"} {
		t.Run(mode, func(t *testing.T) { runOwnedExecutionChild(t, mode) })
	}
}

func assertNativePromiseTermination(t *testing.T, mode string) {
	t.Helper()
	c := newCtxFromHTML(t, "<html></html>", nil)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	called, accepted := false, true
	_, err := c.execute(ctx, func() (*Value, error) {
		resolver, err := v8.NewPromiseResolver(c.v8ctx)
		if err != nil {
			return nil, err
		}
		defer resolver.Release()
		value, err := v8.NewValue(c.rt.iso, "actual settlement")
		if err != nil {
			return nil, err
		}
		defer value.Release()
		callback := v8.NewFunctionTemplate(c.rt.iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			defer info.Release()
			called = true
			c.rt.execution.interrupted = true
			cancel()
			c.rt.iso.TerminateExecution()
			interrupted, failure := c.v8ctx.RunScript("0", "<interrupted-promise-owner>")
			if interrupted != nil {
				interrupted.Release()
			}
			c.rt.execution.recordFailure(failure)
			if mode == "promise-resolve-termination" {
				accepted = resolver.Resolve(value)
			} else {
				accepted = resolver.Reject(value)
			}
			return nil
		}).GetFunction(c.v8ctx)
		defer callback.Release()
		global := c.v8ctx.Global()
		defer global.Release()
		if err := global.Set("__actualPromiseTermination", callback); err != nil {
			return nil, err
		}
		result, err := c.v8ctx.RunScript("__actualPromiseTermination()", "<native-promise-termination>")
		if result != nil {
			result.Release()
		}
		return nil, err
	})
	if !called || accepted || err == nil {
		t.Fatalf("interrupted native settlement acknowledged success: called=%t accepted=%t err=%v", called, accepted, err)
	}
	assertExecutionRecovery(t, c)
}

func assertNativeSetterErrors(t *testing.T) {
	t.Helper()
	c := newCtxFromHTML(t, "<html></html>", nil)
	for _, test := range []struct {
		name, expression, message string
		assign                    func(*v8.Object) error
	}{
		{"named", `({set field(value) { throw Error('named-setter-failure') }})`, "named-setter-failure", func(object *v8.Object) error { return object.Set("field", "value") }},
		{"indexed", `({set 0(value) { throw Error('indexed-setter-failure') }})`, "indexed-setter-failure", func(object *v8.Object) error { return object.SetIdx(0, "value") }},
		{"batch", `({set field(value) { throw Error('batch-setter-failure') }})`, "batch-setter-failure", func(object *v8.Object) error {
			return object.SetMany([]string{"before", "field", "after"}, []interface{}{true, "value", true})
		}},
		{"proxy exception", `new Proxy({}, {set() { throw Error('proxy-setter-failure') }})`, "proxy-setter-failure", func(object *v8.Object) error { return object.Set("field", "value") }},
		{"empty exception", `({set field(value) { throw '' }})`, "JavaScript exception (empty message)", func(object *v8.Object) error { return object.Set("field", "value") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := c.execute(t.Context(), func() (*Value, error) {
				raw, err := c.v8ctx.RunScript(test.expression, "<actual-native-setter-control>")
				if err != nil {
					return nil, err
				}
				defer raw.Release()
				object, err := raw.AsObject()
				if err != nil {
					return nil, err
				}
				failure := test.assign(object)
				if test.name == "batch" {
					value, err := object.Get("after")
					if err != nil {
						return nil, err
					}
					defer value.Release()
					if !value.IsUndefined() {
						t.Error("native property batch continued after failure")
					}
				}
				return nil, failure
			})
			var nativeError *v8.JSError
			if !errors.As(err, &nativeError) || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("native setter did not preserve its actual failure: %v", err)
			}
			assertExecutionRecovery(t, c)
		})
	}
}

func assertNativeSetterDeadline(t *testing.T) {
	t.Helper()
	c := newCtxFromHTML(t, "<html></html>", nil)
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	_, err := c.execute(ctx, func() (*Value, error) {
		raw, err := c.v8ctx.RunScript(`({set field(value) { for (;;) {} }})`, "<actual-native-setter-deadline>")
		if err != nil {
			return nil, err
		}
		defer raw.Release()
		object, err := raw.AsObject()
		if err != nil {
			return nil, err
		}
		return nil, object.Set("field", "value")
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("native setter ignored its actual execution deadline: %v", err)
	}
	assertExecutionRecovery(t, c)
}

func assertActualFrameCancellation(t *testing.T, mode string) {
	t.Helper()
	entered := make(chan struct{})
	c := newCtxFromHTMLOpts(t, `<iframe src="/child"></iframe>`, ContextOpts{
		Console: FuncConsole(func(_, message string) {
			if message == "frame-entered" {
				close(entered)
			}
		}),
		LoadIFrame: func(ctx context.Context, target string) (*IFrameResponse, error) {
			return &IFrameResponse{URL: target, Body: []byte(`<script>window.addEventListener('message', () => { console.log('frame-entered'); for (;;) {} })</script>`)}, nil
		},
	})
	value, err := c.Eval(t.Context(), `document.querySelector('iframe').contentDocument`)
	if err != nil {
		t.Fatal(err)
	}
	value.Raw().Release()
	var child *Context
	c.iframes.mu.Lock()
	for _, owner := range c.iframes.ctxs {
		child = owner
	}
	c.iframes.mu.Unlock()
	if child == nil {
		t.Fatal("actual iframe did not create a child context")
	}
	ctx := t.Context()
	want := context.Canceled
	if mode == "iframe-deadline" {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()
		want = context.DeadlineExceeded
	}
	evaluated := make(chan error, 1)
	go func() {
		value, err := c.Eval(ctx, `document.querySelector('iframe').contentWindow.postMessage('run')`)
		if value != nil {
			value.Raw().Release()
		}
		evaluated <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("actual nested script never entered")
	}
	if mode == "iframe-close" {
		child.Close()
	}
	if err := <-evaluated; !errors.Is(err, want) {
		t.Fatalf("nested execution lost its cancellation owner: %v", err)
	}
	assertExecutionRecovery(t, c)
}

func assertBorrowedCancellation(t *testing.T) {
	t.Helper()
	for _, closeLifetime := range []bool{false, true} {
		caller, stopCaller := context.WithCancel(t.Context())
		lifetime, stopLifetime := context.WithCancel(t.Context())
		borrowed, release := borrowContext(caller, lifetime)
		if closeLifetime {
			stopLifetime()
			<-borrowed.Done()
		}
		release()
		release()
		if !errors.Is(borrowed.Err(), context.Canceled) || caller.Err() != nil || !closeLifetime && lifetime.Err() != nil {
			t.Fatal("borrowed cancellation changed a parent or lost its own cancellation")
		}
		stopLifetime()
		stopCaller()
	}
}

func runOwnedExecutionChild(t *testing.T, mode string) {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "-test.run=^TestExecutionCancellationIsOwned$", "-test.count=1")
	command.Env = append(os.Environ(), executionChildEnvironment+"="+mode)
	command.WaitDelay = time.Second
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("owned pure-JS cancellation/recovery child failed: %v owner=%v output=%s", err, ctx.Err(), output)
	}
}

func assertActualExecutionDeadline(t *testing.T, mode string) {
	t.Helper()
	c := newCtxFromHTML(t, "<html></html>", nil)
	sibling, err := c.rt.NewContext(c.doc, ContextOpts{})
	if err != nil {
		t.Fatal(err)
	}
	defer sibling.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	source := "for (;;) {}"
	if mode == "timer" {
		source = "setTimeout(() => { for (;;) {} }, 0)"
	} else if mode == "microtask" {
		source = "Promise.resolve().then(() => { for (;;) {} })"
	} else if mode == "observer" {
		source = "new MutationObserver(() => { for (;;) {} }).observe(document.body, {attributes:true}); document.body.setAttribute('data-trigger', 'run')"
	} else if mode == "loading" {
		if _, err := c.Eval(t.Context(), "document.addEventListener('DOMContentLoaded', () => { for (;;) {} })"); err != nil {
			t.Fatal(err)
		}
		if err := c.CompleteDocumentLoading(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("loading callback ignored actual deadline: %v", err)
		}
		assertExecutionRecovery(t, c)
		assertExecutionRecovery(t, sibling)
		return
	}
	if value, err := c.Eval(ctx, source); value != nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("infinite JavaScript ignored actual owner deadline: value=%v err=%v", value, err)
	}
	assertExecutionRecovery(t, c)
	assertExecutionRecovery(t, sibling)
}

func assertExecutionRecovery(t *testing.T, c *Context) {
	t.Helper()
	value, err := c.Eval(t.Context(), "6 * 7")
	if err != nil || value.Int64() != 42 {
		t.Fatalf("terminated execution corrupted later isolate use: value=%v err=%v", value, err)
	}
	value.Raw().Release()
}

func startOwnedInfiniteExecution(t *testing.T) (*Context, context.CancelFunc, <-chan error) {
	t.Helper()
	entered := make(chan struct{})
	c := newCtxFromHTMLOpts(t, "<html></html>", ContextOpts{Console: FuncConsole(func(_, message string) {
		if message == "entered" {
			close(entered)
		}
	})})
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	finished := make(chan error, 1)
	go func() {
		_, err := c.Eval(ctx, "console.log('entered'); for (;;) {}")
		finished <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("actual infinite script never entered")
	}
	return c, cancel, finished
}

func assertActualExecutionClose(t *testing.T, mode string) {
	t.Helper()
	c, _, evaluated := startOwnedInfiniteExecution(t)
	closed := make(chan struct{})
	go func() {
		if mode == "runtime-close" {
			c.rt.Close()
		} else {
			c.Close()
		}
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("close did not retire actual running JavaScript")
	}
	if err := <-evaluated; !errors.Is(err, context.Canceled) {
		t.Fatalf("close lost actual execution cancellation: %v", err)
	}
	next, err := c.rt.NewContext(c.doc, ContextOpts{})
	if mode == "runtime-close" {
		if next != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("closed runtime admitted a native context: %v", err)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	assertExecutionRecovery(t, next)
}

func assertActualQueuedCancellation(t *testing.T) {
	t.Helper()
	c, stop, evaluated := startOwnedInfiniteExecution(t)
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if value, err := c.Eval(ctx, "globalThis.queuedEffect = true"); value != nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("queued caller did not cancel while another script held the isolate: %v", err)
	}
	stop()
	if err := <-evaluated; !errors.Is(err, context.Canceled) {
		t.Fatalf("running owner did not retire: %v", err)
	}
	value, err := c.EvalString(t.Context(), "typeof queuedEffect")
	if err != nil || value != "undefined" {
		t.Fatalf("cancelled admission performed a script effect: %q %v", value, err)
	}
}
