package js

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	v8 "github.com/tommie/v8go"

	"github.com/Christopher-Schulze/Artemis/network"
	"github.com/Christopher-Schulze/Artemis/webapi"
)

// Context binds a JS execution context to a webapi.Document. Each Context
// must be Closed when no longer needed.
type Context struct {
	rt                    *Runtime
	v8ctx                 *v8.Context
	doc                   *webapi.Document
	console               Console
	fetcher               FetchFunc
	getCookie             func() string
	setCookie             func(string)
	async                 *asyncChan
	asyncFetch            bool
	nodes                 *nodeTable
	localStorage          *memStorage
	sessionStorage        *memStorage
	timers                *timerQueue
	observers             *observerRegistry
	ws                    *wsRegistry
	policy                *network.Policy
	proxy                 *url.URL
	sessionID             string
	wsClientOnce          sync.Once
	wsClient              *http.Client
	styleMgr              *styleManager
	iframes               *iframeRegistry
	bootstraps            []bootstrapEntry
	bootstrapsSkipped     bool
	storageHandleIDs      []uint32 // handles into Runtime.storageHandles to free on Close
	fetchBodyIDs          []uint32 // handles into Runtime.fetchBodies to free on Close
	fetchBodyBytes        int64
	responseFactory       *v8.Function
	domExceptionClass     *v8.Function
	performanceOrigin     time.Time
	requestHeaderEncoder  *v8.Function
	fetchSignalFactory    *v8.Function
	responseTrue          *v8.Value
	responseFalse         *v8.Value
	callbackValues        []*v8.Value
	ownedValues           map[*Value]struct{}
	evalCtx               context.Context
	closed                atomic.Bool
	pageExposed           atomic.Bool
	closeOnce             sync.Once
	documentEventDispatch *v8.Function
	documentLoadingStep   int
	documentLoadingError  error
	pageStorage           *pageStorage
	ownsPageStorage       bool
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
	// CookieJar binds document.cookie to the owning HTTP session. Frame contexts
	// resolve their own document URL rather than inheriting the parent's cookies.
	// The jar must also implement DocumentCookies and SetDocumentCookies; opaque
	// HTTP-only jars cannot prove script authority and fail construction closed.
	CookieJar http.CookieJar
	// RequestContext is borrowed only during construction for stylesheet loads.
	// Subsequent network work uses the active Eval and context lifetime.
	RequestContext context.Context
	pageStorage    *pageStorage
	// LoadStylesheet fetches an external stylesheet by URL (resolved by
	// the caller). Nil rejects documents with active external stylesheets.
	LoadStylesheet StylesheetLoader
	// LoadIFrame fetches the HTML for an `<iframe src=...>` element.
	// Nil disables iframe content access.
	LoadIFrame IFrameLoader
	// Policy is the mandatory outbound network boundary for WebSocket
	// handshakes. Nil fails WebSocket creation closed.
	Policy *network.Policy
	// Proxy routes WebSocket handshakes through the session's validated
	// upstream proxy. Nil connects directly under Policy.
	Proxy *url.URL
	// SessionID owns retained localStorage origins and redacted network identity.
	SessionID string
}

// NewContext creates a context bound to doc. When the Runtime was built
// via NewRuntimeWithPool, NewContext may borrow a pristine v8.Context and
// rebind current page options. A realm exposed to page execution or native
// integrations is retired on Close; property deletion cannot reset its
// lexical environment or prove absence of prototype pollution.
// NewContext serialises isolate access via r.ctxMu against any other NewContext,
// Close, or Eval on the same Runtime. A Runtime's Contexts all share r.iso, and
// v8::Isolate is single-threaded: concurrent V8 work on one isolate (creation,
// GlobalHandles bookkeeping, or script execution) from two goroutines crashes the
// process. The lock is the single point that serialises all of it.
func (r *Runtime) NewContext(doc *webapi.Document, opts ContextOpts) (created *Context, returnErr error) {
	ctx, cancel := borrowContext(opts.RequestContext, r.lifetime)
	defer cancel()
	if err := r.ctxMu.lockContext(ctx, r.lifetime); err != nil {
		return nil, err
	}
	span := beginExecution(r, ctx, r.lifetime)
	defer func() {
		returnErr = errors.Join(returnErr, span.finish())
		r.ctxMu.Unlock()
		if returnErr != nil && created != nil {
			created.Close()
			created = nil
		}
	}()
	opts.RequestContext = ctx
	return r.newContextLocked(doc, opts)
}

// newContextLocked builds the Context assuming the caller already holds r.ctxMu.
// It is called by NewContext (which takes the lock) and by the iframe sub-Context
// builder invoked from a __iframe_load callback during Eval (which already holds
// the lock), so it must NOT re-acquire ctxMu.
func (r *Runtime) newContextLocked(doc *webapi.Document, opts ContextOpts) (*Context, error) {
	if r.closed.Load() {
		return nil, context.Canceled
	}
	if doc == nil {
		return nil, errors.New("nil document")
	}
	ownsPageStorage := opts.pageStorage == nil
	requestCtx := opts.RequestContext
	if requestCtx == nil {
		requestCtx = context.Background()
	}
	styleMgr, err := newStyleManager(requestCtx, doc, opts.LoadStylesheet)
	if err != nil {
		return nil, fmt.Errorf("document CSS: %w", err)
	}
	localStorage, sessionStorage, err := r.contextStorage(doc.URL(), &opts)
	if err != nil {
		return nil, err
	}
	console := opts.Console
	if console == nil {
		console = DiscardConsole{}
	}
	var v8ctx *v8.Context
	pooled := false
	if r.poolEnabled && !r.isolateExposed.Load() {
		select {
		case v8ctx = <-r.ctxPool:
			pooled = true
		default:
		}
	}
	if v8ctx == nil {
		if v8ctx, err = r.newV8Context(); err != nil {
			return nil, err
		}
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
		localStorage:      localStorage,
		sessionStorage:    sessionStorage,
		timers:            newTimerQueue(),
		observers:         newObserverRegistry(),
		asyncFetch:        opts.AsyncFetch,
		async:             newAsyncChan(r.lifetime),
		ws:                newWSRegistry(),
		policy:            opts.Policy,
		proxy:             opts.Proxy,
		sessionID:         opts.SessionID,
		styleMgr:          styleMgr,
		bootstrapsSkipped: bootstrapsCached,
		pageStorage:       opts.pageStorage,
		ownsPageStorage:   ownsPageStorage,
		evalCtx:           requestCtx,
		performanceOrigin: time.Now(),
	}
	complete := false
	defer func() {
		c.evalCtx = nil
		if !complete {
			r.contextRegistry.Delete(v8ctx)
			c.closed.Store(true)
			c.async.cancelInflight()
			c.releaseOwnedStorage()
		}
	}()
	if err := v8ctx.EnablePromiseRejectionTracking(); err != nil {
		v8ctx.Close()
		return nil, err
	}
	if opts.CookieJar != nil {
		var err error
		c.getCookie, c.setCookie, err = documentCookies(opts.CookieJar, doc.URL())
		if err != nil {
			v8ctx.Close()
			return nil, fmt.Errorf("document cookies: %w", err)
		}
	}
	opts.RequestContext = nil
	r.contextRegistry.Store(v8ctx, c)
	if r.execution != nil {
		r.execution.observePromiseContext(c)
	}
	// Sub-Context builder for iframes: build a fresh Context against the
	// iframe's document, sharing parent's runtime + opts (minus LoadIFrame
	// to avoid recursion of nested iframes for now).
	iframeBuilder := func(d *webapi.Document) (*Context, error) {
		subOpts := opts
		subOpts.LoadIFrame = nil
		subOpts.RequestContext = c.evalCtx
		// Runs from a __iframe_load callback during the parent's Eval, which
		// already holds r.ctxMu; use the locked variant to avoid re-acquiring it.
		return r.newContextLocked(d, subOpts)
	}
	c.iframes = newIFrameRegistry(opts.LoadIFrame, doc.URL(), iframeBuilder)

	// Pooled-reuse fast path: skip install* and flushBootstraps. All
	// template bindings on globalThis are still in place from the
	// previous use; their callbacks dispatch to *this* *js.Context via
	// r.contextRegistry which we updated above. JS-side state (mutated
	// globals, customElements registrations, history stack, performance
	// entries) gets cleared by __artemis_reset; storage objects are
	// re-bound to the fresh per-Context memStorage.
	if pooled {
		if err := resetPooledContext(c, doc.URL(), opts.Navigator); err != nil {
			r.contextRegistry.Delete(v8ctx)
			v8ctx.Close()
			return nil, fmt.Errorf("pool reset: %w", err)
		}
		if err := c.bindDocumentEventDispatcher(); err != nil {
			r.contextRegistry.Delete(v8ctx)
			v8ctx.Close()
			return nil, err
		}
		if err := c.bindResponseFactory(); err != nil {
			c.releaseBoundFunctions()
			v8ctx.Close()
			return nil, err
		}
		if err := c.bindDOMExceptionConstructor(); err != nil {
			c.releaseBoundFunctions()
			v8ctx.Close()
			return nil, err
		}
		complete = true
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
	if err := installPerformance(c); err != nil {
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
	c.registerBootstrap("artemis-fetch-response", fetchResponseBootstrap)
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
	if err := c.bindDocumentEventDispatcher(); err != nil {
		r.contextRegistry.Delete(v8ctx)
		v8ctx.Close()
		return nil, err
	}
	if err := c.bindResponseFactory(); err != nil {
		c.releaseBoundFunctions()
		v8ctx.Close()
		return nil, err
	}
	if err := c.bindDOMExceptionConstructor(); err != nil {
		c.releaseBoundFunctions()
		v8ctx.Close()
		return nil, err
	}
	// Seed the pristine global key set for __artemis_reset. After this
	// the Context is fully built and ready for first-time use OR for
	// pooled reuse later.
	if r.poolEnabled {
		if err := seedPoolReset(c); err != nil {
			c.releaseBoundFunctions()
			r.contextRegistry.Delete(v8ctx)
			v8ctx.Close()
			return nil, fmt.Errorf("seed pool reset: %w", err)
		}
	}
	complete = true
	return c, nil
}

// Close releases the context. Safe to call multiple times. When the
// Runtime has a non-empty Context pool, the underlying v8.Context is
// returned to the pool only if no page execution or native exposure occurred.
// The next NewContext rebinds that pristine realm. Per-Context Go-side
// resources (timers, observers, ws, fetch bodies, storage handles) are
// always freed eagerly here — only the v8 Context object is recycled.
func (c *Context) Close() {
	if c == nil {
		return
	}
	c.closeOnce.Do(c.closeOwned)
}

func (c *Context) closeOwned() {
	c.closed.Store(true)
	// Wake this owner's pending-fetch pump before descendants need ctxMu.
	if c.async != nil {
		c.async.cancelInflight()
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
	c.closeWebSocketTransport()
	if c.rt != nil {
		c.rt.ctxMu.Lock()
		defer c.rt.ctxMu.Unlock()
	}
	if c.async != nil {
		c.async.closeLocked()
	}
	if c.timers != nil {
		c.timers.close()
	}
	if c.observers != nil {
		c.observers.close()
	}
	c.releaseCallbackValues()
	c.releaseBoundFunctions()
	c.releaseOwnedStorage()
	c.invalidateEvalValuesLocked()
	if c.rt != nil {
		c.rt.contextRegistry.Delete(c.v8ctx)
		// Only an open runtime may retain an admitted context in its pool.
		if !c.rt.closed.Load() && !c.pageExposed.Load() && !c.rt.isolateExposed.Load() && c.rt.poolEnabled && c.rt.ctxPool != nil {
			select {
			case c.rt.ctxPool <- c.v8ctx:
				return
			default:
			}
		}
	}
	c.v8ctx.Close()
}

func (c *Context) ownEvalValueLocked(value *Value) {
	if c.ownedValues == nil {
		c.ownedValues = make(map[*Value]struct{})
	}
	value.owner.Store(c)
	c.ownedValues[value] = struct{}{}
}

func (c *Context) releaseEvalValueLocked(value *Value) {
	delete(c.ownedValues, value)
	raw := value.raw.Swap(nil)
	value.owner.Store(nil)
	if raw != nil {
		raw.Release()
	}
}

func (c *Context) invalidateEvalValuesLocked() {
	for value := range c.ownedValues {
		value.raw.Store(nil)
		value.owner.Store(nil)
	}
	clear(c.ownedValues)
	c.ownedValues = nil
}

func (c *Context) releaseBoundFunctions() {
	if c.domExceptionClass != nil {
		c.domExceptionClass.Release()
		c.domExceptionClass = nil
	}
	if c.documentEventDispatch != nil {
		c.documentEventDispatch.Release()
		c.documentEventDispatch = nil
	}
	if c.responseFactory != nil {
		c.responseFactory.Release()
		c.responseFactory = nil
	}
	if c.requestHeaderEncoder != nil {
		c.requestHeaderEncoder.Release()
		c.requestHeaderEncoder = nil
	}
	if c.fetchSignalFactory != nil {
		c.fetchSignalFactory.Release()
		c.fetchSignalFactory = nil
	}
	if c.responseTrue != nil {
		c.responseTrue.Release()
		c.responseTrue = nil
	}
	if c.responseFalse != nil {
		c.responseFalse.Release()
		c.responseFalse = nil
	}
}

func (c *Context) releaseOwnedStorage() {
	if c.rt != nil && c.rt.storageHandles != nil {
		for _, id := range c.storageHandleIDs {
			c.rt.storageHandles.remove(id)
		}
	}
	c.storageHandleIDs = nil
	if c.ownsPageStorage {
		for _, store := range c.pageStorage.local {
			store.clear()
		}
		for _, store := range c.pageStorage.session {
			store.clear()
		}
	}
	if storageOrigin(c.doc.URL()) == "" {
		c.localStorage.clear()
		c.sessionStorage.clear()
	}
	if c.rt != nil && c.rt.fetchBodies != nil {
		for _, id := range c.fetchBodyIDs {
			c.rt.fetchBodies.remove(id)
		}
	}
	c.fetchBodyIDs = nil
}

// Eval evaluates expr and returns the result. V8's default microtask
// policy (kAuto) drains promise continuations at script end. After that
// the timer queue is drained so setTimeout-scheduled callbacks fire too.
// Eval serialises V8 execution on the shared isolate. A Runtime's pooled Contexts
// all share r.iso and v8::Isolate is single-threaded; without this, two goroutines
// evaluating on different Contexts of the same Runtime race inside V8 and crash
// (SIGSEGV in cgo, e.g. mid-callback in Value.String).
func (c *Context) Eval(ctx context.Context, expr string) (*Value, error) {
	return c.execute(ctx, func() (*Value, error) { return c.evalLocked(expr) })
}

// EvalDiscard runs a script for its effects and retires the result while still
// owning the isolate. No live native value crosses this operation boundary.
func (c *Context) EvalDiscard(ctx context.Context, expr string) error {
	_, err := c.execute(ctx, func() (*Value, error) {
		value, err := c.evalLocked(expr)
		if value != nil {
			value.Release()
		}
		return nil, err
	})
	return err
}

// EvalString evaluates a string-producing script and copies its actual value
// while owning the isolate. The retained V8 value never leaves this boundary.
func (c *Context) EvalString(ctx context.Context, expr string) (string, error) {
	var result string
	_, err := c.execute(ctx, func() (*Value, error) {
		value, err := c.evalLocked(expr)
		if err != nil {
			return nil, err
		}
		if value == nil {
			return nil, errors.New("eval: string result required")
		}
		defer value.Release()
		if !value.IsString() {
			return nil, errors.New("eval: string result required")
		}
		result = value.String()
		return nil, nil
	})
	if err != nil {
		return "", err
	}
	return result, nil
}

// evalLocked runs the script plus its post-script drains/timer/mutation firing,
// assuming the caller already holds c.rt.ctxMu. It is called by Eval (which takes
// the lock) and by runIframeScriptsInCtx, which runs from a __iframe_load callback
// during the parent's Eval (already holding the lock), so it must NOT re-acquire
// ctxMu. iframe sub-Context creation triggered here goes through newContextLocked,
// also without re-locking.
func (c *Context) evalLocked(expr string) (*Value, error) {
	val, err := c.v8ctx.RunScript(expr, "<eval>")
	if cancellation := errors.Join(c.evalCtx.Err(), c.async.ctx.Err()); cancellation != nil {
		if val != nil {
			val.Release()
		}
		return nil, errors.Join(cancellation, formatJSError(err), c.styleMgr.takeFailure())
	}
	if c.async != nil {
		c.async.drain(c)
	}
	if c.ws != nil {
		c.ws.drain(c)
	}
	observerErr := c.fireMutationObservers()
	var timerErr error
	if observerErr == nil {
		timerErr = c.fireTimers()
		if timerErr == nil {
			observerErr = c.fireMutationObservers()
		}
	}
	if failure := errors.Join(formatJSError(err), observerErr, timerErr, c.takeDocumentEventErrors(), c.styleMgr.takeFailure()); failure != nil {
		if val != nil {
			val.Release()
		}
		return nil, failure
	}
	return newValue(val), nil
}

// Document returns the bound document.
func (c *Context) Document() *webapi.Document { return c.doc }

// V8Context exposes the underlying v8go context for advanced integrations.
// It returns nil after the Context or Runtime closes. Call it outside an
// active V8 callback, and serialize use of the returned handle against Close.
func (c *Context) V8Context() *v8.Context {
	if c == nil || c.rt == nil {
		return nil
	}
	c.rt.ctxMu.Lock()
	defer c.rt.ctxMu.Unlock()
	if c.closed.Load() || c.rt.closed.Load() {
		return nil
	}
	c.pageExposed.Store(true)
	return c.v8ctx
}

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
