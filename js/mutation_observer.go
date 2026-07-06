package js

import (
	"sync"

	v8 "rogchap.com/v8go"

	"github.com/Christopher-Schulze/Artemis/webapi"
)

// MutationRecordType is the kind of DOM mutation a record describes.
type MutationRecordType string

const (
	MutationChildList  MutationRecordType = "childList"
	MutationAttributes MutationRecordType = "attributes"
)

// mutationRecord is the Go-side captured event before it crosses to JS.
type mutationRecord struct {
	Type          MutationRecordType
	TargetID      uint32
	AddedIDs      []uint32
	RemovedIDs    []uint32
	AttributeName string
}

// observer is one registered MutationObserver, holding its target +
// options + JS callback function. Records accumulate in pending until
// flush.
type observer struct {
	id        uint32
	targetID  uint32
	subtree   bool
	childList bool
	attrs     bool
	callback  *v8.Function
	pending   []mutationRecord
}

// observerRegistry holds all observers for a Context.
type observerRegistry struct {
	mu       sync.Mutex
	nextID   uint32
	by       map[uint32]*observer
	byTarget map[uint32][]*observer // direct-target index
}

func newObserverRegistry() *observerRegistry {
	return &observerRegistry{
		by:       make(map[uint32]*observer),
		byTarget: make(map[uint32][]*observer),
	}
}

func (r *observerRegistry) add(o *observer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	o.id = r.nextID
	r.by[o.id] = o
	r.byTarget[o.targetID] = append(r.byTarget[o.targetID], o)
}

func (r *observerRegistry) disconnect(id uint32) {
	r.mu.Lock()
	defer r.mu.Unlock()
	o, ok := r.by[id]
	if !ok {
		return
	}
	delete(r.by, id)
	out := r.byTarget[o.targetID][:0]
	for _, x := range r.byTarget[o.targetID] {
		if x.id != id {
			out = append(out, x)
		}
	}
	r.byTarget[o.targetID] = out
}

// recordMutation pushes a record into every observer that watches
// targetID, including ancestor observers when subtree=true.
func (r *observerRegistry) recordMutation(c *Context, rec mutationRecord, targetNode *webapi.Node) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Direct: observers on targetID
	for _, o := range r.byTarget[rec.TargetID] {
		if !shouldNotify(o, rec) {
			continue
		}
		o.pending = append(o.pending, rec)
	}
	// Ancestors: walk up; observers with subtree=true on any ancestor
	// also receive the record.
	if targetNode != nil {
		for p := targetNode.Parent(); p != nil; p = p.Parent() {
			pid := c.nodes.Handle(p)
			if pid == 0 {
				break
			}
			for _, o := range r.byTarget[pid] {
				if !o.subtree {
					continue
				}
				if !shouldNotify(o, rec) {
					continue
				}
				o.pending = append(o.pending, rec)
			}
		}
	}
}

// take returns all observers with pending records and clears them.
func (r *observerRegistry) take() []*observer {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*observer
	for _, o := range r.by {
		if len(o.pending) > 0 {
			out = append(out, o)
		}
	}
	return out
}

func shouldNotify(o *observer, rec mutationRecord) bool {
	switch rec.Type {
	case MutationChildList:
		return o.childList
	case MutationAttributes:
		return o.attrs
	}
	return false
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
		register: v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			c := r.contextFor(info.Context())
			args := info.Args()
			if c == nil || len(args) < 3 {
				return mustValue(iso, int32(0))
			}
			optsObj, err := args[1].AsObject()
			if err != nil {
				return mustValue(iso, int32(0))
			}
			callback, err := args[2].AsFunction()
			if err != nil {
				return mustValue(iso, int32(0))
			}
			o := &observer{
				targetID: uint32(args[0].Integer()),
				callback: callback,
			}
			if v, err := optsObj.Get("childList"); err == nil {
				o.childList = v.Boolean()
			}
			if v, err := optsObj.Get("attributes"); err == nil {
				o.attrs = v.Boolean()
			}
			if v, err := optsObj.Get("subtree"); err == nil {
				o.subtree = v.Boolean()
			}
			c.observers.add(o)
			return mustValue(iso, int32(o.id))
		}),
		disconnect: v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			c := r.contextFor(info.Context())
			args := info.Args()
			if c == nil || len(args) < 1 {
				return v8.Null(iso)
			}
			c.observers.disconnect(uint32(args[0].Integer()))
			return v8.Null(iso)
		}),
		take: v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			c := r.contextFor(info.Context())
			args := info.Args()
			if c == nil || len(args) < 1 {
				return idsToArray(info.Context(), iso, nil)
			}
			id := uint32(args[0].Integer())
			c.observers.mu.Lock()
			o, ok := c.observers.by[id]
			var recs []mutationRecord
			if ok {
				recs = o.pending
				o.pending = nil
			}
			c.observers.mu.Unlock()
			if !ok {
				return idsToArray(info.Context(), iso, nil)
			}
			return mutationsToJSArray(info.Context(), iso, recs)
		}),
	}
	return r.observerTemplates
}

func installMutationObserver(iso *v8.Isolate, v8ctx *v8.Context, c *Context) error {
	t := c.rt.ensureObserverTemplates()
	g := v8ctx.Global()
	if err := g.Set("__observer_register", t.register.GetFunction(v8ctx)); err != nil {
		return err
	}
	if err := g.Set("__observer_disconnect", t.disconnect.GetFunction(v8ctx)); err != nil {
		return err
	}
	if err := g.Set("__observer_take", t.take.GetFunction(v8ctx)); err != nil {
		return err
	}
	c.registerBootstrap("artemis-mutation-observer", mutationObserverBootstrap)
	return nil
}

func mutationsToJSArray(v8ctx *v8.Context, iso *v8.Isolate, recs []mutationRecord) *v8.Value {
	arr, err := v8.NewObjectTemplate(iso).NewInstance(v8ctx)
	if err != nil {
		return v8.Null(iso)
	}
	for i, r := range recs {
		obj, err := v8.NewObjectTemplate(iso).NewInstance(v8ctx)
		if err != nil {
			continue
		}
		_ = obj.Set("type", string(r.Type))
		_ = obj.Set("targetId", int32(r.TargetID))
		_ = obj.Set("attributeName", r.AttributeName)
		added, _ := v8.NewObjectTemplate(iso).NewInstance(v8ctx)
		for j, id := range r.AddedIDs {
			_ = added.SetIdx(uint32(j), int32(id))
		}
		_ = added.Set("length", int32(len(r.AddedIDs)))
		_ = obj.Set("addedIds", added)
		removed, _ := v8.NewObjectTemplate(iso).NewInstance(v8ctx)
		for j, id := range r.RemovedIDs {
			_ = removed.SetIdx(uint32(j), int32(id))
		}
		_ = removed.Set("length", int32(len(r.RemovedIDs)))
		_ = obj.Set("removedIds", removed)
		_ = arr.SetIdx(uint32(i), obj)
	}
	_ = arr.Set("length", int32(len(recs)))
	return arr.Value
}

// fireMutationObservers drains pending records and invokes the JS-side
// callback for each observer that has any.
func (c *Context) fireMutationObservers() {
	if c.observers == nil {
		return
	}
	for {
		obs := c.observers.take()
		if len(obs) == 0 {
			return
		}
		for _, o := range obs {
			recs := o.pending
			o.pending = nil
			if len(recs) == 0 || o.callback == nil {
				continue
			}
			arr := mutationsToJSArray(c.v8ctx, c.rt.iso, recs)
			_, _ = o.callback.Call(c.v8ctx.Global(), arr)
		}
	}
}

const mutationObserverBootstrap = `
(() => {
  class MutationObserver {
    constructor(callback) {
      this._cb = callback;
      this._ids = [];
    }
    observe(target, options) {
      options = options || {};
      const native = (records) => {
        // records is a sparse array-shaped object built native-side
        const out = [];
        for (let i = 0; i < (records.length || 0); i++) {
          const r = records[i];
          if (!r) continue;
          const added = [];
          for (let k = 0; k < (r.addedIds.length || 0); k++) added.push(__wrap(r.addedIds[k]));
          const removed = [];
          for (let k = 0; k < (r.removedIds.length || 0); k++) removed.push(__wrap(r.removedIds[k]));
          out.push({
            type: r.type,
            target: __wrap(r.targetId),
            addedNodes: added,
            removedNodes: removed,
            attributeName: r.attributeName,
            previousSibling: null,
            nextSibling: null,
          });
        }
        try { this._cb(out, this); } catch (e) {}
      };
      const id = __observer_register(target.__id, options, native.bind(this));
      this._ids.push(id);
    }
    disconnect() {
      for (const id of this._ids) __observer_disconnect(id);
      this._ids = [];
    }
    takeRecords() {
      const out = [];
      for (const id of this._ids) {
        const recs = __observer_take(id);
        for (let i = 0; i < (recs.length || 0); i++) {
          const r = recs[i];
          if (!r) continue;
          out.push({ type: r.type, target: __wrap(r.targetId) });
        }
      }
      return out;
    }
  }
  globalThis.MutationObserver = MutationObserver;
})();
`
