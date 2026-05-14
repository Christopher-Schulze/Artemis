package js

import (
	"net/url"

	v8 "rogchap.com/v8go"
)

// installExtras installs the Headers class, History API, and the
// `crypto` global (via installCrypto). Also installs `__url_parse` used
// by the History bootstrap to split a URL into components, since V8
// builds without WHATWG URL.
func installExtras(iso *v8.Isolate, v8ctx *v8.Context, c *Context) error {
	if err := installCrypto(iso, v8ctx); err != nil {
		return err
	}
	if err := installURLHelper(c); err != nil {
		return err
	}
	c.registerBootstrap("artemis-extras", extrasBootstrap)
	return nil
}

var urlParseKeys = v8.PrepareKeys([]string{
	"href", "protocol", "host", "hostname", "port", "pathname", "search", "hash", "origin",
})

func (r *Runtime) ensureURLHelperTemplate() *v8.FunctionTemplate {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.urlHelperTemplate != nil {
		return r.urlHelperTemplate
	}
	iso := r.iso
	r.urlHelperTemplate = v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		args := info.Args()
		if len(args) == 0 {
			return v8.Null(iso)
		}
		href := args[0].String()
		base := ""
		if len(args) >= 2 && !args[1].IsNullOrUndefined() {
			base = args[1].String()
		}
		var u *url.URL
		var err error
		if base == "" {
			u, err = url.Parse(href)
		} else {
			b, perr := url.Parse(base)
			if perr != nil {
				return v8.Null(iso)
			}
			ref, perr := url.Parse(href)
			if perr != nil {
				return v8.Null(iso)
			}
			u = b.ResolveReference(ref)
		}
		if err != nil || u == nil {
			return v8.Null(iso)
		}
		obj, err := v8.NewObjectTemplate(iso).NewInstance(info.Context())
		if err != nil {
			return v8.Null(iso)
		}
		origin := ""
		if u.Scheme != "" && u.Host != "" {
			origin = u.Scheme + "://" + u.Host
		}
		path := u.Path
		if path == "" {
			path = "/"
		}
		search := ""
		if u.RawQuery != "" {
			search = "?" + u.RawQuery
		}
		hash := ""
		if u.Fragment != "" {
			hash = "#" + u.Fragment
		}
		_ = obj.SetManyPrepared(urlParseKeys, []interface{}{
			u.String(), u.Scheme + ":", u.Host, u.Hostname(), u.Port(), path, search, hash, origin,
		})
		return obj.Value
	})
	return r.urlHelperTemplate
}

// installURLHelper exposes a Go-side URL parser as `__url_parse(href, base)`
// returning {href, protocol, host, hostname, port, pathname, search, hash, origin}.
// Template is cached at Runtime level since the callback is stateless.
func installURLHelper(c *Context) error {
	return c.v8ctx.Global().Set("__url_parse", c.rt.ensureURLHelperTemplate().GetFunction(c.v8ctx))
}

const extrasBootstrap = `
(() => {
  // ---------------- Headers ----------------
  class Headers {
    constructor(init) {
      this._k = [];
      this._v = new Map();
      if (!init) return;
      if (init instanceof Headers) {
        init.forEach((v, k) => this.append(k, v));
        return;
      }
      if (Array.isArray(init)) {
        for (const [k, v] of init) this.append(k, v);
        return;
      }
      if (typeof init === 'object') {
        for (const k in init) this.append(k, init[k]);
      }
    }
    _key(k) { return String(k).toLowerCase(); }
    append(k, v) {
      const lk = this._key(k);
      const cur = this._v.get(lk);
      const nv = String(v);
      if (cur !== undefined) {
        this._v.set(lk, cur + ', ' + nv);
      } else {
        this._k.push(lk);
        this._v.set(lk, nv);
      }
    }
    set(k, v) {
      const lk = this._key(k);
      if (!this._v.has(lk)) this._k.push(lk);
      this._v.set(lk, String(v));
    }
    delete(k) {
      const lk = this._key(k);
      if (!this._v.has(lk)) return;
      this._v.delete(lk);
      this._k = this._k.filter(x => x !== lk);
    }
    has(k) { return this._v.has(this._key(k)); }
    get(k) {
      const v = this._v.get(this._key(k));
      return v === undefined ? null : v;
    }
    forEach(fn, thisArg) {
      for (const k of this._k) fn.call(thisArg, this._v.get(k), k, this);
    }
    *entries() { for (const k of this._k) yield [k, this._v.get(k)]; }
    *keys() { for (const k of this._k) yield k; }
    *values() { for (const k of this._k) yield this._v.get(k); }
    [Symbol.iterator]() { return this.entries(); }
  }
  globalThis.Headers = Headers;

  // ---------------- History ----------------
  const history = {
    _stack: [{ state: null, url: location.href }],
    _index: 0,
    get length() { return this._stack.length; },
    get state() { return this._stack[this._index].state; },
    pushState(state, _title, url) {
      this._stack = this._stack.slice(0, this._index + 1);
      const next = url == null ? location.href : __url_parse(url, location.href).href;
      this._stack.push({ state, url: next });
      this._index = this._stack.length - 1;
      _historyApply(next);
    },
    replaceState(state, _title, url) {
      const next = url == null ? location.href : __url_parse(url, location.href).href;
      this._stack[this._index] = { state, url: next };
      _historyApply(next);
    },
    back() {
      if (this._index === 0) return;
      this._index--;
      _historyApply(this._stack[this._index].url);
    },
    forward() {
      if (this._index >= this._stack.length - 1) return;
      this._index++;
      _historyApply(this._stack[this._index].url);
    },
    go(delta) {
      const target = Math.max(0, Math.min(this._stack.length - 1, this._index + (delta | 0)));
      if (target === this._index) return;
      this._index = target;
      _historyApply(this._stack[this._index].url);
    },
  };
  globalThis.history = history;

  function _historyApply(href) {
    const u = __url_parse(href);
    if (!u) return;
    location.href = u.href;
    location.protocol = u.protocol;
    location.host = u.host;
    location.hostname = u.hostname;
    location.port = u.port;
    location.pathname = u.pathname;
    location.search = u.search;
    location.hash = u.hash;
    location.origin = u.origin;
  }

  // ---------------- XMLHttpRequest ----------------
  class XMLHttpRequest {
    constructor() {
      this.readyState = 0;
      this.status = 0;
      this.statusText = '';
      this.responseText = '';
      this.responseType = '';
      this.onreadystatechange = null;
      this.onload = null;
      this.onerror = null;
      this._headers = {};
      this._respHeaders = {};
      this._method = 'GET';
      this._url = '';
      this._aborted = false;
    }
    open(method, url, _async) {
      this._method = String(method || 'GET').toUpperCase();
      this._url = String(url);
      this.readyState = 1;
      this._fire('readystatechange');
    }
    setRequestHeader(k, v) {
      this._headers[String(k)] = String(v);
    }
    abort() { this._aborted = true; }
    send(body) {
      if (this._aborted) return;
      const opts = { method: this._method, headers: this._headers };
      if (body != null && this._method !== 'GET') opts.body = String(body);
      const self = this;
      fetch(this._url, opts).then(async resp => {
        if (self._aborted) return;
        self.status = resp.status;
        self.statusText = resp.statusText || '';
        self._respHeaders = resp.headers || {};
        self.responseText = await resp.text();
        self.readyState = 4;
        self._fire('readystatechange');
        self._fire('load');
      }, err => {
        if (self._aborted) return;
        self.readyState = 4;
        self._fire('readystatechange');
        self._fire('error', err);
      });
    }
    getAllResponseHeaders() {
      const lines = [];
      for (const k in this._respHeaders) lines.push(k + ': ' + this._respHeaders[k]);
      return lines.join('\r\n');
    }
    getResponseHeader(k) { return this._respHeaders[k] || null; }
    _fire(name, arg) {
      const fn = this['on' + name];
      if (typeof fn === 'function') {
        try { fn.call(this, arg); } catch (e) {}
      }
    }
  }
  XMLHttpRequest.UNSENT = 0;
  XMLHttpRequest.OPENED = 1;
  XMLHttpRequest.HEADERS_RECEIVED = 2;
  XMLHttpRequest.LOADING = 3;
  XMLHttpRequest.DONE = 4;
  globalThis.XMLHttpRequest = XMLHttpRequest;
})();
`
