package js

import (
	v8 "rogchap.com/v8go"
)

// installInputProps adds live properties on Element prototype that are
// commonly read on form elements: .checked, .value (live, not snapshot),
// .disabled, .selected, .readOnly, .required, .multiple, .name, .form.
// All are backed by attributes on the Go DOM so reads and writes are
// consistent with getAttribute/setAttribute.
func installInputProps(iso *v8.Isolate, v8ctx *v8.Context, c *Context) error {
	c.registerBootstrap("artemis-input-props", inputPropsBootstrap)
	return nil
}

const inputPropsBootstrap = `
(() => {
  if (!globalThis.__ELEM_PROTO) return;

  // boolean attribute helper: presence => true; remove on false.
  function defBool(prop, attr) {
    Object.defineProperty(__ELEM_PROTO, prop, {
      get() { return this.getAttribute(attr) !== null; },
      set(v) {
        if (v) this.setAttribute(attr, '');
        else this.removeAttribute(attr);
      },
      configurable: true,
    });
  }
  // string attribute helper: read/write to the named attribute.
  function defStr(prop, attr) {
    Object.defineProperty(__ELEM_PROTO, prop, {
      get() { return this.getAttribute(attr) || ''; },
      set(v) { this.setAttribute(attr, String(v)); },
      configurable: true,
    });
  }

  // Don't redefine if the v3 batch already set them.
  if (!Object.getOwnPropertyDescriptor(__ELEM_PROTO, 'checked')) {
    defBool('checked', 'checked');
  }
  if (!Object.getOwnPropertyDescriptor(__ELEM_PROTO, 'disabled')) {
    defBool('disabled', 'disabled');
  }
  if (!Object.getOwnPropertyDescriptor(__ELEM_PROTO, 'readOnly')) {
    defBool('readOnly', 'readonly');
  }
  if (!Object.getOwnPropertyDescriptor(__ELEM_PROTO, 'required')) {
    defBool('required', 'required');
  }
  if (!Object.getOwnPropertyDescriptor(__ELEM_PROTO, 'multiple')) {
    defBool('multiple', 'multiple');
  }
  if (!Object.getOwnPropertyDescriptor(__ELEM_PROTO, 'autofocus')) {
    defBool('autofocus', 'autofocus');
  }
  if (!Object.getOwnPropertyDescriptor(__ELEM_PROTO, 'selected')) {
    defBool('selected', 'selected');
  }

  // Override .name and .value already exist as setAttribute proxies; reinstall
  // for textarea where the value lives in textContent. This block is a thin
  // override:
  Object.defineProperty(__ELEM_PROTO, 'name', {
    get() { return this.getAttribute('name') || ''; },
    set(v) { this.setAttribute('name', String(v)); },
    configurable: true,
  });

  // .value: textarea uses textContent, others use value attribute.
  Object.defineProperty(__ELEM_PROTO, 'value', {
    get() {
      if (this.tagName === 'TEXTAREA') return this.textContent || '';
      return this.getAttribute('value') || '';
    },
    set(v) {
      if (this.tagName === 'TEXTAREA') {
        this.textContent = String(v);
      } else {
        this.setAttribute('value', String(v));
      }
    },
    configurable: true,
  });

  // .form: walk up to the nearest <form>
  Object.defineProperty(__ELEM_PROTO, 'form', {
    get() {
      let n = this.parentNode;
      while (n) {
        if (n.tagName === 'FORM') return n;
        n = n.parentNode;
      }
      return null;
    },
    configurable: true,
  });

  // Convenience numeric / specialized properties
  Object.defineProperty(__ELEM_PROTO, 'maxLength', {
    get() {
      const v = this.getAttribute('maxlength');
      return v == null ? -1 : parseInt(v, 10);
    },
    set(v) { this.setAttribute('maxlength', String(v|0)); },
    configurable: true,
  });
  Object.defineProperty(__ELEM_PROTO, 'minLength', {
    get() {
      const v = this.getAttribute('minlength');
      return v == null ? -1 : parseInt(v, 10);
    },
    set(v) { this.setAttribute('minlength', String(v|0)); },
    configurable: true,
  });
  Object.defineProperty(__ELEM_PROTO, 'placeholder', {
    get() { return this.getAttribute('placeholder') || ''; },
    set(v) { this.setAttribute('placeholder', String(v)); },
    configurable: true,
  });

  // <select>.selectedIndex
  Object.defineProperty(__ELEM_PROTO, 'selectedIndex', {
    get() {
      if (this.tagName !== 'SELECT') return -1;
      const opts = this.querySelectorAll('option');
      for (let i = 0; i < opts.length; i++) {
        if (opts[i].selected) return i;
      }
      return -1;
    },
    set(v) {
      if (this.tagName !== 'SELECT') return;
      const opts = this.querySelectorAll('option');
      const want = v|0;
      for (let i = 0; i < opts.length; i++) {
        opts[i].selected = (i === want);
      }
    },
    configurable: true,
  });

  // <select>.options + <select>.value
  Object.defineProperty(__ELEM_PROTO, 'options', {
    get() {
      if (this.tagName !== 'SELECT') return [];
      return this.querySelectorAll('option');
    },
    configurable: true,
  });
  // .value override for <select>: returns selected option's value
  // We layer this on top of the generic .value above by checking tagName.
  const _proto = __ELEM_PROTO;
  const _genericValueDesc = Object.getOwnPropertyDescriptor(_proto, 'value');
  Object.defineProperty(_proto, 'value', {
    get() {
      if (this.tagName === 'SELECT') {
        const opts = this.querySelectorAll('option');
        for (let i = 0; i < opts.length; i++) {
          if (opts[i].selected) return opts[i].getAttribute('value') || opts[i].textContent || '';
        }
        if (opts.length > 0) return opts[0].getAttribute('value') || opts[0].textContent || '';
        return '';
      }
      return _genericValueDesc.get.call(this);
    },
    set(v) {
      if (this.tagName === 'SELECT') {
        const opts = this.querySelectorAll('option');
        for (let i = 0; i < opts.length; i++) {
          opts[i].selected = ((opts[i].getAttribute('value') || '') === String(v));
        }
        return;
      }
      _genericValueDesc.set.call(this, v);
    },
    configurable: true,
  });

  // <a>.host, .pathname, .search, .hash, .hostname (parsed from href)
  function defLinkPart(prop) {
    Object.defineProperty(__ELEM_PROTO, prop, {
      get() {
        if (this.tagName !== 'A' && this.tagName !== 'AREA') return '';
        const href = this.getAttribute('href');
        if (!href) return '';
        const u = __url_parse(href, location ? location.href : '');
        return u ? (u[prop] || '') : '';
      },
      configurable: true,
    });
  }
  for (const p of ['host', 'pathname', 'search', 'hash', 'hostname', 'port', 'protocol', 'origin']) {
    defLinkPart(p);
  }
})();
`
