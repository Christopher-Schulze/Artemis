package js

import (
	"strings"

	"golang.org/x/net/html"
	v8 "rogchap.com/v8go"

	"github.com/Christopher-Schulze/Artemis/parser"
	"github.com/Christopher-Schulze/Artemis/webapi"
)

// installExtrasV2 runs after the v1 extras and adds AbortController,
// DOMException, on*-attribute compilation, plus the 013 batch (DOMParser,
// TextEncoder, TextDecoder, URL, URLSearchParams, customElements).
func installExtrasV2(iso *v8.Isolate, v8ctx *v8.Context, c *Context) error {
	if err := installDOMParser(iso, v8ctx, c); err != nil {
		return err
	}
	if err := installOnAttrsHelper(iso, v8ctx, c); err != nil {
		return err
	}
	c.registerBootstrap("artemis-extras-v2", extrasV2Bootstrap)
	c.registerBootstrap("artemis-extras-v2-onattrs", extrasV2OnAttrsBootstrap)
	return nil
}

// extrasV2Templates caches the 2 helper templates at Runtime level.
type extrasV2Templates struct {
	parseHTML, listOnAttrs *v8.FunctionTemplate
}

func (r *Runtime) ensureExtrasV2Templates() *extrasV2Templates {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.extrasV2 != nil {
		return r.extrasV2
	}
	iso := r.iso
	r.extrasV2 = &extrasV2Templates{
		parseHTML: v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			c := r.contextFor(info.Context())
			args := info.Args()
			if c == nil || len(args) < 1 {
				return v8.Null(iso)
			}
			doc, err := parser.ParseHTML(strings.NewReader(args[0].String()), "")
			if err != nil {
				return v8.Null(iso)
			}
			root := doc.Root()
			if root == nil {
				return v8.Null(iso)
			}
			return mustValue(iso, int32(c.nodes.Handle(root)))
		}),
		listOnAttrs: v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			c := r.contextFor(info.Context())
			if c == nil {
				return idsToArray(info.Context(), iso, nil)
			}
			root := c.doc.Root()
			if root == nil {
				return idsToArray(info.Context(), iso, nil)
			}
			var triples []any
			webapi.Walk(root, func(n *webapi.Node) webapi.WalkAction {
				if n.Type() != webapi.NodeElement {
					return webapi.WalkContinue
				}
				for _, a := range n.Raw().Attr {
					lower := strings.ToLower(a.Key)
					if !strings.HasPrefix(lower, "on") || len(lower) < 3 {
						continue
					}
					ev := lower[2:]
					id := c.nodes.Handle(n)
					obj, err := v8.NewObjectTemplate(iso).NewInstance(info.Context())
					if err != nil {
						continue
					}
					_ = obj.Set("id", int32(id))
					_ = obj.Set("event", ev)
					_ = obj.Set("body", a.Val)
					triples = append(triples, obj.Value)
				}
				return webapi.WalkContinue
			})
			return idsToArray(info.Context(), iso, triples)
		}),
	}
	return r.extrasV2
}

func installDOMParser(iso *v8.Isolate, v8ctx *v8.Context, c *Context) error {
	t := c.rt.ensureExtrasV2Templates()
	return v8ctx.Global().Set("__parse_html", t.parseHTML.GetFunction(v8ctx))
}

// installOnAttrsHelper exposes `__list_on_attrs()` returning all (handle,
// event, body) tuples found in the static document. The JS bootstrap
// iterates and registers a listener per tuple via the JS-side
// __addListener.
func installOnAttrsHelper(iso *v8.Isolate, v8ctx *v8.Context, c *Context) error {
	t := c.rt.ensureExtrasV2Templates()
	return v8ctx.Global().Set("__list_on_attrs", t.listOnAttrs.GetFunction(v8ctx))
}

// silence unused import warning when html is only used transitively.
var _ = html.ElementNode

const extrasV2Bootstrap = `
(() => {
  // ---------------- DOMException ----------------
  class DOMException extends Error {
    constructor(message, name) {
      super(message || '');
      this.name = name || 'Error';
      this.code = DOMException._codes[this.name] || 0;
    }
  }
  DOMException._codes = {
    IndexSizeError: 1, HierarchyRequestError: 3, WrongDocumentError: 4,
    InvalidCharacterError: 5, NoModificationAllowedError: 7,
    NotFoundError: 8, NotSupportedError: 9, InUseAttributeError: 10,
    InvalidStateError: 11, SyntaxError: 12, InvalidModificationError: 13,
    NamespaceError: 14, InvalidAccessError: 15, SecurityError: 18,
    NetworkError: 19, AbortError: 20, URLMismatchError: 21,
    QuotaExceededError: 22, TimeoutError: 23, InvalidNodeTypeError: 24,
    DataCloneError: 25,
  };
  globalThis.DOMException = DOMException;

  // ---------------- AbortController / AbortSignal ----------------
  class _AbortET {
    constructor() { this._lst = new Map(); }
    addEventListener(type, fn) {
      if (typeof fn !== 'function') return;
      let s = this._lst.get(type);
      if (!s) { s = new Set(); this._lst.set(type, s); }
      s.add(fn);
    }
    removeEventListener(type, fn) {
      const s = this._lst.get(type);
      if (s) s.delete(fn);
    }
    dispatchEvent(ev) {
      const s = this._lst.get(ev && ev.type);
      if (!s) return true;
      for (const fn of s) { try { fn.call(this, ev); } catch(e){} }
      return !ev.defaultPrevented;
    }
  }
  class AbortSignal extends _AbortET {
    constructor() {
      super();
      this.aborted = false;
      this.reason = undefined;
    }
    throwIfAborted() {
      if (this.aborted) throw this.reason || new DOMException('aborted', 'AbortError');
    }
  }
  AbortSignal.abort = function(reason) {
    const s = new AbortSignal();
    s.aborted = true;
    s.reason = reason || new DOMException('aborted', 'AbortError');
    return s;
  };
  AbortSignal.timeout = function(_ms) {
    return AbortSignal.abort(new DOMException('timeout', 'TimeoutError'));
  };
  class AbortController {
    constructor() { this.signal = new AbortSignal(); }
    abort(reason) {
      if (this.signal.aborted) return;
      this.signal.aborted = true;
      this.signal.reason = reason || new DOMException('aborted', 'AbortError');
      const ev = new Event('abort');
      this.signal.dispatchEvent(ev);
    }
  }
  globalThis.AbortController = AbortController;
  globalThis.AbortSignal = AbortSignal;

  // ---------------- DOMParser ----------------
  class DOMParser {
    parseFromString(src, _mimeType) {
      const id = __parse_html(String(src));
      if (!id) return null;
      const wrapped = __wrap(id);
      const docLike = Object.create(wrapped);
      docLike.body = wrapped.querySelector('body');
      docLike.documentElement = wrapped.querySelector('html') || wrapped;
      const titleEl = wrapped.querySelector('title');
      docLike.title = titleEl ? titleEl.textContent : '';
      docLike.querySelector = (s) => wrapped.querySelector(s);
      docLike.querySelectorAll = (s) => wrapped.querySelectorAll(s);
      docLike.getElementById = (id) => wrapped.querySelector('#' + id);
      return docLike;
    }
  }
  globalThis.DOMParser = DOMParser;

  // ---------------- XMLSerializer ----------------
  // Serialises a DOM node to a string. Real browsers walk the tree
  // emitting XML/HTML; we lean on outerHTML when available because the
  // bridge already produces faithful HTML. Document nodes serialise as
  // their documentElement.
  class XMLSerializer {
    serializeToString(node) {
      if (!node) return '';
      if (node.outerHTML) return node.outerHTML;
      if (node.documentElement && node.documentElement.outerHTML) return node.documentElement.outerHTML;
      // Text/comment fallback.
      const t = node.nodeType;
      if (t === 3) return String(node.data || node.textContent || '');
      if (t === 8) return '<!--' + String(node.data || '') + '-->';
      return '';
    }
  }
  globalThis.XMLSerializer = XMLSerializer;

  // ---------------- TextEncoder / TextDecoder ----------------
  class TextEncoder {
    constructor() { this.encoding = 'utf-8'; }
    encode(input) {
      input = String(input == null ? '' : input);
      const out = [];
      for (let i = 0; i < input.length; i++) {
        let cp = input.charCodeAt(i);
        if (cp >= 0xD800 && cp <= 0xDBFF && i + 1 < input.length) {
          const next = input.charCodeAt(i+1);
          if (next >= 0xDC00 && next <= 0xDFFF) {
            cp = 0x10000 + ((cp - 0xD800) << 10) + (next - 0xDC00);
            i++;
          }
        }
        if (cp < 0x80) out.push(cp);
        else if (cp < 0x800) { out.push(0xC0|(cp>>6)); out.push(0x80|(cp&0x3F)); }
        else if (cp < 0x10000) { out.push(0xE0|(cp>>12)); out.push(0x80|((cp>>6)&0x3F)); out.push(0x80|(cp&0x3F)); }
        else { out.push(0xF0|(cp>>18)); out.push(0x80|((cp>>12)&0x3F)); out.push(0x80|((cp>>6)&0x3F)); out.push(0x80|(cp&0x3F)); }
      }
      const arr = new Array(out.length);
      for (let i = 0; i < out.length; i++) arr[i] = out[i];
      arr.byteLength = arr.length;
      return arr;
    }
  }
  class TextDecoder {
    constructor(label) { this.encoding = (label || 'utf-8').toLowerCase(); }
    decode(buf) {
      if (!buf) return '';
      const arr = buf.length != null ? buf : Array.from(buf);
      let s = '';
      let i = 0;
      while (i < arr.length) {
        const b = arr[i++] & 0xFF;
        if (b < 0x80) s += String.fromCharCode(b);
        else if (b < 0xC0) s += '�';
        else if (b < 0xE0) {
          const b2 = arr[i++] & 0x3F;
          s += String.fromCharCode(((b & 0x1F) << 6) | b2);
        }
        else if (b < 0xF0) {
          const b2 = arr[i++] & 0x3F;
          const b3 = arr[i++] & 0x3F;
          s += String.fromCharCode(((b & 0x0F) << 12) | (b2 << 6) | b3);
        }
        else {
          const b2 = arr[i++] & 0x3F;
          const b3 = arr[i++] & 0x3F;
          const b4 = arr[i++] & 0x3F;
          let cp = ((b & 0x07) << 18) | (b2 << 12) | (b3 << 6) | b4;
          cp -= 0x10000;
          s += String.fromCharCode(0xD800 + (cp >> 10), 0xDC00 + (cp & 0x3FF));
        }
      }
      return s;
    }
  }
  globalThis.TextEncoder = TextEncoder;
  globalThis.TextDecoder = TextDecoder;

  // ---------------- URLSearchParams ----------------
  class URLSearchParams {
    constructor(init) {
      this._pairs = [];
      if (!init) return;
      if (typeof init === 'string') {
        let s = init.startsWith('?') ? init.slice(1) : init;
        if (s === '') return;
        for (const part of s.split('&')) {
          const eq = part.indexOf('=');
          if (eq < 0) this._pairs.push([decodeURIComponent(part), '']);
          else this._pairs.push([
            decodeURIComponent(part.slice(0, eq).replace(/\+/g, ' ')),
            decodeURIComponent(part.slice(eq+1).replace(/\+/g, ' ')),
          ]);
        }
        return;
      }
      if (Array.isArray(init)) { for (const [k, v] of init) this.append(k, v); return; }
      if (typeof init === 'object') { for (const k in init) this.append(k, init[k]); }
    }
    append(k, v) { this._pairs.push([String(k), String(v)]); }
    set(k, v) { this.delete(k); this.append(k, v); }
    delete(k) { this._pairs = this._pairs.filter(p => p[0] !== k); }
    get(k) { for (const p of this._pairs) if (p[0] === k) return p[1]; return null; }
    getAll(k) { return this._pairs.filter(p => p[0] === k).map(p => p[1]); }
    has(k) { return this.get(k) !== null; }
    toString() {
      return this._pairs.map(p =>
        encodeURIComponent(p[0]) + '=' + encodeURIComponent(p[1])).join('&');
    }
    *entries() { for (const p of this._pairs) yield [p[0], p[1]]; }
    *keys() { for (const p of this._pairs) yield p[0]; }
    *values() { for (const p of this._pairs) yield p[1]; }
    [Symbol.iterator]() { return this.entries(); }
    forEach(fn, thisArg) { for (const [k, v] of this._pairs) fn.call(thisArg, v, k, this); }
  }
  globalThis.URLSearchParams = URLSearchParams;

  // ---------------- URL ----------------
  class URL {
    constructor(href, base) {
      const parsed = __url_parse(String(href), base ? String(base) : null);
      if (!parsed) throw new TypeError('Invalid URL: ' + href);
      this.href = parsed.href;
      this.protocol = parsed.protocol;
      this.host = parsed.host;
      this.hostname = parsed.hostname;
      this.port = parsed.port;
      this.pathname = parsed.pathname;
      this.search = parsed.search;
      this.hash = parsed.hash;
      this.origin = parsed.origin;
      this.searchParams = new URLSearchParams(parsed.search || '');
    }
    toString() { return this.href; }
  }
  globalThis.URL = URL;

  // ---------------- customElements ----------------
  const _registry = new Map();
  const _whenDefined = new Map();
  globalThis.customElements = {
    define(name, ctor, _opts) {
      if (_registry.has(name)) throw new DOMException('already defined: ' + name, 'NotSupportedError');
      _registry.set(name, ctor);
      const existing = document.getElementsByTagName(name);
      for (const el of existing) {
        try {
          if (typeof ctor.prototype.connectedCallback === 'function') {
            ctor.prototype.connectedCallback.call(el);
          }
        } catch (e) {}
      }
      const w = _whenDefined.get(name);
      if (w) { w.forEach(r => r(ctor)); _whenDefined.delete(name); }
    },
    get(name) { return _registry.get(name); },
    // _reset clears the registry. Used by __artemis_reset between
    // pool-reused pages so 'already defined' doesn't throw on the
    // next page's customElements.define call.
    _reset() { _registry.clear(); _whenDefined.clear(); },
    whenDefined(name) {
      if (_registry.has(name)) return Promise.resolve(_registry.get(name));
      return new Promise(r => {
        let s = _whenDefined.get(name);
        if (!s) { s = new Set(); _whenDefined.set(name, s); }
        s.add(r);
      });
    },
    upgrade() {},
  };

  // ---------------- Blob / File / FileReader (skeleton) ----------------
  class Blob {
    constructor(parts, options) {
      parts = parts || [];
      this.size = 0;
      this.type = (options && options.type) || '';
      this._chunks = [];
      for (const p of parts) {
        if (typeof p === 'string') { this._chunks.push(p); this.size += p.length; }
        else if (p && p.length != null) { this._chunks.push(p); this.size += p.length; }
      }
    }
    text() {
      let s = '';
      for (const c of this._chunks) {
        if (typeof c === 'string') s += c;
        else { const dec = new TextDecoder(); s += dec.decode(c); }
      }
      return Promise.resolve(s);
    }
    arrayBuffer() {
      const enc = new TextEncoder();
      const out = [];
      for (const c of this._chunks) {
        if (typeof c === 'string') {
          const b = enc.encode(c);
          for (let i = 0; i < b.length; i++) out.push(b[i]);
        } else {
          for (let i = 0; i < c.length; i++) out.push(c[i] & 0xFF);
        }
      }
      out.byteLength = out.length;
      return Promise.resolve(out);
    }
    slice(start, end, contentType) {
      const flat = this._chunks.map(c => typeof c === 'string' ? c : Array.from(c).map(b => String.fromCharCode(b)).join('')).join('');
      return new Blob([flat.slice(start || 0, end)], { type: contentType || this.type });
    }
  }
  class File extends Blob {
    constructor(parts, name, options) {
      super(parts, options);
      this.name = String(name);
      this.lastModified = (options && options.lastModified) || Date.now();
    }
  }
  class FileReader {
    constructor() { this.readyState = 0; this.result = null; this.onload = null; this.onerror = null; }
    readAsText(blob) { blob.text().then(t => { this.result = t; this.readyState = 2; if (this.onload) try { this.onload({}); } catch (e) {} }); }
    readAsArrayBuffer(blob) { blob.arrayBuffer().then(b => { this.result = b; this.readyState = 2; if (this.onload) try { this.onload({}); } catch (e) {} }); }
  }
  globalThis.Blob = Blob;
  globalThis.File = File;
  globalThis.FileReader = FileReader;

  // ---------------- Event subtypes ----------------
  class UIEvent extends Event {
    constructor(type, init) { super(type, init); init = init || {}; this.detail = init.detail || 0; this.view = init.view || globalThis; }
  }
  class MouseEvent extends UIEvent {
    constructor(type, init) {
      super(type, init); init = init || {};
      this.clientX = init.clientX || 0; this.clientY = init.clientY || 0;
      this.screenX = init.screenX || 0; this.screenY = init.screenY || 0;
      this.button = init.button || 0; this.buttons = init.buttons || 0;
      this.altKey = !!init.altKey; this.shiftKey = !!init.shiftKey;
      this.ctrlKey = !!init.ctrlKey; this.metaKey = !!init.metaKey;
      this.relatedTarget = init.relatedTarget || null;
    }
  }
  class KeyboardEvent extends UIEvent {
    constructor(type, init) {
      super(type, init); init = init || {};
      this.key = init.key || ''; this.code = init.code || '';
      this.keyCode = init.keyCode || 0; this.which = init.which || this.keyCode;
      this.altKey = !!init.altKey; this.shiftKey = !!init.shiftKey;
      this.ctrlKey = !!init.ctrlKey; this.metaKey = !!init.metaKey;
      this.repeat = !!init.repeat;
    }
  }
  class InputEvent extends UIEvent {
    constructor(type, init) {
      super(type, init); init = init || {};
      this.data = init.data == null ? '' : String(init.data);
      this.inputType = init.inputType || 'insertText';
      this.isComposing = !!init.isComposing;
    }
  }
  class FocusEvent extends UIEvent {
    constructor(type, init) {
      super(type, init); init = init || {};
      this.relatedTarget = init.relatedTarget || null;
    }
  }
  class CustomEvent extends Event {
    constructor(type, init) { super(type, init); this.detail = (init && init.detail) || null; }
  }
  globalThis.UIEvent = UIEvent;
  globalThis.MouseEvent = MouseEvent;
  globalThis.KeyboardEvent = KeyboardEvent;
  globalThis.InputEvent = InputEvent;
  globalThis.FocusEvent = FocusEvent;
  globalThis.CustomEvent = CustomEvent;

})();
`

// extrasV2OnAttrsBootstrap compiles the per-document on* HTML attribute
// handlers. Split out of extrasV2Bootstrap because __list_on_attrs is
// per-Context (its return value depends on the loaded HTML), so this
// snippet must run per-Context even when a snapshot is loaded.
const extrasV2OnAttrsBootstrap = `
(() => {
  const triples = __list_on_attrs() || [];
  for (let i = 0; i < (triples.length || 0); i++) {
    const t = triples[i];
    if (!t) continue;
    try {
      const fn = new Function('event', t.body);
      __addListener(t.id, t.event, fn);
    } catch (e) {}
  }
})();
`
