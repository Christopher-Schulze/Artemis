package renderless

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/js"
)

func TestInstalledRegistryUsesActualSourceBindings(t *testing.T) {
	registry := NewWebAPIRegistry()
	inventory, err := registry.InstalledBindings()
	if err != nil {
		t.Fatal(err)
	}
	profile := GenerateCapabilityProfile(EngineConfig{}, registry)
	expected, err := js.SourceHash()
	if err != nil || profile.BindingSourceHash != expected || len(profile.BindingInventoryHash) != 64 || profile.BindingDescriptorCount != len(inventory.Bindings) {
		t.Fatalf("profile lost actual binding identity/count: %q %d %v", profile.BindingSourceHash, profile.BindingDescriptorCount, err)
	}
	actual := make(map[string]js.InstalledBinding, len(inventory.Bindings))
	for _, binding := range inventory.Bindings {
		actual[binding.Path] = binding
		name := binding.Path
		if strings.ContainsAny(name, ".[$@") || strings.HasPrefix(name, "__") || slices.Contains(inventory.NativeGlobals, name) {
			continue
		}
		if _, exists := registry.Get(name); !exists {
			t.Fatalf("installed public global omitted by registry: %s", name)
		}
	}
	for _, definition := range registry.All() {
		if definition.Implemented {
			if _, exists := actual[definition.BindingPath]; !exists {
				t.Fatalf("registry invented binding presence: %+v", definition)
			}
		}
	}
	inventory.Bindings[0].Path = "caller mutation"
	inventory.NativeGlobals[0] = "caller mutation"
	second, err := registry.InstalledBindings()
	if err != nil || second.Bindings[0].Path == "caller mutation" || second.NativeGlobals[0] == "caller mutation" {
		t.Fatalf("caller altered cached source observation: %v", err)
	}
	t.Logf("%d registered APIs, %d descriptors, %d native globals", registry.Count(), len(second.Bindings), len(second.NativeGlobals))
}

func TestBindingLookupResolvesRealAliasesAndPrototypes(t *testing.T) {
	registry := NewWebAPIRegistry()
	for _, test := range []struct{ path, kind string }{
		{"document.querySelector", "function"}, {"@element.appendChild", "function"},
		{"@input.validity", "accessor"}, {"crypto.subtle.digest", "function"},
		{"window.setTimeout", "function"},
	} {
		binding, exists := registry.observation.lookup(test.path)
		if !exists || binding.Kind != test.kind {
			t.Fatalf("actual inherited/aliased binding lost: %s: %+v %v", test.path, binding, exists)
		}
	}
	registry.Register(WebAPIGlobal{Name: "uninstalledAPI", Type: "function", Implemented: true, Category: CategorySupportedReal, BindingPath: "$"})
	if registry.IsImplemented("uninstalledAPI") || GenerateCapabilityProfile(EngineConfig{}, registry).CapabilityMatch("uninstalledAPI") != CategoryUnsupportedEscalate {
		t.Fatal("caller declaration invented an installed API")
	}
	if definition, exists := registry.Get("uninstalledAPI"); !exists || definition.BindingPath != "" {
		t.Fatalf("caller supplied an invented source target: %+v", definition)
	}
}

func TestMissingBindingObservationCannotGrantEngineCapabilities(t *testing.T) {
	registry := NewWebAPIRegistry()
	registry.observation = nil
	profile := GenerateCapabilityProfile(EngineConfig{}, registry)
	if profile.IsCapable() || profile.FetchSupport || profile.XHRSupport || profile.CookieJar || profile.InterceptSupport || profile.DeterministicWait || profile.BindingInventoryError == "" {
		t.Fatalf("missing observation produced capable engine: %+v", profile)
	}
}

func TestEveryObservedBindingHasOneSourceLinkedClassification(t *testing.T) {
	registry := NewWebAPIRegistry()
	inventory, err := registry.InstalledBindings()
	if err != nil {
		t.Fatal(err)
	}
	profile := GenerateCapabilityProfile(EngineConfig{}, registry)
	if len(profile.BindingCapabilities) != len(inventory.Bindings) {
		t.Fatalf("descriptor classifications are incomplete: %d of %d", len(profile.BindingCapabilities), len(inventory.Bindings))
	}
	seen := make(map[string]BindingCapability)
	for index, binding := range inventory.Bindings {
		claim := profile.BindingCapabilities[index]
		if claim.Path != binding.Path {
			t.Fatalf("classification lost exact source order: %q != %q", claim.Path, binding.Path)
		}
		if _, duplicate := seen[claim.Path]; duplicate {
			t.Fatalf("descriptor received multiple classifications: %s", claim.Path)
		}
		evidence, exists := registry.conformance.cases[claim.ConformanceCase]
		if !exists || evidence.Category != claim.Category || len(evidence.Tests) == 0 {
			t.Fatalf("descriptor lacks matching current conformance: %+v", claim)
		}
		seen[claim.Path] = claim
	}
	for _, path := range []string{"fetch", "document", "Worker", "PerformanceObserver", "crypto.subtle.digest", "__fetch"} {
		if _, exists := seen[path]; !exists {
			t.Fatalf("actual public/nested/private binding disappeared: %s", path)
		}
	}
	if seen["fetch"].Category != CategorySupportedReal || seen["Worker"].Category != CategoryStubCompatible || seen["PerformanceObserver"].Category != CategoryUnsupportedEscalate || seen["crypto.subtle.digest"].Category != CategoryUnsupportedEscalate || seen["__fetch"].Category != CategoryUnsupportedEscalate {
		t.Fatalf("object or public evidence leaked into unproved children/private entries: fetch=%+v worker=%+v performance_observer=%+v digest=%+v private=%+v", seen["fetch"], seen["Worker"], seen["PerformanceObserver"], seen["crypto.subtle.digest"], seen["__fetch"])
	}
	profile.BindingCapabilities[0].Path = "caller mutation"
	if next := GenerateCapabilityProfile(EngineConfig{}, registry); next.BindingCapabilities[0].Path != inventory.Bindings[0].Path {
		t.Fatal("caller altered cached descriptor classification")
	}
	encoded, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	var decoded RenderlessCapabilityProfile
	if err := json.Unmarshal(encoded, &decoded); err != nil || !reflect.DeepEqual(decoded.BindingCapabilities, profile.BindingCapabilities) {
		t.Fatalf("serialized profile lost exact binding classifications: %v", err)
	}
	t.Logf("classified=%d profile_json_bytes=%d", len(profile.BindingCapabilities), len(encoded))
}

func TestBindingClassificationConflictsCannotPromoteAliasesOrChildren(t *testing.T) {
	registry := NewWebAPIRegistry()
	apis := registry.All()
	fetch, exists := registry.Get("fetch")
	if !exists || fetch.Category != CategorySupportedReal {
		t.Fatal("actual fetch evidence is unavailable")
	}
	conflict := fetch
	conflict.Category, conflict.ConformanceCase = CategoryStubCompatible, "workers-synthetic"
	apis = append(apis, conflict, fetch)
	forward := classifyInstalledBindings(registry.observation.inventory.Bindings, apis)
	slices.Reverse(apis)
	reverse := classifyInstalledBindings(registry.observation.inventory.Bindings, apis)
	if !reflect.DeepEqual(forward, reverse) {
		t.Fatal("conflicting descriptor evidence depends on registration order")
	}
	for _, claim := range forward {
		if claim.Path == fetch.BindingPath && (claim.Category != CategoryUnsupportedEscalate || claim.ConformanceCase != "binding-admission") {
			t.Fatalf("conflicting semantic evidence acquired authority: %+v", claim)
		}
	}
	registry.Register(WebAPIGlobal{Name: "FETCH", Implemented: true, Category: CategorySupportedReal})
	profile := GenerateCapabilityProfile(EngineConfig{}, registry)
	if profile.CapabilityMatch("fetch") != CategoryUnsupportedEscalate {
		t.Fatal("uninstalled case alias failed to deny the global capability")
	}
	for _, claim := range profile.BindingCapabilities {
		if claim.Path == fetch.BindingPath && claim.Category != CategoryUnsupportedEscalate {
			t.Fatalf("descriptor bypassed global case-conflict denial: %+v", claim)
		}
	}
}
