package js

import (
	"context"
	"sync/atomic"

	v8 "rogchap.com/v8go"
)

// pendingFetch carries an HTTP fetch result from the goroutine that
// performed it back to the V8 thread which must construct the Response
// JS object and resolve/reject the Promise.
type pendingFetch struct {
	resolver *v8.PromiseResolver
	resp     *FetchResponse
	err      error
}

// asyncChan is a buffered channel of pending fetch results. The
// goroutine that issued the request writes; the V8 thread drains.
//
// The cancel context is the global signal for "this *js.Context is
// closing, abandon any in-flight work". Each fetch goroutine selects
// between sending its result and reading ctx.Done(); on ctx.Done it
// decrements inflight and exits without writing into a channel that
// no one will drain.
type asyncChan struct {
	pending  chan *pendingFetch
	inflight atomic.Int64
	ctx      context.Context
	cancel   context.CancelFunc
}

func newAsyncChan() *asyncChan {
	ctx, cancel := context.WithCancel(context.Background())
	return &asyncChan{
		pending: make(chan *pendingFetch, 64),
		ctx:     ctx,
		cancel:  cancel,
	}
}

// cancelInflight signals every fetch goroutine to abandon its in-flight
// work. Goroutines that are inside c.fetcher(ctx, req) see ctx.Done()
// and (if their fetcher is well-behaved) return with an error; those
// blocked on the pending-channel send fall through via the select.
// Idempotent.
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
	for {
		select {
		case p := <-a.pending:
			a.resolveOne(c, p)
			count++
		default:
			return count
		}
	}
}

func (a *asyncChan) resolveOne(c *Context, p *pendingFetch) {
	defer a.inflight.Add(-1)
	if p.err != nil {
		rejectErr(c.rt.iso, p.resolver, p.err)
		return
	}
	respObj, err := buildResponseObject(c, c.v8ctx, p.resp)
	if err != nil {
		rejectErr(c.rt.iso, p.resolver, err)
		return
	}
	_ = p.resolver.Resolve(respObj)
}

// WaitIdle blocks until pending fetches have settled and any pending
// WebSocket events have been dispatched. Each round it drains the
// channels, pumps microtasks (so .then chains observe resolution), and
// fires mutation observers (in case .then / onmessage callbacks mutated
// DOM). Returns ctx.Err() on cancellation.
func (c *Context) WaitIdle(ctx context.Context) error {
	if c.async == nil {
		return nil
	}
	pump := func() {
		_, _ = c.v8ctx.RunScript("0", "<artemis-microtask-pump>")
		c.fireMutationObservers()
		c.fireTimers()
	}
	for {
		drained := c.async.drain(c)
		if c.ws != nil {
			drained += c.ws.drain(c)
		}
		if drained > 0 {
			pump()
		}
		if c.async.inflight.Load() == 0 {
			return nil
		}
		var wsCh <-chan wsEvent
		if c.ws != nil {
			wsCh = c.ws.events
		}
		select {
		case p := <-c.async.pending:
			c.async.resolveOne(c, p)
			pump()
		case ev := <-wsCh:
			if c.ws != nil {
				c.ws.fireEvent(c, ev)
				pump()
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// asyncFetchPath is what installFetch uses when the Context was built
// with async=true. The goroutine performs the HTTP and posts the result
// back to the V8 thread via the channel.
//
// Lifecycle: the goroutine respects c.async.ctx so a Context.Close ->
// cancelInflight signals fetcher to bail. Whatever the goroutine did,
// inflight is decremented exactly once (either via resolveOne on the
// V8 thread or via the cancel branch of this select).
func (c *Context) asyncFetchPath(req FetchRequest, resolver *v8.PromiseResolver) {
	c.async.inflight.Add(1)
	a := c.async
	go func() {
		resp, err := c.fetcher(a.ctx, req)
		p := &pendingFetch{resolver: resolver, resp: resp, err: err}
		select {
		case a.pending <- p:
			// resolveOne on the V8 thread will decrement inflight.
		case <-a.ctx.Done():
			a.inflight.Add(-1)
		}
	}()
}
