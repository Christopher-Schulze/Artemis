package js

import (
	"context"
	"errors"
	"math"

	v8 "github.com/tommie/v8go"
)

// Signal watches belong to the existing Context Fetch owner. Every native field
// and registry transition is accessed under that Runtime's isolate gate.
type fetchSignalWatch struct {
	owner      *Context
	id         uint32
	value      *v8.Object
	algorithms *v8.Value
	key        *v8.Value
	cancel     context.CancelFunc
	references int
	bodyIDs    []uint32
}

func fetchSignalArgument(args []*v8.Value) (*v8.Value, error) {
	if len(args) < 2 || args[1].IsNullOrUndefined() {
		return nil, nil
	}
	options, err := args[1].AsObject()
	if err != nil {
		return nil, err
	}
	signal, err := options.Get("signal")
	if err != nil {
		return nil, err
	}
	if signal.IsNullOrUndefined() {
		signal.Release()
		return nil, nil
	}
	return signal, nil
}

func (c *Context) watchFetchSignal(signal *v8.Value, cancel context.CancelFunc) (*fetchSignalWatch, error) {
	if signal == nil {
		return nil, nil
	}
	a := c.async
	if a.signalSeq >= math.MaxInt32 || len(a.signals) >= maxFetchBodyHandles {
		return nil, errors.New("fetch: signal ownership limit exceeded")
	}
	a.signalSeq++
	id, err := v8.NewValue(c.rt.iso, int32(a.signalSeq))
	if err != nil {
		return nil, err
	}
	global := c.v8ctx.Global()
	defer global.Release()
	value, err := c.fetchSignalFactory.Call(global, signal, id)
	if err != nil {
		id.Release()
		return nil, err
	}
	object, err := value.AsObject()
	if err != nil {
		id.Release()
		value.Release()
		return nil, err
	}
	algorithms, err := object.Get("algorithms")
	if err != nil {
		id.Release()
		value.Release()
		return nil, err
	}
	if !algorithms.IsMap() {
		algorithms.Release()
		id.Release()
		value.Release()
		return nil, errors.New("fetch: invalid signal algorithm owner")
	}
	watch := &fetchSignalWatch{owner: c, id: a.signalSeq, value: object, algorithms: algorithms, key: id, cancel: cancel, references: 1}
	if a.signals == nil {
		a.signals = make(map[uint32]*fetchSignalWatch)
	}
	a.signals[watch.id] = watch
	return watch, nil
}

func (signal *fetchSignalWatch) isAborted() (bool, error) {
	if signal == nil {
		return false, nil
	}
	aborted, err := signal.value.Get("aborted")
	if err != nil {
		return false, err
	}
	defer aborted.Release()
	return aborted.Boolean(), nil
}

func (signal *fetchSignalWatch) rejectAborted(resolver *v8.PromiseResolver) (bool, error) {
	aborted, err := signal.isAborted()
	if err != nil || !aborted {
		return false, err
	}
	reason, err := signal.value.Get("reason")
	if err != nil {
		return true, err
	}
	defer reason.Release()
	if !resolver.Reject(reason) {
		return true, errors.New("fetch: native abort rejection failed")
	}
	return true, nil
}

func (signal *fetchSignalWatch) release() {
	signal.algorithms.Release()
	signal.key.Release()
	signal.value.Release()
	signal.algorithms = nil
	signal.key = nil
	signal.value = nil
	signal.bodyIDs = nil
}

func (signal *fetchSignalWatch) close() error {
	defer signal.release()
	return signal.algorithms.DeleteMapKey(signal.key)
}

func (signal *fetchSignalWatch) retire(closing bool) {
	if signal == nil {
		return
	}
	signal.references--
	if signal.references != 0 {
		return
	}
	delete(signal.owner.async.signals, signal.id)
	if closing {
		signal.release()
		return
	}
	span := signal.owner.rt.execution
	if span == nil {
		if err := signal.close(); err != nil {
			thrown := signal.owner.rt.iso.ThrowException(v8.Null(signal.owner.rt.iso))
			thrown.Release()
		}
		return
	}
	if len(span.retiredSignals) >= maxCallbackReturnValues {
		signal.release()
		span.recordFailure(errors.New("fetch: signal retirement budget exceeded"))
		span.interrupted = true
		span.iso.TerminateExecution()
		return
	}
	span.retiredSignals = append(span.retiredSignals, signal)
}

func (r *Runtime) abortFetch(info *v8.FunctionCallbackInfo) *v8.Value {
	defer info.Release()
	c := r.contextFor(info.Context())
	args := info.Args()
	if c == nil || len(args) != 1 || !args[0].IsInt32() || args[0].Int32() <= 0 {
		return r.responseError(info, errors.New("fetch: invalid abort owner"))
	}
	if signal := c.async.signals[uint32(args[0].Int32())]; signal != nil {
		aborted, err := signal.isAborted()
		if err != nil {
			return r.responseError(info, err)
		}
		if !aborted {
			return r.responseError(info, errors.New("fetch: signal has not aborted"))
		}
		signal.cancel()
		for _, id := range signal.bodyIDs {
			if body := r.fetchBodies.get(id); body != nil && body.signal == signal {
				body.releasePayload()
			}
		}
		// Aborted bodies retain their reason for repeat reads/clones, but no
		// transport cancellation route remains active after this abort step.
		delete(c.async.signals, signal.id)
	}
	return v8.Undefined(r.iso)
}
