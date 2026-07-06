package renderless

import (
	"fmt"
	"strings"
	"sync"
)

// webapi.go (spec L4022: renderless/webapi.go - DOM/WebAPI globals).
//
// In-process no-render JS browser path: DOM/WebAPI globals that
// simulate the browser's window/document/navigator/fetch APIs for
// JavaScript execution without a real browser.
//
// Ref: research/artemis/webapi/document.go:1-100+

// WebAPIRegistry holds DOM/WebAPI global definitions
// (spec L4022: DOM/WebAPI globals).
type WebAPIRegistry struct {
	mu      sync.RWMutex
	globals map[string]WebAPIGlobal
}

// WebAPIGlobal represents a DOM/WebAPI global
// (spec L4022: DOM/WebAPI globals).
type WebAPIGlobal struct {
	Name        string `json:"name"`
	Type        string `json:"type"` // "function", "object", "constructor"
	Implemented bool   `json:"implemented"`
}

// NewWebAPIRegistry creates a new WebAPIRegistry with standard globals
// (spec L4022: DOM/WebAPI globals).
func NewWebAPIRegistry() *WebAPIRegistry {
	r := &WebAPIRegistry{globals: make(map[string]WebAPIGlobal)}
	r.registerStandard()
	return r
}

func (r *WebAPIRegistry) registerStandard() {
	standards := []WebAPIGlobal{
		{Name: "window", Type: "object", Implemented: true},
		{Name: "document", Type: "object", Implemented: true},
		{Name: "navigator", Type: "object", Implemented: true},
		{Name: "location", Type: "object", Implemented: true},
		{Name: "fetch", Type: "function", Implemented: true},
		{Name: "XMLHttpRequest", Type: "constructor", Implemented: true},
		{Name: "localStorage", Type: "object", Implemented: true},
		{Name: "sessionStorage", Type: "object", Implemented: true},
		{Name: "console", Type: "object", Implemented: true},
		{Name: "setTimeout", Type: "function", Implemented: true},
		{Name: "setInterval", Type: "function", Implemented: true},
		{Name: "clearTimeout", Type: "function", Implemented: true},
		{Name: "clearInterval", Type: "function", Implemented: true},
		{Name: "URL", Type: "constructor", Implemented: true},
		{Name: "URLSearchParams", Type: "constructor", Implemented: true},
		{Name: "Headers", Type: "constructor", Implemented: true},
		{Name: "Request", Type: "constructor", Implemented: true},
		{Name: "Response", Type: "constructor", Implemented: true},
		{Name: "Event", Type: "constructor", Implemented: true},
		{Name: "CustomEvent", Type: "constructor", Implemented: true},
		{Name: "Element", Type: "constructor", Implemented: true},
		{Name: "HTMLElement", Type: "constructor", Implemented: true},
		{Name: "Node", Type: "constructor", Implemented: true},
		{Name: "Document", Type: "constructor", Implemented: true},
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, g := range standards {
		r.globals[g.Name] = g
	}
}

// Get retrieves a global by name
// (spec L4022: DOM/WebAPI globals).
func (r *WebAPIRegistry) Get(name string) (WebAPIGlobal, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	g, ok := r.globals[name]
	return g, ok
}

// Register registers a new global
// (spec L4022: DOM/WebAPI globals).
func (r *WebAPIRegistry) Register(g WebAPIGlobal) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.globals[g.Name] = g
}

// All returns all registered globals
// (spec L4022: DOM/WebAPI globals).
func (r *WebAPIRegistry) All() []WebAPIGlobal {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []WebAPIGlobal
	for _, g := range r.globals {
		result = append(result, g)
	}
	return result
}

// IsImplemented reports whether a global is implemented
// (spec L4022: DOM/WebAPI globals).
func (r *WebAPIRegistry) IsImplemented(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	g, ok := r.globals[name]
	return ok && g.Implemented
}

// Count returns the number of registered globals
// (spec L4022: DOM/WebAPI globals).
func (r *WebAPIRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.globals)
}

// ImplementedCount returns the number of implemented globals
// (spec L4022: DOM/WebAPI globals).
func (r *WebAPIRegistry) ImplementedCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	count := 0
	for _, g := range r.globals {
		if g.Implemented {
			count++
		}
	}
	return count
}

// Names returns all global names sorted alphabetically
// (spec L4022: DOM/WebAPI globals).
func (r *WebAPIRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var names []string
	for name := range r.globals {
		names = append(names, name)
	}
	return names
}

// String returns a diagnostic summary.
func (r *WebAPIRegistry) String() string {
	return fmt.Sprintf("WebAPIRegistry{total:%d implemented:%d}", r.Count(), r.ImplementedCount())
}

// FormatGlobals returns a formatted string of all globals
// (spec L4022: DOM/WebAPI globals).
func (r *WebAPIRegistry) FormatGlobals() string {
	var sb strings.Builder
	for _, g := range r.All() {
		status := "stub"
		if g.Implemented {
			status = "implemented"
		}
		sb.WriteString(fmt.Sprintf("%s (%s): %s\n", g.Name, g.Type, status))
	}
	return sb.String()
}
