// Copyright 2026 the artemis authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package v8go

// #include <stdlib.h>
// #include "v8go.h"
import "C"

import (
	"errors"
	"unsafe"
)

// SnapshotCreator wraps v8::SnapshotCreator. The lifecycle is strict:
// New -> RunScript* -> CreateBlob -> Dispose. After CreateBlob the
// underlying isolate is invalidated; further RunScript calls fail.
type SnapshotCreator struct {
	ptr C.SnapshotCreatorPtr
}

// NewSnapshotCreator creates a fresh SnapshotCreator with its own
// isolate. Pure JS evaluated via RunScript becomes part of the default
// context that NewIsolateFromSnapshot will deserialise.
func NewSnapshotCreator() *SnapshotCreator {
	initializeIfNecessary()
	return &SnapshotCreator{ptr: C.SnapshotCreatorNew()}
}

// RunScript executes JS in the snapshot creator's default context.
// origin is used for stack traces only.
func (s *SnapshotCreator) RunScript(source, origin string) error {
	if s.ptr == nil {
		return errors.New("snapshot creator disposed")
	}
	cSrc := C.CString(source)
	cOri := C.CString(origin)
	defer C.free(unsafe.Pointer(cSrc))
	defer C.free(unsafe.Pointer(cOri))
	rtn := C.SnapshotCreatorRunScript(s.ptr, cSrc, cOri)
	if rtn.msg != nil {
		msg := C.GoString(rtn.msg)
		C.free(unsafe.Pointer(rtn.msg))
		if rtn.location != nil {
			C.free(unsafe.Pointer(rtn.location))
		}
		if rtn.stack != nil {
			C.free(unsafe.Pointer(rtn.stack))
		}
		return errors.New(msg)
	}
	return nil
}

// CreateBlob finalises the snapshot creator and returns the serialised
// startup data. After this call the SnapshotCreator is one-way frozen;
// subsequent RunScript calls will fail.
func (s *SnapshotCreator) CreateBlob() ([]byte, error) {
	if s.ptr == nil {
		return nil, errors.New("snapshot creator disposed")
	}
	rtn := C.SnapshotCreatorCreateBlob(s.ptr)
	if rtn.error.msg != nil {
		msg := C.GoString(rtn.error.msg)
		C.free(unsafe.Pointer(rtn.error.msg))
		return nil, errors.New(msg)
	}
	if rtn.data == nil || rtn.length <= 0 {
		return nil, errors.New("empty snapshot blob")
	}
	out := C.GoBytes(unsafe.Pointer(rtn.data), rtn.length)
	C.SnapshotBlobFree((*C.uint8_t)(unsafe.Pointer(rtn.data)))
	return out, nil
}

// Dispose releases the underlying SnapshotCreator. Safe to call after
// CreateBlob (no-op on already-freed creator). Always call to avoid
// leaking the V8 isolate.
func (s *SnapshotCreator) Dispose() {
	if s.ptr == nil {
		return
	}
	C.SnapshotCreatorDelete(s.ptr)
	s.ptr = nil
}

// NewIsolateFromSnapshot creates an isolate seeded with a startup
// snapshot blob produced by SnapshotCreator. Contexts created on this
// isolate inherit the snapshot's default context state, skipping the
// parse + first-run cost of whatever JS was baked in.
//
// snapshot must remain valid for the lifetime of the isolate; we copy
// it into a heap-allocated buffer that IsolateDispose frees.
func NewIsolateFromSnapshot(snapshot []byte) *Isolate {
	initializeIfNecessary()
	if len(snapshot) == 0 {
		return NewIsolate()
	}
	iso := &Isolate{
		ptr: C.NewIsolateWithSnapshot(
			(*C.uint8_t)(unsafe.Pointer(&snapshot[0])),
			C.int(len(snapshot)),
		),
		cbs: make(map[int]FunctionCallback),
	}
	iso.null = newValueNull(iso)
	iso.undefined = newValueUndefined(iso)
	return iso
}
