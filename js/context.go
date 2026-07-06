package js

import (
	"context"
	"errors"
	"fmt"
	"strings"

	v8 "rogchap.com/v8go"

	"github.com/Christopher-Schulze/Artemis/webapi"
)

// Context binds a JS execution context to a webapi.Document. Each Context
// must be Closed when no longer needed.
type Context struct {
	rt                *Runtime
	v8ctx             *v8.Context
	doc               *webapi.Document
	console           Console
	fetcher           FetchFunc
	getCookie         func() string
	setCookie         func(string)
	async             *asyncChan
	asyncFetch        bool
	nodes             *nodeTable
	localStorage      *memStorage
	sessionStorage    *memStorage
	timers            *timerQueue
	observers         *observerRegistry
	ws                *wsRegistry
	styleMgr          *styleManager
	iframes           *iframeRegistry
	bootstraps        []bootstrapEntry
	bootstrapsSkipped bool
	storageHandleIDs  []uint32 // handles into Runtime.storageHandles to free on Close
	fetchBodyIDs      []uint32 // handles into Runtime.fetchBodies to free on Close
	closed            bool
}

// registerBootstrap queues a JS bootstrap source. Sources are flushed as
// a single concatenated RunScript at the end of NewContext to amortise
// cgo crossing cost. Each source must be wrapped in an IIFE or otherwise
// avoid top-level identifier collisions across registrations.
//
// Hot-path optimisation: once Runtime has cached the combined string for
// this snapshot mode, every Context registers the same set of bootstraps
// in the same order. Storing 22 entries per Context only to discard them
// after concatenation is wasted work, so on cache hit we drop the entry
// entirely and rely on flushBootstraps to re-run the cached string.
func (c *Context) registerBootstrap(name, source string) {
	if c.bootstrapsSkipped {
		return
	}
	c.bootstraps = append(c.bootstraps, bootstrapEntry{name: name, source: source})
}

// flushBootstraps runs queued bootstraps as a single concatenated script
// via the per-Runtime UnboundScript cache. The concatenation is done
// exactly once per Runtime (cached on Runtime.cachedBootstrap) because
// every Context registers the same set of bootstraps in the same order.
//
// When a V8 startup snapshot is loaded (Runtime.hasSnapshot), only the
// tainted bootstraps run -- those whose closures capture cross-
// bootstrap functions and so cannot be safely frozen at snapshot time.
// Non-tainted bootstraps are already baked into every new Context's
// default state by the snapshot; re-running them would just be wasted
// cycles.
func (c *Context) flushBootstraps() error {
	cacheKey := "all"
	if c.rt.hasSnapshot {
		cacheKey = "tainted"
	}
	c.rt.mu.Lock()
	combined := c.rt.cachedBootstraps[cacheKey]
	if combined == "" {
		if len(c.bootstraps) == 0 {
			c.rt.mu.Unlock()
			return nil
		}
		var b strings.Builder
		for _, e := range c.bootstraps {
			if c.rt.hasSnapshot && !taintedBootstrapNames[e.name] {
				continue
			}
			b.WriteString("//#")
			b.WriteString(e.name)
			b.WriteByte('\n')
			b.WriteString(e.source)
			b.WriteString(";\n")
		}
		combined = b.String()
		if c.rt.cachedBootstraps == nil {
			c.rt.cachedBootstraps = make(map[string]string, 2)
		}
		c.rt.cachedBootstraps[cacheKey] = combined
	}
	c.rt.mu.Unlock()
	c.bootstraps = nil
	if combined == "" {
		return nil
	}
	_, err := c.rt.cache.run(c.rt.iso, c.v8ctx, "<artemis-batched-bootstrap>", combined)
	return err
}

// ContextOpts configures a JS Context.
type ContextOpts struct {
	// Console sinks console.* output. Nil means discard.
	Console Console
	// Fetch performs HTTP requests for the JS fetch() global. Nil disables
	// fetch (calls throw).
	Fetch FetchFunc
	// AsyncFetch routes fetch() through a goroutine so multiple
	// concurrent fetch() calls run in parallel. The Promise returned to
	// JS stays pending until the goroutine posts its result back to the
	// V8 thread (drained between scripts and via Context.WaitIdle).
	// Default false preserves the simple sync-resolved behavior.
	AsyncFetch bool
	// Navigator overrides navigator.* values; defaults are applied for
	// any zero-value fields.
	Navigator NavigatorConfig
	// GetCookie returns the cookie string for `document.cookie`.
	// Nil returns the empty string.
	GetCookie func() string
	// SetCookie is invoked when JS assigns to `document.cookie`. The
	// value is a single cookie attribute set per Set-Cookie semantics.
	// Nil makes the setter a no-op.
	SetCookie func(string)
	// LoadStylesheet fetches an external stylesheet by URL (resolved by
	// the caller). Nil disables external stylesheet loading.
	LoadStylesheet StylesheetLoader
	// LoadIFrame fetches the HTML for an `<iframe src=...>` element.
	// Nil disables iframe content access.
	LoadIFrame IFrameLoader
}

// NewContext creates a context bound to doc. When the Runtime was built
// via NewRuntimeWithPool, NewContext borrows a v8.Context from the pool
// if one is available and runs `__artemis_reset()` instead of the full
// install* + bootstrap pipeline; this skips ~30% of the per-Context
// CPU cost. State that persists across pages (custom-element
// definitions, mutations to built-in prototypes) is the caller's
// responsibility.
// NewContext serialises isolate access via r.ctxMu against any other NewContext,
// Close, or Eval on the same Runtime. A Runtime's Contexts all share r.iso, and
// v8::Isolate is single-threaded: concurrent V8 work on one isolate (creation,
// GlobalHandles bookkeeping, or script execution) from two goroutines crashes the
// process. The lock is the single point that serialises all of it.
func (r *Runtime) NewContext(doc *webapi.Document, opts ContextOpts) (*Context, error) {
	r.ctxMu.Lock()
	defer r.ctxMu.Unlock()
	return r.newContextLocked(doc, opts)
}

// newContextLocked builds the Context assuming the caller already holds r.ctxMu.
// It is called by NewContext (which takes the lock) and by the iframe sub-Context
// builder invoked from a __iframe_load callback during Eval (which already holds
// the lock), so it must NOT re-acquire ctxMu.
func (r *Runtime) newContextLocked(doc *webapi.Document, opts ContextOpts) (*Context, error) {
	if doc == nil {
		return nil, errors.New("nil document")
	}
	console := opts.Console
	if console == nil {
		console = DiscardConsole{}
	}
	var v8ctx *v8.Context
	pooled := false
	if r.poolEnabled {
		select {
		case v8ctx = <-r.ctxPool:
			pooled = true
		default:
			v8ctx = v8.NewContext(r.iso)
		}
	} else {
		v8ctx = v8.NewContext(r.iso)
	}
	cacheKey := "all"
	if r.hasSnapshot {
		cacheKey = "tainted"
	}
	r.mu.Lock()
	bootstrapsCached := r.cachedBootstraps[cacheKey] != ""
	r.mu.Unlock()
	c := &Context{
		rt:                r,
		v8ctx:             v8ctx,
		doc:               doc,
		console:           console,
		fetcher:           opts.Fetch,
		getCookie:         opts.GetCookie,
		setCookie:         opts.SetCookie,
		nodes:             newNodeTable(),
		localStorage:      newMemStorage(),
		sessionStorage:    newMemStorage(),
		timers:            newTimerQueue(),
		observers:         newObserverRegistry(),
		asyncFetch:        opts.AsyncFetch,
		async:             newAsyncChan(),
		ws:                newWSRegistry(),
		styleMgr:          newStyleManager(doc, opts.LoadStylesheet),
		bootstrapsSkipped: bootstrapsCached,
	}
	r.contextRegistry.Store(v8ctx, c)
	// Sub-Context builder for iframes: build a fresh Context against the
	// iframe's document, sharing parent's runtime + opts (minus LoadIFrame
	// to avoid recursion of nested iframes for now).
	iframeBuilder := func(d *webapi.Document) (*Context, error) {
		subOpts := opts
		subOpts.LoadIFrame = nil
		// Runs from a __iframe_load callback during the parent's Eval, which
		// already holds r.ctxMu; use the locked variant to avoid re-acquiring it.
		return r.newContextLocked(d, subOpts)
	}
	var ifLoader func(string) ([]byte, error)
	if opts.LoadIFrame != nil {
		ifLoader = func(s string) ([]byte, error) { return opts.LoadIFrame(s) }
	}
	c.iframes = newIFrameRegistry(ifLoader, doc.URL(), iframeBuilder)

	// Pooled-reuse fast path: skip install* and flushBootstraps. All
	// template bindings on globalThis are still in place from the
	// previous use; their callbacks dispatch to *this* *js.Context via
	// r.contextRegistry which we updated above. JS-side state (mutated
	// globals, customElements registrations, history stack, performance
	// entries) gets cleared by __artemis_reset; storage objects are
	// re-bound to the fresh per-Context memStorage.
	if pooled {
		if err := resetPooledContext(c, doc.URL()); err != nil {
			r.contextRegistry.Delete(v8ctx)
			v8ctx.Close()
			return nil, fmt.Errorf("pool reset: %w", err)
		}
		return c, nil
	}

	if err := installConsole(r.iso, v8ctx, c); err != nil {
		v8ctx.Close()
		return nil, fmt.Errorf("install console: %w", err)
	}
	if err := installDocument(r.iso, v8ctx, c); err != nil {
		v8ctx.Close()
		return nil, fmt.Errorf("install document: %w", err)
	}
	if err := installFetch(r.iso, v8ctx, c); err != nil {
		v8ctx.Close()
		return nil, fmt.Errorf("install fetch: %w", err)
	}
	if err := installWindow(r.iso, v8ctx, c, doc.URL(), opts.Navigator); err != nil {
		v8ctx.Close()
		return nil, fmt.Errorf("install window: %w", err)
	}
	if err := installExtras(r.iso, v8ctx, c); err != nil {
		v8ctx.Close()
		return nil, fmt.Errorf("install extras: %w", err)
	}
	if err := installExtrasV2(r.iso, v8ctx, c); err != nil {
		v8ctx.Close()
		return nil, fmt.Errorf("install extras v2: %w", err)
	}
	if err := installCryptoSubtle(r.iso, v8ctx, c); err != nil {
		v8ctx.Close()
		return nil, fmt.Errorf("install crypto.subtle: %w", err)
	}
	if err := installPerformance(r.iso, v8ctx); err != nil {
		v8ctx.Close()
		return nil, fmt.Errorf("install performance: %w", err)
	}
	if err := installMutationObserver(r.iso, v8ctx, c); err != nil {
		v8ctx.Close()
		return nil, fmt.Errorf("install mutation observer: %w", err)
	}
	if err := installWebAPIMore(r.iso, v8ctx, c); err != nil {
		v8ctx.Close()
		return nil, fmt.Errorf("install webapi-more: %w", err)
	}
	if err := installWebAPIV3(r.iso, v8ctx, c); err != nil {
		v8ctx.Close()
		return nil, fmt.Errorf("install webapi-v3: %w", err)
	}
	if err := installWebSocket(r.iso, v8ctx, c); err != nil {
		v8ctx.Close()
		return nil, fmt.Errorf("install websocket: %w", err)
	}
	if err := installNavigatorExtras(r.iso, v8ctx, c); err != nil {
		v8ctx.Close()
		return nil, fmt.Errorf("install navigator extras: %w", err)
	}
	if err := installCryptoAES(r.iso, v8ctx, c); err != nil {
		v8ctx.Close()
		return nil, fmt.Errorf("install crypto aes: %w", err)
	}
	if err := installCryptoAsymmetric(r.iso, v8ctx, c); err != nil {
		v8ctx.Close()
		return nil, fmt.Errorf("install crypto asym: %w", err)
	}
	if err := installCryptoComplete(r.iso, v8ctx, c); err != nil {
		v8ctx.Close()
		return nil, fmt.Errorf("install crypto complete: %w", err)
	}
	if err := installCryptoExtra(r.iso, v8ctx, c); err != nil {
		v8ctx.Close()
		return nil, fmt.Errorf("install crypto extra: %w", err)
	}
	if err := installCryptoPKCS8(r.iso, v8ctx, c); err != nil {
		v8ctx.Close()
		return nil, fmt.Errorf("install crypto pkcs8: %w", err)
	}
	if err := installInputProps(r.iso, v8ctx, c); err != nil {
		v8ctx.Close()
		return nil, fmt.Errorf("install input props: %w", err)
	}
	if err := installIframe(r.iso, v8ctx, c); err != nil {
		v8ctx.Close()
		return nil, fmt.Errorf("install iframe: %w", err)
	}
	if err := installStyleManagerBridge(r.iso, v8ctx, c); err != nil {
		v8ctx.Close()
		return nil, fmt.Errorf("install style manager: %w", err)
	}
	if err := installParityGaps(r.iso, v8ctx, c); err != nil {
		v8ctx.Close()
		return nil, fmt.Errorf("install parity gaps: %w", err)
	}
	// Pool-reset bootstrap registers `__artemis_reset(url)` JS function +
	// captures the pristine globalThis surface. Always installed (cheap)
	// so reuse is opt-in at Runtime construction without per-Context
	// branching here.
	if err := installPoolReset(c); err != nil {
		v8ctx.Close()
		return nil, fmt.Errorf("install pool reset: %w", err)
	}
	// All install functions have queued their bootstraps via
	// registerBootstrap; flush them as a single concatenated RunScript.
	// This collapses ~16 cgo crossings into one.
	if err := c.flushBootstraps(); err != nil {
		v8ctx.Close()
		return nil, fmt.Errorf("flush bootstraps: %w", err)
	}
	// Seed the pristine global key set for __artemis_reset. After this
	// the Context is fully built and ready for first-time use OR for
	// pooled reuse later.
	if r.poolEnabled {
		if err := seedPoolReset(c); err != nil {
			v8ctx.Close()
			return nil, fmt.Errorf("seed pool reset: %w", err)
		}
	}
	return c, nil
}

// Close releases the context. Safe to call multiple times. When the
// Runtime has a non-empty Context pool, the underlying v8.Context is
// returned to the pool instead of disposed; the next NewContext call
// will reuse it after running __artemis_reset(). Per-Context Go-side
// resources (timers, observers, ws, fetch bodies, storage handles) are
// always freed eagerly here — only the v8 Context object is recycled.
func (c *Context) Close() {
	if c == nil || c.closed {
		return
	}
	// Iframe sub-Contexts close themselves (each acquires ctxMu); do
	// this BEFORE we acquire ctxMu so we don't deadlock with the
	// recursive sub-Close path.
	if c.iframes != nil {
		c.iframes.closeAll()
	}
	// Cancel every open WebSocket and tear down its read goroutine
	// before we release the *js.Context. Without this, the per-conn
	// reader keeps pushing events into c.ws.events after the JS-side
	// is gone, which can leak goroutines and (in pooled mode) deliver
	// stale events to a freshly-built Context that recycled the same
	// v8.Context.
	if c.ws != nil {
		c.ws.closeAll()
	}
	// Drain async fetch in-flight: cancel pending resolutions and
	// release reference to the resolver so the goroutine can exit
	// cleanly without writing into a closed channel.
	if c.async != nil {
		c.async.cancelInflight()
	}
	if c.rt != nil {
		c.rt.ctxMu.Lock()
		defer c.rt.ctxMu.Unlock()
		// Re-check under the lock — another goroutine might have
		// closed us between the early-out and the lock acquire.
		if c.closed {
			return
		}
	}
	if c.rt != nil && c.rt.storageHandles != nil {
		for _, id := range c.storageHandleIDs {
			c.rt.storageHandles.remove(id)
		}
	}
	c.storageHandleIDs = nil
	if c.rt != nil && c.rt.fetchBodies != nil {
		for _, id := range c.fetchBodyIDs {
			c.rt.fetchBodies.remove(id)
		}
	}
	c.fetchBodyIDs = nil
	if c.rt != nil {
		c.rt.contextRegistry.Delete(c.v8ctx)
		// Try to return the v8.Context to the pool. Non-blocking: if the
		// pool is full or disabled, fall through to v8ctx.Close().
		if c.rt.poolEnabled && c.rt.ctxPool != nil {
			select {
			case c.rt.ctxPool <- c.v8ctx:
				c.closed = true
				return
			default:
			}
		}
	}
	c.v8ctx.Close()
	c.closed = true
}

// Eval evaluates expr and returns the result. V8's default microtask
// policy (kAuto) drains promise continuations at script end. After that
// the timer queue is drained so setTimeout-scheduled callbacks fire too.
// Eval serialises V8 execution on the shared isolate. A Runtime's pooled Contexts
// all share r.iso and v8::Isolate is single-threaded; without this, two goroutines
// evaluating on different Contexts of the same Runtime race inside V8 and crash
// (SIGSEGV in cgo, e.g. mid-callback in Value.String).
func (c *Context) Eval(_ context.Context, expr string) (*Value, error) {
	if c.closed {
		return nil, errors.New("eval on closed context")
	}
	c.rt.ctxMu.Lock()
	defer c.rt.ctxMu.Unlock()
	return c.evalLocked(expr)
}

// evalLocked runs the script plus its post-script drains/timer/mutation firing,
// assuming the caller already holds c.rt.ctxMu. It is called by Eval (which takes
// the lock) and by runIframeScriptsInCtx, which runs from a __iframe_load callback
// during the parent's Eval (already holding the lock), so it must NOT re-acquire
// ctxMu. iframe sub-Context creation triggered here goes through newContextLocked,
// also without re-locking.
func (c *Context) evalLocked(expr string) (*Value, error) {
	val, err := c.v8ctx.RunScript(expr, "<eval>")
	if c.async != nil {
		c.async.drain(c)
	}
	if c.ws != nil {
		c.ws.drain(c)
	}
	c.fireMutationObservers()
	c.fireTimers()
	c.fireMutationObservers()
	if err != nil {
		return nil, formatJSError(err)
	}
	return newValue(val), nil
}

// Document returns the bound document.
func (c *Context) Document() *webapi.Document { return c.doc }

// V8Context exposes the underlying v8go context. Intended for advanced
// integrations.
func (c *Context) V8Context() *v8.Context { return c.v8ctx }

// HandleFor returns the JS handle for n, registering it if new. The
// handle is stable for the lifetime of the Context.
func (c *Context) HandleFor(n *webapi.Node) uint32 {
	if c == nil || c.nodes == nil {
		return 0
	}
	return c.nodes.Handle(n)
}

func formatJSError(err error) error {
	if err == nil {
		return nil
	}
	var jsErr *v8.JSError
	if errors.As(err, &jsErr) {
		return fmt.Errorf("js error: %s\n%s", jsErr.Message, jsErr.StackTrace)
	}
	return fmt.Errorf("js error: %w", err)
}
