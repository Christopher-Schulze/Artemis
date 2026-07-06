package js

import (
	"sync"
	"sync/atomic"

	v8 "rogchap.com/v8go"
)

// storageHandles maps a uint32 handle to the *memStorage that backs it.
// Stored on Runtime so the cached storage method templates can dispatch
// to the right per-Context storage by reading the handle from the
// receiver's internal field 0. Released on Context.Close to bound the
// map size for long-running runtimes.
type storageHandles struct {
	seq atomic.Uint32
	m   sync.Map // uint32 -> *memStorage
}

func (h *storageHandles) put(s *memStorage) uint32 {
	id := h.seq.Add(1)
	h.m.Store(id, s)
	return id
}

func (h *storageHandles) get(id uint32) *memStorage {
	v, _ := h.m.Load(id)
	if v == nil {
		return nil
	}
	return v.(*memStorage)
}

func (h *storageHandles) remove(id uint32) {
	h.m.Delete(id)
}

// storageTemplates is the Runtime-level cache of v8 templates used to
// build localStorage / sessionStorage. The object template carries one
// internal field (the storage handle); the function templates' callbacks
// read that field via info.This() and dispatch to the right *memStorage.
//
// Caching across all Contexts derived from the Runtime means we register
// 6 v8 callbacks per Isolate instead of 6 per Context, and skip
// NewObjectTemplate / NewFunctionTemplate allocations on the hot path.
type storageTemplates struct {
	objTmpl *v8.ObjectTemplate
	getItem *v8.FunctionTemplate
	setItem *v8.FunctionTemplate
	rmItem  *v8.FunctionTemplate
	clear   *v8.FunctionTemplate
	key     *v8.FunctionTemplate
	lenOf   *v8.FunctionTemplate
}

// storageFromInfo reads the storage handle from info.This()'s internal
// field 0 and returns the bound *memStorage. Returns nil if the receiver
// is missing internal data (caller should bail out gracefully).
func storageFromInfo(info *v8.FunctionCallbackInfo, h *storageHandles) *memStorage {
	this := info.This()
	if this == nil {
		return nil
	}
	field := this.GetInternalField(0)
	if field == nil {
		return nil
	}
	id := uint32(field.Integer())
	if id == 0 {
		return nil
	}
	return h.get(id)
}

func newStorageTemplates(iso *v8.Isolate, h *storageHandles) *storageTemplates {
	st := &storageTemplates{}
	st.objTmpl = v8.NewObjectTemplate(iso)
	st.objTmpl.SetInternalFieldCount(1)
	st.getItem = v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		s := storageFromInfo(info, h)
		args := info.Args()
		if s == nil || len(args) < 1 {
			return v8.Null(iso)
		}
		v, ok := s.getItem(args[0].String())
		if !ok {
			return v8.Null(iso)
		}
		return mustValue(iso, v)
	})
	st.setItem = v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		s := storageFromInfo(info, h)
		args := info.Args()
		if s == nil || len(args) < 2 {
			return v8.Null(iso)
		}
		s.setItem(args[0].String(), args[1].String())
		return v8.Null(iso)
	})
	st.rmItem = v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		s := storageFromInfo(info, h)
		args := info.Args()
		if s == nil || len(args) < 1 {
			return v8.Null(iso)
		}
		s.removeItem(args[0].String())
		return v8.Null(iso)
	})
	st.clear = v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		s := storageFromInfo(info, h)
		if s != nil {
			s.clear()
		}
		return v8.Null(iso)
	})
	st.key = v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		s := storageFromInfo(info, h)
		args := info.Args()
		if s == nil || len(args) < 1 {
			return v8.Null(iso)
		}
		k, ok := s.key(int(args[0].Integer()))
		if !ok {
			return v8.Null(iso)
		}
		return mustValue(iso, k)
	})
	st.lenOf = v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		s := storageFromInfo(info, h)
		if s == nil {
			return mustValue(iso, int32(0))
		}
		return mustValue(iso, int32(s.length()))
	})
	return st
}

// buildStorageCached produces a Storage-shaped object using Runtime-level
// cached templates. Allocates one v8 Object instance and 6 Function
// instances (templates and their callbacks are amortised across the
// Runtime). The handle is freed on Context.Close so the registry doesn't
// grow unboundedly.
func buildStorageCached(c *Context, s *memStorage) (*v8.Value, error) {
	r := c.rt
	r.mu.Lock()
	if r.storageHandles == nil {
		r.storageHandles = &storageHandles{}
	}
	if r.storageTemplates == nil {
		r.storageTemplates = newStorageTemplates(r.iso, r.storageHandles)
	}
	st := r.storageTemplates
	h := r.storageHandles
	r.mu.Unlock()

	id := h.put(s)
	c.storageHandleIDs = append(c.storageHandleIDs, id)

	obj, err := st.objTmpl.NewInstance(c.v8ctx)
	if err != nil {
		h.remove(id)
		return nil, err
	}
	if err := obj.SetInternalField(0, int32(id)); err != nil {
		h.remove(id)
		return nil, err
	}
	_ = obj.Set("getItem", st.getItem.GetFunction(c.v8ctx))
	_ = obj.Set("setItem", st.setItem.GetFunction(c.v8ctx))
	_ = obj.Set("removeItem", st.rmItem.GetFunction(c.v8ctx))
	_ = obj.Set("clear", st.clear.GetFunction(c.v8ctx))
	_ = obj.Set("key", st.key.GetFunction(c.v8ctx))
	_ = obj.Set("lengthOf", st.lenOf.GetFunction(c.v8ctx))
	_ = obj.Set("length", int32(s.length()))
	return obj.Value, nil
}
