package actions

import (
	"context"
	"fmt"
	"strings"

	"artemis/scraper"
)

// resolve.go (spec L4020: bridge/actions/resolve.go - unified
// selector resolution).
//
// High-level actions: unified selector resolution that resolves
// element references (eN), CSS selectors, and XPath expressions
// to a canonical form for use by other actions.

// SelectorKind enumerates selector types
// (spec L4020: unified selector resolution).
type SelectorKind string

const (
	SelectorKindRef  SelectorKind = "ref"   // eN element reference
	SelectorKindCSS  SelectorKind = "css"   // CSS selector
	SelectorKindXPath SelectorKind = "xpath" // XPath expression
	SelectorKindText SelectorKind = "text"  // text-based lookup
)

// ResolvedSelector is a resolved selector with its kind and canonical
// form (spec L4020: unified selector resolution).
type ResolvedSelector struct {
	Kind     SelectorKind `json:"kind"`
	Original string       `json:"original"` // original selector string
	Canonical string      `json:"canonical"` // canonical form (e.g., XPath)
}

// ResolveSelector resolves a selector string to its kind and canonical
// form (spec L4020: unified selector resolution).
// - "e123" -> ref kind, canonical = "e123"
// - "//div[@id='x']" -> xpath kind, canonical = "//div[@id='x']"
// - "div.container" -> css kind, canonical = XPath translation
// - other -> text kind
func ResolveSelector(selector string) (ResolvedSelector, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return ResolvedSelector{}, fmt.Errorf("resolve: empty selector")
	}

	// Check if it's an element reference (eN pattern)
	if IsElementRef(selector) {
		return ResolvedSelector{
			Kind:      SelectorKindRef,
			Original:  selector,
			Canonical: selector,
		}, nil
	}

	// Check if it's XPath
	if scraper.IsXPath(selector) {
		return ResolvedSelector{
			Kind:      SelectorKindXPath,
			Original:  selector,
			Canonical: selector,
		}, nil
	}

	// Check if it's CSS
	if scraper.IsCSS(selector) {
		xpath, err := scraper.CSSToXPath(selector)
		if err != nil {
			// CSS but translation failed; keep as CSS
			return ResolvedSelector{
				Kind:      SelectorKindCSS,
				Original:  selector,
				Canonical: selector,
			}, nil
		}
		return ResolvedSelector{
			Kind:      SelectorKindCSS,
			Original:  selector,
			Canonical: xpath,
		}, nil
	}

	// Default: text-based lookup
	return ResolvedSelector{
		Kind:      SelectorKindText,
		Original:  selector,
		Canonical: selector,
	}, nil
}

// IsElementRef reports whether a selector is an element reference (eN)
// (spec L4020: unified selector resolution).
func IsElementRef(selector string) bool {
	if len(selector) < 2 || selector[0] != 'e' {
		return false
	}
	for _, ch := range selector[1:] {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

// ResolveAndValidate resolves a selector and validates it
// (spec L4020: unified selector resolution).
func ResolveAndValidate(ctx context.Context, selector string) (ResolvedSelector, error) {
	resolved, err := ResolveSelector(selector)
	if err != nil {
		return ResolvedSelector{}, err
	}
	if !IsValidSelectorKind(resolved.Kind) {
		return ResolvedSelector{}, fmt.Errorf("resolve: invalid selector kind %q", resolved.Kind)
	}
	return resolved, nil
}

// IsValidSelectorKind reports whether a selector kind is valid
// (spec L4020: unified selector resolution).
func IsValidSelectorKind(kind SelectorKind) bool {
	switch kind {
	case SelectorKindRef, SelectorKindCSS, SelectorKindXPath, SelectorKindText:
		return true
	}
	return false
}

// String returns a diagnostic summary.
func (r ResolvedSelector) String() string {
	return fmt.Sprintf("ResolvedSelector{kind:%s original:%s canonical:%s}",
		r.Kind, r.Original, r.Canonical)
}

// IsRef reports whether the resolved selector is an element reference.
func (r ResolvedSelector) IsRef() bool {
	return r.Kind == SelectorKindRef
}

// IsCSS reports whether the resolved selector is a CSS selector.
func (r ResolvedSelector) IsCSS() bool {
	return r.Kind == SelectorKindCSS
}

// IsXPath reports whether the resolved selector is an XPath expression.
func (r ResolvedSelector) IsXPath() bool {
	return r.Kind == SelectorKindXPath
}

// IsText reports whether the resolved selector is a text-based lookup.
func (r ResolvedSelector) IsText() bool {
	return r.Kind == SelectorKindText
}
