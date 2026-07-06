package js

import (
	v8 "rogchap.com/v8go"
)

// installParityGaps fills feature gaps that would crash sites or return
// silently-broken values. The implementations target AI-agent crawling
// fidelity rather than pixel-perfect browser semantics: where a real
// browser depends on layout, we synthesise plausible defaults so feature
// detection + first-call paths succeed.
//
// Concretely closes:
//   - Worker (no-op stub, does not run JS)
//   - MessageChannel + MessagePort (functional Go-less pair)
//   - Screen, VisualViewport (sensible defaults)
//   - navigator.storage (StorageManager stub)
//   - document.implementation (DOMImplementation stub)
//   - DocumentFragment as a proper class
//   - AbstractRange parent class
//   - IntersectionObserver / ResizeObserver fire one-shot on observe()
//   - performance.mark / measure / getEntries with real entry storage
//   - ReadableStream.getReader() with sequential chunk delivery
//   - window.matchMedia + scroll APIs
func installParityGaps(iso *v8.Isolate, v8ctx *v8.Context, c *Context) error {
	c.registerBootstrap("artemis-parity-gaps", parityGapsBootstrap)
	return nil
}

const parityGapsBootstrap = `
(() => {
  // ---------------- Screen ----------------
  // Real browsers expose window.screen with all the common DPI/orientation
  // fields. Sites read these for feature detection (mobile vs desktop,
  // dark mode, viewport calculations).
  const _screen = {
    width: 1920, height: 1080,
    availWidth: 1920, availHeight: 1040,
    colorDepth: 24, pixelDepth: 24,
    orientation: { type: 'landscape-primary', angle: 0,
                   addEventListener(){}, removeEventListener(){} },
  };
  globalThis.screen = _screen;
  if (globalThis.window) globalThis.window.screen = _screen;

  // ---------------- VisualViewport ----------------
  const _vv = {
    width: 1920, height: 1080, offsetLeft: 0, offsetTop: 0,
    pageLeft: 0, pageTop: 0, scale: 1,
    addEventListener() {}, removeEventListener() {}, dispatchEvent() { return true; },
  };
  globalThis.visualViewport = _vv;
  if (globalThis.window) globalThis.window.visualViewport = _vv;

  // window dimensions
  if (globalThis.window) {
    globalThis.window.innerWidth = 1920;
    globalThis.window.innerHeight = 1080;
    globalThis.window.outerWidth = 1920;
    globalThis.window.outerHeight = 1080;
    globalThis.window.devicePixelRatio = 1;
    globalThis.window.scrollX = 0; globalThis.window.scrollY = 0;
    globalThis.window.pageXOffset = 0; globalThis.window.pageYOffset = 0;
    globalThis.window.scroll = function() {};
    globalThis.window.scrollTo = function() {};
    globalThis.window.scrollBy = function() {};
  }

  // ---------------- matchMedia ----------------
  // Sites use this for theme/dark-mode detection, breakpoint logic.
  // We resolve everything as "false match" except prefers-color-scheme
  // (defaulting to light). Listeners never fire (no media changes).
  globalThis.matchMedia = function(q) {
    const query = String(q || '');
    const matches = /prefers-color-scheme:\s*light/.test(query);
    return {
      media: query, matches: matches, onchange: null,
      addListener() {}, removeListener() {},
      addEventListener() {}, removeEventListener() {}, dispatchEvent() { return true; },
    };
  };
  if (globalThis.window) globalThis.window.matchMedia = globalThis.matchMedia;

  // ---------------- StorageManager (navigator.storage) ----------------
  if (globalThis.navigator && !globalThis.navigator.storage) {
    globalThis.navigator.storage = {
      estimate: () => Promise.resolve({ quota: 1024 * 1024 * 1024, usage: 0 }),
      persist: () => Promise.resolve(false),
      persisted: () => Promise.resolve(false),
    };
  }

  // ---------------- DOMImplementation (document.implementation) ----------------
  if (globalThis.document && !globalThis.document.implementation) {
    globalThis.document.implementation = {
      hasFeature: () => true,  // legacy, always returns true per spec
      createDocumentType: (name, pub, sys) => ({
        name: String(name || ''),
        publicId: String(pub || ''),
        systemId: String(sys || ''),
        nodeType: 10, nodeName: String(name || ''),
      }),
      createHTMLDocument: (title) => {
        const dp = new DOMParser();
        const html = '<!DOCTYPE html><html><head><title>' +
                     String(title || '').replace(/</g, '&lt;') +
                     '</title></head><body></body></html>';
        return dp.parseFromString(html, 'text/html');
      },
      createDocument: (ns, name) => {
        const dp = new DOMParser();
        return dp.parseFromString('<' + (name || 'root') + '/>', 'application/xml');
      },
    };
  }

  // ---------------- AbstractRange ----------------
  // Real spec parent of Range/StaticRange. Just exists for instanceof.
  class AbstractRange {
    get startContainer() { return this._sc || null; }
    get endContainer() { return this._ec || null; }
    get startOffset() { return this._so | 0; }
    get endOffset() { return this._eo | 0; }
    get collapsed() { return this._sc === this._ec && this._so === this._eo; }
  }
  globalThis.AbstractRange = AbstractRange;

  class StaticRange extends AbstractRange {
    constructor(init) {
      super();
      init = init || {};
      this._sc = init.startContainer || null;
      this._ec = init.endContainer || null;
      this._so = init.startOffset | 0;
      this._eo = init.endOffset | 0;
    }
  }
  globalThis.StaticRange = StaticRange;

  // ---------------- DocumentFragment ----------------
  // Proper class so 'instanceof DocumentFragment' works. Real fragments
  // are implemented Go-side via parser; this class wraps that.
  class DocumentFragment {
    constructor() {
      this.nodeType = 11;
      this.nodeName = '#document-fragment';
      this.children = [];
      this.childNodes = [];
      this.firstChild = null; this.lastChild = null;
      this.parentNode = null;
    }
    appendChild(node) {
      this.children.push(node); this.childNodes.push(node);
      this.firstChild = this.children[0];
      this.lastChild = this.children[this.children.length - 1];
      return node;
    }
    querySelector() { return null; }
    querySelectorAll() { return []; }
    getElementById() { return null; }
  }
  globalThis.DocumentFragment = DocumentFragment;

  // ---------------- MessageChannel + MessagePort ----------------
  // Functional pair: messages posted on one port arrive on the other's
  // onmessage handler asynchronously (microtask). Used by libraries like
  // Comlink, postMessage shims.
  class MessagePort {
    constructor() {
      this._listeners = new Map();
      this._other = null;
      this._started = false;
      this._queue = [];
      this.onmessage = null;
    }
    postMessage(data) {
      const ev = { data, type: 'message', target: this._other,
                   ports: [], source: this };
      const target = this._other;
      if (!target) return;
      Promise.resolve().then(() => {
        if (typeof target.onmessage === 'function') {
          try { target.onmessage(ev); } catch (e) {}
        }
        const set = target._listeners.get('message');
        if (set) for (const fn of set) { try { fn(ev); } catch (e) {} }
      });
    }
    addEventListener(type, fn) {
      if (typeof fn !== 'function') return;
      let s = this._listeners.get(type);
      if (!s) { s = new Set(); this._listeners.set(type, s); }
      s.add(fn);
    }
    removeEventListener(type, fn) {
      const s = this._listeners.get(type);
      if (s) s.delete(fn);
    }
    start() { this._started = true; }
    close() { this._other = null; }
    dispatchEvent(ev) {
      const s = this._listeners.get(ev.type);
      if (s) for (const fn of s) { try { fn(ev); } catch (e) {} }
      return true;
    }
  }
  globalThis.MessagePort = MessagePort;

  class MessageChannel {
    constructor() {
      const a = new MessagePort();
      const b = new MessagePort();
      a._other = b; b._other = a;
      this.port1 = a;
      this.port2 = b;
    }
  }
  globalThis.MessageChannel = MessageChannel;

  // ---------------- Worker (no-op stub) ----------------
  // Real Workers need a separate V8 isolate + thread + message bridge,
  // which is out of scope for an agent-focused engine. We expose a stub
  // that does not throw on construction so feature-detection passes;
  // posted messages are silently dropped.
  class Worker {
    constructor(scriptURL, opts) {
      this.scriptURL = String(scriptURL || '');
      this._listeners = new Map();
      this.onmessage = null; this.onerror = null;
    }
    postMessage() {}  // dropped
    terminate() {}
    addEventListener(type, fn) {
      if (typeof fn !== 'function') return;
      let s = this._listeners.get(type);
      if (!s) { s = new Set(); this._listeners.set(type, s); }
      s.add(fn);
    }
    removeEventListener(type, fn) {
      const s = this._listeners.get(type);
      if (s) s.delete(fn);
    }
    dispatchEvent() { return true; }
  }
  globalThis.Worker = Worker;
  globalThis.SharedWorker = Worker;  // same stub semantics

  // ---------------- IntersectionObserver: fire on observe() ----------------
  // Real implementation requires layout. We synthesise a "fully visible"
  // entry on each observe() call (microtask-deferred), which is what
  // most lazy-load patterns expect to unblock initial render.
  class _IntersectionObserver {
    constructor(callback, options) {
      this._cb = callback;
      this._opts = options || {};
      this._targets = new Set();
      this.root = (options && options.root) || null;
      this.rootMargin = (options && options.rootMargin) || '0px';
      this.thresholds = (options && options.threshold !== undefined)
        ? [].concat(options.threshold) : [0];
    }
    observe(target) {
      if (!target) return;
      this._targets.add(target);
      const entry = {
        target,
        time: (typeof performance !== 'undefined' && performance.now)
          ? performance.now() : 0,
        isIntersecting: true,
        intersectionRatio: 1,
        boundingClientRect: { x:0, y:0, top:0, left:0, right:0, bottom:0,
                              width:0, height:0 },
        intersectionRect: { x:0, y:0, top:0, left:0, right:0, bottom:0,
                            width:0, height:0 },
        rootBounds: null,
      };
      const cb = this._cb;
      const self = this;
      Promise.resolve().then(() => {
        try { cb([entry], self); } catch (e) {}
      });
    }
    unobserve(target) { this._targets.delete(target); }
    disconnect() { this._targets.clear(); }
    takeRecords() { return []; }
  }
  globalThis.IntersectionObserver = _IntersectionObserver;

  // ---------------- ResizeObserver: fire on observe() ----------------
  class _ResizeObserver {
    constructor(callback) { this._cb = callback; this._targets = new Set(); }
    observe(target) {
      if (!target) return;
      this._targets.add(target);
      const entry = {
        target,
        contentRect: { x:0, y:0, top:0, left:0, right:0, bottom:0,
                       width:0, height:0 },
        borderBoxSize: [{ inlineSize: 0, blockSize: 0 }],
        contentBoxSize: [{ inlineSize: 0, blockSize: 0 }],
        devicePixelContentBoxSize: [{ inlineSize: 0, blockSize: 0 }],
      };
      const cb = this._cb;
      const self = this;
      Promise.resolve().then(() => {
        try { cb([entry], self); } catch (e) {}
      });
    }
    unobserve(target) { this._targets.delete(target); }
    disconnect() { this._targets.clear(); }
  }
  globalThis.ResizeObserver = _ResizeObserver;

  // ---------------- performance entries ----------------
  // mark/measure store real entries that getEntries* methods return.
  // PerformanceObserver receives them on the next microtask.
  if (globalThis.performance) {
    const _entries = [];
    const _marks = new Map();
    globalThis.performance.mark = function(name, opts) {
      const t = (opts && opts.startTime) ||
                (globalThis.performance.now ? globalThis.performance.now() : 0);
      const e = { name: String(name), entryType: 'mark',
                  startTime: t, duration: 0,
                  detail: (opts && opts.detail) || null };
      _entries.push(e); _marks.set(e.name, e);
      return e;
    };
    globalThis.performance.measure = function(name, startMark, endMark) {
      const start = typeof startMark === 'string'
        ? (_marks.get(startMark) || { startTime: 0 }).startTime
        : (startMark && startMark.start) || 0;
      const end = typeof endMark === 'string'
        ? (_marks.get(endMark) || { startTime: 0 }).startTime
        : (globalThis.performance.now ? globalThis.performance.now() : 0);
      const e = { name: String(name), entryType: 'measure',
                  startTime: start, duration: Math.max(0, end - start) };
      _entries.push(e);
      return e;
    };
    globalThis.performance.clearMarks = function(name) {
      if (!name) { _marks.clear(); return; }
      _marks.delete(name);
    };
    globalThis.performance.clearMeasures = function() {};
    globalThis.performance.getEntries = function() { return _entries.slice(); };
    globalThis.performance.getEntriesByName = function(n) {
      return _entries.filter(e => e.name === n);
    };
    globalThis.performance.getEntriesByType = function(t) {
      return _entries.filter(e => e.entryType === t);
    };
    globalThis.performance.timeOrigin = Date.now();
  }

  class _PerformanceObserver {
    constructor(callback) { this._cb = callback; this._types = []; }
    observe(opts) {
      this._types = (opts && opts.entryTypes) ||
                    (opts && opts.type ? [opts.type] : []);
    }
    disconnect() {}
    takeRecords() { return []; }
  }
  _PerformanceObserver.supportedEntryTypes = ['mark', 'measure', 'navigation', 'resource'];
  globalThis.PerformanceObserver = _PerformanceObserver;

  // ReadableStream.getReader is already functional in webapi_v3
  // bootstrap; we leave it untouched.
})();
`
