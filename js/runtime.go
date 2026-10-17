// Package js wraps the V8 JavaScript engine (via rogchap.com/v8go) and
// the bridge that exposes Artemis DOM types to JS code. Phase 2 covers
// isolate management, per-page contexts, console capture, and a minimal
// read-only `document` binding. Mutation, events, Fetch, XHR, etc. land
// in TASK 004.
package js

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	v8 "github.com/tommie/v8go"

	"github.com/Christopher-Schulze/Artemis/parser"
	"github.com/Christopher-Schulze/Artemis/webapi"
)

// Runtime owns a V8 Isolate. Multiple Contexts may share a Runtime.
// A Runtime is safe for concurrent context creation; each individual
// Context must be used from a single goroutine at a time.
type Runtime struct {
	iso *v8.Isolate
	mu  sync.Mutex
	// ctxMu serialises NewContext + Close cgo paths into V8. v8::Isolate
	// is single-threaded; without serialisation, two goroutines calling
	// NewContext concurrently can crash V8 in GlobalHandles::Destroy.
	// Held only across the cgo entry-points, not for the lifetime of
	// a *js.Context — Eval and other in-Context calls serialise
	// internally via v8::Locker.
	ctxMu            isolateGate
	execution        *executionSpan // accessed only while ctxMu is held
	closed           atomic.Bool
	isolateExposed   atomic.Bool
	closeOnce        sync.Once
	lifetime         context.Context
	cancel           context.CancelFunc
	cache            *scriptCache
	cachedBootstraps map[string]string // key "all" or "tainted" -> concat
	hasSnapshot      bool              // V8 startup snapshot baked in
	snapshotState    SnapshotState
	snapshotReason   string

	// Context pool: when poolSize > 0, NewContext borrows v8.Contexts
	// from this channel instead of creating fresh ones. Close returns
	// only pristine realms; executed/exposed realms are disposed.
	// Reuse rebinds native page options and skips install*/flushBootstraps on
	// reuse since all template bindings are Runtime-cached and resolve
	// per-call via contextRegistry. Sized at NewRuntimeWithPool time.
	ctxPool              chan *v8.Context
	poolSize             int
	poolEnabled          bool
	storageTemplates     *storageTemplates // cached templates for buildStorageCached
	storageHandles       *storageHandles   // handle table backing internal field 0
	locationTemplate     *v8.ObjectTemplate
	timerTemplates       *timerTemplates // cached setTimeout/clearTimeout/setInterval/clearInterval
	consoleTemplates     *consoleTemplates
	domBridgeTemplates   *domBridgeTemplates
	observerTemplates    *observerTemplates
	cryptoSubtle         *cryptoSubtleTemplates
	cryptoRandom         *cryptoRandomTemplates
	iframeTemplates      *iframeTemplates
	fetchTemplates       *fetchTemplates
	fetchBodies          *fetchBodyHandles
	urlHelperTemplate    *v8.FunctionTemplate
	wsTemplates          *wsTemplates
	extrasV2             *extrasV2Templates
	cascadeStyleTemplate *v8.FunctionTemplate
	performanceTemplate  *v8.FunctionTemplate
	contextRegistry      sync.Map // *v8.Context -> *Context, populated by NewContext, drained by Close
	localStores          map[storageIdentity]*memStorage
	storageBudget        *storageBudget
}

// SnapshotState describes whether the embedded startup snapshot was admitted
// for this runtime. Invalid assets fail closed to a cold isolate and remain
// observable through SnapshotStatus.
type SnapshotState string

const (
	SnapshotActive      SnapshotState = "active"
	SnapshotUnavailable SnapshotState = "unavailable"
)

// SnapshotActivation is the runtime-visible snapshot admission result.
type SnapshotActivation struct {
	State  SnapshotState
	Reason string
}

// contextFor returns the *js.Context bound to v8ctx, or nil if v8ctx
// has been closed or was never registered. Used by Runtime-cached
// FunctionTemplate callbacks to dispatch to per-Context state.
func (r *Runtime) contextFor(v8ctx *v8.Context) *Context {
	v, _ := r.contextRegistry.Load(v8ctx)
	if v == nil {
		return nil
	}
	return v.(*Context)
}

// NewRuntime creates a V8 isolate seeded with the embedded startup
// snapshot (TASK 042). The snapshot bakes the parsed + first-run state
// of every BootstrapSource so NewContext only needs to bind native
// callbacks instead of re-evaluating ~30K lines of JS each time. If the
// snapshot blob is absent, stale or incompatible at build time (see
// snapshot_data.go), falls back to the from-scratch isolate path and exposes
// the admission failure through SnapshotStatus.
func NewRuntime() *Runtime {
	return newRuntime(0)
}

// NewRuntimeWithPool creates a Runtime with a v8.Context pool of the
// given size. Only pristine contexts are admitted to reuse. Page execution
// or native context exposure permanently retires that realm from the pool;
// built-in prototypes, non-configurable properties and lexical bindings
// cannot be safely reset by deleting globals. Warm contexts remain useful
// for initial admission. Set size to 0 to disable pooling.
func NewRuntimeWithPool(size int) *Runtime {
	return newRuntime(size)
}

// NewRuntimeWithWarmPool creates a Runtime with a v8.Context pool of
// the given size and PRE-BUILDS all N v8.Contexts up-front. The first
// NewContext call gets the pool fast path immediately instead of
// paying the cold-build cost; useful when you control the whole agent
// loop and want predictable per-page latency. Pre-warming takes the
// same wall time as N cold NewContext calls (~1ms per slot on M1).
//
// The pre-built Contexts are owned by the Runtime; they get torn down
// in Runtime.Close along with the Isolate.
func NewRuntimeWithWarmPool(size int) (*Runtime, error) {
	r := newRuntime(size)
	if size <= 0 {
		return r, nil
	}
	// Build N *js.Contexts in parallel-shape: keep them all open at
	// once so each NewContext sees an empty pool and builds a fresh
	// v8.Context. After all N are built, close them all so they go
	// back into the pool. This guarantees N distinct v8.Contexts in
	// the pool rather than 1 reused N times.
	stub, err := stubDocForWarmup()
	if err != nil {
		r.Close()
		return nil, err
	}
	ctxs := make([]*Context, 0, size)
	for i := 0; i < size; i++ {
		c, err := r.NewContext(stub, ContextOpts{})
		if err != nil {
			for _, prev := range ctxs {
				prev.Close()
			}
			r.Close()
			return nil, err
		}
		ctxs = append(ctxs, c)
	}
	for _, c := range ctxs {
		c.Close()
	}
	return r, nil
}

// stubDocForWarmup builds a tiny *webapi.Document used to feed
// NewContext during pool warmup. The doc only needs to be non-nil
// and parseable; user scripts never touch it.
func stubDocForWarmup() (*webapi.Document, error) {
	return parser.ParseHTML(strings.NewReader("<html></html>"), "about:blank")
}

func newRuntime(poolSize int) *Runtime {
	hasSnapshot := false
	snapshotState := SnapshotUnavailable
	var snapshotReason string
	// V8 shares one read-only heap per process, so the admitted snapshot is
	// the process snapshot of every isolate; it activates only before V8
	// starts. Its default context is pristine and the bootstrap realm is
	// snapshot context 0 (newV8Context).
	if _, err := CurrentSnapshotManifest(); err != nil {
		snapshotReason = err.Error()
	} else if err := v8.SetProcessSnapshot(snapshotBlob); err != nil {
		snapshotReason = "process snapshot: " + err.Error()
	} else {
		hasSnapshot = true
		snapshotState = SnapshotActive
	}
	iso := v8.NewIsolate()
	r := &Runtime{iso: iso, cache: newScriptCache(), hasSnapshot: hasSnapshot, snapshotState: snapshotState, snapshotReason: snapshotReason, storageBudget: &storageBudget{}}
	iso.SetPromiseRejectionObserver(r.observePromiseRejection)
	r.lifetime, r.cancel = context.WithCancel(context.Background())
	if poolSize > 0 {
		r.ctxPool = make(chan *v8.Context, poolSize)
		r.poolSize = poolSize
		r.poolEnabled = true
	}
	return r
}

// newV8Context opens a realm: the snapshot bootstrap realm when the snapshot
// is active, otherwise a pristine context the bootstrap sources then run in.
func (r *Runtime) newV8Context() (*v8.Context, error) {
	if !r.hasSnapshot {
		return v8.NewContext(r.iso), nil
	}
	ctx, err := v8.NewContextFromSnapshot(r.iso, 0)
	if err != nil {
		return nil, fmt.Errorf("js: open snapshot realm: %w", err)
	}
	return ctx, nil
}

// SnapshotStatus returns the immutable admission result for this runtime.
func (r *Runtime) SnapshotStatus() SnapshotActivation {
	r.mu.Lock()
	defer r.mu.Unlock()
	return SnapshotActivation{State: r.snapshotState, Reason: r.snapshotReason}
}

// Close releases the isolate. Safe to call multiple times.
func (r *Runtime) Close() {
	if r == nil {
		return
	}
	r.closeOnce.Do(r.closeOwned)
}

func (r *Runtime) closeOwned() {
	r.closed.Store(true)
	r.cancel()
	r.ctxMu.Lock()
	var contexts []*Context
	r.contextRegistry.Range(func(_, value any) bool {
		contexts = append(contexts, value.(*Context))
		return true
	})
	r.ctxMu.Unlock()
	for _, owner := range contexts {
		owner.Close()
	}
	r.ctxMu.Lock()
	defer r.ctxMu.Unlock()
	if r.ctxPool != nil {
		// Drain pooled v8.Contexts so V8's internal handles are released
		// before Isolate.Dispose. Each pooled context has no live *js.Context
		// associated; just close the v8 side.
		close(r.ctxPool)
		for v8ctx := range r.ctxPool {
			v8ctx.Close()
		}
		r.ctxPool = nil
	}
	r.iso.Dispose()
	for _, store := range r.localStores {
		store.clear()
	}
	clear(r.localStores)
}

// Isolate returns the underlying v8go Isolate for advanced integrations.
// It returns nil after Runtime.Close. Call it outside an active V8 callback,
// and serialize use of the returned handle against Close.
func (r *Runtime) Isolate() *v8.Isolate {
	if r == nil {
		return nil
	}
	r.ctxMu.Lock()
	defer r.ctxMu.Unlock()
	if r.closed.Load() {
		return nil
	}
	r.isolateExposed.Store(true)
	return r.iso
}
