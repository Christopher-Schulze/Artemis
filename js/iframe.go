package js

import (
	"strings"
	"sync"

	v8 "rogchap.com/v8go"

	"github.com/Christopher-Schulze/Artemis/parser"
	"github.com/Christopher-Schulze/Artemis/webapi"
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
	loader          func(url string) ([]byte, error)
	baseURL         string
	subContextBuild func(doc *webapi.Document) (*Context, error)
	pendingMsgs     map[uint32][]string
}

func newIFrameRegistry(loader func(string) ([]byte, error), baseURL string,
	build func(doc *webapi.Document) (*Context, error)) *iframeRegistry {
	return &iframeRegistry{
		docs:            make(map[uint32]*webapi.Document),
		ctxs:            make(map[uint32]*Context),
		loader:          loader,
		baseURL:         baseURL,
		subContextBuild: build,
		pendingMsgs:     make(map[uint32][]string),
	}
}

func (r *iframeRegistry) load(handle uint32, src string, parent *Context) *webapi.Document {
	r.mu.Lock()
	if d, ok := r.docs[handle]; ok {
		r.mu.Unlock()
		return d
	}
	r.mu.Unlock()
	if r.loader == nil {
		return nil
	}
	body, err := r.loader(src)
	if err != nil || len(body) == 0 {
		return nil
	}
	doc, err := parser.ParseHTML(strings.NewReader(string(body)), src)
	if err != nil {
		return nil
	}
	r.mu.Lock()
	r.docs[handle] = doc
	r.mu.Unlock()

	// Create the iframe's own JS Context so scripts run in isolation.
	if r.subContextBuild != nil {
		subCtx, err := r.subContextBuild(doc)
		if err == nil && subCtx != nil {
			r.mu.Lock()
			r.ctxs[handle] = subCtx
			r.mu.Unlock()
			runIframeScriptsInCtx(subCtx, doc)
		}
	}
	return doc
}

func (r *iframeRegistry) subContext(handle uint32) *Context {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ctxs[handle]
}

func (r *iframeRegistry) queueMessage(handle uint32, payload string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pendingMsgs[handle] = append(r.pendingMsgs[handle], payload)
}

func (r *iframeRegistry) drainMessagesFor(handle uint32) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.pendingMsgs[handle]
	r.pendingMsgs[handle] = nil
	return out
}

func (r *iframeRegistry) closeAll() {
	r.mu.Lock()
	ctxs := make([]*Context, 0, len(r.ctxs))
	for _, c := range r.ctxs {
		ctxs = append(ctxs, c)
	}
	r.ctxs = make(map[uint32]*Context)
	r.mu.Unlock()
	for _, c := range ctxs {
		c.Close()
	}
}

// IFrameLoader is what installIframe uses to fetch iframe HTML. The
// engine provides one wrapping its HTTP client.
type IFrameLoader func(url string) ([]byte, error)

// runIframeScriptsInCtx walks doc and evaluates every inline <script>
// in the iframe's own sub-Context.
func runIframeScriptsInCtx(subCtx *Context, doc *webapi.Document) {
	root := doc.Root()
	if root == nil {
		return
	}
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
		_, _ = subCtx.evalLocked(code)
		return webapi.WalkContinue
	})
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
			c := r.contextFor(info.Context())
			args := info.Args()
			if c == nil || len(args) < 1 {
				return mustValue(iso, false)
			}
			handle := uint32(args[0].Integer())
			n := c.nodes.Get(handle)
			if n == nil || n.Tag() != "iframe" {
				return mustValue(iso, false)
			}
			src, _ := n.Attr("src")
			if src == "" {
				return mustValue(iso, false)
			}
			doc := c.iframes.load(handle, src, c)
			return mustValue(iso, doc != nil)
		}),
		root: v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			c := r.contextFor(info.Context())
			args := info.Args()
			if c == nil || len(args) < 1 {
				return mustValue(iso, int32(0))
			}
			handle := uint32(args[0].Integer())
			c.iframes.mu.Lock()
			doc, ok := c.iframes.docs[handle]
			c.iframes.mu.Unlock()
			if !ok || doc == nil || doc.Root() == nil {
				return mustValue(iso, int32(0))
			}
			// Register the iframe doc's root in PARENT's nodeTable so parent
			// JS can __wrap it. Mutations via parent or iframe both touch
			// the same Go-side *html.Node, so DOM state stays consistent.
			return mustValue(iso, int32(c.nodes.Handle(doc.Root())))
		}),
		// Cross-context postMessage: parent's contentWindow.postMessage
		// pushes data into the iframe's pending message queue. The iframe
		// sub-Context drains the queue immediately and dispatches a
		// MessageEvent on its window.
		post: v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			c := r.contextFor(info.Context())
			args := info.Args()
			if c == nil || len(args) < 2 {
				return v8.Null(iso)
			}
			handle := uint32(args[0].Integer())
			jsonGlobal, err := info.Context().Global().Get("JSON")
			if err != nil {
				return v8.Null(iso)
			}
			jsonObj, _ := jsonGlobal.AsObject()
			stringifyVal, _ := jsonObj.Get("stringify")
			stringifyFn, _ := stringifyVal.AsFunction()
			jsoned, err := stringifyFn.Call(jsonObj, args[1])
			if err != nil {
				return v8.Null(iso)
			}
			c.iframes.queueMessage(handle, jsoned.String())
			dispatchPendingIFrameMessages(c.iframes, handle)
			return v8.Null(iso)
		}),
	}
	return r.iframeTemplates
}

func installIframe(iso *v8.Isolate, v8ctx *v8.Context, c *Context) error {
	t := c.rt.ensureIframeTemplates()
	g := v8ctx.Global()
	if err := g.Set("__iframe_load", t.load.GetFunction(v8ctx)); err != nil {
		return err
	}
	if err := g.Set("__iframe_root", t.root.GetFunction(v8ctx)); err != nil {
		return err
	}
	if err := g.Set("__iframe_postmessage", t.post.GetFunction(v8ctx)); err != nil {
		return err
	}
	c.registerBootstrap("artemis-iframe", iframeBootstrap)
	return nil
}

// dispatchPendingIFrameMessages drains queued messages for the iframe
// and dispatches them as MessageEvents in the iframe's sub-Context.
func dispatchPendingIFrameMessages(r *iframeRegistry, handle uint32) {
	subCtx := r.subContext(handle)
	if subCtx == nil {
		return
	}
	msgs := r.drainMessagesFor(handle)
	for _, payload := range msgs {
		script := "(() => { try { const data = JSON.parse(" + jsStringLit(payload) + ");" +
			" window.dispatchEvent(new MessageEvent('message', {data})); } catch (e) {} })()"
		_, _ = subCtx.v8ctx.RunScript(script, "<iframe-msg>")
	}
}

const iframeBootstrap = `
(() => {
  if (!globalThis.__ELEM_PROTO) return;

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
        postMessage(data, _origin) {
          __iframe_postmessage(handle, data);
        },
        addEventListener() {},
        removeEventListener() {},
      };
    },
    configurable: true,
  });
})();
`
