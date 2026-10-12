package renderless

import (
	"fmt"
	"strings"
	"sync"
)

// css.go (spec L4022: renderless/css.go - CSS parse/cascade/computed
// style).
//
// In-process no-render JS browser path: CSS parse/cascade/computed
// style. Parses CSS rules, applies cascade, and computes final
// styles for DOM elements.
//
// Ref: research/artemis/css/

// CSSRule represents a CSS rule
// (spec L4022: CSS parse/cascade/computed style).
type CSSRule struct {
	Selector    string            `json:"selector"`
	Properties  map[string]string `json:"properties"`
	Specificity int               `json:"specificity"`
	Source      string            `json:"source"` // stylesheet source
}

// CSSParser parses CSS text into rules
// (spec L4022: CSS parse/cascade/computed style).
type CSSParser struct {
	mu sync.Mutex
}

// NewCSSParser creates a new CSSParser
// (spec L4022: CSS parse/cascade/computed style).
func NewCSSParser() *CSSParser {
	return &CSSParser{}
}

// Parse parses CSS text into rules
// (spec L4022: CSS parse/cascade/computed style).
func (p *CSSParser) Parse(css string) []CSSRule {
	p.mu.Lock()
	defer p.mu.Unlock()
	var rules []CSSRule
	// Simple CSS parser: selector { prop: val; }
	for len(css) > 0 {
		braceStart := strings.Index(css, "{")
		if braceStart < 0 {
			break
		}
		braceEnd := strings.Index(css, "}")
		if braceEnd < 0 {
			break
		}
		selector := strings.TrimSpace(css[:braceStart])
		declarations := strings.TrimSpace(css[braceStart+1 : braceEnd])
		properties := parseDeclarations(declarations)
		if selector != "" && len(properties) > 0 {
			rules = append(rules, CSSRule{
				Selector:    selector,
				Properties:  properties,
				Specificity: computeSpecificity(selector),
			})
		}
		css = css[braceEnd+1:]
	}
	return rules
}

// parseDeclarations parses CSS declarations into a property map
func parseDeclarations(decl string) map[string]string {
	props := make(map[string]string)
	for _, part := range strings.Split(decl, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		colon := strings.Index(part, ":")
		if colon < 0 {
			continue
		}
		prop := strings.TrimSpace(part[:colon])
		val := strings.TrimSpace(part[colon+1:])
		if prop != "" && val != "" {
			props[prop] = val
		}
	}
	return props
}

// computeSpecificity computes a simple CSS specificity score
// (spec L4022: CSS parse/cascade/computed style).
func computeSpecificity(selector string) int {
	spec := 0
	// ID selectors (#id) -> 100
	spec += strings.Count(selector, "#") * 100
	// Class selectors (.class) -> 10
	spec += strings.Count(selector, ".") * 10
	// Type selectors (div, p, etc.) -> 1
	parts := strings.Fields(selector)
	for _, part := range parts {
		cleanPart := strings.TrimLeft(part, ".#")
		if cleanPart != "" && !strings.Contains(part, ".") && !strings.Contains(part, "#") {
			spec += 1
		}
	}
	return spec
}

// ComputedStyle computes the final style for an element
// (spec L4022: CSS parse/cascade/computed style).
type ComputedStyle struct {
	mu    sync.RWMutex
	rules []CSSRule
}

// NewComputedStyle creates a new ComputedStyle
// (spec L4022: CSS parse/cascade/computed style).
func NewComputedStyle(rules []CSSRule) *ComputedStyle {
	return &ComputedStyle{rules: rules}
}

// Compute computes the style for a given element selector
// (spec L4022: CSS parse/cascade/computed style).
func (c *ComputedStyle) Compute(elementSelector string) map[string]string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	// Sort rules by specificity (lowest first, so higher wins)
	sorted := make([]CSSRule, len(c.rules))
	copy(sorted, c.rules)
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].Specificity < sorted[i].Specificity {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	result := make(map[string]string)
	for _, rule := range sorted {
		if matchesSelector(elementSelector, rule.Selector) {
			for prop, val := range rule.Properties {
				result[prop] = val // higher specificity overrides
			}
		}
	}
	return result
}

// matchesSelector checks if an element selector matches a CSS selector
// (spec L4022: CSS parse/cascade/computed style).
func matchesSelector(element, selector string) bool {
	if selector == "*" {
		return true
	}
	return strings.Contains(element, selector) || strings.Contains(selector, element)
}

// RuleCount returns the number of CSS rules
// (spec L4022: CSS parse/cascade/computed style).
func (c *ComputedStyle) RuleCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.rules)
}

// String returns a diagnostic summary.
func (r CSSRule) String() string {
	return fmt.Sprintf("CSSRule{selector:%s props:%d spec:%d}", r.Selector, len(r.Properties), r.Specificity)
}
