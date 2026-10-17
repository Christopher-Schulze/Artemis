package js

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strings"
	"sync"

	v8 "github.com/tommie/v8go"

	"github.com/Christopher-Schulze/Artemis/network"
	"github.com/Christopher-Schulze/Artemis/parser"
	"github.com/Christopher-Schulze/Artemis/webapi"
)

const (
	maxIFrameContexts     = 64
	maxIFrameSourceBytes  = 16 << 20
	maxIFrameMessages     = 256
	maxIFrameMessageBytes = 1 << 20
)

// iframeRegistry holds the parsed sub-document AND its dedicated
// js.Context for each <iframe>. Sub-Contexts share the parent's V8
// Isolate but each has its own globals (window, document, localStorage,
// timers, observers). Iframe scripts run in their own Context so they
// cannot pollute parent globals.
type iframeRegistry struct {
	mu              sync.Mutex
	docs            map[uint32]*webapi.Document
	ctxs            map[uint32]*Context
	loader          IFrameLoader
	baseURL         string
	subContextBuild func(doc *webapi.Document) (*Context, error)
	pendingMsgs     map[uint32][]string
	failures        map[uint32]error
	sourceBytes     int
	messageBytes    int
	messageCount    int
}

func newIFrameRegistry(loader IFrameLoader, baseURL string,
	build func(doc *webapi.Document) (*Context, error)) *iframeRegistry {
	return &iframeRegistry{
		docs:            make(map[uint32]*webapi.Document),
		ctxs:            make(map[uint32]*Context),
		loader:          loader,
		baseURL:         baseURL,
		subContextBuild: build,
		pendingMsgs:     make(map[uint32][]string),
		failures:        make(map[uint32]error),
	}
}

func (r *iframeRegistry) load(handle uint32, src string, parent *Context) *webapi.Document {
	r.mu.Lock()
	if err := r.failures[handle]; err != nil {
		r.mu.Unlock()
		if span := parent.rt.execution; span != nil {
			span.recordFailure(err)
		}
		return nil
	}
	if d, ok := r.docs[handle]; ok {
		r.mu.Unlock()
		return d
	}
	if len(r.docs)+len(r.failures) >= maxIFrameContexts {
		r.mu.Unlock()
		if span := parent.rt.execution; span != nil {
			span.recordFailure(errors.New("iframe: context admission limit exceeded"))
		}
		return nil
	}
	r.mu.Unlock()
	if r.loader == nil {
		return nil
	}
	base, err := url.Parse(r.baseURL)
	if err != nil {
		r.recordFailure(handle, fmt.Errorf("iframe base URL: %w", err), parent)
		return nil
	}
	reference, err := url.Parse(src)
	if err != nil {
		r.recordFailure(handle, fmt.Errorf("iframe source URL: %w", err), parent)
		return nil
	}
	src = base.ResolveReference(reference).String()
	ctx, cancel := parent.fetchContext()
	defer cancel()
	response, err := r.loader(ctx, src)
	if errors.Is(err, network.ErrAdBlocked) {
		// A blocked frame stays empty, as in a browser; no page failure.
		return nil
	}
	if err != nil {
		r.recordFailure(handle, fmt.Errorf("iframe load: %w", err), parent)
		return nil
	}
	if response == nil {
		r.recordFailure(handle, errors.New("iframe: missing loader response"), parent)
		return nil
	}
	if storageOrigin(response.URL) == "" {
		r.recordFailure(handle, errors.New("iframe: valid final HTTP URL required"), parent)
		return nil
	}
	limit := maxIFrameSourceBytes - r.sourceBytes
	if parent.policy != nil {
		limit = int(min(int64(limit), parent.policy.Config().MaxResponseBodyBytes))
	}
	if len(response.Body) > limit {
		r.recordFailure(handle, errors.New("iframe: retained source byte limit exceeded"), parent)
		return nil
	}
	if err := ctx.Err(); err != nil {
		r.recordFailure(handle, err, parent)
		return nil
	}
	doc, err := parser.ParseHTML(bytes.NewReader(response.Body), response.URL)
	if err != nil {
		r.recordFailure(handle, fmt.Errorf("iframe parse: %w", err), parent)
		return nil
	}
	r.mu.Lock()
	r.docs[handle] = doc
	r.sourceBytes += len(response.Body)
	r.mu.Unlock()
	// Create the iframe's own JS Context so scripts run in isolation.
	if r.subContextBuild != nil {
		subCtx, err := r.subContextBuild(doc)
		if err != nil {
			r.recordFailure(handle, err, parent)
			return nil
		}
		if subCtx == nil {
			r.recordFailure(handle, errors.New("iframe: missing child context"), parent)
			return nil
		}
		r.mu.Lock()
		r.ctxs[handle] = subCtx
		r.mu.Unlock()
		if err := runIframeScriptsInCtx(ctx, subCtx, doc); err != nil {
			r.recordFailure(handle, err, parent)
			return nil
		}
	}
	return doc
}

func (r *iframeRegistry) recordFailure(handle uint32, err error, parent *Context) {
	r.mu.Lock()
	r.failures[handle] = err
	delete(r.docs, handle)
	r.mu.Unlock()
	if span := parent.rt.execution; span != nil {
		span.recordFailure(err)
	}
}

func (r *iframeRegistry) subContext(handle uint32) *Context {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ctxs[handle]
}

func (r *iframeRegistry) queueMessage(handle uint32, payload string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ctxs[handle] == nil || r.docs[handle] == nil {
		return errors.New("iframe: loaded child required for message delivery")
	}
	if len(payload) > maxIFrameMessageBytes-r.messageBytes || r.messageCount >= maxIFrameMessages {
		return errors.New("iframe: pending message limit exceeded")
	}
	r.pendingMsgs[handle] = append(r.pendingMsgs[handle], payload)
	r.messageBytes += len(payload)
	r.messageCount++
	return nil
}

func (r *iframeRegistry) drainMessagesFor(handle uint32) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.pendingMsgs[handle]
	delete(r.pendingMsgs, handle)
	for _, payload := range out {
		r.messageBytes -= len(payload)
		r.messageCount--
	}
	return out
}

func (r *iframeRegistry) closeAll() {
	r.mu.Lock()
	ctxs := make([]*Context, 0, len(r.ctxs))
	for _, c := range r.ctxs {
		ctxs = append(ctxs, c)
	}
	r.ctxs = make(map[uint32]*Context)
	clear(r.docs)
	clear(r.failures)
	clear(r.pendingMsgs)
	r.sourceBytes, r.messageBytes, r.messageCount = 0, 0, 0
	r.mu.Unlock()
	for _, c := range ctxs {
		c.Close()
	}
}

// IFrameResponse retains the final URL so redirects cannot change frame
// origin without changing document/cookie/storage identity.
type IFrameResponse struct {
	URL  string
	Body []byte
}

// IFrameLoader fetches frame HTML under the active evaluation/lifetime context.
type IFrameLoader func(ctx context.Context, url string) (*IFrameResponse, error)

// runIframeScriptsInCtx walks doc and evaluates every inline <script>
// in the iframe's own sub-Context.
func runIframeScriptsInCtx(ctx context.Context, subCtx *Context, doc *webapi.Document) error {
	return subCtx.executeNested(ctx, func() error { return runIframeScriptsLocked(subCtx, doc) })
}

func runIframeScriptsLocked(subCtx *Context, doc *webapi.Document) error {
	root := doc.Root()
	if root == nil {
		return nil
	}
	var scriptErr error
	webapi.Walk(root, func(n *webapi.Node) webapi.WalkAction {
		if n.Type() != webapi.NodeElement || n.Tag() != "script" {
			return webapi.WalkContinue
		}
		if _, hasSrc := n.Attr("src"); hasSrc {
			return webapi.WalkContinue
		}
		code := n.Text()
		if strings.TrimSpace(code) == "" {
			return webapi.WalkContinue
		}
		// Runs from a __iframe_load callback during the parent's Eval, which holds
		// r.ctxMu (shared across the Runtime's Contexts); use the locked variant so
		// we do not re-acquire the non-reentrant lock and deadlock.
		value, err := subCtx.evalLocked(code)
		if value != nil {
			value.Release()
		}
		if err != nil {
			scriptErr = err
			return webapi.WalkStop
		}
		return webapi.WalkContinue
	})
	return scriptErr
}

// iframeTemplates caches the 3 iframe trampolines at Runtime level.
// Per-Context state (c.nodes, c.iframes) is resolved via Runtime.contextFor.
type iframeTemplates struct {
	load, root, post *v8.FunctionTemplate
}

func (r *Runtime) ensureIframeTemplates() *iframeTemplates {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.iframeTemplates != nil {
		return r.iframeTemplates
	}
	iso := r.iso
	r.iframeTemplates = &iframeTemplates{
		load: v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			defer info.Release()
			c := r.contextFor(info.Context())
			args := info.Args()
			if c == nil || len(args) < 1 {
				return r.callbackPrimitive(info, false)
			}
			identity, err := mutationInteger(args[0], math.MaxUint32)
			if err != nil {
				return domCallbackResult(iso, domCallbackError(iso, err))
			}
			handle := uint32(identity)
			n := c.nodes.Get(handle)
			if n == nil || n.Tag() != "iframe" {
				return r.callbackPrimitive(info, false)
			}
			src, _ := n.Attr("src")
			if src == "" {
				return r.callbackPrimitive(info, false)
			}
			doc := c.iframes.load(handle, src, c)
			return r.callbackPrimitive(info, doc != nil)
		}),
		root: v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			defer info.Release()
			c := r.contextFor(info.Context())
			args := info.Args()
			if c == nil || len(args) < 1 {
				return r.callbackPrimitive(info, int32(0))
			}
			identity, err := mutationInteger(args[0], math.MaxUint32)
			if err != nil {
				return domCallbackResult(iso, domCallbackError(iso, err))
			}
			handle := uint32(identity)
			c.iframes.mu.Lock()
			doc, ok := c.iframes.docs[handle]
			c.iframes.mu.Unlock()
			if !ok || doc == nil || doc.Root() == nil {
				return r.callbackPrimitive(info, int32(0))
			}
			origin := storageOrigin(c.doc.URL())
			if origin == "" || origin != storageOrigin(doc.URL()) {
				return r.callbackPrimitive(info, int32(0))
			}
			// Register the iframe doc's root in PARENT's nodeTable so parent
			// JS can __wrap it. Mutations via parent or iframe both touch
			// the same Go-side *html.Node, so DOM state stays consistent.
			return domCallbackResult(iso, nodeHandleValue(iso, c.nodes.Handle(doc.Root())))
		}),
		// Cross-context postMessage: parent's contentWindow.postMessage
		// pushes data into the iframe's pending message queue. The iframe
		// sub-Context drains the queue immediately and dispatches a
		// MessageEvent on its window.
		post: v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			defer info.Release()
			c := r.contextFor(info.Context())
			args := info.Args()
			if c == nil || len(args) < 2 {
				return v8.Null(iso)
			}
			identity, err := mutationInteger(args[0], math.MaxUint32)
			if err != nil {
				return domCallbackResult(iso, domCallbackError(iso, err))
			}
			if !args[1].IsString() {
				return domCallbackResult(iso, domCallbackError(iso, errors.New("iframe: serialized JSON string required")))
			}
			payload, err := args[1].StringWithLimit(maxIFrameMessageBytes)
			if err != nil {
				return domCallbackResult(iso, domCallbackError(iso, err))
			}
			if !json.Valid([]byte(payload)) {
				return domCallbackResult(iso, domCallbackError(iso, errors.New("iframe: invalid serialized JSON message")))
			}
			handle := uint32(identity)
			targetOrigin := storageOrigin(c.doc.URL())
			if len(args) > 2 {
				if !args[2].IsString() {
					return domCallbackResult(iso, domCallbackError(iso, errors.New("iframe: target origin string required")))
				}
				target, err := args[2].StringWithLimit(maxIFrameMessageBytes)
				if err != nil {
					return domCallbackResult(iso, domCallbackError(iso, err))
				}
				if target == "*" {
					targetOrigin = "*"
				} else if target != "/" {
					targetOrigin = storageOrigin(target)
					if targetOrigin == "" {
						return domCallbackResult(iso, domCallbackError(iso, errors.New("iframe: invalid target origin URL")))
					}
				}
			}
			c.iframes.mu.Lock()
			doc := c.iframes.docs[handle]
			c.iframes.mu.Unlock()
			if doc == nil {
				return domCallbackResult(iso, domCallbackError(iso, errors.New("iframe: loaded child required for message delivery")))
			}
			if targetOrigin != "*" && (targetOrigin == "" || targetOrigin != storageOrigin(doc.URL())) {
				return v8.Null(iso)
			}
			if err := c.iframes.queueMessage(handle, payload); err != nil {
				return domCallbackResult(iso, domCallbackError(iso, err))
			}
			if err := dispatchPendingIFrameMessages(c.evalCtx, c.iframes, handle); err != nil {
				return domCallbackResult(iso, domCallbackError(iso, err))
			}
			return v8.Null(iso)
		}),
	}
	return r.iframeTemplates
}

func installIframe(iso *v8.Isolate, v8ctx *v8.Context, c *Context) error {
	t := c.rt.ensureIframeTemplates()
	g := v8ctx.Global()
	defer g.Release()
	load, root, post := t.load.GetFunction(v8ctx), t.root.GetFunction(v8ctx), t.post.GetFunction(v8ctx)
	defer load.Release()
	defer root.Release()
	defer post.Release()
	if err := g.Set("__iframe_load", load); err != nil {
		return err
	}
	if err := g.Set("__iframe_root", root); err != nil {
		return err
	}
	if err := g.Set("__iframe_postmessage", post); err != nil {
		return err
	}
	c.registerBootstrap("artemis-iframe", iframeBootstrap)
	return nil
}

// dispatchPendingIFrameMessages drains queued messages for the iframe
// and dispatches them as MessageEvents in the iframe's sub-Context.
func dispatchPendingIFrameMessages(ctx context.Context, r *iframeRegistry, handle uint32) error {
	subCtx := r.subContext(handle)
	if subCtx == nil {
		return nil
	}
	return subCtx.executeNested(ctx, func() error { return dispatchIFrameMessagesLocked(r, handle, subCtx) })
}

func dispatchIFrameMessagesLocked(r *iframeRegistry, handle uint32, subCtx *Context) error {
	msgs := r.drainMessagesFor(handle)
	for _, payload := range msgs {
		script := "(() => { const data = JSON.parse(" + jsStringLit(payload) + ");" +
			" window.dispatchEvent(new MessageEvent('message', {data, origin:" + jsStringLit(iframeMessageOrigin(r.baseURL)) + "})); })()"
		value, err := subCtx.v8ctx.RunScript(script, "<iframe-msg>")
		if value != nil {
			value.Release()
		}
		failure := errors.Join(formatJSError(err), subCtx.takeDocumentEventErrors(), subCtx.styleMgr.takeFailure())
		if failure != nil {
			return failure
		}
	}
	return nil
}

func iframeMessageOrigin(rawURL string) string {
	origin := storageOrigin(rawURL)
	if origin == "" {
		return "null"
	}
	if strings.HasPrefix(origin, "https://") {
		return strings.TrimSuffix(origin, ":443")
	}
	return strings.TrimSuffix(origin, ":80")
}

const iframeBootstrap = `
(() => {
  if (!globalThis.__ELEM_PROTO) return;
  const stringifyMessage = JSON.stringify;

  // Top-level window plumbing for parent context.
  if (globalThis.window === globalThis) {
    Object.defineProperty(window, 'parent', { get() { return window; }, configurable: true });
    Object.defineProperty(window, 'top', { get() { return window; }, configurable: true });
    Object.defineProperty(window, 'self', { get() { return window; }, configurable: true });
    Object.defineProperty(window, 'frames', {
      get() {
        const out = [];
        const iframes = document.getElementsByTagName('iframe');
        for (let i = 0; i < iframes.length; i++) out.push(iframes[i].contentWindow);
        out.length = iframes.length;
        return out;
      },
      configurable: true,
    });
    Object.defineProperty(window, 'opener', { value: null, configurable: true });
    Object.defineProperty(window, 'frameElement', { value: null, configurable: true });
  }

  Object.defineProperty(__ELEM_PROTO, 'contentDocument', {
    get() {
      if (this.tagName !== 'IFRAME') return null;
      if (!__iframe_load(this.__id)) return null;
      const root = __iframe_root(this.__id);
      if (!root) return null;
      const wrapped = __wrap(root);
      const doc = Object.create(wrapped);
      doc.body = wrapped.querySelector('body') || wrapped;
      doc.documentElement = wrapped.querySelector('html') || wrapped;
      const titleEl = wrapped.querySelector('title');
      doc.title = titleEl ? titleEl.textContent : '';
      doc.querySelector = (s) => wrapped.querySelector(s);
      doc.querySelectorAll = (s) => wrapped.querySelectorAll(s);
      doc.getElementById = (id) => wrapped.querySelector('#' + id);
      doc.getElementsByTagName = (t) => wrapped.querySelectorAll(t);
      return doc;
    },
    configurable: true,
  });
  Object.defineProperty(__ELEM_PROTO, 'contentWindow', {
    get() {
      if (this.tagName !== 'IFRAME') return null;
      const handle = this.__id;
      const self = this;
      return {
        get document() { return self.contentDocument; },
        get location() {
          const href = self.getAttribute('src') || '';
          const u = href ? __url_parse(href, location ? location.href : '') : null;
          return u || { href: href };
        },
        postMessage(data, targetOrigin = '/', transfer) {
          if (targetOrigin !== null && typeof targetOrigin === 'object') {
            transfer = targetOrigin.transfer;
            targetOrigin = targetOrigin.targetOrigin;
            if (targetOrigin === undefined) targetOrigin = '/';
          }
          if (typeof targetOrigin === 'symbol') throw new TypeError('iframe target origin requires a string');
          targetOrigin = String(targetOrigin);
          if (transfer !== undefined && Array.from(transfer).length) throw new DOMException('iframe transferable messages require browser escalation', 'DataCloneError');
          const payload = stringifyMessage(data);
          if (typeof payload !== 'string') throw new TypeError('iframe message requires JSON-serializable data');
          if (!__iframe_load(handle)) return;
          __iframe_postmessage(handle, payload, targetOrigin);
        },
        addEventListener() {},
        removeEventListener() {},
      };
    },
    configurable: true,
  });
})();
`
