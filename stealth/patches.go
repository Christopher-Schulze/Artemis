package stealth

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// Profile carries per-session deterministic overrides so that the
// same profile always generates the same canvas/audio fingerprint.
type Profile struct {
	ViewportWidth    int
	ViewportHeight   int
	DevicePixelRatio float64
	UserAgent        string
	Vendor           string
	Platform         string
	Languages        string // e.g. "de-DE,de,en-US,en"
	Timezone         string // e.g. "Europe/Berlin"
	ColorScheme      string // "light" or "dark"
	ReducedMotion    bool
	// Seed is hashed into canvas/audio randomness so the same profile
	// is stable across restarts but different profiles differ.
	Seed string
}

// Defaults returns a Profile with sensible defaults.
func Defaults() Profile {
	return Profile{
		ViewportWidth:    1920,
		ViewportHeight:   1080,
		DevicePixelRatio: 2.0,
		UserAgent:        "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36",
		Vendor:           "Google Inc.",
		Platform:         "MacIntel",
		Languages:        "de-DE,de,en-US,en",
		Timezone:         "Europe/Berlin",
		ColorScheme:      "light",
		ReducedMotion:    false,
		Seed:             "artemis-default",
	}
}

// Script returns the full stealth patch script for the profile.
func Script(p Profile) string {
	if p.UserAgent == "" {
		d := Defaults()
		p.UserAgent = d.UserAgent
		p.Vendor = d.Vendor
		p.Platform = d.Platform
		p.Languages = d.Languages
		p.Timezone = d.Timezone
		p.ColorScheme = d.ColorScheme
		p.ReducedMotion = d.ReducedMotion
		if p.ViewportWidth == 0 {
			p.ViewportWidth = d.ViewportWidth
		}
		if p.ViewportHeight == 0 {
			p.ViewportHeight = d.ViewportHeight
		}
		if p.DevicePixelRatio == 0 {
			p.DevicePixelRatio = d.DevicePixelRatio
		}
		if p.Seed == "" {
			p.Seed = d.Seed
		}
	}
	seedHash := sha256.Sum256([]byte(p.Seed))
	seedHex := fmt.Sprintf("%x", seedHash[:8])

	var b strings.Builder
	b.WriteString("(() => {\n")
	b.WriteString("  const _defineProperty = Object.defineProperty;\n")
	b.WriteString("  const _seed = '" + seedHex + "';\n")
	b.WriteString("  const _rand = (n) => {\n")
	b.WriteString("    let h = 0; for (let i = 0; i < _seed.length; i++) h = ((h << 5) - h) + _seed.charCodeAt(i), h |= 0;\n")
	b.WriteString("    return Math.abs(h % n);\n")
	b.WriteString("  };\n")

	// 1. navigator.webdriver = false
	b.WriteString("  _defineProperty(navigator, 'webdriver', { get: () => undefined });\n")

	// 2. navigator.plugins
	b.WriteString("  _defineProperty(navigator, 'plugins', {\n")
	b.WriteString("    get: () => [\n")
	b.WriteString("      {name:'Chrome PDF Viewer',filename:'internal-pdf-viewer',description:'Portable Document Format'},\n")
	b.WriteString("      {name:'Native Client',filename:'internal-nacl-plugin',description:'Native Client module'},\n")
	b.WriteString("    ]\n")
	b.WriteString("  });\n")

	// 3. navigator.languages
	b.WriteString("  _defineProperty(navigator, 'languages', { get: () => ['" + strings.ReplaceAll(p.Languages, ",", "','") + "'] });\n")

	// 4. window.chrome runtime with csi/loadTimes
	b.WriteString("  window.chrome = window.chrome || {};\n")
	b.WriteString("  window.chrome.runtime = window.chrome.runtime || {\n")
	b.WriteString("    connect: () => ({ onMessage: { addListener: () => {}, removeListener: () => {} }, postMessage: () => {}, disconnect: () => {} }),\n")
	b.WriteString("    sendMessage: () => {},\n")
	b.WriteString("    onConnect: { addListener: () => {}, removeListener: () => {} },\n")
	b.WriteString("    onMessage: { addListener: () => {}, removeListener: () => {} }\n")
	b.WriteString("  };\n")
	b.WriteString("  window.chrome.csi = window.chrome.csi || (() => ({ startE: Date.now(), onloadT: Date.now(), tran: 15 }));\n")
	b.WriteString("  window.chrome.loadTimes = window.chrome.loadTimes || (() => ({ commitLoadTime: Date.now()/1000}));\n")

	// 5. navigator.permissions query override
	b.WriteString("  if (navigator.permissions) {\n")
	b.WriteString("    const _orig = navigator.permissions.query;\n")
	b.WriteString("    navigator.permissions.query = (params) => {\n")
	b.WriteString("      if (params.name === 'notifications' || params.name === 'clipboard-read' || params.name === 'clipboard-write')\n")
	b.WriteString("        return Promise.resolve({ state: 'prompt', onchange: null, addEventListener: () => {}, removeEventListener: () => {} });\n")
	b.WriteString("      return _orig.call(navigator.permissions, params);\n")
	b.WriteString("    };\n")
	b.WriteString("  }\n")

	// 6. WebGL vendor/renderer
	b.WriteString("  const _getParam = WebGLRenderingContext.prototype.getParameter;\n")
	b.WriteString("  WebGLRenderingContext.prototype.getParameter = function(p) {\n")
	b.WriteString("    if (p === 0x1F00) return 'Intel Inc.';\n")               // VENDOR
	b.WriteString("    if (p === 0x1F01) return 'Intel Iris OpenGL Engine';\n") // RENDERER
	b.WriteString("    return _getParam.call(this, p);\n")
	b.WriteString("  };\n")

	// 7. Canvas fingerprint randomization
	b.WriteString("  const _getImageData = CanvasRenderingContext2D.prototype.getImageData;\n")
	b.WriteString("  CanvasRenderingContext2D.prototype.getImageData = function(x, y, w, h) {\n")
	b.WriteString("    const img = _getImageData.call(this, x, y, w, h);\n")
	b.WriteString("    for (let i = 0; i < img.data.length; i += 4) {\n")
	b.WriteString("      img.data[i] = (img.data[i] + _rand(3)) % 256;\n")
	b.WriteString("    }\n")
	b.WriteString("    return img;\n")
	b.WriteString("  };\n")

	// 8. AudioContext fingerprint randomization
	b.WriteString("  if (window.AudioContext) {\n")
	b.WriteString("    const _createOscillator = AudioContext.prototype.createOscillator;\n")
	b.WriteString("    AudioContext.prototype.createOscillator = function() {\n")
	b.WriteString("      const osc = _createOscillator.call(this);\n")
	b.WriteString("      const _f = osc.frequency.value;\n")
	b.WriteString("      osc.frequency.value = _f + (_rand(5) - 2);\n")
	b.WriteString("      return osc;\n")
	b.WriteString("    };\n")
	b.WriteString("  }\n")

	// 9. Notification.permission default-deny
	b.WriteString("  if (window.Notification) {\n")
	b.WriteString("    _defineProperty(Notification, 'permission', { get: () => 'default' });\n")
	b.WriteString("  }\n")

	// 10. navigator.mimeTypes
	b.WriteString("  _defineProperty(navigator, 'mimeTypes', {\n")
	b.WriteString("    get: () => [\n")
	b.WriteString("      {type:'application/pdf',suffixes:'pdf',description:'Portable Document Format',enabledPlugin:{name:'Chrome PDF Viewer'}},\n")
	b.WriteString("      {type:'application/x-google-chrome-pdf',suffixes:'pdf',description:'Portable Document Format',enabledPlugin:{name:'Chrome PDF Viewer'}}\n")
	b.WriteString("    ]\n")
	b.WriteString("  });\n")

	// 11. window.outerWidth/Height match viewport
	b.WriteString(fmt.Sprintf("  _defineProperty(window, 'outerWidth', { get: () => %d });\n", p.ViewportWidth))
	b.WriteString(fmt.Sprintf("  _defineProperty(window, 'outerHeight', { get: () => %d });\n", p.ViewportHeight))

	// 12. window.devicePixelRatio
	b.WriteString(fmt.Sprintf("  _defineProperty(window, 'devicePixelRatio', { get: () => %v });\n", p.DevicePixelRatio))

	// 13. navigator.vendor
	b.WriteString(fmt.Sprintf("  _defineProperty(navigator, 'vendor', { get: () => '%s' });\n", p.Vendor))

	// 14. navigator.platform
	b.WriteString(fmt.Sprintf("  _defineProperty(navigator, 'platform', { get: () => '%s' });\n", p.Platform))

	// 15. navigator.deviceMemory
	b.WriteString("  _defineProperty(navigator, 'deviceMemory', { get: () => 8 });\n")

	// 16. navigator.hardwareConcurrency
	b.WriteString("  _defineProperty(navigator, 'hardwareConcurrency', { get: () => 8 });\n")

	// 17-18. screen dimensions
	b.WriteString(fmt.Sprintf("  _defineProperty(screen, 'width', { get: () => %d });\n", p.ViewportWidth))
	b.WriteString(fmt.Sprintf("  _defineProperty(screen, 'height', { get: () => %d });\n", p.ViewportHeight))
	b.WriteString(fmt.Sprintf("  _defineProperty(screen, 'availWidth', { get: () => %d });\n", p.ViewportWidth))
	b.WriteString(fmt.Sprintf("  _defineProperty(screen, 'availHeight', { get: () => %d });\n", p.ViewportHeight-40))

	// 19. Intl.DateTimeFormat timezone
	b.WriteString(fmt.Sprintf("  const _origDateTimeFormat = Intl.DateTimeFormat;\n"))
	b.WriteString(fmt.Sprintf("  Intl.DateTimeFormat = function(locales, options) {\n"))
	b.WriteString(fmt.Sprintf("    options = options || {};\n"))
	b.WriteString(fmt.Sprintf("    options.timeZone = options.timeZone || '%s';\n", p.Timezone))
	b.WriteString(fmt.Sprintf("    return _origDateTimeFormat.call(this, locales, options);\n"))
	b.WriteString(fmt.Sprintf("  };\n"))
	b.WriteString(fmt.Sprintf("  Intl.DateTimeFormat.prototype = _origDateTimeFormat.prototype;\n"))
	b.WriteString(fmt.Sprintf("  Intl.DateTimeFormat.supportedLocalesOf = _origDateTimeFormat.supportedLocalesOf;\n"))

	// 20. PerformanceEntry type filter (no-op, just presence)
	b.WriteString("  // PerformanceEntry patch: presence only\n")

	// 21. navigator.maxTouchPoints
	b.WriteString("  _defineProperty(navigator, 'maxTouchPoints', { get: () => 0 });\n")

	// 22. window.deviceOrientation presence
	b.WriteString("  if (!window.DeviceOrientationEvent) window.DeviceOrientationEvent = function() {};\n")

	// 23-24. matchMedia overrides
	b.WriteString("  const _origMatchMedia = window.matchMedia;\n")
	b.WriteString("  window.matchMedia = function(query) {\n")
	b.WriteString("    const m = _origMatchMedia.call(window, query);\n")
	b.WriteString(fmt.Sprintf("    if (query === '(prefers-color-scheme: %s)') { return { matches: true, media: query, addEventListener:()=>{}, removeEventListener:()=>{}, addListener:()=>{}, removeListener:()=>{}, onchange:null, dispatchEvent:()=>false }; }\n", p.ColorScheme))
	b.WriteString(fmt.Sprintf("    if (query === '(prefers-color-scheme: %s)') { return { matches: false, media: query, addEventListener:()=>{}, removeEventListener:()=>{}, addListener:()=>{}, removeListener:()=>{}, onchange:null, dispatchEvent:()=>false }; }\n", map[string]string{"light": "dark", "dark": "light"}[p.ColorScheme]))
	if p.ReducedMotion {
		b.WriteString("    if (query === '(prefers-reduced-motion: reduce)') { return { matches: true, media: query, addEventListener:()=>{}, removeEventListener:()=>{}, addListener:()=>{}, removeListener:()=>{}, onchange:null, dispatchEvent:()=>false }; }\n")
		b.WriteString("    if (query === '(prefers-reduced-motion: no-preference)') { return { matches: false, media: query, addEventListener:()=>{}, removeEventListener:()=>{}, addListener:()=>{}, removeListener:()=>{}, onchange:null, dispatchEvent:()=>false }; }\n")
	} else {
		b.WriteString("    if (query === '(prefers-reduced-motion: reduce)') { return { matches: false, media: query, addEventListener:()=>{}, removeEventListener:()=>{}, addListener:()=>{}, removeListener:()=>{}, onchange:null, dispatchEvent:()=>false }; }\n")
		b.WriteString("    if (query === '(prefers-reduced-motion: no-preference)') { return { matches: true, media: query, addEventListener:()=>{}, removeEventListener:()=>{}, addListener:()=>{}, removeListener:()=>{}, onchange:null, dispatchEvent:()=>false }; }\n")
	}
	b.WriteString("    return m;\n")
	b.WriteString("  };\n")

	// 25. console.debug no-op
	b.WriteString("  console.debug = function() {};\n")

	// 26. navigator.credentials presence
	b.WriteString("  if (!navigator.credentials) navigator.credentials = { get: () => Promise.resolve(null), create: () => Promise.resolve(null), preventSilentAccess: () => Promise.resolve() };\n")

	// 27. window.speechSynthesis presence
	b.WriteString("  if (!window.speechSynthesis) window.speechSynthesis = { getVoices: () => [], speak: () => {}, cancel: () => {}, pause: () => {}, resume: () => {} };\n")

	// 28. navigator.connection LIVE RTT refresh 60s
	b.WriteString("  const _conn = navigator.connection || navigator.mozConnection || navigator.webkitConnection;\n")
	b.WriteString("  if (_conn) {\n")
	b.WriteString("    _defineProperty(_conn, 'rtt', { get: () => window.__artemisRTT || 50 });\n")
	b.WriteString("    _defineProperty(_conn, 'effectiveType', { get: () => '4g' });\n")
	b.WriteString("    _defineProperty(_conn, 'downlink', { get: () => 10 });\n")
	b.WriteString("    _defineProperty(_conn, 'saveData', { get: () => false });\n")
	b.WriteString("    _defineProperty(_conn, 'type', { get: () => 'wifi' });\n")
	b.WriteString("  } else {\n")
	b.WriteString("    _defineProperty(navigator, 'connection', { get: () => ({ rtt: 50, effectiveType: '4g', downlink: 10, saveData: false, type: 'wifi', downlinkMax: Infinity }) });\n")
	b.WriteString("  }\n")
	b.WriteString("  window.__artemisRTT = 50;\n")
	b.WriteString("  setInterval(function() { window.__artemisRTT = 30 + Math.floor(Math.random() * 40); }, 60000);\n")

	// 29. connection.downlinkMax = Infinity
	b.WriteString("  if (navigator.connection) {\n")
	b.WriteString("    _defineProperty(navigator.connection, 'downlinkMax', { get: () => Infinity });\n")
	b.WriteString("  }\n")

	// 30. navigator.userAgentData brands
	b.WriteString("  if (!navigator.userAgentData) {\n")
	b.WriteString("    const _major = '126';\n")
	b.WriteString("    _defineProperty(navigator, 'userAgentData', { get: () => ({\n")
	b.WriteString("      brands: [{ brand: 'Chromium', version: _major }, { brand: 'Google Chrome', version: _major }],\n")
	b.WriteString("      mobile: false,\n")
	b.WriteString(fmt.Sprintf("      platform: '%s',\n", p.Platform))
	b.WriteString("      getHighEntropyValues: (hints) => Promise.resolve({\n")
	b.WriteString(fmt.Sprintf("        architecture: 'arm', brands: [{ brand: 'Chromium', version: _major }, { brand: 'Google Chrome', version: _major }],\n"))
	b.WriteString(fmt.Sprintf("        bitness: '64', mobile: false, model: '', platform: '%s', platformVersion: '14.0', uaFullVersion: '126.0.6478.126'\n", p.Platform))
	b.WriteString("      })\n")
	b.WriteString("    }) });\n")
	b.WriteString("  }\n")

	// 31. history.length = 1
	b.WriteString("  _defineProperty(history, 'length', { get: () => 1 });\n")

	// 32. CDP Marker cleanup
	b.WriteString("  try { delete window.cdc_adoQpoasnfa76pfcZLmcfl_Array; } catch(e) {}\n")
	b.WriteString("  try { delete window.cdc_adoQpoasnfa76pfcZLmcfl_Promise; } catch(e) {}\n")
	b.WriteString("  try { delete window.cdc_adoQpoasnfa76pfcZLmcfl_Symbol; } catch(e) {}\n")
	b.WriteString("  try { delete window.__webdriver; } catch(e) {}\n")
	b.WriteString("  try { delete window.__selenium; } catch(e) {}\n")
	b.WriteString("  try { delete window.__driver_evaluate; } catch(e) {}\n")
	b.WriteString("  try { delete window.__webdriver_script_fn; } catch(e) {}\n")
	b.WriteString("  try { delete window.__fxdriver; } catch(e) {}\n")
	b.WriteString("  try { delete window.$chrome_asyncScriptInfo; } catch(e) {}\n")

	// 33. UA version coherence - ensure Sec-CH-UA matches navigator
	b.WriteString("  const _origGet = Object.getOwnPropertyDescriptor(navigator, 'userAgent');\n")
	b.WriteString("  if (_origGet && _origGet.get) {\n")
	b.WriteString("    const _ua = _origGet.get.call(navigator);\n")
	b.WriteString(fmt.Sprintf("    const _expectedUA = %q;\n", p.UserAgent))
	b.WriteString("    if (_ua !== _expectedUA) {\n")
	b.WriteString("      _defineProperty(navigator, 'userAgent', { get: () => _expectedUA });\n")
	b.WriteString("    }\n")
	b.WriteString("  }\n")

	// 34. Battery API rejection
	b.WriteString("  if (navigator.getBattery) {\n")
	b.WriteString("    navigator.getBattery = () => Promise.reject(new TypeError('getBattery is not a function'));\n")
	b.WriteString("  } else {\n")
	b.WriteString("    _defineProperty(navigator, 'getBattery', { get: () => (() => Promise.reject(new TypeError('getBattery is not a function'))) });\n")
	b.WriteString("  }\n")

	b.WriteString("})();\n")
	return b.String()
}

// Quick returns the default stealth script.
func Quick() string { return Script(Defaults()) }
