package renderless

import (
	"fmt"
	"strings"
)

// capabilities.go (spec L4022: renderless/capabilities.go - generated
// RenderlessCapabilityProfile).
//
// In-process no-render JS browser path: generated
// RenderlessCapabilityProfile that describes which DOM/WebAPI
// features are available in the renderless engine.

// RenderlessCapabilityProfile describes the capabilities of the
// renderless engine (spec L4022: generated
// RenderlessCapabilityProfile).
type RenderlessCapabilityProfile struct {
	Version           string   `json:"version"`
	WebAPIs           []string `json:"webApis"`
	CSSSupport        []string `json:"cssSupport"`
	ScriptTypes       []string `json:"scriptTypes"`
	FetchSupport      bool     `json:"fetchSupport"`
	XHRSupport        bool     `json:"xhrSupport"`
	CookieJar         bool     `json:"cookieJar"`
	InterceptSupport  bool     `json:"interceptSupport"`
	CacheSupport      bool     `json:"cacheSupport"`
	RobotsGuard       bool     `json:"robotsGuard"`
	PrivateIPGuard    bool     `json:"privateIPGuard"`
	DeterministicWait bool     `json:"deterministicWait"`
}

// GenerateCapabilityProfile generates a capability profile from the
// engine config and WebAPI registry
// (spec L4022: generated RenderlessCapabilityProfile).
func GenerateCapabilityProfile(cfg EngineConfig, registry *WebAPIRegistry) RenderlessCapabilityProfile {
	profile := RenderlessCapabilityProfile{
		Version:          "1.0",
		FetchSupport:     registry.IsImplemented("fetch"),
		XHRSupport:       registry.IsImplemented("XMLHttpRequest"),
		CookieJar:        true,
		InterceptSupport: true,
		CacheSupport:     true,
		RobotsGuard:      cfg.EnableRobots,
		PrivateIPGuard:   cfg.PrivateIPBlock,
		DeterministicWait: true,
	}
	for _, api := range registry.All() {
		if api.Implemented {
			profile.WebAPIs = append(profile.WebAPIs, api.Name)
		}
	}
	profile.CSSSupport = []string{"selectors", "cascade", "computed-style"}
	profile.ScriptTypes = []string{"inline", "external", "module", "classic"}
	return profile
}

// SupportsWebAPI reports whether the profile supports a WebAPI
// (spec L4022: generated RenderlessCapabilityProfile).
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
