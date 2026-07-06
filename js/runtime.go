// Package js wraps the V8 JavaScript engine (via rogchap.com/v8go) and
// the bridge that exposes Artemis DOM types to JS code. Phase 2 covers
// isolate management, per-page contexts, console capture, and a minimal
// read-only `document` binding. Mutation, events, Fetch, XHR, etc. land
// in TASK 004.
package js

import (
	"strings"
	"sync"

	v8 "rogchap.com/v8go"

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
	ctxMu            sync.Mutex
	closed           bool
	cache            *scriptCache
	cachedBootstraps map[string]string // key "all" or "tainted" -> concat
	hasSnapshot      bool              // V8 startup snapshot baked in

	// Context pool: when poolSize > 0, NewContext borrows v8.Contexts
	// from this channel instead of creating fresh ones. Close returns
	// the v8.Context to the pool (after the next __artemis_reset()
	// clears its JS state). Skips install*/flushBootstraps entirely on
	// reuse since all template bindings are Runtime-cached and resolve
	// per-call via contextRegistry. Sized at NewRuntimeWithPool time.
	ctxPool              chan *v8.Context
	poolSize             int
	poolEnabled          bool
	storageTemplates     *storageTemplates // cached templates for buildStorageCached
	storageHandles       *storageHandles   // handle table backing internal field 0
	locationTemplate     *v8.ObjectTemplate
	navigatorTemplate    *v8.ObjectTemplate
	timerTemplates       *timerTemplates // cached setTimeout/clearTimeout/setInterval/clearInterval
	consoleTemplates     *consoleTemplates
	domBridgeTemplates   *domBridgeTemplates
	observerTemplates    *observerTemplates
	cryptoAES            *cryptoAESTemplates
	cryptoSubtle         *cryptoSubtleTemplates
	cryptoComplete       *cryptoCompleteTemplates
	cryptoAsymmetric     *cryptoAsymmetricTemplates
	cryptoExtra          *cryptoExtraTemplates
	cryptoPKCS8          *cryptoPKCS8Templates
	iframeTemplates      *iframeTemplates
	fetchTemplates       *fetchTemplates
	fetchBodies          *fetchBodyHandles
	urlHelperTemplate    *v8.FunctionTemplate
	wsTemplates          *wsTemplates
	extrasV2             *extrasV2Templates
	cascadeStyleTemplate *v8.FunctionTemplate
	contextRegistry      sync.Map // *v8.Context -> *Context, populated by NewContext, drained by Close
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
// snapshot blob is absent at build time (rare; see snapshot_data.go),
// falls back to the from-scratch isolate path.
func NewRuntime() *Runtime {
	return newRuntime(0)
}

// NewRuntimeWithPool creates a Runtime with a v8.Context pool of the
// given size. Pooled Contexts are reused across pages: each Close
// returns the v8.Context to the pool, each NewContext takes one out
// and runs `__artemis_reset()` to clear JS-side state instead of
// re-running the full install* + bootstrap pipeline. Reduces wall
// time per NewContext by ~30% on agent-flow pages where scripts do
// not heavily mutate the global state. Set size to 0 to disable.
//
// Caveats: user scripts that mutate built-in prototypes (e.g.
// Array.prototype.foo = ...) leak across pooled pages. Use a
// non-pooled Runtime when running untrusted JS or full-isolation is
// required.
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
	var iso *v8.Isolate
	hasSnapshot := len(snapshotBlob) > 0
	if hasSnapshot {
		iso = v8.NewIsolateFromSnapshot(snapshotBlob)
	} else {
		iso = v8.NewIsolate()
	}
	r := &Runtime{iso: iso, cache: newScriptCache(), hasSnapshot: hasSnapshot}
	if poolSize > 0 {
		r.ctxPool = make(chan *v8.Context, poolSize)
		r.poolSize = poolSize
		r.poolEnabled = true
	}
	return r
}

// Close releases the isolate. Safe to call multiple times.
func (r *Runtime) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
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
	r.closed = true
}

// Isolate returns the underlying v8go Isolate. Intended for advanced
// integrations.
func (r *Runtime) Isolate() *v8.Isolate { return r.iso }
