package js

import (
	"net/url"

	v8 "github.com/tommie/v8go"
)

// installExtras installs the Headers class, History API, and the
// `crypto` global (via installCrypto). Also installs `__url_parse` used
// by the History bootstrap to split a URL into components, since V8
// builds without WHATWG URL.
func installExtras(iso *v8.Isolate, v8ctx *v8.Context, c *Context) error {
	if err := installCrypto(c); err != nil {
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
	// One-entry memo: JS sites call new URL()/url resolution with the same
	// document.baseURI repeatedly — skip re-parsing an unchanged base.
	var lastBase string
	var lastBaseURL *url.URL
	r.urlHelperTemplate = v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		defer info.Release()
		args := info.Args()
		if len(args) == 0 {
			return v8.Null(iso)
		}
		href, conversionErr := args[0].StringChecked()
		if conversionErr != nil {
			return domCallbackResult(iso, domCallbackError(iso, conversionErr))
		}
		base := ""
		if len(args) >= 2 && !args[1].IsNullOrUndefined() {
			base, conversionErr = args[1].StringChecked()
			if conversionErr != nil {
				return domCallbackResult(iso, domCallbackError(iso, conversionErr))
			}
		}
		var u *url.URL
		var err error
		if base == "" {
			u, err = url.Parse(href)
		} else {
			if base != lastBase {
				lastBase = base
				lastBaseURL, _ = url.Parse(base)
			}
			if lastBaseURL == nil {
				return v8.Null(iso)
			}
			ref, perr := url.Parse(href)
			if perr != nil {
				return v8.Null(iso)
			}
			u = lastBaseURL.ResolveReference(ref)
		}
		if err != nil || u == nil {
			return v8.Null(iso)
		}
		obj, err := info.Context().NewNullPrototypeObject()
		if err != nil {
			return domCallbackResult(iso, domCallbackError(iso, err))
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
		if err := obj.SetManyPrepared(urlParseKeys, []interface{}{
			u.String(), u.Scheme + ":", u.Host, u.Hostname(), u.Port(), path, search, hash, origin,
		}); err != nil {
			obj.Release()
			return domCallbackResult(iso, domCallbackError(iso, err))
		}
		return obj.Value.ReturnFromCallback()
	})
	return r.urlHelperTemplate
}

// installURLHelper exposes a Go-side URL parser as `__url_parse(href, base)`
// returning {href, protocol, host, hostname, port, pathname, search, hash, origin}.
// Template is cached at Runtime level since the callback is stateless.
func installURLHelper(c *Context) error {
	global := c.v8ctx.Global()
	defer global.Release()
	function := c.rt.ensureURLHelperTemplate().GetFunction(c.v8ctx)
	defer function.Release()
	return global.Set("__url_parse", function)
}

const extrasBootstrap = `
(() => {
  // ---------------- Headers ----------------
  const headerStates = new WeakMap();
  const stateGet = Function.call.bind(WeakMap.prototype.get), stateSet = Function.call.bind(WeakMap.prototype.set);
  const stateHas = Function.call.bind(WeakMap.prototype.has);
  const headerString = value => {
    if (typeof value === 'symbol') throw new TypeError('Headers require byte strings');
    const text = String(value);
    for (let index = 0; index < text.length; index++) if (text.charCodeAt(index) > 255) throw new TypeError('Headers require byte strings');
    return text;
  };
  const headerName = value => {
    const name = headerString(value);
    if (!/^[!#$%&'*+.^_\x60|~0-9A-Za-z-]+$/.test(name)) throw new TypeError('Invalid header name');
    return name.toLowerCase();
  };
  const headerValue = value => {
    const text = headerString(value).replace(/^[\t ]+|[\t ]+$/g, '');
    if (/[\x00\r\n]/.test(text)) throw new TypeError('Invalid header value');
    return text;
  };
  const headerState = (headers, mutate = false) => {
    const state = stateGet(headerStates, headers);
    if (!state) throw new TypeError('Invalid Headers receiver');
    if (mutate && state.immutable) throw new TypeError('Headers are immutable');
    return state;
  };
  const appendHeader = (headers, name, value) => {
    const key = headerName(name), text = headerValue(value), state = headerState(headers, true);
    if (key === 'set-cookie') state.cookies.push(text);
    const current = state.values.get(key);
    if (current !== undefined) state.values.set(key, current + ', ' + text);
    else { state.keys.push(key); state.values.set(key, text); }
    state.version++;
  };
  const headerEntries = state => {
    const entries = [];
    for (const key of state.keys.slice().sort()) {
      if (key === 'set-cookie') for (const value of state.cookies) entries.push([key, value]);
      else entries.push([key, state.values.get(key)]);
    }
    return entries;
  };
  class Headers {
    constructor(init) {
      stateSet(headerStates, this, {keys: [], values: new Map(), cookies: [], immutable: false, version: 0});
      if (init === undefined) return;
      if (init === null || typeof init !== 'object' && typeof init !== 'function') throw new TypeError('Invalid Headers initializer');
      if (init[Symbol.iterator] != null) {
        if (typeof init[Symbol.iterator] !== 'function') throw new TypeError('Invalid Headers iterator');
        for (const pair of init) {
          if (!pair || typeof pair !== 'object' || typeof pair[Symbol.iterator] !== 'function') throw new TypeError('Invalid header pair');
          const entry = Array.from(pair);
          if (entry.length !== 2) throw new TypeError('A header pair requires two values');
          appendHeader(this, entry[0], entry[1]);
        }
        return;
      }
      for (const k of Object.keys(init)) appendHeader(this, k, init[k]);
    }
    append(k, v) {
      if (arguments.length < 2) throw new TypeError('append requires name and value');
      appendHeader(this, k, v);
    }
    set(k, v) {
      if (arguments.length < 2) throw new TypeError('set requires name and value');
      const lk = headerName(k), value = headerValue(v), state = headerState(this, true);
      if (!state.values.has(lk)) state.keys.push(lk);
      state.values.set(lk, value);
      if (lk === 'set-cookie') state.cookies = [value];
      state.version++;
    }
    delete(k) {
      if (!arguments.length) throw new TypeError('delete requires a name');
      const lk = headerName(k), state = headerState(this, true);
      if (!state.values.has(lk)) return;
      state.values.delete(lk);
      state.keys = state.keys.filter(x => x !== lk);
      if (lk === 'set-cookie') state.cookies = [];
      state.version++;
    }
    has(k) {
      if (!arguments.length) throw new TypeError('has requires a name');
      return headerState(this).values.has(headerName(k));
    }
    get(k) {
      if (!arguments.length) throw new TypeError('get requires a name');
      const v = headerState(this).values.get(headerName(k));
      return v === undefined ? null : v;
    }
    getSetCookie() { return headerState(this).cookies.slice(); }
    forEach(fn, thisArg) {
      if (typeof fn !== 'function') throw new TypeError('forEach requires a callback');
      for (const [key, value] of this.entries()) fn.call(thisArg, value, key, this);
    }
    *entries() {
      const state = headerState(this);
      let version = -1, entries;
      for (let index = 0;; index++) {
        if (version !== state.version) { entries = headerEntries(state); version = state.version; }
        if (index >= entries.length) return;
        yield entries[index];
      }
    }
    *keys() { for (const [key] of this.entries()) yield key; }
    *values() { for (const [, value] of this.entries()) yield value; }
    [Symbol.iterator]() { return this.entries(); }
  }
  globalThis.Headers = Headers;
  Object.defineProperty(globalThis, '__artemisFreezeHeaders', {value: headers => {
    headerState(headers).immutable = true;
    return headers;
  }});
  Object.defineProperty(globalThis, '__artemisEncodeRequestHeaders', {value: init => {
    const result = Object.create(null);
    const originalNames = Object.create(null);
    let size = 0, count = 0;
    const put = (name, value) => {
      const normalized = headerName(name); value = headerValue(value);
      size += name.length + value.length;
      if (size > 1048576 || ++count > 1024) throw new TypeError('Request headers exceed limit');
      const original = originalNames[normalized];
      if (original !== undefined) result[original] += ', ' + value;
      else { originalNames[normalized] = name; result[name] = value; }
    };
    if (stateHas(headerStates, init) || init && init[Symbol.iterator] != null) {
      const headers = new Headers(init);
      for (const [key, value] of headers.entries()) put(key, value);
    } else {
      if (!init || typeof init !== 'object' && typeof init !== 'function') throw new TypeError('Invalid request headers');
      for (const key of Object.keys(init)) put(key, init[key]);
    }
    const encoded = JSON.stringify(result);
    if (encoded.length > 1048576) throw new TypeError('Encoded request headers exceed limit');
    return encoded;
  }});

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
      if (this._respHeaders instanceof Headers) {
        for (const [key, value] of this._respHeaders) lines.push(key + ': ' + value);
      } else for (const k in this._respHeaders) lines.push(k + ': ' + this._respHeaders[k]);
      return lines.join('\r\n');
    }
    getResponseHeader(k) { return this._respHeaders instanceof Headers ? this._respHeaders.get(k) : this._respHeaders[k] || null; }
    _fire(name, arg) {
      const fn = this['on' + name];
      if (typeof fn === 'function') {
        try { fn.call(this, arg); } catch (e) { __artemisDispatchDocumentEvent(5, e); }
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
