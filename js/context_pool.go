package js

import (
	v8 "rogchap.com/v8go"
)

// poolResetBootstrap installs `__artemis_pristine` (set of property
// names captured on first call) and `__artemis_reset(newURL)` which
// clears any property added since pristine, resets known mutable
// holders (custom elements, window listeners, history stack), and
// rebinds location to newURL.
//
// Run as the very last step of NewContext on first-time builds so the
// pristine set captures the full installed surface.
const poolResetBootstrap = `
(() => {
  if (globalThis.__artemis_reset) return; // already installed (snapshot-cached path)
  globalThis.__artemis_pristine = null;
  globalThis.__artemis_reset = function(newURL) {
    // First call seeds the pristine set with the post-install global names.
    if (globalThis.__artemis_pristine === null) {
      globalThis.__artemis_pristine = new Set(Object.getOwnPropertyNames(globalThis));
      return;
    }
    // Drop any property that wasn't part of the pristine surface. Tries to
    // delete configurable properties; non-configurable (rare; usually means
    // the user set with defineProperty + configurable:false) are left as-is.
    const pristine = globalThis.__artemis_pristine;
    for (const k of Object.getOwnPropertyNames(globalThis)) {
      if (!pristine.has(k)) {
        try { delete globalThis[k]; } catch (_) {}
      }
    }
    // CustomElement registry: clear so re-defining 'my-tag' on the next page
    // doesn't throw "already defined".
    if (globalThis.customElements && typeof globalThis.customElements._reset === 'function') {
      try { globalThis.customElements._reset(); } catch (_) {}
    }
    // window.addEventListener listeners (installed via inlineWindowEventsBootstrap).
    if (globalThis._winListeners) {
      try { globalThis._winListeners.clear(); } catch (_) {}
    }
    // History stack: reset to a single entry with the new URL.
    if (globalThis.history && globalThis.history._stack) {
      globalThis.history._stack = [{state: null, url: newURL || ''}];
      globalThis.history._index = 0;
    }
    // Performance entries: reset to empty so a new page sees no marks/measures.
    if (globalThis.performance && typeof globalThis.performance.clearMarks === 'function') {
      try { globalThis.performance.clearMarks(); globalThis.performance.clearMeasures(); } catch (_) {}
    }
    // Location: rebind via __url_parse so all 9 fields update atomically.
    if (newURL && globalThis.location && globalThis.__url_parse) {
      const u = globalThis.__url_parse(newURL);
      if (u) {
        globalThis.location.href = u.href;
        globalThis.location.protocol = u.protocol;
        globalThis.location.host = u.host;
        globalThis.location.hostname = u.hostname;
        globalThis.location.port = u.port;
        globalThis.location.pathname = u.pathname;
        globalThis.location.search = u.search;
        globalThis.location.hash = u.hash;
        globalThis.location.origin = u.origin;
      }
    }
    // localStorage / sessionStorage: backed by Go-side memStorage which is
    // fresh per *js.Context (we re-bind the storage object via internal
    // field 0 in resetStorageBindings on the Go side); the JS-visible
    // 'length' value snapshot is reset here.
    if (globalThis.localStorage) globalThis.localStorage.length = 0;
    if (globalThis.sessionStorage) globalThis.sessionStorage.length = 0;
  };
})();
`

// resetPooledContext runs the JS reset script and rebinds Go-side
// per-page state (location URL, document handle, storage internals)
// onto the existing v8.Context. Called when a v8.Context is reused
// from the Runtime.ctxPool.
func resetPooledContext(c *Context, pageURL string) error {
	// JS reset clears mutated globals + rebinds location.
	// Wrap the URL in JSON.stringify-safe form via a string literal.
	resetExpr := "__artemis_reset(" + jsonStringLiteral(pageURL) + ")"
	if _, err := c.v8ctx.RunScript(resetExpr, "<artemis-pool-reset>"); err != nil {
		return err
	}
	// Re-bind Storage objects: their internal-field-0 still points at the
	// PREVIOUS Context's storage handle. Allocate new handles on this
	// Runtime's slab and patch the existing JS-side localStorage /
	// sessionStorage objects.
	if err := rebindPooledStorage(c, "localStorage", c.localStorage); err != nil {
		return err
	}
	if err := rebindPooledStorage(c, "sessionStorage", c.sessionStorage); err != nil {
		return err
	}
	return nil
}

func rebindPooledStorage(c *Context, globalName string, s *memStorage) error {
	r := c.rt
	r.mu.Lock()
	if r.storageHandles == nil {
		r.storageHandles = &storageHandles{}
	}
	h := r.storageHandles
	r.mu.Unlock()
	id := h.put(s)
	c.storageHandleIDs = append(c.storageHandleIDs, id)
	val, err := c.v8ctx.Global().Get(globalName)
	if err != nil {
		return err
	}
	obj, err := val.AsObject()
	if err != nil {
		return err
	}
	return obj.SetInternalField(0, int32(id))
}

// jsonStringLiteral returns a quoted JSON string suitable for embedding
// into a JS expression. Tiny manual encoder; only handles the chars
// we actually emit (URLs).
//
// Optimization (TASK-2344): pre-allocate the buffer to len(s)+2 (the
// minimum possible output: 2 quote chars + the input unchanged) to
// avoid reallocations. Worst case (every char escaped) is 6x expansion,
// but URLs rarely contain escaped chars, so the pre-allocation is
// almost always exact or needs at most one growth.
func jsonStringLiteral(s string) string {
	b := make([]byte, 0, len(s)+2)
	b = append(b, '"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\\':
			b = append(b, '\\', '\\')
		case '"':
			b = append(b, '\\', '"')
		case '\n':
			b = append(b, '\\', 'n')
		case '\r':
			b = append(b, '\\', 'r')
		case '\t':
			b = append(b, '\\', 't')
		default:
			if c < 0x20 {
				b = append(b, '\\', 'u', '0', '0', hexNib(c>>4), hexNib(c&0xf))
			} else {
				b = append(b, c)
			}
		}
	}
	b = append(b, '"')
	return string(b)
}

func hexNib(n byte) byte {
	if n < 10 {
		return '0' + n
	}
	return 'a' + (n - 10)
}

// installPoolReset registers the reset bootstrap. Called from NewContext
// on FIRST build only (skipped on pool-reuse since the bootstrap is
// already loaded).
func installPoolReset(c *Context) error {
	c.registerBootstrap("artemis-pool-reset", poolResetBootstrap)
	return nil
}

// seedPoolReset captures the pristine global property set on the
// freshly-built v8.Context. Runs as the very last step of NewContext
// AFTER all installs and bootstraps so the pristine set covers the
// full installed surface.
func seedPoolReset(c *Context) error {
	_, err := c.v8ctx.RunScript("__artemis_reset()", "<artemis-pool-seed>")
	return err
}

var _ = v8.NewIsolate // silence import check on builds without the pool path
