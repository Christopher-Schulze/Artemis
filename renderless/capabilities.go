package renderless

import (
	"fmt"
	"strings"

	artemisengine "github.com/Christopher-Schulze/Artemis/engine"
)

// capabilities.go (spec L4022: renderless/capabilities.go - production
// RenderlessCapabilityProfile).
//
// In-process no-render JS browser path: RenderlessCapabilityProfile describes
// which DOM/WebAPI features are available in the production engine.

// CapabilityCategory classifies a WebAPI by its renderless support
// level (spec L3992: supported_real | synthetic_compatible | unsupported_escalate).
type CapabilityCategory string

const (
	// CategorySupportedReal: fully implemented with real semantics.
	CategorySupportedReal CapabilityCategory = "supported_real"
	// Synthetic-compatible category: returns synthetic values that
	// don't break page scripts, but lack real semantics. Escalation
	// required when a task needs real semantics from a synthetic.
	CategoryStubCompatible CapabilityCategory = "stub_compatible"
	// CategoryUnsupportedEscalate: absent in renderless;
	// any use requires escalation to chromium_cdp.
	CategoryUnsupportedEscalate CapabilityCategory = "unsupported_escalate"
)

// RenderlessCapabilityProfile describes the capabilities of the
// production-backed renderless engine (spec L4022:
// RenderlessCapabilityProfile). Uses the 3-category model from
// ss28.3a: supported_real | synthetic_compatible | unsupported_escalate.
type RenderlessCapabilityProfile struct {
	Version             string   `json:"version"`
	WebAPIs             []string `json:"webApis"`
	CSSSupport          []string `json:"cssSupport"`
	ScriptTypes         []string `json:"scriptTypes"`
	FetchSupport        bool     `json:"fetchSupport"`
	XHRSupport          bool     `json:"xhrSupport"`
	CookieJar           bool     `json:"cookieJar"`
	InterceptSupport    bool     `json:"interceptSupport"`
	CacheSupport        bool     `json:"cacheSupport"`
	RobotsGuard         bool     `json:"robotsGuard"`
	PrivateIPGuard      bool     `json:"privateIPGuard"`
	DeterministicWait   bool     `json:"deterministicWait"`
	SupportedReal       []string `json:"supportedReal"`
	StubCompatible      []string `json:"stubCompatible"`
	UnsupportedEscalate []string `json:"unsupportedEscalate"`
}

// GenerateCapabilityProfile generates a capability profile from the
// engine config and WebAPI registry
// (spec L4022: production-derived RenderlessCapabilityProfile).
func GenerateCapabilityProfile(cfg EngineConfig, registry *WebAPIRegistry) RenderlessCapabilityProfile {
	profile := RenderlessCapabilityProfile{
		Version:          "1.0",
		CookieJar:        registry != nil,
		InterceptSupport: registry != nil,
		// The production engine exposes OnRequest interception, but it does
		// not own a cache. Cache semantics remain caller-owned and are not
		// published as a renderless engine capability.
		CacheSupport:        false,
		RobotsGuard:         cfg.EnableRobots,
		PrivateIPGuard:      cfg.PrivateIPBlock,
		DeterministicWait:   true,
		UnsupportedEscalate: defaultUnsupportedEscalate(),
	}
	if registry == nil {
		return failClosedProfile(profile)
	}
	profile.FetchSupport = registry.IsImplemented("fetch")
	profile.XHRSupport = registry.IsImplemented("XMLHttpRequest")
	for _, api := range registry.All() {
		if !api.Implemented {
			continue
		}
		profile.WebAPIs = append(profile.WebAPIs, api.Name)
		category := api.Category
		if category == "" {
			category = CategorySupportedReal
		}
		switch category {
		case CategorySupportedReal:
			profile.SupportedReal = append(profile.SupportedReal, api.Name)
		case CategoryStubCompatible:
			profile.StubCompatible = append(profile.StubCompatible, api.Name)
		default:
			profile.UnsupportedEscalate = append(profile.UnsupportedEscalate, api.Name)
		}
	}
	profile.CSSSupport = []string{"selectors", "cascade", "computed-style"}
	// The production engine evaluates inline, classic and external scripts.
	// Module code is accepted as classic code but has no module loader/import
	// semantics, so it stays explicitly synthetic-compatible.
	profile.ScriptTypes = []string{"inline", "external", "classic"}
	return profile
}

func failClosedProfile(profile RenderlessCapabilityProfile) RenderlessCapabilityProfile {
	profile.DeterministicWait = false
	profile.UnsupportedEscalate = append(profile.UnsupportedEscalate, "unknown")
	return profile
}

// CapabilityProfileForEngine is the single capability authority for the
// production Artemis engine. A nil engine yields a non-capable profile.
func CapabilityProfileForEngine(backend *artemisengine.Engine) RenderlessCapabilityProfile {
	if backend == nil {
		return failClosedProfile(RenderlessCapabilityProfile{Version: "1.0"})
	}
	config := backend.Config()
	return GenerateCapabilityProfile(EngineConfig{
		UserAgent:      config.UserAgent,
		FetchTimeout:   config.Timeout,
		EnableRobots:   config.ObeyRobots,
		PrivateIPBlock: !config.PolicyConfig.AllowPrivateNetworks,
	}, NewWebAPIRegistry())
}

// defaultSupportedReal returns the WebAPIs that are fully implemented
// in the renderless engine with real semantics (spec L3992).
func defaultSupportedReal() []string {
	return []string{
		"window", "document", "navigator", "console", "element", "Element", "HTMLElement", "Node", "Document", "querySelector", "querySelectorAll",
		"getElementById", "getElementsByClassName", "getElementsByTagName",
		"addEventListener", "removeEventListener", "dispatchEvent",
		"setTimeout", "setInterval", "clearTimeout", "clearInterval",
		"requestAnimationFrame", "Promise", "queueMicrotask",
		"fetch", "XMLHttpRequest", "FormData", "Headers", "Request", "Response", "Event", "CustomEvent", "URL", "URLSearchParams",
		"cookie", "localStorage", "sessionStorage",
		"history", "location",
		"SubtleCrypto", "crypto",
		"customElements", "MutationObserver",
		"WebSocket", "ReadableStream", "WritableStream", "BroadcastChannel",
		"HTMLIFrameElement",
		"HTMLFormElement", "validityState",
		"Range", "Selection",
		"CSSStyleSheet", "CSSStyleDeclaration", "getComputedStyle",
	}
}

// Returns the WebAPIs that are synthetic-compatible — they return
// synthetic values that don't break page scripts but lack real
// semantics (spec L3992). Escalation required when a task needs
// real semantics from a synthetic-compatible API.
func defaultStubCompatible() []string {
	return []string{
		"canvas",               // 2D context, no pixel readback
		"Element.animate",      // immediate-settle, no real animation
		"IntersectionObserver", // one-shot, no real layout
		"ResizeObserver",       // one-shot, no real layout
		"Worker",               // no-execute synthetic
		"SharedWorker",         // no-execute synthetic
		"visualViewport",       // synthetic values
		"screen",               // synthetic values
		"matchMedia",           // synthetic media query results
	}
}

// defaultUnsupportedEscalate returns the WebAPIs that are not
// implemented in renderless and require escalation to chromium_cdp
// (spec L3992).
func defaultUnsupportedEscalate() []string {
	return []string{
		"getBoundingClientRect",                                  // layout-sensitive
		"getClientRects",                                         // layout-sensitive
		"offsetWidth", "offsetHeight", "offsetTop", "offsetLeft", // layout
		"WebGLRenderingContext", "WebGL2RenderingContext", // real GPU
		"HTMLMediaElement", "HTMLVideoElement", "HTMLAudioElement", // media
		"WebAuthn", "CredentialsContainer", // authentication
		"ServiceWorker", "caches", // PWA cache
		"window.scroll", "window.scrollTo", "window.scrollBy", // real scrolling
		"chrome.runtime.id", "chrome.extension", // extension/fingerprint
		"External", // CAPTCHA/WAF
	}
}

// CapabilityMatch classifies a required WebAPI against the profile
// and returns the category. If the API is not found in any category,
// it defaults to unsupported_escalate (fail-closed upward).
func (p RenderlessCapabilityProfile) CapabilityMatch(api string) CapabilityCategory {
	for _, s := range p.SupportedReal {
		if strings.EqualFold(s, api) {
			return CategorySupportedReal
		}
	}
	for _, s := range p.StubCompatible {
		if strings.EqualFold(s, api) {
			return CategoryStubCompatible
		}
	}
	for _, s := range p.UnsupportedEscalate {
		if strings.EqualFold(s, api) {
			return CategoryUnsupportedEscalate
		}
	}
	return CategoryUnsupportedEscalate
}

// RequiresEscalation reports whether a task that needs real semantics
// from the given API would require escalation to chromium_cdp.
// Per spec: escalate on any task requiring real semantics from a synthetic.
func (p RenderlessCapabilityProfile) RequiresEscalation(api string, needsRealSemantics bool) bool {
	cat := p.CapabilityMatch(api)
	if cat == CategoryUnsupportedEscalate {
		return true
	}
	if cat == CategoryStubCompatible && needsRealSemantics {
		return true
	}
	return false
}

// CapabilityCategoryName exposes the profile category to the router without
// coupling the router package to renderless types.
func (p RenderlessCapabilityProfile) CapabilityCategoryName(api string) string {
	return string(p.CapabilityMatch(api))
}

// SupportsWebAPI reports whether the profile supports a WebAPI
// (spec L4022: production-derived RenderlessCapabilityProfile).
func (p RenderlessCapabilityProfile) SupportsWebAPI(name string) bool {
	for _, api := range p.WebAPIs {
		if strings.EqualFold(api, name) {
			return true
		}
	}
	return false
}

// SupportsCSS reports whether the profile supports a CSS feature
// (spec L4022: generated RenderlessCapabilityProfile).
func (p RenderlessCapabilityProfile) SupportsCSS(feature string) bool {
	for _, f := range p.CSSSupport {
		if strings.EqualFold(f, feature) {
			return true
		}
	}
	return false
}

// SupportsScriptType reports whether the profile supports a script type
// (spec L4022: generated RenderlessCapabilityProfile).
func (p RenderlessCapabilityProfile) SupportsScriptType(t string) bool {
	for _, st := range p.ScriptTypes {
		if strings.EqualFold(st, t) {
			return true
		}
	}
	return false
}

// Summary returns a human-readable summary
// (spec L4022: generated RenderlessCapabilityProfile).
func (p RenderlessCapabilityProfile) Summary() string {
	return fmt.Sprintf("RenderlessCapabilityProfile{version:%s apis:%d css:%d scripts:%d fetch:%v xhr:%v}",
		p.Version, len(p.WebAPIs), len(p.CSSSupport), len(p.ScriptTypes), p.FetchSupport, p.XHRSupport)
}

// String returns a diagnostic summary.
func (p RenderlessCapabilityProfile) String() string {
	return p.Summary()
}

// IsCapable reports whether the profile meets minimum requirements
// (spec L4022: generated RenderlessCapabilityProfile).
func (p RenderlessCapabilityProfile) IsCapable() bool {
	return p.FetchSupport && len(p.WebAPIs) >= 10 && p.DeterministicWait
}

// MissingWebAPIs returns WebAPIs that are not supported
// (spec L4022: generated RenderlessCapabilityProfile).
func (p RenderlessCapabilityProfile) MissingWebAPIs(required []string) []string {
	var missing []string
	for _, req := range required {
		if !p.SupportsWebAPI(req) {
			missing = append(missing, req)
		}
	}
	return missing
}
