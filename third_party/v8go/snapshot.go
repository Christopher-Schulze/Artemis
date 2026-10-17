// Copyright 2026 the artemis authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package v8go

// #include <stdlib.h>
// #include "v8go.h"
import "C"

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"unsafe"
)

// SnapshotCreator wraps v8::SnapshotCreator. The lifecycle is strict:
// New -> RunScript* -> CreateBlob -> Dispose. After CreateBlob the
// underlying isolate is invalidated; further RunScript calls fail.
type SnapshotCreator struct {
	ptr C.SnapshotCreatorPtr
}

// NewSnapshotCreator creates a fresh SnapshotCreator with its own
// isolate. Pure JS evaluated via RunScript becomes the scripted
// realm that NewContextFromSnapshot(iso, 0) opens once SetProcessSnapshot
// installed the blob.
func NewSnapshotCreator() *SnapshotCreator {
	if ProcessSnapshotActive() {
		// The creator must start from V8's own snapshot, which the process
		// snapshot replaced; such a creator would also break the shared
		// read-only heap. RunScript and CreateBlob report the refusal.
		return &SnapshotCreator{}
	}
	initializeIfNecessary()
	return &SnapshotCreator{ptr: C.SnapshotCreatorNew()}
}

// RunScript executes JS in the snapshot creator's default context.
// origin is used for stack traces only.
func (s *SnapshotCreator) RunScript(source, origin string) error {
	if s.ptr == nil {
		return errors.New("snapshot creator unavailable: disposed, or a process snapshot is active")
	}
	cSrc := C.CString(source)
	cOri := C.CString(origin)
	defer C.free(unsafe.Pointer(cSrc))
	defer C.free(unsafe.Pointer(cOri))
	rtn := C.SnapshotCreatorRunScript(s.ptr, cSrc, cOri)
	if rtn.msg != nil {
		return newJSError(rtn)
	}
	return nil
}

// CreateBlob finalises the snapshot creator and returns the serialised
// startup data. After this call the SnapshotCreator is one-way frozen;
// subsequent RunScript calls will fail.
func (s *SnapshotCreator) CreateBlob() ([]byte, error) {
	if s.ptr == nil {
		return nil, errors.New("snapshot creator unavailable: disposed, or a process snapshot is active")
	}
	rtn := C.SnapshotCreatorCreateBlob(s.ptr)
	if rtn.error.msg != nil {
		return nil, newJSError(rtn.error)
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

// ErrV8Initialized is returned by SetProcessSnapshot after V8 started
// without that snapshot.
var ErrV8Initialized = errors.New("v8go: V8 is already initialized without this process snapshot")

// SetProcessSnapshot makes snapshot the startup snapshot of every isolate in
// this process. V8 shares one read-only heap across a process's isolates, so
// isolates from different snapshots cannot coexist; the snapshot is therefore
// process-wide and must be set before the first isolate exists. Setting the
// same bytes again is a no-op. The snapshot's default context is pristine,
// so plain NewContext realms are unaffected; NewContextFromSnapshot opens the
// scripted realm. The bytes are copied and kept for the process lifetime.
func SetProcessSnapshot(snapshot []byte) error {
	if len(snapshot) == 0 || len(snapshot) > math.MaxInt32 || !SnapshotBlobIsValid(snapshot) {
		return errors.New("v8go: valid snapshot blob required")
	}
	sum := sha256.Sum256(snapshot)
	processSnapshotMu.Lock()
	defer processSnapshotMu.Unlock()
	if processSnapshotSet {
		if sum == processSnapshotSum {
			return nil
		}
		return errors.New("v8go: a different process snapshot is already set")
	}
	if v8Initialized {
		return ErrV8Initialized
	}
	C.SetProcessSnapshot((*C.uint8_t)(unsafe.Pointer(&snapshot[0])), C.int(len(snapshot)))
	processSnapshotSum, processSnapshotSet = sum, true
	return nil
}

// ProcessSnapshotActive reports whether isolates of this process start from
// the process snapshot.
func ProcessSnapshotActive() bool {
	processSnapshotMu.Lock()
	defer processSnapshotMu.Unlock()
	return processSnapshotSet
}

// NewContextFromSnapshot opens the scripted realm (additional context index)
// of the process snapshot on iso.
func NewContextFromSnapshot(iso *Isolate, index int) (*Context, error) {
	if iso == nil || iso.ptr == nil || !ProcessSnapshotActive() {
		return nil, errors.New("v8go: live isolate with a process snapshot required")
	}
	ctxRegMu.Lock()
	ctxSeq++
	ref := ctxSeq
	ctxRegMu.Unlock()
	ptr := C.NewContextFromSnapshot(iso.ptr, C.int(index), C.int(ref))
	if ptr == nil {
		return nil, fmt.Errorf("v8go: process snapshot has no context %d", index)
	}
	ctx := &Context{ref: ref, ptr: ptr, iso: iso}
	ctx.register()
	return ctx, nil
}

// SnapshotBlobIsValid checks the V8 checksum and serialized snapshot shape
// against the linked V8 runtime without creating an isolate or mutating the
// supplied bytes.
func SnapshotBlobIsValid(snapshot []byte) bool {
	if len(snapshot) == 0 || len(snapshot) > math.MaxInt32 {
		return false
	}
	return C.SnapshotBlobIsValid(
		(*C.uint8_t)(unsafe.Pointer(&snapshot[0])),
		C.int(len(snapshot)),
	) != 0
}
