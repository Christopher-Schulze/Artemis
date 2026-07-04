package renderless

import "testing"

func TestCapabilityCategories(t *testing.T) {
	// Verify the 3 categories exist and have correct values
	if CategorySupportedReal != "supported_real" {
		t.Errorf("CategorySupportedReal = %q, want supported_real", CategorySupportedReal)
	}
	if CategoryStubCompatible != "stub_compatible" {
		t.Errorf("CategoryStubCompatible = %q, want stub_compatible", CategoryStubCompatible)
	}
	if CategoryUnsupportedEscalate != "unsupported_escalate" {
		t.Errorf("CategoryUnsupportedEscalate = %q, want unsupported_escalate", CategoryUnsupportedEscalate)
	}
}

func TestDefaultSupportedReal(t *testing.T) {
	apis := defaultSupportedReal()
	// Must include key APIs from spec L3992
	required := []string{
		"fetch", "XMLHttpRequest", "FormData", "Headers",
		"URL", "URLSearchParams", "cookie", "localStorage",
		"history", "location", "SubtleCrypto", "customElements",
		"MutationObserver", "WebSocket", "CSSStyleSheet",
	}
	for _, req := range required {
		found := false
		for _, a := range apis {
			if a == req {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("supported_real missing %q", req)
		}
	}
}

func TestDefaultStubCompatible(t *testing.T) {
	apis := defaultStubCompatible()
	required := []string{
		"canvas", "Element.animate", "IntersectionObserver",
		"ResizeObserver", "Worker", "SharedWorker",
		"visualViewport", "screen", "matchMedia",
	}
	for _, req := range required {
		found := false
		for _, a := range apis {
			if a == req {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("stub_compatible missing %q", req)
		}
	}
}

func TestDefaultUnsupportedEscalate(t *testing.T) {
	apis := defaultUnsupportedEscalate()
	required := []string{
		"getBoundingClientRect", "getClientRects",
		"WebGLRenderingContext", "WebGL2RenderingContext",
		"HTMLMediaElement", "WebAuthn", "ServiceWorker", "caches",
		"window.scroll", "window.scrollTo",
	}
	for _, req := range required {
		found := false
		for _, a := range apis {
			if a == req {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("unsupported_escalate missing %q", req)
		}
	}
}

func TestCapabilityMatch(t *testing.T) {
	p := RenderlessCapabilityProfile{
		SupportedReal:       defaultSupportedReal(),
		StubCompatible:      defaultStubCompatible(),
		UnsupportedEscalate: defaultUnsupportedEscalate(),
	}

	cases := []struct {
		api  string
		want CapabilityCategory
	}{
		{"fetch", CategorySupportedReal},
		{"FETCH", CategorySupportedReal}, // case-insensitive
		{"XMLHttpRequest", CategorySupportedReal},
		{"canvas", CategoryStubCompatible},
		{"Canvas", CategoryStubCompatible},
		{"Worker", CategoryStubCompatible},
		{"getBoundingClientRect", CategoryUnsupportedEscalate},
		{"WebGLRenderingContext", CategoryUnsupportedEscalate},
		{"unknownAPI", CategoryUnsupportedEscalate}, // default fail-closed
		{"", CategoryUnsupportedEscalate},           // empty fail-closed
	}

	for _, c := range cases {
		t.Run(c.api, func(t *testing.T) {
			got := p.CapabilityMatch(c.api)
			if got != c.want {
				t.Errorf("CapabilityMatch(%q) = %s, want %s", c.api, got, c.want)
			}
		})
	}
}

func TestRequiresEscalation(t *testing.T) {
	p := RenderlessCapabilityProfile{
		SupportedReal:       defaultSupportedReal(),
		StubCompatible:      defaultStubCompatible(),
		UnsupportedEscalate: defaultUnsupportedEscalate(),
	}

	// supported_real: no escalation regardless of semantics
	if p.RequiresEscalation("fetch", true) {
		t.Error("fetch with real semantics should not escalate")
	}
	if p.RequiresEscalation("fetch", false) {
		t.Error("fetch without real semantics should not escalate")
	}

	// stub_compatible: escalate only if real semantics needed
	if p.RequiresEscalation("canvas", false) {
		t.Error("canvas without real semantics should not escalate")
	}
	if !p.RequiresEscalation("canvas", true) {
		t.Error("canvas with real semantics should escalate")
	}

	// unsupported_escalate: always escalate
	if !p.RequiresEscalation("getBoundingClientRect", false) {
		t.Error("getBoundingClientRect should always escalate")
	}
	if !p.RequiresEscalation("WebGLRenderingContext", true) {
		t.Error("WebGL should always escalate")
	}

	// unknown: fail-closed upward
	if !p.RequiresEscalation("unknownAPI", false) {
		t.Error("unknown API should escalate (fail-closed)")
	}
}

func TestGenerateCapabilityProfileHasCategories(t *testing.T) {
	registry := NewWebAPIRegistry()
	registry.Register(WebAPIGlobal{Name: "fetch", Implemented: true})
	registry.Register(WebAPIGlobal{Name: "XMLHttpRequest", Implemented: true})

	cfg := EngineConfig{}
	profile := GenerateCapabilityProfile(cfg, registry)

	if len(profile.SupportedReal) == 0 {
		t.Error("SupportedReal is empty")
	}
	if len(profile.StubCompatible) == 0 {
		t.Error("StubCompatible is empty")
	}
	if len(profile.UnsupportedEscalate) == 0 {
		t.Error("UnsupportedEscalate is empty")
	}

	// Verify fetch is in supported_real
	found := false
	for _, a := range profile.SupportedReal {
		if a == "fetch" {
			found = true
			break
		}
	}
	if !found {
		t.Error("fetch not in SupportedReal")
	}
}
