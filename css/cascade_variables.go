package css

import (
	"errors"
	"sort"
	"strings"

	"golang.org/x/net/html"
)

const maxCSSVariableRollbacks = 128

// Unsupported shorthands and time-dependent declarations cannot silently
// disappear when a supported longhand is requested.
func (c *cssCascadeNode) checkComputedDependencies(property string) error {
	if !strings.HasPrefix(property, "--") {
		property = cssASCIILower(property)
	}
	for _, name := range []string{"animation", "animation-name", "transition", "transition-property"} {
		if len(c.candidates[name]) != 0 {
			return cssDiagnostic(DiagnosticUnsupported, 0, "time-dependent computed style requires browser semantics")
		}
	}
	if strings.HasPrefix(property, "--") {
		return nil
	}
	if len(c.candidates["all"]) != 0 || (strings.HasPrefix(property, "font-") || property == "line-height") && len(c.candidates["font"]) != 0 {
		return cssDiagnostic(DiagnosticUnsupported, 0, "overlapping shorthand requires complete computed semantics")
	}
	for _, family := range []string{"margin", "padding"} {
		if property != family && !strings.HasPrefix(property, family+"-") {
			continue
		}
		for _, logical := range []string{"-inline", "-inline-start", "-inline-end", "-block", "-block-start", "-block-end"} {
			if len(c.candidates[family+logical]) != 0 {
				return cssDiagnostic(DiagnosticUnsupported, 0, "logical box cascade requires writing-mode semantics")
			}
		}
	}
	return nil
}

type cssCascadeNode struct {
	winners    map[string]cssCandidate
	candidates map[string][]cssCandidate
	variables  cssVariables
}

type cssPropertyResolution struct {
	tokens       []cssToken
	valid        bool
	hasVariables bool
}

// cascadeCSSNode selects ordered declarations and computes custom properties
// in the parent's already-computed scope. Ordinary values remain token streams
// for the property-specific computed-value owner.
func cascadeCSSNode(sheets []*Stylesheet, node *html.Node, inline string, parent cssVariables) (*cssCascadeNode, error) {
	candidates, err := collectCSSCandidates(sheets, node, inline)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(candidates))
	for name, values := range candidates {
		names = append(names, name)
		candidates[name] = resolveCSSRollbacks(values)
	}
	sort.Strings(names)
	for attempt := 0; attempt <= maxCSSVariableRollbacks; attempt++ {
		winners := make(map[string]cssCandidate)
		declared := make(cssVariables)
		for _, name := range names {
			values := candidates[name]
			if len(values) == 0 {
				continue
			}
			winners[name] = values[0]
			if strings.HasPrefix(name, "--") {
				declared[name] = values[0].declaration.tokens
			}
		}
		variables, err := resolveCSSVariables(parent, declared)
		if err == nil {
			return &cssCascadeNode{winners: winners, candidates: candidates, variables: variables}, nil
		}
		var rollback *cssVariableRollback
		if !errors.As(err, &rollback) {
			return nil, err
		}
		values := candidates[rollback.name]
		if len(values) == 0 {
			return nil, cssDiagnostic(DiagnosticSyntax, 0, "CSS variable rollback has no winning declaration")
		}
		candidates[rollback.name] = resolveCSSRollbacks(rollbackCSSCandidates(values, values[0], rollback.keyword))
	}
	return nil, cssDiagnostic(DiagnosticBudget, 0, "CSS variable origin rollback limit exceeded")
}

func (c *cssCascadeNode) resolveProperty(name string) (cssPropertyResolution, error) {
	values := c.candidates[name]
	for len(values) > 0 {
		winner := values[0]
		hasVariables := len(cssVariableReferences(winner.declaration.tokens)) > 0
		resolved, valid, err := substituteCSSVariables(winner.declaration.tokens, func(name string) ([]cssToken, bool, error) {
			value, exists := c.variables[name]
			return value, exists, nil
		}, 0)
		if err != nil || !valid {
			return cssPropertyResolution{hasVariables: hasVariables}, err
		}
		keyword := cssWideKeyword(resolved)
		if keyword != "revert" && keyword != "revert-layer" {
			resolved, err = cssShorthandTokens(winner.declaration, name, resolved)
			var diagnostic *Diagnostic
			if errors.As(err, &diagnostic) && diagnostic.Code == DiagnosticSyntax && hasVariables {
				return cssPropertyResolution{hasVariables: true}, nil
			}
			return cssPropertyResolution{tokens: resolved, valid: err == nil, hasVariables: hasVariables}, err
		}
		values = resolveCSSRollbacks(rollbackCSSCandidates(values, winner, keyword))
	}
	return cssPropertyResolution{}, nil
}
