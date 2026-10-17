package js

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
	"unsafe"

	v8 "github.com/tommie/v8go"

	"github.com/Christopher-Schulze/Artemis/webapi"
)

const (
	maxMutationCallbacksPerPump        = 4096
	maxMutationFailuresPerPump         = 16
	maxMutationRegistrationsPerContext = 4096
	maxMutationPendingRecords          = 4096
	maxMutationPendingBytes            = 4 << 20
)

// MutationRecordType is the kind of DOM mutation a record describes.
type MutationRecordType string

const (
	MutationChildList     MutationRecordType = "childList"
	MutationAttributes    MutationRecordType = "attributes"
	MutationCharacterData MutationRecordType = "characterData"
)

// mutationRecord is the Go-side captured event before it crosses to JS.
type mutationRecord struct {
	Type              MutationRecordType
	TargetID          uint32
	AddedIDs          []uint32
	RemovedIDs        []uint32
	AttributeName     string
	OldValue          *string
	PreviousSiblingID uint32
	NextSiblingID     uint32
}

// observer owns one callback, per-target options and a chronological queue.
type observer struct {
	id           uint32
	order        uint64
	targets      map[uint32]observerOptions
	callback     *v8.Function
	pending      []mutationRecord
	pendingBytes int
	transients   map[transientKey]*transientRegistration
}

// observerRegistry holds all observers for a Context.
type observerRegistry struct {
	mu               sync.Mutex
	nextID           uint32
	by               map[uint32]*observer
	byTarget         map[uint32][]*observer // direct-target index
	pendingRecords   int
	pendingBytes     int
	registrations    int
	optionsBytes     int
	transientTargets map[uint32][]*transientRegistration
}

func newObserverRegistry() *observerRegistry {
	return &observerRegistry{
		by:               make(map[uint32]*observer),
		byTarget:         make(map[uint32][]*observer),
		transientTargets: make(map[uint32][]*transientRegistration),
	}
}

func (r *observerRegistry) register(targetID, existingID uint32, order uint64, options observerOptions, callback *v8.Function) (*observer, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	o := r.by[existingID]
	if existingID != 0 && o == nil {
		return nil, false, errors.New("mutation observer: owner no longer registered")
	}
	created := o == nil
	previous, exists := observerOptions{}, false
	if !created {
		previous, exists = o.targets[targetID]
	}
	if !exists && r.registrations >= maxMutationRegistrationsPerContext {
		return nil, false, errors.New("mutation observer registration limit exceeded")
	}
	bytes := r.optionsBytes + options.retainedBytes()
	if exists {
		bytes -= previous.retainedBytes()
		bytes -= r.transientBytes(o, targetID)
	}
	if bytes > maxMutationPendingBytes {
		return nil, false, errors.New("mutation observer options byte limit exceeded")
	}
	if created {
		if r.nextID >= math.MaxInt32 {
			return nil, false, errors.New("mutation observer identity exhausted")
		}
		r.nextID++
		if order == 0 {
			order = uint64(r.nextID)
		}
		o = &observer{id: r.nextID, order: order, callback: callback, targets: make(map[uint32]observerOptions)}
		r.by[o.id] = o
	}
	if !exists {
		r.byTarget[targetID] = append(r.byTarget[targetID], o)
		r.registrations++
	}
	if exists {
		r.removeTransients(o, targetID)
	}
	o.targets[targetID] = options
	r.optionsBytes = bytes
	return o, created, nil
}

func (r *observerRegistry) disconnect(id uint32) {
	r.mu.Lock()
	defer r.mu.Unlock()
	o, ok := r.by[id]
	if !ok {
		return
	}
	delete(r.by, id)
	for targetID, options := range o.targets {
		out := r.byTarget[targetID][:0]
		for _, x := range r.byTarget[targetID] {
			if x.id != id {
				out = append(out, x)
			}
		}
		clear(r.byTarget[targetID][len(out):])
		if len(out) == 0 {
			delete(r.byTarget, targetID)
		} else {
			r.byTarget[targetID] = out
		}
		r.registrations--
		r.optionsBytes -= options.retainedBytes()
	}
	o.targets = nil
	r.removeTransients(o, 0)
	if o.callback != nil {
		o.callback.Release()
		o.callback = nil
	}
	r.releasePending(o)
}

func (r *observerRegistry) close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, observer := range r.by {
		if observer.callback != nil {
			observer.callback.Release()
			observer.callback = nil
		}
		r.releasePending(observer)
		observer.targets = nil
		observer.transients = nil
	}
	clear(r.by)
	clear(r.byTarget)
	clear(r.transientTargets)
	r.registrations, r.optionsBytes = 0, 0
}

// recordMutation pushes a record into every observer that watches
// targetID, including ancestor observers when subtree=true.
func (r *observerRegistry) recordMutation(c *Context, rec mutationRecord, targetNode *webapi.Node) error {
	if rec.Type == MutationChildList && len(rec.AddedIDs) == 0 && len(rec.RemovedIDs) == 0 {
		return nil
	}
	return r.recordMutations(c, []mutationEvent{{record: rec, target: targetNode}})
}

func (r *observerRegistry) recordMutations(c *Context, events []mutationEvent) (failure error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	defer func() {
		if failure != nil && c.rt.execution != nil {
			c.rt.execution.recordFailure(failure)
		}
	}()
	if err := c.mutationContextError(); err != nil {
		return err
	}
	if len(r.by) == 0 {
		return nil
	}
	planned, err := r.planTransients(c, events)
	if err != nil {
		return err
	}
	var deliveries []mutationDelivery
	for _, event := range events {
		current, err := r.mutationDeliveries(c, event.record, event.target)
		if err != nil {
			return err
		}
		deliveries = append(deliveries, current...)
	}
	if err := r.appendPending(deliveries); err != nil {
		return err
	}
	r.commitTransients(planned)
	return nil
}

func (r *observerRegistry) mutationDeliveries(c *Context, rec mutationRecord, targetNode *webapi.Node) ([]mutationDelivery, error) {
	interested := make(map[*observer]bool)
	r.collectMutationInterests(interested, rec.TargetID, rec, true)
	// Ancestors: walk up; observers with subtree=true on any ancestor
	// also receive the record.
	if targetNode != nil {
		for p := targetNode.Parent(); p != nil; p = p.Parent() {
			if err := c.mutationContextError(); err != nil {
				return nil, err
			}
			pid := c.nodes.Handle(p)
			if pid == 0 {
				break
			}
			r.collectMutationInterests(interested, pid, rec, false)
		}
	}
	deliveries := make([]mutationDelivery, 0, len(interested))
	for owner, oldValue := range interested {
		record := rec
		if !oldValue {
			record.OldValue = nil
		}
		deliveries = append(deliveries, mutationDelivery{owner: owner, record: record})
	}
	return deliveries, nil
}

func (r *observerRegistry) collectMutationInterests(interested map[*observer]bool, targetID uint32, record mutationRecord, direct bool) {
	for _, owner := range r.byTarget[targetID] {
		options := owner.targets[targetID]
		if (direct || options.subtree) && options.matches(record) {
			interested[owner] = interested[owner] || options.retainsOldValue(record.Type)
		}
	}
	for _, registration := range r.transientTargets[targetID] {
		options, owner := registration.options, registration.origin.owner
		if (direct || options.subtree) && options.matches(record) {
			interested[owner] = interested[owner] || options.retainsOldValue(record.Type)
		}
	}
}

// appendPending and releasePending require r.mu. Admission is atomic across
// every matching registration; failed fanout never silently loses one branch.
type mutationDelivery struct {
	owner  *observer
	record mutationRecord
	bytes  int
}

func (r *observerRegistry) appendPending(deliveries []mutationDelivery) error {
	if len(deliveries) == 0 {
		return nil
	}
	if len(deliveries) > maxMutationPendingRecords-r.pendingRecords {
		return errors.New("mutation observer pending record limit exceeded")
	}
	bytes := 0
	for index := range deliveries {
		size, err := mutationRetainedBytes(deliveries[index].record)
		if err != nil {
			return err
		}
		if size > maxMutationPendingBytes-r.pendingBytes-bytes {
			return errors.New("mutation observer pending byte limit exceeded")
		}
		deliveries[index].bytes = size
		bytes += size
	}
	for _, delivery := range deliveries {
		delivery.owner.pending = append(delivery.owner.pending, delivery.record)
		delivery.owner.pendingBytes += delivery.bytes
	}
	r.pendingRecords += len(deliveries)
	r.pendingBytes += bytes
	return nil
}

func mutationRetainedBytes(record mutationRecord) (int, error) {
	size := int(unsafe.Sizeof(record))
	oldBytes := 0
	if record.OldValue != nil {
		oldBytes = len(*record.OldValue)
	}
	for _, length := range []int{len(record.Type), len(record.AttributeName), oldBytes} {
		if length > maxMutationPendingBytes-size {
			return 0, errors.New("mutation observer pending byte limit exceeded")
		}
		size += length
	}
	for _, ids := range [][]uint32{record.AddedIDs, record.RemovedIDs} {
		if cap(ids) > (maxMutationPendingBytes-size)/4 {
			return 0, errors.New("mutation observer pending byte limit exceeded")
		}
		size += cap(ids) * 4
	}
	return size, nil
}

func (r *observerRegistry) releasePending(owner *observer) {
	r.pendingRecords -= len(owner.pending)
	r.pendingBytes -= owner.pendingBytes
	owner.pending = nil
	owner.pendingBytes = 0
}

// take returns pending observers in construction order. The isolate owner
// consumes records immediately before each callback, respecting disconnects.
func (r *observerRegistry) take() []*observer {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*observer
	for _, o := range r.by {
		if len(o.pending) > 0 || len(o.transients) > 0 {
			out = append(out, o)
		}
	}
	sort.Slice(out, func(left, right int) bool {
		if out[left].order != out[right].order {
			return out[left].order < out[right].order
		}
		return out[left].id < out[right].id
	})
	return out
}

// installMutationObserver registers `__observer_register`,
// `__observer_disconnect`, `__observer_take`, plus the JS-side
// MutationObserver class that bridges to them.
// observerTemplates caches the 3 mutation-observer trampolines.
type observerTemplates struct {
	register   *v8.FunctionTemplate
	disconnect *v8.FunctionTemplate
	take       *v8.FunctionTemplate
}

func (r *Runtime) ensureObserverTemplates() *observerTemplates {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.observerTemplates != nil {
		return r.observerTemplates
	}
	iso := r.iso
	r.observerTemplates = &observerTemplates{
		register: v8.NewFunctionTemplate(iso, r.registerMutationObserver),
		disconnect: v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			defer info.Release()
			c := r.contextFor(info.Context())
			args := info.Args()
			if c == nil || len(args) < 1 {
				return v8.Null(iso)
			}
			id, ok := checkedInt64ToUint32(args[0].Integer())
			if !ok {
				return v8.Null(iso)
			}
			c.observers.disconnect(id)
			return v8.Null(iso)
		}),
		take: v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			defer info.Release()
			c := r.contextFor(info.Context())
			args := info.Args()
			var recs []mutationRecord
			var o *observer
			if c != nil && len(args) > 0 {
				if id, ok := checkedInt64ToUint32(args[0].Integer()); ok {
					c.observers.mu.Lock()
					o = c.observers.by[id]
					if o != nil {
						recs = o.pending
					}
					c.observers.mu.Unlock()
				}
			}
			array, err := mutationsToJSArray(info.Context(), recs)
			if err != nil {
				return r.responseError(info, err)
			}
			if o != nil {
				c.observers.mu.Lock()
				c.observers.releasePending(o)
				c.observers.mu.Unlock()
			}
			return array.ReturnFromCallback()
		}),
	}
	return r.observerTemplates
}

func installMutationObserver(iso *v8.Isolate, v8ctx *v8.Context, c *Context) error {
	t := c.rt.ensureObserverTemplates()
	g := v8ctx.Global()
	defer g.Release()
	for _, binding := range []struct {
		name     string
		template *v8.FunctionTemplate
	}{{"__observer_register", t.register}, {"__observer_disconnect", t.disconnect}, {"__observer_take", t.take}} {
		function := binding.template.GetFunction(v8ctx)
		err := g.Set(binding.name, function)
		function.Release()
		if err != nil {
			return err
		}
	}
	c.registerBootstrap("artemis-mutation-observer", mutationObserverBootstrap)
	return nil
}

func mutationsToJSArray(v8ctx *v8.Context, recs []mutationRecord) (*v8.Value, error) {
	if len(recs) > math.MaxInt32 {
		return nil, errors.New("mutation observer record count exceeds native bound")
	}
	arr, err := v8ctx.NewNullPrototypeObject()
	if err != nil {
		return nil, err
	}
	complete := false
	defer func() {
		if !complete {
			arr.Release()
		}
	}()
	for index, record := range recs {
		obj, err := mutationToJSObject(v8ctx, record)
		if err != nil {
			return nil, err
		}
		err = arr.SetIdx(uint32(index), obj)
		obj.Release()
		if err != nil {
			return nil, err
		}
	}
	if err := arr.Set("length", int32(len(recs))); err != nil {
		return nil, err
	}
	complete = true
	return arr.Value, nil
}

func mutationToJSObject(v8ctx *v8.Context, record mutationRecord) (*v8.Object, error) {
	targetID, ok := checkedUint32ToInt32(record.TargetID)
	if !ok {
		return nil, errors.New("mutation observer target exceeds native bound")
	}
	previousID, previousOK := checkedUint32ToInt32(record.PreviousSiblingID)
	nextID, nextOK := checkedUint32ToInt32(record.NextSiblingID)
	if !previousOK || !nextOK {
		return nil, errors.New("mutation observer sibling exceeds native bound")
	}
	added, err := mutationIDsToJSObject(v8ctx, record.AddedIDs)
	if err != nil {
		return nil, err
	}
	defer added.Release()
	removed, err := mutationIDsToJSObject(v8ctx, record.RemovedIDs)
	if err != nil {
		return nil, err
	}
	defer removed.Release()
	obj, err := v8ctx.NewNullPrototypeObject()
	if err != nil {
		return nil, err
	}
	var attributeName, oldValue any = v8.Null(v8ctx.Isolate()), v8.Null(v8ctx.Isolate())
	if record.Type == MutationAttributes {
		attributeName = record.AttributeName
	}
	if record.OldValue != nil {
		oldValue = *record.OldValue
	}
	err = obj.SetMany([]string{"type", "targetId", "attributeName", "attributeNamespace", "oldValue", "addedIds", "removedIds", "previousSiblingId", "nextSiblingId"},
		[]interface{}{string(record.Type), targetID, attributeName, v8.Null(v8ctx.Isolate()), oldValue, added, removed, previousID, nextID})
	if err != nil {
		obj.Release()
		return nil, err
	}
	return obj, nil
}

func mutationIDsToJSObject(v8ctx *v8.Context, ids []uint32) (*v8.Object, error) {
	if len(ids) > math.MaxInt32 {
		return nil, errors.New("mutation observer node count exceeds native bound")
	}
	obj, err := v8ctx.NewNullPrototypeObject()
	if err != nil {
		return nil, err
	}
	complete := false
	defer func() {
		if !complete {
			obj.Release()
		}
	}()
	for index, id := range ids {
		value, ok := checkedUint32ToInt32(id)
		if !ok {
			return nil, errors.New("mutation observer node exceeds native bound")
		}
		if err := obj.SetIdx(uint32(index), value); err != nil {
			return nil, err
		}
	}
	if err := obj.Set("length", int32(len(ids))); err != nil {
		return nil, err
	}
	complete = true
	return obj, nil
}

// fireMutationObservers drains pending records and invokes the JS-side
// callback for each observer that has any.
func (c *Context) fireMutationObservers() error {
	if c.observers == nil {
		return nil
	}
	callbacks := 0
	var callbackFailures []error
	omittedFailures := 0
	failures := func() error {
		failure := errors.Join(callbackFailures...)
		if omittedFailures > 0 {
			failure = errors.Join(failure, fmt.Errorf("%d additional mutation observer callback failures omitted", omittedFailures))
		}
		return failure
	}
	for {
		obs := c.observers.take()
		if len(obs) == 0 {
			return failures()
		}
		for _, o := range obs {
			if err := errors.Join(c.evalCtx.Err(), c.async.ctx.Err()); err != nil {
				return errors.Join(failures(), err)
			}
			recs := o.pending
			if len(recs) == 0 || o.callback == nil {
				c.observers.mu.Lock()
				c.observers.removeTransients(o, 0)
				c.observers.mu.Unlock()
				continue
			}
			if callbacks >= maxMutationCallbacksPerPump {
				return errors.Join(failures(), errors.New("mutation observer callback budget exceeded"))
			}
			callbacks++
			if err := c.fireMutationObserver(o, recs); err != nil {
				if len(callbackFailures) < maxMutationFailuresPerPump {
					callbackFailures = append(callbackFailures, err)
				} else {
					omittedFailures++
				}
			}
		}
		if len(callbackFailures) > 0 || omittedFailures > 0 {
			return failures()
		}
	}
}

func (c *Context) fireMutationObserver(observer *observer, records []mutationRecord) error {
	array, err := mutationsToJSArray(c.v8ctx, records)
	if err != nil {
		return fmt.Errorf("mutation observer records: %w", err)
	}
	defer array.Release()
	c.observers.mu.Lock()
	c.observers.releasePending(observer)
	c.observers.removeTransients(observer, 0)
	c.observers.mu.Unlock()
	global := c.v8ctx.Global()
	defer global.Release()
	result, err := observer.callback.Call(global, array)
	if result != nil {
		result.Release()
	}
	if err != nil {
		return fmt.Errorf("mutation observer callback: %w", formatJSError(err))
	}
	return nil
}

const mutationObserverBootstrap = `
(() => {
  const apply = Reflect.apply, defineProperty = Object.defineProperty;
  const TypeError = globalThis.TypeError, RangeError = globalThis.RangeError;
  const String = globalThis.String, Boolean = globalThis.Boolean;
  const maximumCreationOrder = Number.MAX_SAFE_INTEGER;
  const appendValue = (array, value) => defineProperty(array, array.length, {value, writable: true, enumerable: true, configurable: true});
  const convertRecords = (records) => {
    const out = [];
    for (let i = 0; i < records.length; i++) {
      const r = records[i];
      const added = [];
      for (let k = 0; k < r.addedIds.length; k++) appendValue(added, __wrap(r.addedIds[k]));
      const removed = [];
      for (let k = 0; k < r.removedIds.length; k++) appendValue(removed, __wrap(r.removedIds[k]));
      appendValue(out, {
        type: r.type,
        target: __wrap(r.targetId),
        addedNodes: added,
        removedNodes: removed,
        attributeName: r.attributeName,
        attributeNamespace: r.attributeNamespace,
        oldValue: r.oldValue,
        previousSibling: __wrap(r.previousSiblingId),
        nextSibling: __wrap(r.nextSiblingId),
      });
    }
    return out;
  };
  const states = new WeakMap(), stateGet = WeakMap.prototype.get, stateSet = WeakMap.prototype.set;
  let creationOrder = 0;
  const stateFor = owner => {
    const state = apply(stateGet, states, [owner]);
    if (!state) throw new TypeError('Invalid MutationObserver receiver');
    return state;
  };
  const normalizeOptions = input => {
    if (input == null) input = {};
    if (typeof input !== 'object' && typeof input !== 'function') throw new TypeError('MutationObserver options must be a dictionary');
    const rawFilter = input.attributeFilter;
    let filter;
    if (rawFilter !== undefined) {
      if (rawFilter === null || (typeof rawFilter !== 'object' && typeof rawFilter !== 'function')) throw new TypeError('attributeFilter must be an iterable object');
      filter = [];
      let characters = 0;
      for (const value of rawFilter) {
        if (filter.length >= 1024) throw new RangeError('mutation observer: attribute filter name limit exceeded');
        if (typeof value === 'symbol') throw new TypeError('attributeFilter requires DOMStrings');
        const text = String(value);
        characters += text.length;
        if (characters > 65536) throw new RangeError('mutation observer: attribute filter byte limit exceeded');
        appendValue(filter, text);
      }
    }
    const rawAttributeOldValue = input.attributeOldValue, rawAttributes = input.attributes;
    const rawCharacterData = input.characterData, rawCharacterDataOldValue = input.characterDataOldValue;
    const childList = Boolean(input.childList), subtree = Boolean(input.subtree);
    const attributes = rawAttributes === undefined ? rawAttributeOldValue !== undefined || filter !== undefined : Boolean(rawAttributes);
    const characterData = rawCharacterData === undefined ? rawCharacterDataOldValue !== undefined : Boolean(rawCharacterData);
    const attributeOldValue = Boolean(rawAttributeOldValue), characterDataOldValue = Boolean(rawCharacterDataOldValue);
    if (!childList && !attributes && !characterData) throw new TypeError('MutationObserver observation type required');
    if ((!attributes && (attributeOldValue || filter !== undefined)) || (!characterData && characterDataOldValue)) throw new TypeError('MutationObserver options conflict');
    return {attributeFilter: filter, attributeOldValue, attributes, characterData, characterDataOldValue, childList, subtree};
  };
  class MutationObserver {
    constructor(callback) {
      if (typeof callback !== 'function') throw new TypeError('MutationObserver callback must be callable');
      if (creationOrder >= maximumCreationOrder) throw new RangeError('MutationObserver creation order exhausted');
      const state = {id: 0, callback, order: ++creationOrder};
      state.deliver = records => apply(callback, this, [convertRecords(records), this]);
      apply(stateSet, states, [this, state]);
    }
    observe(target, options) {
      const state = stateFor(this);
      const targetID = __nodeIdentity(target);
      if (!targetID) throw new TypeError('MutationObserver target must be a Node');
      const normalized = normalizeOptions(options);
      state.id = __observer_register(targetID, normalized, state.deliver, state.id, state.order);
    }
    disconnect() {
      const state = stateFor(this);
      if (state.id) __observer_disconnect(state.id);
      state.id = 0;
    }
    takeRecords() {
      const state = stateFor(this);
      return state.id ? convertRecords(__observer_take(state.id)) : [];
    }
  }
  globalThis.MutationObserver = MutationObserver;
})();
`
