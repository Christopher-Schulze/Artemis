package renderless

import (
	"fmt"
	"slices"
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
	// CategorySupportedReal has actual semantics within its recorded scope.
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
	ClassificationSourceHash string                      `json:"classificationSourceHash,omitempty"`
	ConformanceError         string                      `json:"conformanceError,omitempty"`
	Conformance              []WebAPIConformanceEvidence `json:"conformance,omitempty"`
	ConformanceCoverage      ConformanceCoverage         `json:"conformanceCoverage"`
	BindingSourceHash        string                      `json:"bindingSourceHash,omitempty"`
	BindingInventoryHash     string                      `json:"bindingInventoryHash,omitempty"`
	BindingDescriptorCount   int                         `json:"bindingDescriptorCount,omitempty"`
	BindingCapabilities      []BindingCapability         `json:"bindingCapabilities,omitempty"`
	BindingInventoryError    string                      `json:"bindingInventoryError,omitempty"`
	Version                  string                      `json:"version"`
	WebAPIs                  []string                    `json:"webApis"`
	CSSSupport               []string                    `json:"cssSupport"`
	CSSConformance           *CSSConformanceEvidence     `json:"cssConformance,omitempty"`
	ScriptTypes              []string                    `json:"scriptTypes"`
	FetchSupport             bool                        `json:"fetchSupport"`
	XHRSupport               bool                        `json:"xhrSupport"`
	CookieJar                bool                        `json:"cookieJar"`
	InterceptSupport         bool                        `json:"interceptSupport"`
	CacheSupport             bool                        `json:"cacheSupport"`
	RobotsGuard              bool                        `json:"robotsGuard"`
	PrivateIPGuard           bool                        `json:"privateIPGuard"`
	DeterministicWait        bool                        `json:"deterministicWait"`
	SupportedReal            []string                    `json:"supportedReal"`
	StubCompatible           []string                    `json:"stubCompatible"`
	UnsupportedEscalate      []string                    `json:"unsupportedEscalate"`
}

// CSSConformanceEvidence is a generated observation projection. Permission,
// current receipt admission and release authentication remain host-owned.
type CSSConformanceEvidence struct {
	SourceHash   string                      `json:"source_hash"`
	CSSHash      string                      `json:"css_hash"`
	BindingHash  string                      `json:"binding_hash"`
	Observations []CSSConformanceObservation `json:"observations"`
}

type CSSConformanceObservation struct {
	CaseID      string `json:"case_id"`
	Runner      string `json:"runner"`
	FixtureID   string `json:"fixture_id"`
	ResultHash  string `json:"result_hash"`
	EvidenceRef string `json:"evidence_ref,omitempty"`
}

// GenerateCapabilityProfile generates a capability profile from the
// engine config and WebAPI registry
// (spec L4022: production-derived RenderlessCapabilityProfile).
func GenerateCapabilityProfile(cfg EngineConfig, registry *WebAPIRegistry) RenderlessCapabilityProfile {
	profile := RenderlessCapabilityProfile{
		Version: "1.0",
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
	if registry.observationError != nil || registry.observation == nil {
		profile.BindingInventoryError = "binding_inventory_unavailable"
		return failClosedProfile(profile)
	}
	if registry.conformanceError != nil || registry.conformance == nil {
		profile.ConformanceError = "conformance_sources_unavailable"
		if registry.conformanceError != nil {
			profile.ConformanceError = registry.conformanceError.Error()
		}
		return failClosedProfile(profile)
	}
	profile.ClassificationSourceHash = registry.conformance.sourceHash
	profile.ConformanceCoverage = registry.conformance.coverage
	profile.ConformanceCoverage.UnmappedTests = slices.Clone(registry.conformance.coverage.UnmappedTests)
	profile.BindingSourceHash = registry.observation.inventory.SourceHash
	profile.BindingInventoryHash = registry.observation.digest
	profile.BindingDescriptorCount = len(registry.observation.inventory.Bindings)
	profile.CookieJar = registry.IsImplemented("cookie")
	profile.InterceptSupport = true
	classifications := make(map[string]WebAPIGlobal)
	for _, name := range profile.UnsupportedEscalate {
		classifications[webAPIIdentity(name)] = WebAPIGlobal{Name: name, Category: CategoryUnsupportedEscalate}
	}
	profile.UnsupportedEscalate = nil
	seen := make(map[string]bool)
	caseIDs := map[string]bool{"binding-admission": true}
	apis := registry.All()
	for _, api := range apis {
		caseIDs[api.ConformanceCase] = true
		if api.Implemented {
			profile.WebAPIs = append(profile.WebAPIs, api.Name)
		}
		key := webAPIIdentity(api.Name)
		category := webAPICategory(api)
		if previous, exists := classifications[key]; exists {
			api.Name = previous.Name
			if seen[key] && previous.ConformanceCase != api.ConformanceCase || previous.Category == CategoryUnsupportedEscalate {
				category = CategoryUnsupportedEscalate
			}
		}
		api.Category = category
		classifications[key] = api
		seen[key] = true
	}
	for index := range apis {
		apis[index].Category = classifications[webAPIIdentity(apis[index].Name)].Category
		if apis[index].Category == CategoryUnsupportedEscalate {
			apis[index].ConformanceCase = "binding-admission"
		}
	}
	profile.BindingCapabilities = classifyInstalledBindings(registry.observation.inventory.Bindings, apis)
	names := make([]string, 0, len(classifications))
	for name := range classifications {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		api := classifications[name]
		switch api.Category {
		case CategorySupportedReal:
			profile.SupportedReal = append(profile.SupportedReal, api.Name)
		case CategoryStubCompatible:
			profile.StubCompatible = append(profile.StubCompatible, api.Name)
		default:
			profile.UnsupportedEscalate = append(profile.UnsupportedEscalate, api.Name)
		}
	}
	profile.FetchSupport = profile.CapabilityMatch("fetch") == CategorySupportedReal
	profile.XHRSupport = profile.CapabilityMatch("XMLHttpRequest") == CategorySupportedReal
	profile.Conformance = registry.conformance.evidenceFor(caseIDs)
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

// defaultSupportedReal derives the real, explicitly scoped conformance entries.
func defaultSupportedReal() []string {
	return conformanceNames(CategorySupportedReal)
}

// Returns the WebAPIs that are synthetic-compatible — they return
// synthetic values that don't break page scripts but lack real
// semantics (spec L3992). Escalation required when a task needs
// real semantics from a synthetic-compatible API.
func defaultStubCompatible() []string {
	return conformanceNames(CategoryStubCompatible)
}

// defaultUnsupportedEscalate returns the WebAPIs that are not
// implemented in renderless and require escalation to chromium_cdp
// (spec L3992).
func defaultUnsupportedEscalate() []string {
	return []string{
		"CSSStyleSheet", "CSSStyleDeclaration", "getComputedStyle",
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
	if api == "" {
		return CategoryUnsupportedEscalate
	}
	for _, s := range p.UnsupportedEscalate {
		if strings.EqualFold(s, api) {
			return CategoryUnsupportedEscalate
		}
	}
	real := false
	for _, s := range p.SupportedReal {
		if strings.EqualFold(s, api) {
			real = true
			break
		}
	}
	for _, s := range p.StubCompatible {
		if strings.EqualFold(s, api) {
			if real {
				return CategoryUnsupportedEscalate
			}
			return CategoryStubCompatible
		}
	}
	if real {
		return CategorySupportedReal
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
