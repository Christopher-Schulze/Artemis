package js

import (
	"context"
	"errors"
	"sync"

	v8 "github.com/tommie/v8go"
)

// isolateGate is the single existing isolate owner, with cancellable admission.
// Its zero value supports the inherited Lock/Unlock/TryLock callers.
type isolateGate struct {
	once  sync.Once
	owned chan struct{}
}

func (g *isolateGate) initialize() { g.once.Do(func() { g.owned = make(chan struct{}, 1) }) }

func (g *isolateGate) Lock() {
	g.initialize()
	g.owned <- struct{}{}
}

func (g *isolateGate) Unlock() { <-g.owned }

func (g *isolateGate) TryLock() bool {
	g.initialize()
	select {
	case g.owned <- struct{}{}:
		return true
	default:
		return false
	}
}

func (g *isolateGate) lockContext(ctx, lifetime context.Context) error {
	if err := errors.Join(ctx.Err(), lifetime.Err()); err != nil {
		return err
	}
	g.initialize()
	select {
	case g.owned <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	case <-lifetime.Done():
		return lifetime.Err()
	}
	if err := errors.Join(ctx.Err(), lifetime.Err()); err != nil {
		g.Unlock()
		return err
	}
	return nil
}

type executionInterruption struct {
	stop func() bool
	done chan struct{}
}

// borrowContext joins lifetime cancellation without retaining a watcher after
// the borrowing operation returns. It never transfers lifetime ownership.
func borrowContext(ctx, lifetime context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		return context.WithCancel(lifetime)
	}
	borrowed, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	stop := context.AfterFunc(lifetime, func() {
		cancel()
		close(done)
	})
	var once sync.Once
	return borrowed, func() {
		once.Do(func() {
			if !stop() {
				<-done
			}
			cancel()
		})
	}
}

type executionSpan struct {
	runtime         *Runtime
	iso             *v8.Isolate
	owners          [2]context.Context
	interruptions   [2]executionInterruption
	interrupted     bool
	nestedError     error
	fetchBorrows    []executionFetchBorrow
	retiredSignals  []*fetchSignalWatch
	promiseContexts map[*Context]struct{}
}

type executionFetchBorrow struct {
	owner  context.Context
	fetch  *pendingFetch
	stop   func() bool
	joined chan struct{}
}

// Borrow caller cancellation only until execution returns. A successful return
// transfers the unfinished request to its existing Context lifetime; a cancelled
// operation joins actual transport cleanup before acknowledging cancellation.
func (s *executionSpan) borrowFetch(owner context.Context, fetch *pendingFetch) error {
	if owner == nil || owner.Done() == nil {
		return nil
	}
	if len(s.fetchBorrows) >= maxCallbackReturnValues {
		return errors.New("fetch: operation ownership limit exceeded")
	}
	joined := make(chan struct{})
	stop := context.AfterFunc(owner, func() { fetch.cancel(); close(joined) })
	s.fetchBorrows = append(s.fetchBorrows, executionFetchBorrow{owner: owner, fetch: fetch, stop: stop, joined: joined})
	return nil
}

func (s *executionSpan) finishFetchBorrows() {
	for _, borrow := range s.fetchBorrows {
		if !borrow.stop() {
			<-borrow.joined
		}
		if err := borrow.owner.Err(); err != nil {
			borrow.fetch.cancel()
			<-borrow.fetch.finished
			s.recordFailure(err)
		}
	}
}

func beginExecution(runtime *Runtime, ctx, lifetime context.Context) *executionSpan {
	span := &executionSpan{runtime: runtime, iso: runtime.iso, owners: [2]context.Context{ctx, lifetime}}
	runtime.execution = span
	for index, owner := range span.owners {
		span.interruptions[index] = span.watch(owner)
	}
	return span
}

func (s *executionSpan) watch(owner context.Context) executionInterruption {
	if owner.Done() == nil {
		return executionInterruption{}
	}
	done := make(chan struct{})
	stop := context.AfterFunc(owner, func() {
		s.iso.TerminateExecution()
		close(done)
	})
	return executionInterruption{stop: stop, done: done}
}

func (s *executionSpan) join(interruption executionInterruption) {
	if interruption.stop != nil && !interruption.stop() {
		<-interruption.done
		s.interrupted = true
	}
}

// Retain the first failure without allowing page callbacks to grow Go memory.
func (s *executionSpan) recordFailure(err error) {
	if s.nestedError == nil {
		s.nestedError = err
	}
}

// finish runs after native frames unwind, before releasing isolate ownership.
// A callback already started must join before clearing termination for reuse.
func (s *executionSpan) finish() error {
	for _, interruption := range s.interruptions {
		s.join(interruption)
	}
	s.finishFetchBorrows()
	if s.interrupted {
		s.iso.CancelTerminateExecution()
	}
	promiseError := s.takePromiseRejections()
	s.runtime.execution = nil
	var cleanupError error
	for _, signal := range s.retiredSignals {
		cleanupError = errors.Join(cleanupError, signal.close())
	}
	return errors.Join(s.owners[0].Err(), s.owners[1].Err(), s.nestedError, promiseError, cleanupError)
}

// executeNested borrows the active isolate span. Only its outermost owner may
// clear native termination, after every nested JavaScript frame has unwound.
func (c *Context) executeNested(ctx context.Context, operation func() error) (returnErr error) {
	span := c.rt.execution
	if span == nil || ctx == nil {
		return errors.New("nested JavaScript execution: active owner required")
	}
	if err := errors.Join(ctx.Err(), c.async.ctx.Err()); err != nil {
		span.recordFailure(err)
		return err
	}
	interruption := span.watch(c.async.ctx)
	c.pageExposed.Store(true)
	previous := c.evalCtx
	c.evalCtx = ctx
	defer func() {
		c.evalCtx = previous
		span.join(interruption)
		c.releaseCallbackValues()
		cancellation := errors.Join(ctx.Err(), c.async.ctx.Err())
		returnErr = errors.Join(returnErr, cancellation)
		span.recordFailure(returnErr)
	}()
	return operation()
}

// execute owns every native result through cancellation acknowledgement.
// Void operations may return no Value after completing their real work.
func (c *Context) execute(ctx context.Context, operation func() (*Value, error)) (value *Value, returnErr error) {
	if c == nil || c.closed.Load() {
		return nil, context.Canceled
	}
	if ctx == nil {
		return nil, errors.New("JavaScript execution: context required")
	}
	if err := c.rt.ctxMu.lockContext(ctx, c.async.ctx); err != nil {
		return nil, err
	}
	defer c.rt.ctxMu.Unlock()
	if c.closed.Load() || c.rt.closed.Load() {
		return nil, context.Canceled
	}
	span := beginExecution(c.rt, ctx, c.async.ctx)
	span.observePromiseContext(c)
	c.pageExposed.Store(true)
	defer func() {
		returnErr = errors.Join(returnErr, span.finish())
		c.releaseCallbackValues()
		if value != nil {
			if returnErr != nil {
				value.Release()
				value = nil
			} else {
				c.ownEvalValueLocked(value)
			}
		}
	}()
	c.evalCtx = ctx
	defer func() { c.evalCtx = nil }()
	return operation()
}
