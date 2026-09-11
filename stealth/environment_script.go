package stealth

import (
	"encoding/json"
	"fmt"
)

// NewDocumentScript renders the canonical, JSON-escaped page pre-script. It
// contains only values from a validated EnvironmentProfile and never invents
// WebGL or network values that were not measured.
func NewDocumentScript(profile EnvironmentProfile) (string, error) {
	if err := profile.Validate(); err != nil {
		return "", err
	}
	if profile.Level == StealthDefault {
		return "", nil
	}
	profileJSON, err := json.Marshal(struct {
		Platform      string  `json:"platform"`
		Width         int     `json:"width"`
		Height        int     `json:"height"`
		DPR           float64 `json:"dpr"`
		Cores         int     `json:"cores"`
		Memory        int     `json:"memory"`
		WebGLVendor   string  `json:"webglVendor"`
		WebGLRenderer string  `json:"webglRenderer"`
		RTT           int     `json:"rtt"`
		Locale        string  `json:"locale"`
	}{
		Platform: profile.Platform,
		Width:    profile.ViewportWidth, Height: profile.ViewportHeight, DPR: profile.DevicePixelRatio,
		Cores: profile.HardwareConcurrency, Memory: profile.DeviceMemoryGB,
		WebGLVendor: profile.WebGLVendor, WebGLRenderer: profile.WebGLRenderer, RTT: profile.NetworkRTTMillis,
		Locale: profile.Locale,
	})
	if err != nil {
		return "", fmt.Errorf("stealth environment: encode profile: %w", err)
	}
	return fmt.Sprintf(`(() => {
  "use strict";
  const p = JSON.parse(%q);

  // Lies-detection shield: every installed function must report as
  // "[native code]" — detectors call Function.prototype.toString on
  // patched getters/methods (navigator.userAgent getter, getParameter,
  // permissions.query). Without this every patch below is an instant tell.
  const __origFnToString = Function.prototype.toString;
  const __nativeSet = new WeakSet();
  const __native = (fn) => { __nativeSet.add(fn); return fn; };
  const __nativeToString = __native(function toString() {
    if (__nativeSet.has(this)) {
      return "function " + (this.name || "") + "() { [native code] }";
    }
    return __origFnToString.call(this);
  });
  Function.prototype.toString = __nativeToString;

  const nav = globalThis.navigator;
  if (!nav) return;

  // define() installs a getter that carries a native-looking name
  // ("get userAgent") so toString output matches real Chrome getters.
  const define = (target, name, getter) => {
    try {
      const g = __native(getter);
      try { Object.defineProperty(g, "name", {value: "get " + name}); } catch (_) {}
      Object.defineProperty(target, name, {get: g, configurable: true});
    } catch (_) {}
  };

  define(nav, "webdriver", () => false);
  // navigator.platform is not covered by CDP emulation — keep the measured
  // value here. userAgent, userAgentData, languages and timezone are handled
  // natively via Emulation.* overrides (request headers included).
  define(nav, "platform", () => p.platform);
  if (p.cores > 0) define(nav, "hardwareConcurrency", () => p.cores);
  if (p.memory > 0) define(nav, "deviceMemory", () => p.memory);

	if (globalThis.window) {
    // window.chrome: real Chrome exposes {app, csi, loadTimes, runtime}.
    // Headless keeps the object but drops runtime — patch the gap, keep
    // whatever is real.
    const chromeRuntimeShape = {OnInstalledReason:{CHROME_UPDATE:"chrome_update",INSTALL:"install",SHARED_MODULE_UPDATE:"shared_module_update",UPDATE:"update"},OnRestartRequiredReason:{APP_UPDATE:"app_update",OS_UPDATE:"os_update",PERIODIC:"periodic"},PlatformArch:{ARM:"arm",MIPS:"mips",MIPS64:"mips64",X86_32:"x86-32",X86_64:"x86-64"},PlatformNaclArch:{ARM:"arm",MIPS:"mips",MIPS64:"mips64",X86_32:"x86-32",X86_64:"x86-64"},PlatformOs:{ANDROID:"android",CROS:"cros",FUCHSIA:"fuchsia",LINUX:"linux",MAC:"mac",OPENBSD:"openbsd",WIN:"win"},RequestUpdateCheckStatus:{NO_UPDATE:"no_update",THROTTLED:"throttled",UPDATE_AVAILABLE:"update_available"}};
    if (!globalThis.window.chrome) {
      globalThis.window.chrome = {
        app: {isInstalled: false, getDetails: __native(function getDetails(){return null;}), getIsInstalled: __native(function getIsInstalled(){return false;}), InstallState:{DISABLED:"disabled",INSTALLED:"installed",NOT_INSTALLED:"not_installed"}, RunningState:{CANNOT_RUN:"cannot_run",READY_TO_RUN:"ready_to_run",RUNNING:"running"}},
        csi: __native(function csi(){return {};}),
        loadTimes: __native(function loadTimes(){return {};}),
        runtime: chromeRuntimeShape
      };
    } else if (typeof globalThis.window.chrome.runtime !== "object") {
      try { globalThis.window.chrome.runtime = chromeRuntimeShape; } catch (_) {}
    }

    // iframes must see the same chrome object.
    try {
      const cwDesc = Object.getOwnPropertyDescriptor(HTMLIFrameElement.prototype, "contentWindow");
      if (cwDesc && cwDesc.get) {
        const origCW = cwDesc.get;
        const cwGetter = __native(function contentWindow() {
          const w = origCW.call(this);
          if (w) { try { w.chrome = globalThis.window.chrome; } catch (_) {} }
          return w;
        });
        try { Object.defineProperty(cwGetter, "name", {value: "get contentWindow"}); } catch (_) {}
        Object.defineProperty(HTMLIFrameElement.prototype, "contentWindow", {get: cwGetter, configurable: true});
      }
    } catch (_) {}

    define(globalThis.window, "devicePixelRatio", () => p.dpr);
    define(globalThis.window, "outerWidth", () => p.width);
    define(globalThis.window, "outerHeight", () => p.height + 85);

    // Screen geometry: headless reports screen == viewport. Real desktops
    // keep OS chrome in availHeight (macOS menu bar ~25, Win taskbar ~40).
    if (globalThis.screen) {
      define(globalThis.screen, "width", () => p.width);
      define(globalThis.screen, "height", () => p.height + 85);
      const chromeHeight = p.platform === "MacIntel" ? 25 : (p.platform === "Win32" ? 40 : 32);
      define(globalThis.screen, "availWidth", () => p.width);
      define(globalThis.screen, "availHeight", () => p.height + 85 - chromeHeight);
      define(globalThis.screen, "availLeft", () => 0);
      define(globalThis.screen, "availTop", () => p.platform === "MacIntel" ? 25 : 0);
      define(globalThis.screen, "colorDepth", () => 24);
      define(globalThis.screen, "pixelDepth", () => 24);
    }

    // Timezone is applied natively via Emulation.setTimezoneOverride —
    // Date/Intl stay untouched so there is no patched function to detect.

    // Headless reports Notification.permission "denied" — a real desktop
    // default is "default". Align both surfaces.
    try {
      if (globalThis.Notification) {
        define(globalThis.Notification, "permission", () => "default");
      }
    } catch (_) {}

    // permissions.query: wrap the REAL PermissionStatus so instanceof and
    // prototype checks pass; only the state getter is overridden.
    if (nav.permissions && typeof nav.permissions.query === "function") {
      const origQuery = nav.permissions.query.bind(nav.permissions);
      nav.permissions.query = __native(function query(descriptor) {
        const name = descriptor && descriptor.name;
        // Headless denies hardware permissions outright; a real desktop
        // prompts until the user decides. Both must read consistently.
        if (name === "notifications" || name === "clipboard-read" || name === "clipboard-write" ||
            name === "camera" || name === "microphone" || name === "geolocation") {
          return origQuery(descriptor).then((status) => {
            try {
              const want = name === "notifications" ? (globalThis.Notification ? Notification.permission : "default") : "prompt";
              const stateGetter = __native(function state(){ return want; });
              try { Object.defineProperty(stateGetter, "name", {value: "get state"}); } catch (_) {}
              Object.defineProperty(status, "state", {get: stateGetter, configurable: true});
            } catch (_) {}
            return status;
          }).catch(() => origQuery(descriptor));
        }
        return origQuery(descriptor);
      });
    }

    // Headless omits mediaDevices/serviceWorker/wakeLock/hid — desktop
    // Chrome exposes them. Attach prototype-shaped stand-ins; absence is
    // the fingerprint, so presence with honest empty behavior wins.
    if (!nav.mediaDevices) {
      const mkMediaDevice = (kind) => {
        const d = {};
        for (const [k, val] of [["deviceId",""],["groupId",""],["kind",kind],["label",""]]) {
          Object.defineProperty(d, k, {value: val});
        }
        return d;
      };
      const mediaDevices = {
        enumerateDevices: __native(function enumerateDevices(){ return Promise.resolve([mkMediaDevice("audioinput"), mkMediaDevice("videoinput"), mkMediaDevice("audiooutput")]); }),
        getUserMedia: __native(function getUserMedia(){ return Promise.reject(new DOMException("Requested device not found", "NotFoundError")); }),
        getDisplayMedia: __native(function getDisplayMedia(){ return Promise.reject(new DOMException("Not supported", "NotSupportedError")); }),
        addEventListener: __native(function addEventListener(){}),
        removeEventListener: __native(function removeEventListener(){})
      };
      try {
        if (globalThis.MediaDevices) Object.setPrototypeOf(mediaDevices, MediaDevices.prototype);
      } catch (_) {}
      define(nav, "mediaDevices", () => mediaDevices);
    }
    if (!nav.serviceWorker) {
      const swc = {
        register: __native(function register(){ return Promise.reject(new DOMException("The URL protocol of the current origin is not supported.", "NotSupportedError")); }),
        getRegistration: __native(function getRegistration(){ return Promise.resolve(undefined); }),
        getRegistrations: __native(function getRegistrations(){ return Promise.resolve([]); }),
        ready: new Promise(() => {}),
        controller: null,
        addEventListener: __native(function addEventListener(){}),
        removeEventListener: __native(function removeEventListener(){})
      };
      try {
        if (globalThis.ServiceWorkerContainer) Object.setPrototypeOf(swc, ServiceWorkerContainer.prototype);
      } catch (_) {}
      define(nav, "serviceWorker", () => swc);
    }
    // Interface-guarded extras: attach only when the interface exists.
    const ifaceStub = (prop, ctor, body) => {
      if (!nav[prop] && globalThis[ctor]) {
        const obj = body;
        try { Object.setPrototypeOf(obj, globalThis[ctor].prototype); } catch (_) {}
        define(nav, prop, () => obj);
      }
    };
    ifaceStub("wakeLock", "WakeLock", {request: __native(function request(){ return Promise.reject(new DOMException("Not allowed", "NotAllowedError")); })});
    ifaceStub("hid", "HID", {requestDevice: __native(function requestDevice(){ return Promise.resolve([]); }), getDevices: __native(function getDevices(){ return Promise.resolve([]); }), addEventListener: __native(function addEventListener(){}), removeEventListener: __native(function removeEventListener(){})});
    ifaceStub("usb", "USB", {requestDevice: __native(function requestDevice(){ return Promise.reject(new DOMException("No device selected", "NotFoundError")); }), getDevices: __native(function getDevices(){ return Promise.resolve([]); }), addEventListener: __native(function addEventListener(){}), removeEventListener: __native(function removeEventListener(){})});
    ifaceStub("serial", "Serial", {requestPort: __native(function requestPort(){ return Promise.reject(new DOMException("No port selected", "NotFoundError")); }), getPorts: __native(function getPorts(){ return Promise.resolve([]); }), addEventListener: __native(function addEventListener(){}), removeEventListener: __native(function removeEventListener(){})});
    ifaceStub("bluetooth", "Bluetooth", {requestDevice: __native(function requestDevice(){ return Promise.reject(new DOMException("User cancelled the requestDevice() chooser.", "NotFoundError")); }), getAvailability: __native(function getAvailability(){ return Promise.resolve(false); }), getDevices: __native(function getDevices(){ return Promise.resolve([]); }), addEventListener: __native(function addEventListener(){}), removeEventListener: __native(function removeEventListener(){})});
    ifaceStub("clipboard", "Clipboard", {read: __native(function read(){ return Promise.reject(new DOMException("Read permission denied.", "NotAllowedError")); }), readText: __native(function readText(){ return Promise.reject(new DOMException("Read permission denied.", "NotAllowedError")); }), write: __native(function write(){ return Promise.resolve(undefined); }), writeText: __native(function writeText(){ return Promise.resolve(undefined); })});
    ifaceStub("credentials", "CredentialsContainer", {get: __native(function get(){ return Promise.resolve(null); }), store: __native(function store(c){ return Promise.resolve(c); }), create: __native(function create(){ return Promise.resolve(null); }), preventSilentAccess: __native(function preventSilentAccess(){ return Promise.resolve(undefined); })});
    ifaceStub("locks", "LockManager", {request: __native(function request(){ return Promise.reject(new DOMException("Lock request aborted", "AbortError")); }), query: __native(function query(){ return Promise.resolve({held:[], pending:[]}); })});
    ifaceStub("storage", "StorageManager", {estimate: __native(function estimate(){ return Promise.resolve({quota: 281476296704, usage: 0, usageDetails: {}}); }), persist: __native(function persist(){ return Promise.resolve(false); }), persisted: __native(function persisted(){ return Promise.resolve(false); }), getDirectory: __native(function getDirectory(){ return Promise.reject(new DOMException("Not supported", "NotSupportedError")); })});
    ifaceStub("launchQueue", "LaunchQueue", {setConsumer: __native(function setConsumer(){})});
    ifaceStub("virtualKeyboard", "VirtualKeyboard", {overlaysContent: false, show: __native(function show(){}), hide: __native(function hide(){}), getLayoutMap: __native(function getLayoutMap(){ return Promise.resolve(new Map()); }), addEventListener: __native(function addEventListener(){}), removeEventListener: __native(function removeEventListener(){})});
    ifaceStub("managed", "NavigatorManagedData", {getManagedConfiguration: __native(function getManagedConfiguration(){ return Promise.resolve({}); }), addEventListener: __native(function addEventListener(){}), removeEventListener: __native(function removeEventListener(){})});
    ifaceStub("presentation", "Presentation", {defaultRequest: null, receiver: null});
    ifaceStub("keyboard", "Keyboard", {lock: __native(function lock(){ return Promise.reject(new DOMException("Not supported", "NotSupportedError")); }), unlock: __native(function unlock(){ return Promise.resolve(undefined); }), getLayoutMap: __native(function getLayoutMap(){ return Promise.resolve(new Map()); })});

    // WebGL: patch ONLY the debug-renderer unmasked params (0x9291/0x9292).
    // Masked params (0x1F00 VENDOR, 0x1F01 RENDERER) stay native — real
    // Chrome reports "WebKit"/"WebKit WebGL" there, and overriding them is
    // itself a detectable anomaly. Both WebGL1 and WebGL2 are covered.
    if (p.webglVendor && p.webglRenderer) {
      const patchGL = (proto) => {
        if (!proto) return;
        const orig = proto.getParameter;
        proto.getParameter = __native(function getParameter(parameter) {
          if (parameter === 0x9291) return p.webglVendor;
          if (parameter === 0x9292) return p.webglRenderer;
          return orig.call(this, parameter);
        });
      };
      patchGL(globalThis.WebGLRenderingContext && globalThis.WebGLRenderingContext.prototype);
      patchGL(globalThis.WebGL2RenderingContext && globalThis.WebGL2RenderingContext.prototype);
    }

    // speechSynthesis: headless returns zero voices; desktop Chrome has
    // network voices. Construct real SpeechSynthesisVoice prototypes.
    try {
      if (globalThis.speechSynthesis && globalThis.SpeechSynthesisVoice && typeof speechSynthesis.getVoices === "function") {
        const mkVoice = (name, lang, dflt) => {
          const v = Object.create(SpeechSynthesisVoice.prototype);
          for (const [k, val] of [["name",name],["lang",lang],["default",dflt],["localService",false],["voiceURI",name]]) {
            Object.defineProperty(v, k, {value: val});
          }
          return v;
        };
        const voices = [
          mkVoice("Google US English", "en-US", p.locale === "en-US"),
          mkVoice("Google UK English Female", "en-GB", p.locale === "en-GB"),
          mkVoice("Google Deutsch", "de-DE", p.locale === "de-DE")
        ];
        // speechSynthesis instances are non-extensible — patch the
        // prototype so the override survives strict-mode assignment.
        SpeechSynthesis.prototype.getVoices = __native(function getVoices(){ return voices.slice(); });
      }
    } catch (_) {}

    if (p.rtt > 0 && nav.connection) define(nav.connection, "rtt", () => p.rtt);
    for (const marker of ["cdc_adoQpoasnfa76pfcZLmcfl_Array", "cdc_adoQpoasnfa76pfcZLmcfl_Promise", "cdc_adoQpoasnfa76pfcZLmcfl_Symbol", "__webdriver", "__selenium", "$chrome_asyncScriptInfo"]) { try { delete globalThis[marker]; } catch (_) {} }
  }
})();`, string(profileJSON)), nil
}

// NewWorkerScript renders a worker-safe pre-script. It deliberately avoids
// window/document references and shares the same identity values as the page.
func NewWorkerScript(profile EnvironmentProfile) (string, error) {
	if err := profile.Validate(); err != nil {
		return "", err
	}
	if profile.Level == StealthDefault {
		return "", nil
	}
	profileJSON, err := json.Marshal(struct {
		Version      string   `json:"version"`
		UserAgent    string   `json:"userAgent"`
		Platform     string   `json:"platform"`
		PlatformVer  string   `json:"platformVersion"`
		Architecture string   `json:"architecture"`
		Locale       string   `json:"locale"`
		Languages    []string `json:"languages"`
		Cores        int      `json:"cores"`
		Memory       int      `json:"memory"`
	}{Version: profile.ChromeVersion, UserAgent: profile.UserAgent, Platform: profile.Platform, PlatformVer: profile.PlatformVersion, Architecture: profile.Architecture, Locale: profile.Locale, Languages: profile.Languages, Cores: profile.HardwareConcurrency, Memory: profile.DeviceMemoryGB})
	if err != nil {
		return "", fmt.Errorf("stealth worker: encode profile: %w", err)
	}
	return fmt.Sprintf(`(() => {
  "use strict";
  const p = JSON.parse(%q);
  const nav = self.navigator;
  if (!nav) return;
  // Same native-code mask as the page script: patched getters must not
  // leak JS source through Function.prototype.toString.
  const __origFnToString = Function.prototype.toString;
  const __nativeSet = new WeakSet();
  const __native = (fn) => { __nativeSet.add(fn); return fn; };
  Function.prototype.toString = __native(function toString() {
    if (__nativeSet.has(this)) {
      return "function " + (this.name || "") + "() { [native code] }";
    }
    return __origFnToString.call(this);
  });
  const define = (target, name, getter) => {
    try {
      const g = __native(getter);
      try { Object.defineProperty(g, "name", {value: "get " + name}); } catch (_) {}
      Object.defineProperty(target, name, {get: g, configurable: true});
    } catch (_) {}
  };
  define(nav, "webdriver", () => false);
  define(nav, "userAgent", () => p.userAgent);
  define(nav, "platform", () => p.platform);
  define(nav, "language", () => p.locale);
  define(nav, "languages", () => p.languages.slice());
  if (nav.userAgentData) {
    const originalUAData = nav.userAgentData;
    const chPlatform = p.platform === "MacIntel" ? "macOS" : (p.platform === "Win32" ? "Windows" : "Linux");
    define(nav, "userAgentData", () => ({brands:[{brand:"Chromium",version:p.version.split(".")[0]},{brand:"Google Chrome",version:p.version.split(".")[0]}], mobile:false, platform:chPlatform, getHighEntropyValues: async () => { let measured = {}; try { if (originalUAData.getHighEntropyValues) measured = await originalUAData.getHighEntropyValues(["platformVersion","architecture","bitness","model"]); } catch (_) {} return Object.assign({}, measured, {platform:chPlatform, platformVersion:p.platformVersion || measured.platformVersion || "", architecture:p.architecture || measured.architecture || "", bitness:measured.bitness || "64", model:measured.model || "", uaFullVersion:p.version}); }}));
  }
  if (p.cores > 0) define(nav, "hardwareConcurrency", () => p.cores);
  if (p.memory > 0) define(nav, "deviceMemory", () => p.memory);
})();`, string(profileJSON)), nil
}
