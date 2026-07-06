package actions

import (
	"context"
	"fmt"
	"strings"

	"github.com/Christopher-Schulze/Artemis/scraper"
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
	SelectorKindRef      SelectorKind = "ref"      // eN element reference
	SelectorKindCSS      SelectorKind = "css"      // CSS selector
	SelectorKindXPath    SelectorKind = "xpath"    // XPath expression
	SelectorKindText     SelectorKind = "text"     // text-based lookup
	SelectorKindSemantic SelectorKind = "semantic" // NLP-based "find:login button"
)

// SelectorPrefixes maps selector prefixes to their kinds
// (spec L4231: 5 selector types with prefix syntax).
var SelectorPrefixes = map[string]SelectorKind{
	"css:":   SelectorKindCSS,
	"xpath:": SelectorKindXPath,
	"text:":  SelectorKindText,
	"find:":  SelectorKindSemantic,
}

// ResolvedSelector is a resolved selector with its kind and canonical
// form (spec L4020: unified selector resolution).
type ResolvedSelector struct {
	Kind      SelectorKind `json:"kind"`
	Original  string       `json:"original"`  // original selector string
	Canonical string       `json:"canonical"` // canonical form (e.g., XPath)
}

// ResolveSelector resolves a selector string to its kind and canonical
// form (spec L4020: unified selector resolution, spec L4231: 5 types).
// Prefixed syntax: "css:...", "xpath:...", "text:...", "find:..."
// - "e123" -> ref kind, canonical = "e123"
// - "css:#submit" -> css kind
// - "xpath://button" -> xpath kind
// - "text:Submit" -> text kind
// - "find:login button" -> semantic kind
// - "//div[@id='x']" -> xpath kind (auto-detected)
// - "div.container" -> css kind (auto-detected)
// - other -> text kind (default)
func ResolveSelector(selector string) (ResolvedSelector, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return ResolvedSelector{}, fmt.Errorf("resolve: empty selector")
	}

	// Check for explicit prefixes first (spec L4231: prefix syntax)
	for prefix, kind := range SelectorPrefixes {
		if strings.HasPrefix(selector, prefix) {
			rest := strings.TrimSpace(selector[len(prefix):])
			if rest == "" {
				return ResolvedSelector{}, fmt.Errorf("resolve: empty %s selector", kind)
			}
			canonical := rest
			if kind == SelectorKindCSS {
				if xpath, err := scraper.CSSToXPath(rest); err == nil {
					canonical = xpath
				}
			}
			return ResolvedSelector{
				Kind:      kind,
				Original:  selector,
				Canonical: canonical,
			}, nil
		}
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
// (spec L4020: unified selector resolution, spec L4231: 5 types).
func IsValidSelectorKind(kind SelectorKind) bool {
	switch kind {
	case SelectorKindRef, SelectorKindCSS, SelectorKindXPath, SelectorKindText, SelectorKindSemantic:
		return true
	}
	return false
}

// SelectorPriority returns the resolution priority of a selector kind
// (spec L4231: Resolution priority: Ref > CSS > XPath > Text > Semantic).
// Lower number = higher priority.
func SelectorPriority(kind SelectorKind) int {
	switch kind {
	case SelectorKindRef:
		return 0
	case SelectorKindCSS:
		return 1
	case SelectorKindXPath:
		return 2
	case SelectorKindText:
		return 3
	case SelectorKindSemantic:
		return 4
	}
	return 99
}

// CompareSelectorPriority compares two selector kinds by resolution
// priority. Returns -1 if a has higher priority than b, 1 if lower,
// 0 if equal (spec L4231: Ref > CSS > XPath > Text > Semantic).
func CompareSelectorPriority(a, b SelectorKind) int {
	pa := SelectorPriority(a)
	pb := SelectorPriority(b)
	if pa < pb {
		return -1
	}
	if pa > pb {
		return 1
	}
	return 0
}

// SortSelectorsByPriority sorts a slice of resolved selectors by
// resolution priority (highest priority first)
// (spec L4231: Ref > CSS > XPath > Text > Semantic).
func SortSelectorsByPriority(selectors []ResolvedSelector) []ResolvedSelector {
	out := make([]ResolvedSelector, len(selectors))
	copy(out, selectors)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && CompareSelectorPriority(out[j].Kind, out[j-1].Kind) < 0; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
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

// IsSemantic reports whether the resolved selector is a semantic/NLP lookup.
func (r ResolvedSelector) IsSemantic() bool {
	return r.Kind == SelectorKindSemantic
}
