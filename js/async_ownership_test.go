package js

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func assertAsyncFetchOperationCancellation(t *testing.T) {
	t.Helper()
	entered, cancelled, allowReturn := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var release sync.Once
	defer release.Do(func() { close(allowReturn) })
	c := newCtxFromHTMLOpts(t, "<html></html>", ContextOpts{AsyncFetch: true, Fetch: func(ctx context.Context, _ FetchRequest) (*FetchResponse, error) {
		close(entered)
		<-ctx.Done()
		close(cancelled)
		<-allowReturn
		return nil, ctx.Err()
	}})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	evaluated := make(chan error, 1)
	go func() {
		evaluated <- c.EvalDiscard(ctx, `globalThis.observed=''; fetch('/owned').then(()=>{observed='invalid success'},e=>{observed=String(e)}); for(;;){}`)
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("actual async transport never entered")
	}
	cancel()
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("active Eval cancellation did not reach its async transport")
	}
	select {
	case err := <-evaluated:
		t.Fatalf("Eval acknowledged before actual transport cleanup: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	release.Do(func() { close(allowReturn) })
	select {
	case err := <-evaluated:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Eval lost cancellation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled Eval did not join actual transport cleanup")
	}
	if err := c.WaitIdle(t.Context()); err != nil {
		t.Fatal(err)
	}
	got, err := c.EvalString(t.Context(), "observed")
	if err != nil || !strings.Contains(got, "canceled") || c.async.inflight.Load() != 0 {
		t.Fatalf("cancelled request acknowledged success or retained work: %q %v", got, err)
	}
	assertExecutionRecovery(t, c)
}

func TestAsyncFetchSuccessfulEvalTransfersToContextLifetime(t *testing.T) {
	entered, finish := make(chan context.Context, 1), make(chan struct{})
	var release sync.Once
	defer release.Do(func() { close(finish) })
	c := newCtxFromHTMLOpts(t, "<html></html>", ContextOpts{AsyncFetch: true, Fetch: func(ctx context.Context, _ FetchRequest) (*FetchResponse, error) {
		entered <- ctx
		select {
		case <-finish:
			return &FetchResponse{Status: 200, Body: []byte("actual completion")}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if err := c.EvalDiscard(ctx, `globalThis.observed=''; fetch('/handoff').then(r=>r.text()).then(text=>{observed=text},e=>{observed=String(e)});`); err != nil {
		t.Fatal(err)
	}
	var transport context.Context
	select {
	case transport = <-entered:
	case <-time.After(time.Second):
		t.Fatal("actual transport never entered")
	}
	cancel()
	if transport.Err() != nil {
		t.Fatalf("successful Eval retained transient caller cancellation: %v", transport.Err())
	}
	release.Do(func() { close(finish) })
	if err := c.WaitIdle(t.Context()); err != nil {
		t.Fatal(err)
	}
	got, err := c.EvalString(t.Context(), "observed")
	if err != nil || got != "actual completion" {
		t.Fatalf("handed-off request did not settle: %q %v", got, err)
	}
}

func TestAsyncCloseJoinsWorkersBeforeReleasingNativeContext(t *testing.T) {
	entered, cancelled, allowReturn := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var allow sync.Once
	defer allow.Do(func() { close(allowReturn) })
	c := newCtxFromHTMLOpts(t, "<html></html>", ContextOpts{AsyncFetch: true, Fetch: func(ctx context.Context, request FetchRequest) (*FetchResponse, error) {
		close(entered)
		<-ctx.Done()
		close(cancelled)
		<-allowReturn
		return nil, ctx.Err()
	}})
	if _, err := c.EvalString(t.Context(), `fetch('/owned'); 'started'`); err != nil {
		t.Fatal(err)
	}
	<-entered
	closed := make(chan struct{})
	go func() { c.Close(); close(closed) }()
	<-cancelled
	select {
	case <-closed:
		t.Error("Close acknowledged while the real fetch worker was still cleaning up")
	case <-time.After(20 * time.Millisecond):
	}
	allow.Do(func() { close(allowReturn) })
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close did not join completed fetch cleanup")
	}
	if pending := c.async.inflight.Load(); pending != 0 || len(c.async.pending) != 0 {
		t.Fatalf("closed context retained pending native resolver ownership: inflight=%d queued=%d", pending, len(c.async.pending))
	}
}

func TestRuntimeCloseJoinsActiveFetchWorkersBeforeDisposingIsolate(t *testing.T) {
	entered, cancelled, allowReturn := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var allow sync.Once
	defer allow.Do(func() { close(allowReturn) })
	c := newCtxFromHTMLOpts(t, "<html></html>", ContextOpts{AsyncFetch: true, Fetch: func(ctx context.Context, _ FetchRequest) (*FetchResponse, error) {
		close(entered)
		<-ctx.Done()
		close(cancelled)
		<-allowReturn
		return nil, ctx.Err()
	}})
	if _, err := c.EvalString(t.Context(), `fetch('/runtime-close'); 'started'`); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("actual fetch worker never entered")
	}
	closed := make(chan struct{})
	go func() { c.rt.Close(); close(closed) }()
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("Runtime.Close did not cancel its active fetch worker")
	}
	select {
	case <-closed:
		t.Fatal("Runtime.Close disposed its isolate before fetch cleanup returned")
	case <-time.After(20 * time.Millisecond):
	}
	allow.Do(func() { close(allowReturn) })
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Runtime.Close did not join actual fetch cleanup")
	}
	if pending := c.async.inflight.Load(); pending != 0 || len(c.async.pending) != 0 {
		t.Fatalf("runtime close retained fetch ownership: inflight=%d queued=%d", pending, len(c.async.pending))
	}
	if c.V8Context() != nil || c.rt.Isolate() != nil {
		t.Fatal("Runtime.Close left disposed native handles exposed")
	}
}

func TestAsyncFetchAdmissionCannotOutgrowItsResultQueue(t *testing.T) {
	var entered atomic.Int64
	finish := make(chan struct{})
	var release sync.Once
	defer release.Do(func() { close(finish) })
	c := newCtxFromHTMLOpts(t, "<html></html>", ContextOpts{AsyncFetch: true, Fetch: func(ctx context.Context, request FetchRequest) (*FetchResponse, error) {
		entered.Add(1)
		select {
		case <-finish:
			return &FetchResponse{Status: 200, Body: []byte("settled")}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}})
	limit := cap(c.async.pending)
	if _, err := c.EvalString(t.Context(), fmt.Sprintf(`globalThis.rejected = 0; for (let i = 0; i < %d; i++) fetch('/bounded').catch(() => { rejected++ }); 'started'`, limit+1)); err != nil {
		t.Fatal(err)
	}
	if got := c.async.inflight.Load(); got != int64(limit) {
		t.Errorf("unbounded outstanding fetches: got=%d limit=%d", got, limit)
	}
	if got, err := c.EvalString(t.Context(), `String(rejected)`); err != nil || got != "1" {
		t.Fatalf("overflow did not reject exactly the excess promise: %q %v", got, err)
	}
	release.Do(func() { close(finish) })
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := c.WaitIdle(ctx); err != nil {
		t.Fatal(err)
	}
	if got := entered.Load(); got != int64(limit) {
		t.Fatalf("admission spawned unexpected transport work: got=%d limit=%d", got, limit)
	}
	if got := c.async.inflight.Load(); got != 0 || len(c.async.pending) != 0 {
		t.Fatalf("settled workers retained result ownership: inflight=%d queued=%d", got, len(c.async.pending))
	}
}
