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

  // NavigatorUAData - reduced UA API. Brands derive from the configured
  // userAgent so the pair can never contradict each other.
  const __uaMajor = (() => { const m = /Chrome\/(\d+)/.exec(navigator.userAgent || ""); return m ? m[1] : "120"; })();
  const __uaPlatform = (() => { const p = navigator.platform || ""; if (p === 'MacIntel') return 'macOS'; if (p === 'Win32') return 'Windows'; return 'Linux'; })();
  navigator.userAgentData = {
    brands: [
      {brand: 'Chromium', version: __uaMajor},
      {brand: 'Google Chrome', version: __uaMajor},
      {brand: 'Not_A Brand', version: '8'},
    ],
    mobile: false,
    platform: __uaPlatform,
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
      // Desktop Chrome prompts for notification/geo/camera/mic until the
      // user decides; blanket "denied" is a headless tell.
      const name = (desc && desc.name) || '';
      const promptable = name === 'notifications' || name === 'geolocation' || name === 'camera' || name === 'microphone';
      return Promise.resolve({
        state: promptable ? 'prompt' : 'denied',
        name: name,
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

  // hardwareConcurrency / deviceMemory / maxTouchPoints come from
  // NavigatorConfig (Go side) — do not override here or the pair diverges.

  // PluginArray / MimeTypeArray matching real Chrome's built-in PDF
  // viewer entries — empty arrays are a headless tell.
  function _plugin(name, filename, description) {
    const p = {name: name, filename: filename, description: description, length: 0};
    p.item = () => null; p.namedItem = () => null;
    return p;
  }
  const _plugins = [
    _plugin('PDF Viewer', 'internal-pdf-viewer', 'Portable Document Format'),
    _plugin('Chrome PDF Viewer', 'internal-pdf-viewer', 'Portable Document Format'),
    _plugin('Chromium PDF Viewer', 'internal-pdf-viewer', 'Portable Document Format'),
    _plugin('Microsoft Edge PDF Viewer', 'internal-pdf-viewer', 'Portable Document Format'),
    _plugin('WebKit built-in PDF', 'internal-pdf-viewer', 'Portable Document Format'),
  ];
  _plugins.item = (i) => _plugins[i] || null;
  _plugins.namedItem = (n) => _plugins.find(p => p.name === n) || null;
  _plugins.refresh = () => {};
  const _mimeTypes = [
    {type: 'application/pdf', suffixes: 'pdf', description: 'Portable Document Format', enabledPlugin: _plugins[0]},
    {type: 'text/pdf', suffixes: 'pdf', description: 'Portable Document Format', enabledPlugin: _plugins[0]},
  ];
  _mimeTypes.item = (i) => _mimeTypes[i] || null;
  _mimeTypes.namedItem = (n) => _mimeTypes.find(m => m.type === n) || null;
  Object.defineProperty(navigator, 'plugins',   { get: () => _plugins, configurable: true });
  Object.defineProperty(navigator, 'mimeTypes', { get: () => _mimeTypes, configurable: true });
  Object.defineProperty(navigator, 'pdfViewerEnabled', { get: () => true, configurable: true });
  Object.defineProperty(navigator, 'cookieEnabled', { get: () => true, configurable: true });
  Object.defineProperty(navigator, 'onLine',        { get: () => true, configurable: true });
  Object.defineProperty(navigator, 'doNotTrack',    { get: () => null, configurable: true });
  Object.defineProperty(navigator, 'webdriver',     { get: () => false, configurable: true });
})();
`
