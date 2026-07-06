package js

import v8 "rogchap.com/v8go"

// installWebAPIV3 installs the long-tail surfaces: Canvas 2D stubs,
// Web Animations, BroadcastChannel, ReadableStream/WritableStream
// skeletons, ShadowRoot stubs, Form validity API, Range/Selection,
// HTMLElement subclass markers.
func installWebAPIV3(iso *v8.Isolate, v8ctx *v8.Context, c *Context) error {
	c.registerBootstrap("artemis-webapi-v3", webapiV3Bootstrap)
	return nil
}

const webapiV3Bootstrap = `
(() => {
  // ---------------- Canvas 2D context stub ----------------
  // Sites feature-detect via document.createElement('canvas').getContext('2d').
  // We don't render but return a stub so detection passes and code does not throw.
  class CanvasGradient {
    addColorStop(_offset, _color) {}
  }
  class CanvasPattern {}
  class ImageData {
    constructor(w, h) {
      this.width = w; this.height = h;
      this.data = new Array(w * h * 4).fill(0);
    }
  }
  class CanvasRenderingContext2D {
    constructor(canvas) {
      this.canvas = canvas;
      this.fillStyle = '#000';
      this.strokeStyle = '#000';
      this.globalAlpha = 1;
      this.lineWidth = 1;
      this.font = '10px sans-serif';
      this.textAlign = 'start';
      this.textBaseline = 'alphabetic';
    }
    save() {} restore() {} scale() {} rotate() {} translate() {} transform() {}
    setTransform() {} resetTransform() {}
    createLinearGradient() { return new CanvasGradient(); }
    createRadialGradient() { return new CanvasGradient(); }
    createConicGradient() { return new CanvasGradient(); }
    createPattern() { return new CanvasPattern(); }
    fillRect() {} clearRect() {} strokeRect() {}
    beginPath() {} closePath() {} moveTo() {} lineTo() {} bezierCurveTo() {}
    quadraticCurveTo() {} arc() {} arcTo() {} ellipse() {} rect() {}
    fill() {} stroke() {} clip() {}
    fillText() {} strokeText() {}
    measureText(s) { return { width: (String(s||'').length)*6 }; }
    drawImage() {}
    createImageData(w, h) { return new ImageData(w, h); }
    getImageData(_x, _y, w, h) { return new ImageData(w, h); }
    putImageData() {}
    isPointInPath() { return false; }
    isPointInStroke() { return false; }
    setLineDash() {} getLineDash() { return []; }
  }
  globalThis.CanvasRenderingContext2D = CanvasRenderingContext2D;
  globalThis.CanvasGradient = CanvasGradient;
  globalThis.CanvasPattern = CanvasPattern;
  globalThis.ImageData = ImageData;

  // hook getContext into element prototype lazily; canvas is just an element
  if (globalThis.__ELEM_PROTO) {
    __ELEM_PROTO.getContext = function(kind) {
      if (kind === '2d') {
        if (!this._ctx2d) this._ctx2d = new CanvasRenderingContext2D(this);
        return this._ctx2d;
      }
      // webgl, webgl2, bitmaprenderer: return null (feature absent) so detection fails cleanly
      return null;
    };
    __ELEM_PROTO.toDataURL = function() { return 'data:,'; };
    __ELEM_PROTO.toBlob = function(cb) { try { cb(new Blob([])); } catch (e) {} };
    // width/height for canvas
    Object.defineProperty(__ELEM_PROTO, 'width', {
      get() { return parseInt(this.getAttribute('width') || '300', 10); },
      set(v) { this.setAttribute('width', String(v|0)); },
      configurable: true,
    });
    Object.defineProperty(__ELEM_PROTO, 'height', {
      get() { return parseInt(this.getAttribute('height') || '150', 10); },
      set(v) { this.setAttribute('height', String(v|0)); },
      configurable: true,
    });
  }

  // ---------------- Web Animations API (skeleton) ----------------
  class Animation {
    constructor(effect, timeline) {
      this.effect = effect || null; this.timeline = timeline || null;
      this.id = ''; this.startTime = null; this.currentTime = 0;
      this.playState = 'idle'; this.playbackRate = 1;
      this.finished = Promise.resolve(this);
      this.ready = Promise.resolve(this);
      this.onfinish = null; this.oncancel = null;
    }
    play() { this.playState = 'finished'; if (typeof this.onfinish === 'function') queueMicrotask(() => this.onfinish({})); return this; }
    pause() { this.playState = 'paused'; return this; }
    cancel() { this.playState = 'idle'; if (typeof this.oncancel === 'function') queueMicrotask(() => this.oncancel({})); }
    finish() { this.playState = 'finished'; }
    reverse() { return this; }
    updatePlaybackRate(rate) { this.playbackRate = rate; }
  }
  class KeyframeEffect {
    constructor(target, keyframes, options) {
      this.target = target;
      this.keyframes = keyframes || [];
      this.options = options || {};
    }
    getKeyframes() { return this.keyframes.slice(); }
    setKeyframes(k) { this.keyframes = k || []; }
  }
  globalThis.Animation = Animation;
  globalThis.KeyframeEffect = KeyframeEffect;
  if (globalThis.__ELEM_PROTO) {
    __ELEM_PROTO.animate = function(keyframes, options) {
      const eff = new KeyframeEffect(this, keyframes, options);
      const a = new Animation(eff);
      // immediately settled - we don't actually animate
      a.play();
      return a;
    };
    __ELEM_PROTO.getAnimations = function() { return []; };
  }

  // ---------------- BroadcastChannel ----------------
  const _bcChannels = new Map();
  class BroadcastChannel {
    constructor(name) {
      this.name = String(name);
      this.onmessage = null; this.onmessageerror = null;
      this._closed = false;
      let chan = _bcChannels.get(this.name);
      if (!chan) { chan = new Set(); _bcChannels.set(this.name, chan); }
      chan.add(this);
    }
    postMessage(data) {
      if (this._closed) return;
      const chan = _bcChannels.get(this.name);
      if (!chan) return;
      for (const peer of chan) {
        if (peer === this || peer._closed) continue;
        const ev = new MessageEvent('message', {data});
        if (typeof peer.onmessage === 'function') {
          queueMicrotask(() => { try { peer.onmessage(ev); } catch (e) {} });
        }
      }
    }
    close() {
      this._closed = true;
      const chan = _bcChannels.get(this.name);
      if (chan) chan.delete(this);
    }
    addEventListener(type, fn) { if (type === 'message') this.onmessage = fn; }
    removeEventListener(type, fn) { if (type === 'message' && this.onmessage === fn) this.onmessage = null; }
    dispatchEvent() { return true; }
  }
  globalThis.BroadcastChannel = BroadcastChannel;

  // ---------------- ReadableStream / WritableStream / TransformStream ----------------
  // Functional in-memory implementation. No backpressure (every desiredSize
  // is 1, every ready resolves immediately) but honours the actual data
  // flow through getReader / pipeTo / pipeThrough / tee. Sufficient for
  // any site that uses Streams as a chunk-handoff abstraction.
  class ReadableStream {
    constructor(source) {
      this._source = source || {};
      this._chunks = [];
      this._done = false;
      this._error = undefined;
      this._readers = []; // pending {resolve,reject} from read()
      this.locked = false;
      const self = this;
      this._controller = {
        enqueue(c) {
          if (self._done) return;
          if (self._readers.length > 0) {
            self._readers.shift().resolve({value: c, done: false});
          } else {
            self._chunks.push(c);
          }
        },
        close() {
          self._done = true;
          while (self._readers.length > 0) {
            self._readers.shift().resolve({value: undefined, done: true});
          }
        },
        error(e) {
          self._error = e; self._done = true;
          while (self._readers.length > 0) {
            self._readers.shift().reject(e);
          }
        },
        get desiredSize() { return self._done ? null : 1; },
      };
      if (typeof this._source.start === 'function') {
        try { this._source.start(this._controller); } catch (e) { this._error = e; this._done = true; }
      }
    }
    _pull() {
      if (typeof this._source.pull === 'function' && !this._done) {
        try { this._source.pull(this._controller); } catch (e) { this._controller.error(e); }
      }
    }
    getReader() {
      if (this.locked) throw new TypeError('ReadableStream already locked');
      const self = this;
      self.locked = true;
      return {
        read() {
          if (self._error) return Promise.reject(self._error);
          if (self._chunks.length > 0) {
            return Promise.resolve({value: self._chunks.shift(), done: false});
          }
          if (self._done) return Promise.resolve({value: undefined, done: true});
          self._pull();
          if (self._chunks.length > 0) {
            return Promise.resolve({value: self._chunks.shift(), done: false});
          }
          if (self._done) return Promise.resolve({value: undefined, done: true});
          return new Promise((resolve, reject) => self._readers.push({resolve, reject}));
        },
        releaseLock() { self.locked = false; },
        cancel(reason) { return self.cancel(reason); },
        closed: Promise.resolve(),
      };
    }
    cancel(reason) {
      this._done = true;
      this._chunks.length = 0;
      if (typeof this._source.cancel === 'function') {
        try { this._source.cancel(reason); } catch (e) {}
      }
      return Promise.resolve();
    }
    async pipeTo(dest) {
      const reader = this.getReader();
      const writer = dest.getWriter();
      try {
        for (;;) {
          const {value, done} = await reader.read();
          if (done) break;
          await writer.write(value);
        }
        await writer.close();
      } catch (e) {
        try { await writer.abort(e); } catch (_) {}
        throw e;
      } finally {
        reader.releaseLock();
        writer.releaseLock();
      }
    }
    pipeThrough(transform) {
      // Fire-and-forget pipe; the returned readable receives transformed chunks.
      this.pipeTo(transform.writable).catch(() => {});
      return transform.readable;
    }
    tee() {
      const buf1 = [], buf2 = [];
      let done = false, err = undefined;
      const consume = async () => {
        const reader = this.getReader();
        try {
          for (;;) {
            const r = await reader.read();
            if (r.done) break;
            buf1.push(r.value); buf2.push(r.value);
            if (s1 && s1._controller) s1._controller.enqueue(r.value);
            if (s2 && s2._controller) s2._controller.enqueue(r.value);
          }
        } catch (e) {
          err = e;
          if (s1 && s1._controller) s1._controller.error(e);
          if (s2 && s2._controller) s2._controller.error(e);
        } finally {
          done = true;
          if (s1 && s1._controller) s1._controller.close();
          if (s2 && s2._controller) s2._controller.close();
          reader.releaseLock();
        }
      };
      const s1 = new ReadableStream({});
      const s2 = new ReadableStream({});
      consume();
      return [s1, s2];
    }
  }
  class WritableStream {
    constructor(sink) {
      this._sink = sink || {};
      this._closed = false;
      this.locked = false;
      if (typeof this._sink.start === 'function') {
        try { this._sink.start({error: (e) => { this._error = e; }}); } catch (e) { this._error = e; }
      }
    }
    getWriter() {
      if (this.locked) throw new TypeError('WritableStream already locked');
      const self = this;
      self.locked = true;
      return {
        write(chunk) {
          if (self._error) return Promise.reject(self._error);
          if (self._closed) return Promise.reject(new TypeError('writer closed'));
          if (self._sink.write) {
            try { return Promise.resolve(self._sink.write(chunk, this)); }
            catch (e) { return Promise.reject(e); }
          }
          return Promise.resolve();
        },
        close() {
          self._closed = true;
          if (self._sink.close) try { return Promise.resolve(self._sink.close()); } catch (e) { return Promise.reject(e); }
          return Promise.resolve();
        },
        abort(e) {
          self._error = e; self._closed = true;
          if (self._sink.abort) try { self._sink.abort(e); } catch (_) {}
          return Promise.resolve();
        },
        releaseLock() { self.locked = false; },
        closed: Promise.resolve(),
        ready: Promise.resolve(),
        desiredSize: 1,
      };
    }
    abort(e) { this._error = e; this._closed = true; return Promise.resolve(); }
    close() { this._closed = true; return Promise.resolve(); }
  }
  class TransformStream {
    constructor(transformer) {
      transformer = transformer || {};
      let readableController = null;
      this.readable = new ReadableStream({
        start(c) { readableController = c; },
      });
      this.writable = new WritableStream({
        async write(chunk) {
          if (typeof transformer.transform === 'function') {
            await transformer.transform(chunk, readableController);
          } else {
            readableController.enqueue(chunk);
          }
        },
        async close() {
          if (typeof transformer.flush === 'function') {
            await transformer.flush(readableController);
          }
          readableController.close();
        },
        abort(e) { readableController.error(e); },
      });
      if (typeof transformer.start === 'function') {
        try { transformer.start(readableController); } catch (e) { readableController.error(e); }
      }
    }
  }
  globalThis.ReadableStream = ReadableStream;
  globalThis.WritableStream = WritableStream;
  globalThis.TransformStream = TransformStream;

  // ---------------- ShadowRoot stub ----------------
  // attachShadow returns an object that wraps the element itself. Light DOM
  // since we don't actually implement shadow encapsulation.
  if (globalThis.__ELEM_PROTO) {
    __ELEM_PROTO.attachShadow = function(opts) {
      // Return a thin wrapper: same element handle, expose innerHTML and
      // basic query methods.
      const el = this;
      return {
        host: el,
        mode: (opts && opts.mode) || 'open',
        get innerHTML() { return el.innerHTML; },
        set innerHTML(v) { el.innerHTML = v; },
        get childNodes() { return el.childNodes; },
        get firstChild() { return el.firstChild; },
        appendChild(c) { return el.appendChild(c); },
        querySelector(s) { return el.querySelector(s); },
        querySelectorAll(s) { return el.querySelectorAll(s); },
      };
    };
    Object.defineProperty(__ELEM_PROTO, 'shadowRoot', { get() { return null; }, configurable: true });
  }

  // ---------------- Form validity API ----------------
  class ValidityState {
    constructor(input) {
      this.valueMissing = false;
      this.typeMismatch = false;
      this.patternMismatch = false;
      this.tooLong = false;
      this.tooShort = false;
      this.rangeUnderflow = false;
      this.rangeOverflow = false;
      this.stepMismatch = false;
      this.badInput = false;
      this.customError = false;
      this.valid = true;
      // Compute basic validity from attributes
      if (input) {
        const required = input.getAttribute('required') !== null;
        const value = input.getAttribute('value') || '';
        if (required && value === '') {
          this.valueMissing = true; this.valid = false;
        }
        const pattern = input.getAttribute('pattern');
        if (pattern && value !== '') {
          try {
            const re = new RegExp('^(?:' + pattern + ')$');
            if (!re.test(value)) { this.patternMismatch = true; this.valid = false; }
          } catch (e) {}
        }
      }
    }
  }
  globalThis.ValidityState = ValidityState;
  if (globalThis.__ELEM_PROTO) {
    __ELEM_PROTO.checkValidity = function() {
      const v = new ValidityState(this);
      return v.valid;
    };
    __ELEM_PROTO.reportValidity = function() { return this.checkValidity(); };
    __ELEM_PROTO.setCustomValidity = function(msg) { this._customValidity = String(msg || ''); };
    Object.defineProperty(__ELEM_PROTO, 'validity', {
      get() { return new ValidityState(this); },
      configurable: true,
    });
    Object.defineProperty(__ELEM_PROTO, 'validationMessage', {
      get() { return this._customValidity || ''; },
      configurable: true,
    });
    Object.defineProperty(__ELEM_PROTO, 'willValidate', {
      get() {
        const tag = this.tagName;
        return tag === 'INPUT' || tag === 'SELECT' || tag === 'TEXTAREA' || tag === 'BUTTON';
      },
      configurable: true,
    });
  }

  // ---------------- Range / Selection ----------------
  // _childIndexOf returns the index of node within its parent's childNodes
  // (NodeList over real children), or -1 if detached. Used by setStartBefore
  // / setEndAfter to mirror DOM Standard offset semantics.
  function _childIndexOf(node) {
    if (!node || !node.parentNode || !node.parentNode.childNodes) return -1;
    const kids = node.parentNode.childNodes;
    for (let i = 0; i < kids.length; i++) if (kids[i] === node) return i;
    return -1;
  }
  function _commonAncestor(a, b) {
    if (!a) return b; if (!b) return a;
    const aChain = [];
    for (let n = a; n; n = n.parentNode) aChain.push(n);
    const aSet = new Set(aChain);
    for (let n = b; n; n = n.parentNode) if (aSet.has(n)) return n;
    return null;
  }
  class Range {
    constructor() {
      this.startContainer = null; this.endContainer = null;
      this.startOffset = 0; this.endOffset = 0;
      this.collapsed = true;
      this.commonAncestorContainer = null;
    }
    setStart(node, off) { this.startContainer = node; this.startOffset = off|0; this._update(); }
    setEnd(node, off) { this.endContainer = node; this.endOffset = off|0; this._update(); }
    setStartBefore(node) {
      const i = _childIndexOf(node);
      this.setStart(node && node.parentNode, i < 0 ? 0 : i);
    }
    setStartAfter(node) {
      const i = _childIndexOf(node);
      this.setStart(node && node.parentNode, i < 0 ? 0 : i + 1);
    }
    setEndBefore(node) {
      const i = _childIndexOf(node);
      this.setEnd(node && node.parentNode, i < 0 ? 0 : i);
    }
    setEndAfter(node) {
      const i = _childIndexOf(node);
      this.setEnd(node && node.parentNode, i < 0 ? 0 : i + 1);
    }
    selectNode(node) {
      const i = _childIndexOf(node);
      const p = node && node.parentNode;
      this.setStart(p, i < 0 ? 0 : i);
      this.setEnd(p, i < 0 ? 0 : i + 1);
    }
    selectNodeContents(node) {
      this.setStart(node, 0);
      // For text/comment/cdata: endOffset is data length. For element/document:
      // it's child count. Branch on nodeType.
      if (node && (node.nodeType === 3 || node.nodeType === 4 || node.nodeType === 8)) {
        this.setEnd(node, (node.data || node.nodeValue || '').length);
      } else if (node && node.childNodes) {
        this.setEnd(node, node.childNodes.length);
      } else {
        this.setEnd(node, 0);
      }
    }
    collapse(toStart) {
      if (toStart) { this.endContainer = this.startContainer; this.endOffset = this.startOffset; }
      else { this.startContainer = this.endContainer; this.startOffset = this.endOffset; }
      this.collapsed = true;
    }
    cloneRange() {
      const r = new Range();
      r.startContainer = this.startContainer; r.startOffset = this.startOffset;
      r.endContainer = this.endContainer; r.endOffset = this.endOffset;
      r._update();
      return r;
    }
    deleteContents() {
      // Best-effort: clear text content in collapsed/text-only ranges.
      if (this.collapsed) return;
      if (this.startContainer === this.endContainer &&
          this.startContainer && this.startContainer.nodeType === 3) {
        const t = this.startContainer;
        const data = t.data || '';
        t.data = data.slice(0, this.startOffset) + data.slice(this.endOffset);
      }
    }
    extractContents() {
      const f = document.createDocumentFragment();
      // Best-effort: copy substring from a single text node.
      if (this.startContainer === this.endContainer &&
          this.startContainer && this.startContainer.nodeType === 3) {
        const t = this.startContainer;
        const data = t.data || '';
        const slice = data.slice(this.startOffset, this.endOffset);
        f.appendChild(document.createTextNode(slice));
        t.data = data.slice(0, this.startOffset) + data.slice(this.endOffset);
      }
      return f;
    }
    cloneContents() {
      const f = document.createDocumentFragment();
      if (this.startContainer === this.endContainer &&
          this.startContainer && this.startContainer.nodeType === 3) {
        const data = this.startContainer.data || '';
        f.appendChild(document.createTextNode(data.slice(this.startOffset, this.endOffset)));
      }
      return f;
    }
    insertNode(n) {
      if (!n || !this.startContainer || !this.startContainer.appendChild) return;
      this.startContainer.appendChild(n);
    }
    surroundContents(_n) {}
    toString() {
      // Return the text content covered by the range, when both containers
      // are the same text node. Multi-node ranges fall back to empty.
      if (this.startContainer === this.endContainer &&
          this.startContainer && this.startContainer.nodeType === 3) {
        return (this.startContainer.data || '').slice(this.startOffset, this.endOffset);
      }
      return '';
    }
    detach() {}
    _update() {
      this.collapsed = (this.startContainer === this.endContainer && this.startOffset === this.endOffset);
      this.commonAncestorContainer = _commonAncestor(this.startContainer, this.endContainer);
    }
    isPointInRange(node, offset) {
      // Same container only; coarse but useful.
      if (node === this.startContainer && node === this.endContainer) {
        return offset >= this.startOffset && offset <= this.endOffset;
      }
      return false;
    }
    comparePoint(node, offset) {
      if (node === this.startContainer && offset < this.startOffset) return -1;
      if (node === this.endContainer && offset > this.endOffset) return 1;
      return 0;
    }
    intersectsNode(node) {
      if (!node) return false;
      // Cheap approximation: true if node lives under commonAncestorContainer.
      for (let n = node; n; n = n.parentNode) if (n === this.commonAncestorContainer) return true;
      return false;
    }
    getBoundingClientRect() { return new DOMRect(0, 0, 0, 0); }
    getClientRects() { return []; }
  }
  globalThis.Range = Range;

  class Selection {
    constructor() {
      this._ranges = [];
      this.anchorNode = null; this.anchorOffset = 0;
      this.focusNode = null; this.focusOffset = 0;
      this.isCollapsed = true;
      this.rangeCount = 0;
      this.type = 'None';
    }
    addRange(r) {
      this._ranges.push(r); this.rangeCount = this._ranges.length;
      this.anchorNode = r.startContainer; this.anchorOffset = r.startOffset;
      this.focusNode = r.endContainer; this.focusOffset = r.endOffset;
      this.isCollapsed = r.collapsed;
      this.type = r.collapsed ? 'Caret' : 'Range';
    }
    removeAllRanges() { this._ranges = []; this.rangeCount = 0; this.isCollapsed = true; this.type = 'None'; }
    removeRange(_r) { this.removeAllRanges(); }
    getRangeAt(i) { return this._ranges[i] || null; }
    collapse(node, off) {
      this.anchorNode = node; this.focusNode = node;
      this.anchorOffset = off || 0; this.focusOffset = off || 0;
      this.isCollapsed = true; this.type = 'Caret';
      // Spec: collapse() also installs a fresh collapsed Range at the position.
      // Without this, extend() has nothing to mutate.
      const r = new Range();
      r.setStart(node, off || 0); r.setEnd(node, off || 0);
      this._ranges = [r];
      this.rangeCount = 1;
    }
    extend(node, offset) {
      this.focusNode = node; this.focusOffset = offset|0;
      if (this._ranges.length > 0) {
        const r = this._ranges[0];
        r.endContainer = node; r.endOffset = offset|0;
        r._update();
        this.isCollapsed = r.collapsed;
        this.type = r.collapsed ? 'Caret' : 'Range';
      }
    }
    selectAllChildren(node) {
      if (!node) return;
      const r = new Range();
      r.selectNodeContents(node);
      this.removeAllRanges();
      this.addRange(r);
    }
    deleteFromDocument() {
      for (const r of this._ranges) r.deleteContents();
    }
    containsNode(node, partial) {
      // True if any range overlaps node. partial=true means any overlap;
      // partial=false (default) requires full containment (we approximate).
      if (!node) return false;
      for (const r of this._ranges) {
        const sc = r.startContainer, ec = r.endContainer;
        // Direct match on the boundary containers.
        if (node === sc || node === ec) return true;
        // partial: range endpoint sits inside node's subtree.
        if (partial) {
          for (let n = sc; n; n = n.parentNode) if (n === node) return true;
          for (let n = ec; n; n = n.parentNode) if (n === node) return true;
        }
        // Full containment: node itself sits inside the common ancestor.
        for (let n = node; n; n = n.parentNode) {
          if (n === r.commonAncestorContainer) return true;
        }
      }
      return false;
    }
    toString() {
      let out = '';
      for (const r of this._ranges) out += r.toString();
      return out;
    }
  }
  globalThis.Selection = Selection;
  const _docSelection = new Selection();
  if (document) {
    document.createRange = function() { return new Range(); };
    document.getSelection = function() { return _docSelection; };
    globalThis.getSelection = function() { return _docSelection; };
  }

  // ---------------- HTMLElement subclasses (markers via instanceof support) ----------------
  // Sites do "el instanceof HTMLInputElement". We expose the constructor
  // function with a Symbol.hasInstance that checks tagName.
  // _tagInstance returns a hasInstance hook matching one or many tags.
  // Spec sometimes maps multiple tags to one class (HTMLHeadingElement
  // covers H1..H6, HTMLQuoteElement covers Q+BLOCKQUOTE, etc.).
  function _tagInstance(tagOrTags) {
    if (Array.isArray(tagOrTags)) {
      const set = new Set(tagOrTags);
      return {
        [Symbol.hasInstance](el) {
          return el && typeof el === 'object' && set.has(el.tagName);
        },
      };
    }
    return {
      [Symbol.hasInstance](el) {
        return el && typeof el === 'object' && el.tagName === tagOrTags;
      },
    };
  }
  function _defineSubclass(name, tagOrTags) {
    const ctor = function() { throw new TypeError('Illegal constructor'); };
    ctor.prototype = Object.create(globalThis.__ELEM_PROTO || Object.prototype);
    Object.setPrototypeOf(ctor, _tagInstance(tagOrTags));
    globalThis[name] = ctor;
  }
  // Generic HTMLElement / Element / Node — all elements satisfy these.
  const HTMLElement = function() { throw new TypeError('Illegal constructor'); };
  HTMLElement.prototype = Object.create(globalThis.__ELEM_PROTO || Object.prototype);
  Object.defineProperty(HTMLElement, Symbol.hasInstance, {
    value(el) { return el && typeof el === 'object' && typeof el.tagName === 'string'; },
  });
  globalThis.HTMLElement = HTMLElement;
  globalThis.Element = HTMLElement;
  globalThis.Node = HTMLElement;

  // Full HTML living-standard subclass list, plus a handful of legacy
  // aliases (HTMLDirectoryElement, HTMLFontElement, HTMLMarqueeElement,
  // HTMLFrameElement, HTMLFrameSetElement) that deprecated sites still
  // reference. Spec-shared classes (heading, mod, quote, table-section,
  // table-col) take an array of tags.
  const _subclasses = [
    ['HTMLInputElement', 'INPUT'],
    ['HTMLAnchorElement', 'A'],
    ['HTMLImageElement', 'IMG'],
    ['HTMLScriptElement', 'SCRIPT'],
    ['HTMLStyleElement', 'STYLE'],
    ['HTMLLinkElement', 'LINK'],
    ['HTMLMetaElement', 'META'],
    ['HTMLDivElement', 'DIV'],
    ['HTMLSpanElement', 'SPAN'],
    ['HTMLParagraphElement', 'P'],
    ['HTMLHeadingElement', ['H1','H2','H3','H4','H5','H6']],
    ['HTMLTableElement', 'TABLE'],
    ['HTMLTableRowElement', 'TR'],
    ['HTMLTableCellElement', ['TD','TH']],
    ['HTMLTableCaptionElement', 'CAPTION'],
    ['HTMLTableColElement', ['COL','COLGROUP']],
    ['HTMLTableSectionElement', ['THEAD','TBODY','TFOOT']],
    ['HTMLOptionElement', 'OPTION'],
    ['HTMLOptGroupElement', 'OPTGROUP'],
    ['HTMLSelectElement', 'SELECT'],
    ['HTMLTextAreaElement', 'TEXTAREA'],
    ['HTMLFormElement', 'FORM'],
    ['HTMLButtonElement', 'BUTTON'],
    ['HTMLLabelElement', 'LABEL'],
    ['HTMLFieldSetElement', 'FIELDSET'],
    ['HTMLLegendElement', 'LEGEND'],
    ['HTMLOutputElement', 'OUTPUT'],
    ['HTMLLIElement', 'LI'],
    ['HTMLUListElement', 'UL'],
    ['HTMLOListElement', 'OL'],
    ['HTMLDListElement', 'DL'],
    ['HTMLMenuElement', 'MENU'],
    ['HTMLCanvasElement', 'CANVAS'],
    ['HTMLAudioElement', 'AUDIO'],
    ['HTMLVideoElement', 'VIDEO'],
    ['HTMLMediaElement', ['AUDIO','VIDEO']], // base class for media
    ['HTMLIFrameElement', 'IFRAME'],
    ['HTMLBodyElement', 'BODY'],
    ['HTMLHtmlElement', 'HTML'],
    ['HTMLHeadElement', 'HEAD'],
    ['HTMLTitleElement', 'TITLE'],
    ['HTMLPreElement', 'PRE'],
    ['HTMLBRElement', 'BR'],
    ['HTMLHRElement', 'HR'],
    ['HTMLTemplateElement', 'TEMPLATE'],
    ['HTMLSlotElement', 'SLOT'],
    ['HTMLDataListElement', 'DATALIST'],
    ['HTMLDataElement', 'DATA'],
    ['HTMLDetailsElement', 'DETAILS'],
    ['HTMLDialogElement', 'DIALOG'],
    ['HTMLProgressElement', 'PROGRESS'],
    ['HTMLMeterElement', 'METER'],
    ['HTMLTimeElement', 'TIME'],
    ['HTMLPictureElement', 'PICTURE'],
    ['HTMLSourceElement', 'SOURCE'],
    ['HTMLTrackElement', 'TRACK'],
    ['HTMLEmbedElement', 'EMBED'],
    ['HTMLObjectElement', 'OBJECT'],
    ['HTMLAreaElement', 'AREA'],
    ['HTMLBaseElement', 'BASE'],
    ['HTMLMapElement', 'MAP'],
    ['HTMLParamElement', 'PARAM'],
    ['HTMLQuoteElement', ['Q','BLOCKQUOTE']],
    ['HTMLModElement', ['INS','DEL']],
    // Legacy/deprecated but still surfaces in the wild
    ['HTMLFrameElement', 'FRAME'],
    ['HTMLFrameSetElement', 'FRAMESET'],
    ['HTMLMarqueeElement', 'MARQUEE'],
    ['HTMLFontElement', 'FONT'],
    ['HTMLDirectoryElement', 'DIR'],
  ];
  for (const [name, tag] of _subclasses) _defineSubclass(name, tag);

  // HTMLUnknownElement: the spec-default for tags not matched by any
  // defined subclass. Fires for custom-tag-names sites use as data
  // containers (e.g. <my-thing>) before customElements.define resolves.
  const HTMLUnknownElement = function() { throw new TypeError('Illegal constructor'); };
  HTMLUnknownElement.prototype = Object.create(globalThis.__ELEM_PROTO || Object.prototype);
  const _knownTags = new Set();
  for (const [, tag] of _subclasses) {
    if (Array.isArray(tag)) tag.forEach(t => _knownTags.add(t));
    else _knownTags.add(tag);
  }
  Object.defineProperty(HTMLUnknownElement, Symbol.hasInstance, {
    value(el) {
      if (!el || typeof el !== 'object' || typeof el.tagName !== 'string') return false;
      return !_knownTags.has(el.tagName);
    },
  });
  globalThis.HTMLUnknownElement = HTMLUnknownElement;

  // ---------------- DOM collection classes (instanceof markers) ----------------
  // querySelectorAll returns Array (we don't fork DOM core to make it a real
  // NodeList) but sites still do "coll instanceof NodeList". Treat any
  // array-like with .item() OR a plain Array as a collection.
  function _isArrayLike(x) {
    return x && typeof x === 'object' && typeof x.length === 'number';
  }
  const NodeList = function() { throw new TypeError('Illegal constructor'); };
  Object.defineProperty(NodeList, Symbol.hasInstance, {
    value: _isArrayLike,
  });
  globalThis.NodeList = NodeList;

  const HTMLCollection = function() { throw new TypeError('Illegal constructor'); };
  Object.defineProperty(HTMLCollection, Symbol.hasInstance, {
    value: _isArrayLike,
  });
  globalThis.HTMLCollection = HTMLCollection;

  const FileList = function() { throw new TypeError('Illegal constructor'); };
  Object.defineProperty(FileList, Symbol.hasInstance, {
    value: _isArrayLike,
  });
  globalThis.FileList = FileList;

  const DOMTokenList = function() { throw new TypeError('Illegal constructor'); };
  Object.defineProperty(DOMTokenList, Symbol.hasInstance, {
    value: (x) => _isArrayLike(x) && typeof x.contains === 'function',
  });
  globalThis.DOMTokenList = DOMTokenList;

  const NamedNodeMap = function() { throw new TypeError('Illegal constructor'); };
  Object.defineProperty(NamedNodeMap, Symbol.hasInstance, {
    value: _isArrayLike,
  });
  globalThis.NamedNodeMap = NamedNodeMap;

  // ---------------- HTMLMediaElement stubs (for audio/video) ----------------
  if (globalThis.__ELEM_PROTO) {
    __ELEM_PROTO.play = function() { return Promise.resolve(); };
    __ELEM_PROTO.pause = function() {};
    __ELEM_PROTO.load = function() {};
    __ELEM_PROTO.canPlayType = function() { return ''; };
    Object.defineProperty(__ELEM_PROTO, 'currentTime', { get() { return 0; }, set() {}, configurable: true });
    Object.defineProperty(__ELEM_PROTO, 'duration', { get() { return NaN; }, configurable: true });
    Object.defineProperty(__ELEM_PROTO, 'paused', { get() { return true; }, configurable: true });
    Object.defineProperty(__ELEM_PROTO, 'ended', { get() { return false; }, configurable: true });
    Object.defineProperty(__ELEM_PROTO, 'readyState', { get() { return 0; }, configurable: true });
    Object.defineProperty(__ELEM_PROTO, 'volume', { get() { return 1; }, set() {}, configurable: true });
    Object.defineProperty(__ELEM_PROTO, 'muted', { get() { return false; }, set() {}, configurable: true });
  }
})();
`
