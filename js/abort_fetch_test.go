package js

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestFetchHonorsAlreadyAbortedSignal(t *testing.T) {
	c := newCtxFromHTMLOpts(t, `<html></html>`, ContextOpts{
		Fetch: staticFetcher(t, "ok", 200),
	})
	if _, err := c.Eval(context.Background(), `
		var captured = '';
		const ac = new AbortController();
		ac.abort();
		fetch('https://e.test/', {signal: ac.signal})
			.then(() => { captured = 'resolved'; })
			.catch(e => { captured = 'rejected:' + (e && e.message || e); });
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	v, _ := c.Eval(context.Background(), `captured`)
	if !strings.HasPrefix(v.String(), "rejected:") || !strings.Contains(v.String(), "Abort") {
		t.Errorf("got %q, want rejected with AbortError", v.String())
	}
}

func TestFetchSignalNotAbortedProceeds(t *testing.T) {
	c := newCtxFromHTMLOpts(t, `<html></html>`, ContextOpts{
		Fetch: staticFetcher(t, "ok-body", 200),
	})
	if _, err := c.Eval(context.Background(), `
		var captured = '';
		const ac = new AbortController();
		fetch('https://e.test/', {signal: ac.signal}).then(r => r.text()).then(t => { captured = t; });
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	v, _ := c.Eval(context.Background(), `captured`)
	if v.String() != "ok-body" {
		t.Errorf("got %q", v.String())
	}
}

func TestAbortSignalOwnsReadonlyStateAndExactReasons(t *testing.T) {
	for _, reason := range []string{"null", "false", "0", "''", "({actual:42})"} {
		t.Run(reason, func(t *testing.T) {
			c := newCtxFromHTML(t, "<html></html>", nil)
			got, err := c.EvalString(t.Context(), `(() => {
				const reason=`+reason+`, controller=new AbortController(), signal=controller.signal;
				signal.aborted=true; signal.reason='forged'; controller.signal={aborted:true};
				const before=signal.aborted===false && signal.reason===undefined && controller.signal===signal;
				controller.abort(reason); controller.abort('replacement');
				let thrown=false; try{signal.throwIfAborted()}catch(error){thrown=error===reason}
				return [before,signal.aborted,signal.reason===reason,thrown,AbortSignal.abort(reason).reason===reason].join(':');
			})()`)
			if err != nil || got != "true:true:true:true:true" {
				t.Fatalf("abort state or exact reason lost: %q %v", got, err)
			}
		})
	}
}

func TestAbortSignalEventsUseCanonicalErrorAndListenerOwner(t *testing.T) {
	c := newCtxFromHTML(t, "<html></html>", nil)
	err := c.EvalDiscard(t.Context(), `
		globalThis.trace=[];
		const controller=new AbortController(), signal=controller.signal;
		signal.onabort=event=>{trace.push('property:'+String(event.target===signal)); throw Error('actual-abort-listener')};
		signal.addEventListener('abort',()=>trace.push('once'),{once:true});
		const removed=()=>trace.push('removed'); signal.addEventListener('abort',removed); signal.removeEventListener('abort',removed);
		signal.addEventListener('abort',{handleEvent(){trace.push('object')}});
		controller.abort(); controller.abort();
		signal.onabort=null;
		signal.dispatchEvent(new Event('abort'));
	`)
	var listenerError *EventListenerError
	if !errors.As(err, &listenerError) || !strings.Contains(err.Error(), "actual-abort-listener") {
		t.Fatalf("abort callback exception was discarded: %v", err)
	}
	got, err := c.EvalString(t.Context(), "trace.join('|')")
	if err != nil || got != "property:true|once|object|object" {
		t.Fatalf("abort listener identity/options or later delivery failed: %q %v", got, err)
	}
}

func TestAbortSignalTimeoutIsDelayedAndValidated(t *testing.T) {
	c := newCtxFromHTML(t, "<html></html>", nil)
	got, err := c.EvalString(t.Context(), `(() => {
		globalThis.timedSignal=AbortSignal.timeout(30);
		let rejected=0;
		for(const value of [-1,Infinity,NaN,2**64,1n,Symbol('invalid')])try{AbortSignal.timeout(value)}catch(error){if(error instanceof TypeError)rejected++}
		return timedSignal.aborted+':'+rejected;
	})()`)
	if err != nil || got != "false:6" {
		t.Fatalf("timeout aborted early or accepted invalid milliseconds: %q %v", got, err)
	}
	time.Sleep(40 * time.Millisecond)
	if err := c.EvalDiscard(t.Context(), "0"); err != nil {
		t.Fatal(err)
	}
	got, err = c.EvalString(t.Context(), "timedSignal.aborted+':'+timedSignal.reason.name")
	if err != nil || got != "true:TimeoutError" {
		t.Fatalf("due timeout did not abort with typed reason: %q %v", got, err)
	}
}

func TestFetchPreAbortPreservesExactReasonsAndRetiresWatchers(t *testing.T) {
	for _, async := range []bool{false, true} {
		t.Run(fmt.Sprint(async), func(t *testing.T) {
			calls := 0
			c := newCtxFromHTMLOpts(t, "<html></html>", ContextOpts{AsyncFetch: async, Fetch: func(context.Context, FetchRequest) (*FetchResponse, error) {
				calls++
				return &FetchResponse{Status: 200}, nil
			}})
			before := c.rt.iso.RetainedValueCount()
			if err := c.EvalDiscard(t.Context(), `globalThis.observed=[]; for(const reason of [null,false,0,'',{},new Error('actual')]) fetch('/blocked',{signal:AbortSignal.abort(reason)}).then(()=>observed.push('invalid success'),error=>observed.push(error===reason));`); err != nil {
				t.Fatal(err)
			}
			got, err := c.EvalString(t.Context(), "observed.join(':')")
			if err != nil || got != "true:true:true:true:true:true" || calls != 0 || len(c.async.signals) != 0 || c.async.inflight.Load() != 0 {
				t.Fatalf("preabort lost reason/retirement or reached transport: %q %v calls=%d signals=%d", got, err, calls, len(c.async.signals))
			}
			if after := c.rt.iso.RetainedValueCount(); after != before {
				t.Fatalf("signal/abort callback leaked internal native handles: before=%d after=%d", before, after)
			}
		})
	}
}

func TestAsyncFetchAbortCancelsRealWorkerBeforeListeners(t *testing.T) {
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
	if err := c.EvalDiscard(t.Context(), `
		globalThis.controller=new AbortController(), reason={actual:'abort'}, observed='';
		controller.signal.addEventListener('abort',event=>{event.stopImmediatePropagation();throw Error('abort-listener-failure')});
		fetch('/running',{signal:controller.signal}).then(()=>{observed='invalid success'},error=>{observed=String(error===reason)});
	`); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("actual transport never entered")
	}
	err := c.EvalDiscard(t.Context(), "controller.abort(reason)")
	var listenerError *EventListenerError
	if !errors.As(err, &listenerError) {
		t.Fatalf("abort listener failure was hidden: %v", err)
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("event failure/stopImmediatePropagation prevented native transport abort")
	}
	release.Do(func() { close(allowReturn) })
	if err := c.WaitIdle(t.Context()); err != nil {
		t.Fatal(err)
	}
	got, err := c.EvalString(t.Context(), "observed")
	if err != nil || got != "true" || len(c.async.signals) != 0 || c.async.inflight.Load() != 0 {
		t.Fatalf("abort reason or actual retirement lost: %q %v signals=%d", got, err, len(c.async.signals))
	}
}

func TestAsyncFetchTimeoutCancelsWorkerWithTypedReason(t *testing.T) {
	entered := make(chan struct{})
	c := newCtxFromHTMLOpts(t, "<html></html>", ContextOpts{AsyncFetch: true, Fetch: func(ctx context.Context, _ FetchRequest) (*FetchResponse, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}})
	if err := c.EvalDiscard(t.Context(), `globalThis.observed=''; fetch('/timeout',{signal:AbortSignal.timeout(30)}).catch(error=>{observed=String(error instanceof DOMException)+':'+error.name});`); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("actual transport never entered")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := c.WaitIdle(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := c.EvalString(t.Context(), "observed")
	if err != nil || got != "true:TimeoutError" || len(c.async.signals) != 0 {
		t.Fatalf("timeout lost typed reason or request retirement: %q %v", got, err)
	}
}

func TestAsyncFetchCloseRetiresSignalRegistryAndNativeHandles(t *testing.T) {
	entered := make(chan struct{})
	c := newCtxFromHTMLOpts(t, "<html></html>", ContextOpts{AsyncFetch: true, Fetch: func(ctx context.Context, _ FetchRequest) (*FetchResponse, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}})
	if err := c.EvalDiscard(t.Context(), `fetch('/close',{signal:new AbortController().signal}).catch(()=>{});`); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("actual worker never entered")
	}
	if len(c.async.signals) != 1 {
		t.Fatalf("actual signal owner not retained: %d", len(c.async.signals))
	}
	var watch *fetchSignalWatch
	for _, retained := range c.async.signals {
		watch = retained
	}
	c.Close()
	if len(c.async.signals) != 0 || c.async.inflight.Load() != 0 || c.fetchSignalFactory != nil || watch.value != nil || watch.algorithms != nil || watch.key != nil {
		t.Fatal("closed Context retained a signal registry, worker or native handle")
	}
}

func TestAbortSignalRejectsForgedReceiversAndSyntheticAbortDoesNotCancel(t *testing.T) {
	c := newCtxFromHTML(t, "<html></html>", nil)
	got, err := c.EvalString(t.Context(), `(() => {
		const signal=new AbortController().signal;
		signal.dispatchEvent(new Event('abort'));
		let rejected=0;
		for(const action of [()=>new AbortSignal(),()=>AbortSignal.prototype.throwIfAborted.call({aborted:true,reason:'forged'}),()=>AbortController.prototype.abort.call({signal}),()=>Object.getOwnPropertyDescriptor(AbortSignal.prototype,'aborted').get.call({})])try{action()}catch(error){if(error instanceof TypeError)rejected++}
		return signal.aborted+':'+rejected;
	})()`)
	if err != nil || got != "false:4" {
		t.Fatalf("synthetic event or forged receiver changed actual abort authority: %q %v", got, err)
	}
}
