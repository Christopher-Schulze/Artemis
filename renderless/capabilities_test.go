package renderless

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	artemisengine "github.com/Christopher-Schulze/Artemis/engine"
	"github.com/Christopher-Schulze/Artemis/router"
)

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

func TestCSSCapabilitiesRequireCurrentConformanceInsteadOfDeclaredNames(t *testing.T) {
	registry := NewWebAPIRegistry()
	profile := GenerateCapabilityProfile(EngineConfig{}, registry)
	for _, api := range []string{"CSSStyleSheet", "CSSStyleDeclaration", "getComputedStyle"} {
		if profile.CapabilityMatch(api) != CategoryUnsupportedEscalate || !profile.RequiresEscalation(api, true) {
			t.Fatalf("unproved CSS API acquired supported semantics: %s", api)
		}
	}
	if len(profile.CSSSupport) != 0 || profile.CSSConformance != nil {
		t.Fatal("constructor invented CSS conformance evidence")
	}
	if !registry.IsImplemented("getComputedStyle") || registry.IsImplemented("CSSStyleSheet") || registry.IsImplemented("CSSStyleDeclaration") {
		t.Fatal("registry presence differs from actual CSS globals")
	}
}

func TestDefaultSupportedReal(t *testing.T) {
	apis := defaultSupportedReal()
	// Must include key APIs from spec L3992
	required := []string{
		"fetch", "XMLHttpRequest", "FormData", "Headers",
		"URL", "URLSearchParams", "cookie", "localStorage",
		"history", "location", "SubtleCrypto", "customElements",
		"MutationObserver", "WebSocket",
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
	registry.Register(WebAPIGlobal{Name: "fetch", Implemented: true, Category: CategorySupportedReal})
	registry.Register(WebAPIGlobal{Name: "XMLHttpRequest", Implemented: true, Category: CategorySupportedReal})

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

func TestCapabilityProfileForEngineUsesProductionAuthority(t *testing.T) {
	backend, err := artemisengine.New(artemisengine.Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer closeRenderlessTestResource(t, "backend", backend.Close)
	profile := CapabilityProfileForEngine(backend)
	if !profile.FetchSupport || !profile.SupportsWebAPI("document") {
		t.Fatalf("production profile=%+v", profile)
	}
	if profile.CacheSupport {
		t.Fatal("production engine must not claim an owned cache")
	}
	if profile.CapabilityCategoryName("fetch") != string(CategorySupportedReal) {
		t.Fatalf("fetch category=%q", profile.CapabilityCategoryName("fetch"))
	}
	if profile.CapabilityCategoryName("canvas") != string(CategoryStubCompatible) {
		t.Fatalf("canvas category=%q", profile.CapabilityCategoryName("canvas"))
	}
	if !profile.RequiresEscalation("getBoundingClientRect", false) {
		t.Fatal("layout API must escalate")
	}
}

func TestProductionBindingClassificationsDriveActualRouterDecisions(t *testing.T) {
	backend, err := artemisengine.New(artemisengine.Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer closeRenderlessTestResource(t, "backend", backend.Close)
	profile := CapabilityProfileForEngine(backend)
	routerOwner, err := router.New(router.Config{Executors: map[router.Mode]router.Executor{router.ModeRenderlessJS: router.RenderlessExecutor{Engine: backend}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		api      string
		real     bool
		category CapabilityCategory
		mode     router.Mode
	}{
		{"fetch", true, CategorySupportedReal, router.ModeRenderlessJS},
		{"Worker", false, CategoryStubCompatible, router.ModeRenderlessJS},
		{"Worker", true, CategoryStubCompatible, router.ModeChromiumCDP},
		{"getBoundingClientRect", true, CategoryUnsupportedEscalate, router.ModeChromiumCDP},
		{"crypto.subtle.sign", true, CategoryUnsupportedEscalate, router.ModeChromiumCDP},
		{"__fetch_abort", false, CategoryUnsupportedEscalate, router.ModeChromiumCDP},
	} {
		t.Run(test.api+":"+string(test.mode), func(t *testing.T) {
			request := router.RouteRequest{URL: "https://inventory.invalid/", Signals: router.Signals{IsHTML: true}, RequiredWebAPIs: []string{test.api}, NeedsRealSemantics: test.real, Capabilities: profile}
			decision, err := routerOwner.Decide(request)
			if err != nil || decision.Mode != test.mode || len(decision.Capabilities) != 1 {
				t.Fatalf("production authority selected wrong route: %+v %v", decision, err)
			}
			if evidence := decision.Capabilities[0]; evidence.API != test.api || evidence.Category != string(test.category) || evidence.Escalate != (test.mode == router.ModeChromiumCDP) {
				t.Fatalf("actual routing lost source capability evidence: %+v", evidence)
			}
			request.ForceMode = router.ModeRenderlessJS
			if _, err := routerOwner.Decide(request); (test.mode == router.ModeChromiumCDP) != errors.Is(err, router.ErrCapabilityEscalationNeeded) || (test.mode == router.ModeRenderlessJS && err != nil) {
				t.Fatalf("forced mode bypassed production capability authority: %v", err)
			}
		})
	}
}

func TestCapabilityProfileNilEngineFailsClosed(t *testing.T) {
	profile := CapabilityProfileForEngine(nil)
	if profile.IsCapable() || profile.FetchSupport || profile.DeterministicWait {
		t.Fatalf("nil engine produced capable profile=%+v", profile)
	}
	if !profile.RequiresEscalation("unknown", false) {
		t.Fatal("unknown capability must escalate")
	}
}

func TestCapabilityProfileRejectsUnclassifiedAndConflictingClaims(t *testing.T) {
	for _, test := range []struct {
		name       string
		definition WebAPIGlobal
	}{
		{"unclassified", WebAPIGlobal{Name: "fetch", Implemented: true}},
		{"invalid-category", WebAPIGlobal{Name: "fetch", Implemented: true, Category: "typo"}},
		{"absent", WebAPIGlobal{Name: "fetch", Implemented: false, Category: CategorySupportedReal}},
		{"case-conflict", WebAPIGlobal{Name: "FETCH", Implemented: true, Category: CategoryStubCompatible}},
	} {
		t.Run(test.name, func(t *testing.T) {
			registry := NewWebAPIRegistry()
			registry.Register(test.definition)
			profile := GenerateCapabilityProfile(EngineConfig{}, registry)
			if got := profile.CapabilityMatch("fetch"); got != CategoryUnsupportedEscalate || profile.FetchSupport {
				t.Fatalf("unproved/conflicting fetch acquired real support: category=%q fetch=%v", got, profile.FetchSupport)
			}
			count := 0
			for _, names := range [][]string{profile.SupportedReal, profile.StubCompatible, profile.UnsupportedEscalate} {
				for _, name := range names {
					if strings.EqualFold(name, "fetch") {
						count++
					}
				}
			}
			if count != 1 {
				t.Fatalf("binding needs exactly one classification, got %d", count)
			}
		})
	}
}

func TestCapabilityMatchRejectsContradictoryProfileLists(t *testing.T) {
	for _, profile := range []RenderlessCapabilityProfile{
		{SupportedReal: []string{"fetch"}, StubCompatible: []string{"FETCH"}},
		{SupportedReal: []string{"fetch"}, UnsupportedEscalate: []string{"FETCH"}},
		{StubCompatible: []string{"fetch"}, UnsupportedEscalate: []string{"FETCH"}},
	} {
		if got := profile.CapabilityMatch("fetch"); got != CategoryUnsupportedEscalate || !profile.RequiresEscalation("fetch", false) {
			t.Fatalf("contradictory profile acquired execution authority: %q", got)
		}
	}
	if got := (RenderlessCapabilityProfile{SupportedReal: []string{""}}).CapabilityMatch(""); got != CategoryUnsupportedEscalate {
		t.Fatalf("empty API acquired support: %q", got)
	}
}

func TestRegistryCannotPromoteUnprovedCSSCapabilities(t *testing.T) {
	registry := NewWebAPIRegistry()
	registry.Register(WebAPIGlobal{Name: "getComputedStyle", Type: "function", Implemented: true, Category: CategorySupportedReal})
	profile := GenerateCapabilityProfile(EngineConfig{}, registry)
	if got := profile.CapabilityMatch("getComputedStyle"); got != CategoryUnsupportedEscalate || profile.CSSConformance != nil {
		t.Fatalf("registry declaration substituted for current CSS observations: %q", got)
	}
}

func TestCapabilityProfilePartitionsEveryRegisteredBindingDeterministically(t *testing.T) {
	registry := NewWebAPIRegistry()
	registry.Register(WebAPIGlobal{Name: "fetch", Implemented: false, Category: CategorySupportedReal})
	registry.Register(WebAPIGlobal{Name: "WORKER", Implemented: true, Category: CategorySupportedReal})
	registry.Register(WebAPIGlobal{Name: "Headerſ", Implemented: true, Category: CategoryStubCompatible})
	profile := GenerateCapabilityProfile(EngineConfig{}, registry)
	for _, binding := range registry.All() {
		count := 0
		for _, names := range [][]string{profile.SupportedReal, profile.StubCompatible, profile.UnsupportedEscalate} {
			for _, name := range names {
				if strings.EqualFold(name, binding.Name) {
					count++
				}
			}
		}
		if count != 1 {
			t.Fatalf("binding %q has %d classifications", binding.Name, count)
		}
	}
	if got := profile.CapabilityMatch("Worker"); got != CategoryUnsupportedEscalate {
		t.Fatalf("case collision promoted an existing synthetic binding: %q", got)
	}
	if next := GenerateCapabilityProfile(EngineConfig{}, registry); !reflect.DeepEqual(profile, next) {
		t.Fatal("same registry generated different profile ordering or values")
	}
}
