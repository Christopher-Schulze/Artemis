package js

import v8 "rogchap.com/v8go"

// installNavigatorExtras adds NavigatorUAData (UA reduction API),
// clipboard, geolocation, and Permissions to navigator. All are stubs
// that satisfy feature detection and never fail.
func installNavigatorExtras(iso *v8.Isolate, v8ctx *v8.Context, c *Context) error {
	c.registerBootstrap("artemis-navigator-extras", navigatorExtrasBootstrap)
	return nil
}

const navigatorExtrasBootstrap = `
(() => {
  if (!globalThis.navigator) return;

  // NavigatorUAData - reduced UA API
  navigator.userAgentData = {
    brands: [
      {brand: 'Artemis', version: '0.0.1'},
      {brand: 'Chromium', version: '120'},
      {brand: 'Not_A Brand', version: '99'},
    ],
    mobile: false,
    platform: navigator.platform,
    getHighEntropyValues(hints) {
      const out = {
        brands: this.brands,
        mobile: false,
        platform: this.platform,
        platformVersion: '',
        architecture: 'arm',
        bitness: '64',
        model: '',
        wow64: false,
      };
      const filtered = {};
      for (const h of (hints || [])) if (h in out) filtered[h] = out[h];
      return Promise.resolve(filtered);
    },
    toJSON() {
      return {brands: this.brands, mobile: this.mobile, platform: this.platform};
    },
  };

  // clipboard
  navigator.clipboard = {
    _data: '',
    writeText(t) { this._data = String(t); return Promise.resolve(); },
    readText() { return Promise.resolve(this._data); },
    write(items) {
      // items: [ClipboardItem]; we only honor first text/plain
      return Promise.resolve();
    },
    read() { return Promise.resolve([]); },
  };

  // geolocation - reject so apps fall back gracefully
  navigator.geolocation = {
    getCurrentPosition(_succ, err) {
      if (typeof err === 'function') {
        try { err({code: 1, message: 'permission denied'}); } catch (e) {}
      }
    },
    watchPosition(_succ, err) {
      if (typeof err === 'function') {
        try { err({code: 1, message: 'permission denied'}); } catch (e) {}
      }
      return 0;
    },
    clearWatch() {},
  };

  // Permissions API
  navigator.permissions = {
    query(desc) {
      // every permission is 'denied' so feature detection passes but
      // sensitive ops fail predictably.
      return Promise.resolve({
        state: 'denied',
        name: (desc && desc.name) || '',
        onchange: null,
        addEventListener() {}, removeEventListener() {}, dispatchEvent() {},
      });
    },
  };

  // Service Worker registration stub
  navigator.serviceWorker = {
    register() { return Promise.reject(new DOMException('not supported', 'NotSupportedError')); },
    getRegistration() { return Promise.resolve(undefined); },
    getRegistrations() { return Promise.resolve([]); },
    ready: new Promise(() => {}), // never resolves; spec says it waits
    controller: null,
    addEventListener() {}, removeEventListener() {}, dispatchEvent() {},
  };

  // hardware concurrency / memory hints
  Object.defineProperty(navigator, 'hardwareConcurrency', { get: () => 4, configurable: true });
  Object.defineProperty(navigator, 'deviceMemory', { get: () => 4, configurable: true });
  Object.defineProperty(navigator, 'maxTouchPoints', { get: () => 0, configurable: true });

  // PluginArray / MimeTypeArray: empty but iterable. Old fingerprinting
  // libraries enumerate these and crash on undefined.
  function _emptyArrayLike() {
    const a = [];
    a.item = (i) => a[i] || null;
    a.namedItem = () => null;
    a.refresh = () => {};
    return a;
  }
  Object.defineProperty(navigator, 'plugins',   { get: () => _emptyArrayLike(), configurable: true });
  Object.defineProperty(navigator, 'mimeTypes', { get: () => _emptyArrayLike(), configurable: true });
  Object.defineProperty(navigator, 'cookieEnabled', { get: () => true, configurable: true });
  Object.defineProperty(navigator, 'onLine',        { get: () => true, configurable: true });
  Object.defineProperty(navigator, 'doNotTrack',    { get: () => null, configurable: true });
  Object.defineProperty(navigator, 'webdriver',     { get: () => false, configurable: true });
})();
`
