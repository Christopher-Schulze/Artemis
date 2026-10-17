package js

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	v8 "github.com/tommie/v8go"
)

// pendingFetch carries an HTTP fetch result from the goroutine that
// performed it back to the V8 thread which must construct the Response
// JS object and resolve/reject the Promise.
type pendingFetch struct {
	resolver *v8.PromiseResolver
	resp     *FetchResponse
	err      error
	ctx      context.Context
	cancel   context.CancelFunc
	finished chan struct{}
	signal   *fetchSignalWatch
}

// asyncChan is a buffered channel of pending fetch results. The
// goroutine that issued the request writes; the V8 thread drains.
//
// The cancel context stops transports on owner close. Queue capacity bounds
// all unresolved fetches; every worker publishes one completion, and only the
// V8 owner resolves or retires its native resolver and decrements inflight.
type asyncChan struct {
	pending   chan *pendingFetch
	inflight  atomic.Int64
	ctx       context.Context
	cancel    context.CancelFunc
	workers   sync.WaitGroup
	signalSeq uint32
	signals   map[uint32]*fetchSignalWatch
}

func newAsyncChan(owner context.Context) *asyncChan {
	ctx, cancel := context.WithCancel(owner)
	return &asyncChan{
		pending: make(chan *pendingFetch, 64),
		ctx:     ctx,
		cancel:  cancel,
	}
}

// cancelInflight signals every fetch goroutine to abandon its in-flight
// work. Goroutines that are inside c.fetcher(ctx, req) see ctx.Done()
// and return after transport cleanup. closeLocked joins them before native
// ownership is released. Repeated cancellation is safe.
func (a *asyncChan) cancelInflight() {
	if a == nil {
		return
	}
	a.cancel()
}

// drain resolves every pending fetch currently waiting on the channel.
// Runs on the V8 thread. Returns the number of items drained.
func (a *asyncChan) drain(c *Context) int {
	count := 0
	for count < cap(a.pending) {
		select {
		case p := <-a.pending:
			a.resolveOne(c, p)
			count++
		default:
			return count
		}
	}
	return count
}

func (a *asyncChan) resolveOne(c *Context, p *pendingFetch) {
	defer a.inflight.Add(-1)
	defer retireFetchResolver(p, false)
	if a.ctx.Err() != nil {
		return
	}
	if aborted, err := p.signal.rejectAborted(p.resolver); aborted || err != nil {
		c.rt.execution.recordFailure(err)
		return
	}
	if err := errors.Join(p.err, p.ctx.Err()); err != nil {
		rejectErr(c.rt.iso, p.resolver, err)
		return
	}
	respObj, bodyID, err := buildResponseObject(c, c.v8ctx, p.resp, p.signal)
	if err != nil {
		rejectErr(c.rt.iso, p.resolver, err)
		return
	}
	defer respObj.Release()
	if aborted, err := p.signal.rejectAborted(p.resolver); aborted || err != nil {
		c.rt.fetchBodies.remove(bodyID)
		c.rt.execution.recordFailure(err)
		return
	}
	if !p.resolver.Resolve(respObj) {
		c.rt.fetchBodies.remove(bodyID)
		c.rt.execution.recordFailure(errors.New("fetch: native promise resolution failed"))
	}
}

func retireFetchResolver(p *pendingFetch, closing bool) {
	p.cancel()
	p.signal.retire(closing)
	p.resolver.Release()
	p.resp = nil
	p.err = nil
}

// closeLocked joins transport workers after cancellation and isolate admission
// has stopped, then retires their native resolvers on the isolate owner thread.
func (a *asyncChan) closeLocked() {
	a.workers.Wait()
	for {
		select {
		case pending := <-a.pending:
			retireFetchResolver(pending, true)
			a.inflight.Add(-1)
		default:
			return
		}
	}
}

// WaitIdle blocks until pending fetches have settled and any pending
// WebSocket events have been dispatched. Each round it drains the
// channels, pumps microtasks (so .then chains observe resolution), and
// fires mutation observers (in case .then / onmessage callbacks mutated
// DOM). Returns ctx.Err() on cancellation.
func (c *Context) WaitIdle(ctx context.Context) error {
	_, err := c.execute(ctx, func() (*Value, error) { return nil, c.waitIdleLocked(ctx) })
	return err
}

func (c *Context) waitIdleLocked(ctx context.Context) error {
	for {
		c.async.drain(c)
		if c.ws != nil {
			c.ws.drain(c)
		}
		if err := c.pumpAsyncTasks(); err != nil {
			return err
		}
		if c.async.inflight.Load() == 0 {
			return nil
		}
		if err := c.waitForAsyncTask(ctx); err != nil {
			return err
		}
	}
}

func (c *Context) pumpAsyncTasks() error {
	value, err := c.v8ctx.RunScript("0", "<artemis-microtask-pump>")
	if value != nil {
		value.Release()
	}
	if err != nil {
		return errors.Join(formatJSError(err), c.styleMgr.takeFailure())
	}
	if err := c.fireMutationObservers(); err != nil {
		return errors.Join(err, c.takeDocumentEventErrors(), c.styleMgr.takeFailure())
	}
	return errors.Join(c.fireTimers(), c.takeDocumentEventErrors(), c.styleMgr.takeFailure())
}

func (c *Context) waitForAsyncTask(ctx context.Context) error {
	var wsCh <-chan wsEvent
	if c.ws != nil {
		wsCh = c.ws.events
	}
	var wake <-chan time.Time
	if delay, pending := c.timers.nextDelay(); pending {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		wake = timer.C
	}
	select {
	case result := <-c.async.pending:
		c.async.resolveOne(c, result)
	case event := <-wsCh:
		if c.ws != nil {
			c.ws.fireEvent(c, event)
		}
	case <-wake:
	case <-c.async.ctx.Done():
		return context.Canceled
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

// asyncFetchPath is what installFetch uses when the Context was built
// with async=true. The goroutine performs the HTTP and posts the result
// back to the V8 thread via the channel.
//
// Admission counts unresolved results as well as active transports. At most
// one result per queue slot exists, so workers can always publish completion
// without waiting for V8. Close joins all workers before retiring native state.
func (c *Context) asyncFetchPath(req FetchRequest, resolver *v8.PromiseResolver, signal *v8.Value) bool {
	a := c.async
	if err := a.ctx.Err(); err != nil {
		rejectErr(c.rt.iso, resolver, err)
		return false
	}
	if a.inflight.Load() >= int64(cap(a.pending)) {
		rejectErr(c.rt.iso, resolver, errors.New("fetch: outstanding request limit exceeded"))
		return false
	}
	workerContext, cancel := context.WithCancel(a.ctx)
	p := &pendingFetch{resolver: resolver, ctx: workerContext, cancel: cancel, finished: make(chan struct{})}
	var err error
	p.signal, err = c.watchFetchSignal(signal, cancel)
	if err != nil {
		cancel()
		rejectErr(c.rt.iso, resolver, err)
		return false
	}
	if aborted, err := p.signal.rejectAborted(resolver); aborted || err != nil {
		cancel()
		p.signal.retire(false)
		if err != nil {
			c.rt.execution.recordFailure(err)
		}
		return false
	}
	if span := c.rt.execution; span != nil {
		if err := span.borrowFetch(c.evalCtx, p); err != nil {
			cancel()
			p.signal.retire(false)
			rejectErr(c.rt.iso, resolver, err)
			return false
		}
	}
	a.inflight.Add(1)
	a.workers.Add(1)
	go func() {
		defer close(p.finished)
		defer a.workers.Done()
		p.resp, p.err = c.fetcher(workerContext, req)
		a.pending <- p
	}()
	return true
}
