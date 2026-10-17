package renderless

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/Christopher-Schulze/Artemis/js"
)

// The ordinary JS owner generates this immutable observation once per process.
// The existing WebAPIRegistry remains the only classification authority.
var currentBindingObservation = sync.OnceValues(func() (*bindingObservation, error) {
	inventory, err := js.ObserveInstalledBindings(context.Background())
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(inventory)
	if err != nil {
		return nil, err
	}
	observation := &bindingObservation{
		inventory: inventory, digest: fmt.Sprintf("%x", sha256.Sum256(encoded)),
		bindings:      make(map[string]js.InstalledBinding, len(inventory.Bindings)),
		nativeGlobals: make(map[string]bool, len(inventory.NativeGlobals)),
	}
	for _, binding := range inventory.Bindings {
		observation.bindings[binding.Path] = binding
	}
	for _, name := range inventory.NativeGlobals {
		observation.nativeGlobals[name] = true
	}
	return observation, nil
})

type bindingObservation struct {
	inventory     js.BindingInventory
	digest        string
	bindings      map[string]js.InstalledBinding
	nativeGlobals map[string]bool
}

// BindingCapability classifies one exact observed descriptor. Object-level
// evidence never implicitly grants semantics to children or undeclared aliases.
type BindingCapability struct {
	Path            string             `json:"path"`
	Category        CapabilityCategory `json:"category"`
	ConformanceCase string             `json:"conformance_case"`
}

func classifyInstalledBindings(bindings []js.InstalledBinding, apis []WebAPIGlobal) []BindingCapability {
	claims := make(map[string]BindingCapability, len(apis))
	for _, api := range apis {
		if !api.Implemented || api.BindingPath == "" {
			continue
		}
		for _, path := range []string{api.BindingPath, webAPIBindingPath(api.Name)} {
			claim := BindingCapability{Path: path, Category: webAPICategory(api), ConformanceCase: api.ConformanceCase}
			if previous, exists := claims[path]; exists && (previous.Category != claim.Category || previous.ConformanceCase != claim.ConformanceCase) {
				claim.Category, claim.ConformanceCase = CategoryUnsupportedEscalate, "binding-admission"
			}
			claims[path] = claim
		}
	}
	result := make([]BindingCapability, 0, len(bindings))
	for _, binding := range bindings {
		claim, exists := claims[binding.Path]
		if !exists || binding.Kind == "null" || binding.Kind == "undefined" {
			claim = BindingCapability{Path: binding.Path, Category: CategoryUnsupportedEscalate, ConformanceCase: "binding-admission"}
		}
		result = append(result, claim)
	}
	return result
}

// InstalledBindings returns detached source-pinned descriptors, including
// private native callbacks. No descriptor grants semantic or action authority.
func (r *WebAPIRegistry) InstalledBindings() (js.BindingInventory, error) {
	if r.observationError != nil {
		return js.BindingInventory{}, r.observationError
	}
	if r.observation == nil {
		return js.BindingInventory{}, fmt.Errorf("binding inventory: no observation")
	}
	result := r.observation.inventory
	result.Bindings = append([]js.InstalledBinding{}, result.Bindings...)
	result.NativeGlobals = append([]string{}, result.NativeGlobals...)
	return result, nil
}

func (o *bindingObservation) lookup(path string) (js.InstalledBinding, bool) {
	holder := "$"
	for _, member := range strings.Split(path, ".") {
		var found bool
		holder, found = o.member(holder, member)
		if !found {
			return js.InstalledBinding{}, false
		}
	}
	binding := o.bindings[holder]
	if binding.AliasOf != "" {
		binding = o.bindings[binding.AliasOf]
	}
	return binding, true
}

func (o *bindingObservation) member(holder, name string) (string, bool) {
	seen := make(map[string]bool)
	for !seen[holder] {
		seen[holder] = true
		binding, exists := o.bindings[holder]
		if !exists {
			break
		}
		if binding.AliasOf != "" {
			holder = binding.AliasOf
			continue
		}
		path := holder + "." + name
		if holder == "$" {
			path = name
		}
		if _, exists := o.bindings[path]; exists {
			return path, true
		}
		holder += ".[[Prototype]]"
	}
	return "", false
}

func webAPIBindingPath(name string) string {
	switch name {
	case "element":
		return "@element"
	case "cookie":
		return "document.cookie"
	case "querySelector", "querySelectorAll", "getElementById", "getElementsByClassName", "getElementsByTagName":
		return "document." + name
	case "SubtleCrypto":
		return "crypto.subtle"
	case "Element.animate":
		return "@element.animate"
	case "validityState":
		return "@input.validity"
	case "canvas":
		return "@canvas"
	default:
		return name
	}
}

func (r *WebAPIRegistry) observeDefinition(g WebAPIGlobal) WebAPIGlobal {
	g.BindingPath = ""
	g.ConformanceCase = "binding-admission"
	if r.observation == nil {
		g.Implemented, g.Category = false, CategoryUnsupportedEscalate
		return g
	}
	binding, found := r.observation.lookup(webAPIBindingPath(g.Name))
	g.Implemented = g.Implemented && found && binding.Kind != "undefined" && binding.Kind != "null"
	if g.Implemented {
		g.BindingPath = binding.Path
		g.Type = binding.Kind
		if binding.Kind == "function" && webAPIType(g.Name) == "constructor" {
			g.Type = "constructor"
		}
	}
	g.Category = webAPICategory(g)
	if r.conformance == nil {
		g.Category = CategoryUnsupportedEscalate
		return g
	}
	caseID := r.conformance.apiCases[webAPIIdentity(g.Name)]
	if evidence, exists := r.conformance.cases[caseID]; exists && evidence.Category == g.Category && g.Implemented {
		g.ConformanceCase = caseID
	} else {
		g.Category = CategoryUnsupportedEscalate
	}
	return g
}
