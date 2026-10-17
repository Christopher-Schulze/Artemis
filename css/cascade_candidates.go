package css

import (
	"sort"

	"github.com/andybalholm/cascadia"
	"golang.org/x/net/html"
)

// Origin identifies the three static cascade origins. Parsed sheets default
// to author origin; animations, transitions and layers require browser routing.
type Origin uint8

const (
	OriginAuthor Origin = iota
	OriginUser
	OriginUserAgent
)

const maxCSSCascadeDeclarations = 65536

type cssCandidate struct {
	declaration Declaration
	origin      Origin
	inline      bool
	specificity cascadia.Specificity
	order       int
}

func cssOriginLevel(origin Origin) int {
	switch origin {
	case OriginAuthor:
		return 2
	case OriginUser:
		return 1
	default:
		return 0
	}
}

func (c cssCandidate) rank() int {
	level := cssOriginLevel(c.origin)
	if c.declaration.Important {
		return 5 - level
	}
	return level
}

func cssCandidateHigher(left, right cssCandidate) bool {
	if left.rank() != right.rank() {
		return left.rank() > right.rank()
	}
	if left.inline != right.inline {
		return left.inline
	}
	if comparison := cmpSpec(left.specificity, right.specificity); comparison != 0 {
		return comparison > 0
	}
	return left.order > right.order
}

func collectCSSCandidates(sheets []*Stylesheet, node *html.Node, inline string) (map[string][]cssCandidate, error) {
	if len(sheets) > 128 {
		return nil, cssDiagnostic(DiagnosticBudget, 0, "cascade stylesheet count limit exceeded")
	}
	result := make(map[string][]cssCandidate)
	count := 0
	for _, sheet := range sheets {
		if err := sheet.Err(); err != nil {
			return nil, err
		}
		if sheet.Origin > OriginUserAgent {
			return nil, cssDiagnostic(DiagnosticUnsupported, 0, "unsupported CSS cascade origin")
		}
		for _, rule := range sheet.Rules {
			specificity, matched := cssMatchingSpecificity(rule, node)
			if !matched {
				continue
			}
			if err := appendCSSCandidates(result, rule.Declarations, cssCandidate{origin: sheet.Origin, specificity: specificity}, &count); err != nil {
				return nil, err
			}
		}
	}
	block := ParseDeclarations(inline)
	if err := block.Err(); err != nil {
		return nil, err
	}
	if err := appendCSSCandidates(result, block.Declarations, cssCandidate{origin: OriginAuthor, inline: true}, &count); err != nil {
		return nil, err
	}
	for _, candidates := range result {
		sort.SliceStable(candidates, func(i, j int) bool { return cssCandidateHigher(candidates[i], candidates[j]) })
	}
	return result, nil
}

func cssMatchingSpecificity(rule Rule, node *html.Node) (cascadia.Specificity, bool) {
	var specificity cascadia.Specificity
	matched := false
	for _, selector := range rule.Selectors {
		if selector.Match(node) && (!matched || cmpSpec(selector.Specificity(), specificity) > 0) {
			matched, specificity = true, selector.Specificity()
		}
	}
	return specificity, matched
}

func appendCSSCandidates(result map[string][]cssCandidate, declarations []Declaration, prototype cssCandidate, count *int) error {
	for _, source := range declarations {
		for _, declaration := range expandCSSCascadeDeclaration(source) {
			if *count >= maxCSSCascadeDeclarations {
				return cssDiagnostic(DiagnosticBudget, declaration.Offset, "cascade declaration count limit exceeded")
			}
			*count++
			candidate := prototype
			candidate.declaration, candidate.order = declaration, *count
			result[declaration.Property] = append(result[declaration.Property], candidate)
		}
	}
	return nil
}

// The caller owns these candidate slices. Rollback removes whole origins or
// the inline attachment, never resurrects an earlier declaration in that scope.
func rollbackCSSCandidates(candidates []cssCandidate, winner cssCandidate, keyword string) []cssCandidate {
	kept := candidates[:0]
	for _, candidate := range candidates {
		if keyword == "revert-layer" && winner.inline {
			if candidate.inline {
				continue
			}
		} else if cssOriginLevel(candidate.origin) >= cssOriginLevel(winner.origin) {
			continue
		}
		kept = append(kept, candidate)
	}
	return kept
}

func resolveCSSRollbacks(candidates []cssCandidate) []cssCandidate {
	for len(candidates) > 0 {
		winner := candidates[0]
		keyword := cssWideKeyword(winner.declaration.tokens)
		if keyword != "revert" && keyword != "revert-layer" {
			return candidates
		}
		candidates = rollbackCSSCandidates(candidates, winner, keyword)
	}
	return candidates
}
