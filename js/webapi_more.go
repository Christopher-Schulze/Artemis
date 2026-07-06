package js

import (
	v8 "rogchap.com/v8go"
)

// installWebAPIMore installs the small classes that make up most of the
// long-tail Web API surface: tree iteration utilities, observer stubs,
// idle/microtask scheduling, viewport rect utilities, named-property
// getters on document. All implemented JS-side to avoid bridge churn.
func installWebAPIMore(iso *v8.Isolate, v8ctx *v8.Context, c *Context) error {
	c.registerBootstrap("artemis-webapi-more", webapiMoreBootstrap)
	return nil
}

const webapiMoreBootstrap = `
(() => {
  // ---------------- NodeFilter constants ----------------
  const NodeFilter = {
    FILTER_ACCEPT: 1, FILTER_REJECT: 2, FILTER_SKIP: 3,
    SHOW_ALL: 0xFFFFFFFF, SHOW_ELEMENT: 1, SHOW_ATTRIBUTE: 2,
    SHOW_TEXT: 4, SHOW_CDATA_SECTION: 8, SHOW_ENTITY_REFERENCE: 16,
    SHOW_ENTITY: 32, SHOW_PROCESSING_INSTRUCTION: 64, SHOW_COMMENT: 128,
    SHOW_DOCUMENT: 256, SHOW_DOCUMENT_TYPE: 512, SHOW_DOCUMENT_FRAGMENT: 1024,
    SHOW_NOTATION: 2048,
  };
  globalThis.NodeFilter = NodeFilter;

  function _matchesShow(node, whatToShow) {
    if (!node) return false;
    const t = node.nodeType;
    if (t === 1 && (whatToShow & 1)) return true;
    if (t === 3 && (whatToShow & 4)) return true;
    if (t === 8 && (whatToShow & 128)) return true;
    if (t === 9 && (whatToShow & 256)) return true;
    return whatToShow === 0xFFFFFFFF;
  }

  function _accept(node, filter) {
    if (!filter) return NodeFilter.FILTER_ACCEPT;
    if (typeof filter === 'function') return filter(node);
    if (filter && typeof filter.acceptNode === 'function') return filter.acceptNode(node);
    return NodeFilter.FILTER_ACCEPT;
  }

  // ---------------- TreeWalker ----------------
  class TreeWalker {
    constructor(root, whatToShow, filter) {
      this.root = root;
      this.whatToShow = whatToShow >>> 0 || 0xFFFFFFFF;
      this.filter = filter;
      this.currentNode = root;
    }
    _firstAcceptableChild(node) {
      let c = node && node.firstChild;
      while (c) {
        if (_matchesShow(c, this.whatToShow) && _accept(c, this.filter) === NodeFilter.FILTER_ACCEPT) return c;
        const inner = this._firstAcceptableChild(c);
        if (inner) return inner;
        c = c.nextSibling;
      }
      return null;
    }
    firstChild() {
      const n = this._firstAcceptableChild(this.currentNode);
      if (n) this.currentNode = n;
      return n;
    }
    nextSibling() {
      let n = this.currentNode && this.currentNode.nextSibling;
      while (n) {
        if (_matchesShow(n, this.whatToShow) && _accept(n, this.filter) === NodeFilter.FILTER_ACCEPT) {
          this.currentNode = n; return n;
        }
        n = n.nextSibling;
      }
      return null;
    }
    nextNode() {
      const c = this.firstChild();
      if (c) return c;
      let n = this.currentNode;
      while (n && n !== this.root) {
        const s = n.nextSibling;
        if (s && _matchesShow(s, this.whatToShow) && _accept(s, this.filter) === NodeFilter.FILTER_ACCEPT) {
          this.currentNode = s; return s;
        }
        n = n.parentNode;
      }
      return null;
    }
    parentNode() {
      let n = this.currentNode && this.currentNode.parentNode;
      while (n && n !== this.root) {
        if (_matchesShow(n, this.whatToShow) && _accept(n, this.filter) === NodeFilter.FILTER_ACCEPT) {
          this.currentNode = n; return n;
        }
        n = n.parentNode;
      }
      return null;
    }
  }
  globalThis.TreeWalker = TreeWalker;

  class NodeIterator {
    constructor(root, whatToShow, filter) {
      this.root = root;
      this.whatToShow = whatToShow >>> 0 || 0xFFFFFFFF;
      this.filter = filter;
      this.referenceNode = root;
      this.pointerBeforeReferenceNode = true;
    }
    nextNode() {
      const w = new TreeWalker(this.referenceNode, this.whatToShow, this.filter);
      const n = this.pointerBeforeReferenceNode ? this.referenceNode : w.nextNode();
      this.pointerBeforeReferenceNode = false;
      this.referenceNode = n;
      return n;
    }
    detach() {}
  }
  globalThis.NodeIterator = NodeIterator;

  // document.createTreeWalker / createNodeIterator
  if (document) {
    document.createTreeWalker = function(root, whatToShow, filter) {
      return new TreeWalker(root, whatToShow, filter);
    };
    document.createNodeIterator = function(root, whatToShow, filter) {
      return new NodeIterator(root, whatToShow, filter);
    };
    // createDocumentFragment returns a synthetic node with element-like children list
    document.createDocumentFragment = function() {
      const frag = {
        nodeType: 11,
        nodeName: '#document-fragment',
        _children: [],
        appendChild(c) { this._children.push(c); return c; },
        get childNodes() { return this._children.slice(); },
        get firstChild() { return this._children[0] || null; },
        get lastChild() { return this._children[this._children.length-1] || null; },
        querySelector() { return null; },
        querySelectorAll() { return []; },
      };
      return frag;
    };
  }

  // ---------------- DOMRect ----------------
  class DOMRect {
    constructor(x, y, width, height) {
      this.x = x || 0; this.y = y || 0;
      this.width = width || 0; this.height = height || 0;
      this.top = this.y; this.left = this.x;
      this.right = this.x + this.width; this.bottom = this.y + this.height;
    }
  }
  globalThis.DOMRect = DOMRect;

  // Element.getBoundingClientRect: we don't render, return zero rect
  if (globalThis.__ELEM_PROTO) {
    __ELEM_PROTO.getBoundingClientRect = function() { return new DOMRect(0, 0, 0, 0); };
    __ELEM_PROTO.getClientRects = function() {
      const arr = []; arr.length = 0; return arr;
    };
    // scrollIntoView - no-op since we don't render
    __ELEM_PROTO.scrollIntoView = function() {};
    __ELEM_PROTO.focus = function() {};
    __ELEM_PROTO.blur = function() {};
    __ELEM_PROTO.matches = function(sel) {
      const matches = document.querySelectorAll(sel);
      for (const m of matches) if (m && m.__id === this.__id) return true;
      return false;
    };
    __ELEM_PROTO.closest = function(sel) {
      let n = this;
      while (n) {
        if (n.matches && n.matches(sel)) return n;
        n = n.parentNode;
      }
      return null;
    };
    __ELEM_PROTO.contains = function(other) {
      let n = other;
      while (n) {
        if (n.__id === this.__id) return true;
        n = n.parentNode;
      }
      return false;
    };
    // dataset proxy: el.dataset.foo <-> el.getAttribute('data-foo')
    Object.defineProperty(__ELEM_PROTO, 'dataset', {
      get() {
        const self = this;
        return new Proxy({}, {
          get(_t, prop) {
            if (typeof prop !== 'string') return undefined;
            const k = 'data-' + prop.replace(/[A-Z]/g, m => '-' + m.toLowerCase());
            return self.getAttribute(k);
          },
          set(_t, prop, value) {
            if (typeof prop !== 'string') return true;
            const k = 'data-' + prop.replace(/[A-Z]/g, m => '-' + m.toLowerCase());
            self.setAttribute(k, String(value));
            return true;
          },
        });
      },
    });
    // classList proxy
    Object.defineProperty(__ELEM_PROTO, 'classList', {
      get() {
        const self = this;
        const list = (self.getAttribute('class') || '').split(/\s+/).filter(Boolean);
        return {
          length: list.length,
          contains(c) { return list.indexOf(c) >= 0; },
          add(...cs) {
            const cur = (self.getAttribute('class') || '').split(/\s+/).filter(Boolean);
            for (const c of cs) if (cur.indexOf(c) < 0) cur.push(c);
            self.setAttribute('class', cur.join(' '));
          },
          remove(...cs) {
            const cur = (self.getAttribute('class') || '').split(/\s+/).filter(x => x && cs.indexOf(x) < 0);
            self.setAttribute('class', cur.join(' '));
          },
          toggle(c, force) {
            const cur = (self.getAttribute('class') || '').split(/\s+/).filter(Boolean);
            const has = cur.indexOf(c) >= 0;
            const want = force === undefined ? !has : !!force;
            if (want && !has) cur.push(c);
            if (!want && has) cur.splice(cur.indexOf(c), 1);
            self.setAttribute('class', cur.join(' '));
            return want;
          },
          item(i) { return list[i]; },
          toString() { return list.join(' '); },
        };
      },
    });
  }

  // ---------------- queueMicrotask ----------------
  globalThis.queueMicrotask = function(fn) {
    if (typeof fn !== 'function') return;
    Promise.resolve().then(fn);
  };

  // ---------------- requestIdleCallback / cancelIdleCallback ----------------
  let _idleSeq = 0;
  const _idleCallbacks = new Map();
  globalThis.requestIdleCallback = function(fn, _opts) {
    if (typeof fn !== 'function') return 0;
    _idleSeq++;
    const id = _idleSeq;
    _idleCallbacks.set(id, fn);
    setTimeout(() => {
      const cb = _idleCallbacks.get(id);
      if (!cb) return;
      _idleCallbacks.delete(id);
      try { cb({ didTimeout: false, timeRemaining: () => 50 }); } catch (e) {}
    }, 0);
    return id;
  };
  globalThis.cancelIdleCallback = function(id) { _idleCallbacks.delete(id); };

  // ---------------- requestAnimationFrame / cancelAnimationFrame ----------------
  let _rafSeq = 0;
  const _rafCallbacks = new Map();
  globalThis.requestAnimationFrame = function(fn) {
    if (typeof fn !== 'function') return 0;
    _rafSeq++;
    const id = _rafSeq;
    _rafCallbacks.set(id, fn);
    setTimeout(() => {
      const cb = _rafCallbacks.get(id);
      if (!cb) return;
      _rafCallbacks.delete(id);
      try { cb(performance.now()); } catch (e) {}
    }, 0);
    return id;
  };
  globalThis.cancelAnimationFrame = function(id) { _rafCallbacks.delete(id); };

  // ---------------- IntersectionObserver / ResizeObserver (stubs) ----------------
  // We don't render, so these never fire. Existence is enough so feature
  // detection passes.
  class IntersectionObserver {
    constructor(callback, options) { this._cb = callback; this._opts = options || {}; }
    observe() {}
    unobserve() {}
    disconnect() {}
    takeRecords() { return []; }
  }
  class ResizeObserver {
    constructor(callback) { this._cb = callback; }
    observe() {}
    unobserve() {}
    disconnect() {}
  }
  class ReportingObserver {
    constructor(callback) { this._cb = callback; }
    observe() {}
    disconnect() {}
    takeRecords() { return []; }
  }
  globalThis.IntersectionObserver = IntersectionObserver;
  globalThis.ResizeObserver = ResizeObserver;
  globalThis.ReportingObserver = ReportingObserver;

  // ---------------- additional Event subtypes ----------------
  class PointerEvent extends MouseEvent {
    constructor(type, init) {
      super(type, init); init = init || {};
      this.pointerId = init.pointerId || 0;
      this.pointerType = init.pointerType || '';
      this.isPrimary = !!init.isPrimary;
      this.width = init.width || 1; this.height = init.height || 1;
      this.pressure = init.pressure || 0;
      this.tangentialPressure = init.tangentialPressure || 0;
      this.tiltX = init.tiltX || 0; this.tiltY = init.tiltY || 0;
      this.twist = init.twist || 0;
    }
  }
  class WheelEvent extends MouseEvent {
    constructor(type, init) {
      super(type, init); init = init || {};
      this.deltaX = init.deltaX || 0; this.deltaY = init.deltaY || 0; this.deltaZ = init.deltaZ || 0;
      this.deltaMode = init.deltaMode || 0;
    }
  }
  class TouchEvent extends UIEvent {
    constructor(type, init) {
      super(type, init); init = init || {};
      this.touches = init.touches || []; this.targetTouches = init.targetTouches || [];
      this.changedTouches = init.changedTouches || [];
      this.altKey = !!init.altKey; this.shiftKey = !!init.shiftKey;
      this.ctrlKey = !!init.ctrlKey; this.metaKey = !!init.metaKey;
    }
  }
  class CompositionEvent extends UIEvent {
    constructor(type, init) {
      super(type, init); init = init || {};
      this.data = init.data == null ? '' : String(init.data);
    }
  }
  class DragEvent extends MouseEvent {
    constructor(type, init) { super(type, init); this.dataTransfer = (init && init.dataTransfer) || null; }
  }
  class ProgressEvent extends Event {
    constructor(type, init) {
      super(type, init); init = init || {};
      this.lengthComputable = !!init.lengthComputable;
      this.loaded = init.loaded || 0; this.total = init.total || 0;
    }
  }
  class MessageEvent extends Event {
    constructor(type, init) {
      super(type, init); init = init || {};
      this.data = init.data; this.origin = init.origin || '';
      this.lastEventId = init.lastEventId || ''; this.source = init.source || null;
      this.ports = init.ports || [];
    }
  }
  class ErrorEvent extends Event {
    constructor(type, init) {
      super(type, init); init = init || {};
      this.message = init.message || ''; this.filename = init.filename || '';
      this.lineno = init.lineno || 0; this.colno = init.colno || 0;
      this.error = init.error || null;
    }
  }
  class StorageEvent extends Event {
    constructor(type, init) {
      super(type, init); init = init || {};
      this.key = init.key || null; this.oldValue = init.oldValue == null ? null : init.oldValue;
      this.newValue = init.newValue == null ? null : init.newValue;
      this.url = init.url || ''; this.storageArea = init.storageArea || null;
    }
  }
  class HashChangeEvent extends Event {
    constructor(type, init) {
      super(type, init); init = init || {};
      this.oldURL = init.oldURL || ''; this.newURL = init.newURL || '';
    }
  }
  class PopStateEvent extends Event {
    constructor(type, init) {
      super(type, init); init = init || {};
      this.state = init.state == null ? null : init.state;
    }
  }
  class BeforeUnloadEvent extends Event {
    constructor() { super('beforeunload', {cancelable: true}); this.returnValue = ''; }
  }
  class CloseEvent extends Event {
    constructor(type, init) {
      super(type, init); init = init || {};
      this.code = init.code || 1000; this.reason = init.reason || ''; this.wasClean = !!init.wasClean;
    }
  }
  class AnimationEvent extends Event {
    constructor(type, init) {
      super(type, init); init = init || {};
      this.animationName = init.animationName || ''; this.elapsedTime = init.elapsedTime || 0;
      this.pseudoElement = init.pseudoElement || '';
    }
  }
  class TransitionEvent extends Event {
    constructor(type, init) {
      super(type, init); init = init || {};
      this.propertyName = init.propertyName || ''; this.elapsedTime = init.elapsedTime || 0;
      this.pseudoElement = init.pseudoElement || '';
    }
  }
  globalThis.PointerEvent = PointerEvent;
  globalThis.WheelEvent = WheelEvent;
  globalThis.TouchEvent = TouchEvent;
  globalThis.CompositionEvent = CompositionEvent;
  globalThis.DragEvent = DragEvent;
  globalThis.ProgressEvent = ProgressEvent;
  globalThis.MessageEvent = MessageEvent;
  globalThis.ErrorEvent = ErrorEvent;
  globalThis.StorageEvent = StorageEvent;
  globalThis.HashChangeEvent = HashChangeEvent;
  globalThis.PopStateEvent = PopStateEvent;
  globalThis.BeforeUnloadEvent = BeforeUnloadEvent;
  globalThis.CloseEvent = CloseEvent;
  globalThis.AnimationEvent = AnimationEvent;
  globalThis.TransitionEvent = TransitionEvent;

  // ---------------- HTMLCollection-style live wrappers on document ----------------
  if (document) {
    // document.forms / images / links / scripts / styleSheets - return arrays computed at access time
    const liveBy = (sel) => {
      Object.defineProperty(document, sel.name, {
        get() { return document.querySelectorAll(sel.css); },
        configurable: true,
      });
    };
    liveBy({name: 'forms',   css: 'form'});
    liveBy({name: 'images',  css: 'img'});
    liveBy({name: 'links',   css: 'a[href], area[href]'});
    liveBy({name: 'scripts', css: 'script'});
  }

  // ---------------- FormData (simple impl, urlencoded surface) ----------------
  class FormData {
    constructor(form) {
      this._pairs = [];
      if (!form) return;
      // walk form's named inputs/selects/textareas
      const inputs = form.querySelectorAll && form.querySelectorAll('input,select,textarea');
      if (!inputs) return;
      for (let i = 0; i < (inputs.length || 0); i++) {
        const el = inputs[i]; if (!el) continue;
        const name = el.getAttribute('name'); if (!name) continue;
        const type = (el.getAttribute('type') || '').toLowerCase();
        if (type === 'checkbox' || type === 'radio') {
          if (el.getAttribute('checked') == null) continue;
          this.append(name, el.getAttribute('value') || 'on');
        } else if (type === 'file' || type === 'submit' || type === 'button') {
          continue;
        } else {
          this.append(name, el.getAttribute('value') || el.textContent || '');
        }
      }
    }
    append(k, v) { this._pairs.push([String(k), String(v)]); }
    set(k, v) { this.delete(k); this.append(k, v); }
    delete(k) { this._pairs = this._pairs.filter(p => p[0] !== k); }
    get(k) { for (const p of this._pairs) if (p[0] === k) return p[1]; return null; }
    getAll(k) { return this._pairs.filter(p => p[0] === k).map(p => p[1]); }
    has(k) { return this.get(k) !== null; }
    *entries() { for (const p of this._pairs) yield [p[0], p[1]]; }
    *keys() { for (const p of this._pairs) yield p[0]; }
    *values() { for (const p of this._pairs) yield p[1]; }
    [Symbol.iterator]() { return this.entries(); }
    forEach(fn, thisArg) { for (const [k, v] of this._pairs) fn.call(thisArg, v, k, this); }
    toString() {
      return this._pairs.map(p => encodeURIComponent(p[0]) + '=' + encodeURIComponent(p[1])).join('&');
    }
  }
  globalThis.FormData = FormData;

  // ---------------- Atomics / WeakRef-style stubs already provided by V8 ----------------
  // (V8 has these built-in; nothing to do here.)
})();
`
