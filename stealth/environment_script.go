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
		Version       string   `json:"version"`
		UserAgent     string   `json:"userAgent"`
		Platform      string   `json:"platform"`
		PlatformVer   string   `json:"platformVersion"`
		Architecture  string   `json:"architecture"`
		Locale        string   `json:"locale"`
		Languages     []string `json:"languages"`
		Timezone      string   `json:"timezone"`
		Width         int      `json:"width"`
		Height        int      `json:"height"`
		DPR           float64  `json:"dpr"`
		Cores         int      `json:"cores"`
		Memory        int      `json:"memory"`
		WebGLVendor   string   `json:"webglVendor"`
		WebGLRenderer string   `json:"webglRenderer"`
		RTT           int      `json:"rtt"`
	}{
		Version: profile.ChromeVersion, UserAgent: profile.UserAgent, Platform: profile.Platform, PlatformVer: profile.PlatformVersion, Architecture: profile.Architecture,
		Locale: profile.Locale, Languages: profile.Languages, Timezone: profile.Timezone,
		Width: profile.ViewportWidth, Height: profile.ViewportHeight, DPR: profile.DevicePixelRatio,
		Cores: profile.HardwareConcurrency, Memory: profile.DeviceMemoryGB,
		WebGLVendor: profile.WebGLVendor, WebGLRenderer: profile.WebGLRenderer, RTT: profile.NetworkRTTMillis,
	})
	if err != nil {
		return "", fmt.Errorf("stealth environment: encode profile: %w", err)
	}
	return fmt.Sprintf(`(() => {
  "use strict";
  const p = JSON.parse(%q);
  const define = (target, name, getter) => { try { Object.defineProperty(target, name, {get: getter, configurable: true}); } catch (_) {} };
  const nav = globalThis.navigator;
  if (!nav) return;
  define(nav, "webdriver", () => false);
  define(nav, "userAgent", () => p.userAgent);
  define(nav, "platform", () => p.platform);
  define(nav, "language", () => p.locale);
  define(nav, "languages", () => p.languages.slice());
  if (p.cores > 0) define(nav, "hardwareConcurrency", () => p.cores);
  if (p.memory > 0) define(nav, "deviceMemory", () => p.memory);
	if (globalThis.window) {
	    const chPlatform = p.platform === "MacIntel" ? "macOS" : (p.platform === "Win32" ? "Windows" : "Linux");
	    const originalUAData = nav.userAgentData;
	    const uaData = {brands:[{brand:"Chromium",version:p.version.split(".")[0]},{brand:"Google Chrome",version:p.version.split(".")[0]}], mobile:false, platform:chPlatform, getHighEntropyValues: async () => { let measured = {}; try { if (originalUAData && originalUAData.getHighEntropyValues) measured = await originalUAData.getHighEntropyValues(["platformVersion","architecture","bitness","model"]); } catch (_) {} return Object.assign({}, measured, {platform:chPlatform, platformVersion:p.platformVersion || measured.platformVersion || "", architecture:p.architecture || measured.architecture || "", bitness:measured.bitness || "64", model:measured.model || "", uaFullVersion:p.version}); }};
    define(nav, "userAgentData", () => uaData);
    define(globalThis.window, "devicePixelRatio", () => p.dpr);
    define(globalThis.window, "outerWidth", () => p.width + 15);
    define(globalThis.window, "outerHeight", () => p.height + 88);
    if (globalThis.screen) {
      define(globalThis.screen, "width", () => p.width);
      define(globalThis.screen, "height", () => p.height);
    }
    if (globalThis.Intl && globalThis.Intl.DateTimeFormat) {
      const OriginalDateTimeFormat = globalThis.Intl.DateTimeFormat;
      const WrappedDateTimeFormat = function(locales, options) {
        const next = Object.assign({}, options || {});
        if (!next.timeZone) next.timeZone = p.timezone;
        return new OriginalDateTimeFormat(locales, next);
      };
      WrappedDateTimeFormat.prototype = OriginalDateTimeFormat.prototype;
      WrappedDateTimeFormat.supportedLocalesOf = OriginalDateTimeFormat.supportedLocalesOf.bind(OriginalDateTimeFormat);
      globalThis.Intl.DateTimeFormat = WrappedDateTimeFormat;
    }
    if (nav.permissions && typeof nav.permissions.query === "function") {
      const query = nav.permissions.query.bind(nav.permissions);
      nav.permissions.query = (descriptor) => {
        if (descriptor && (descriptor.name === "notifications" || descriptor.name === "clipboard-read" || descriptor.name === "clipboard-write")) return Promise.resolve({state:"prompt", onchange:null});
        return query(descriptor);
      };
    }
    if (p.webglVendor && p.webglRenderer && globalThis.WebGLRenderingContext) {
      const originalGetParameter = WebGLRenderingContext.prototype.getParameter;
      WebGLRenderingContext.prototype.getParameter = function(parameter) {
        if (parameter === 0x1F00) return p.webglVendor;
        if (parameter === 0x1F01) return p.webglRenderer;
        return originalGetParameter.call(this, parameter);
      };
    }
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
  const define = (target, name, getter) => { try { Object.defineProperty(target, name, {get: getter, configurable: true}); } catch (_) {} };
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
