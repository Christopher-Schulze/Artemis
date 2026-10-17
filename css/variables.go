package css

import (
	"sort"
	"strings"

	csssyntax "github.com/tdewolff/parse/v2/css"
)

const maxCSSVariables = 1024
const maxCSSVariableDepth = 128
const maxCSSVariableBytes = 1 << 20

// Absence is guaranteed-invalid; a present empty token slice is a valid value.
type cssVariables map[string][]cssToken

// Origin rollback must be handled by the cascade, never serialized as a value.
type cssVariableRollback struct {
	name    string
	keyword string
}

func (e *cssVariableRollback) Error() string { return "CSS variable requires cascade origin rollback" }

type cssVariableResolver struct {
	parent   cssVariables
	declared cssVariables
	computed cssVariables
	state    map[string]uint8
	cyclic   map[string]bool
	stack    []string
	bytes    int
}

func resolveCSSVariables(parent, declared cssVariables) (cssVariables, error) {
	count := len(parent)
	for name := range declared {
		if _, inherited := parent[name]; !inherited {
			count++
		}
	}
	if count > maxCSSVariables {
		return nil, cssDiagnostic(DiagnosticBudget, 0, "CSS variable count limit exceeded")
	}
	r := cssVariableResolver{parent: parent, declared: declared, computed: make(cssVariables), state: make(map[string]uint8), cyclic: make(map[string]bool)}
	names := make([]string, 0, len(declared))
	for name := range declared {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := r.findCycles(name); err != nil {
			return nil, err
		}
	}
	r.state = make(map[string]uint8)
	for name, value := range parent {
		if _, overridden := declared[name]; !overridden {
			r.computed[name] = value
		}
	}
	for _, name := range names {
		if _, _, err := r.resolve(name, 0); err != nil {
			return nil, err
		}
	}
	return r.computed, nil
}

func (r *cssVariableResolver) findCycles(name string) error {
	if r.state[name] == 2 {
		return nil
	}
	if r.state[name] == 1 {
		for index := len(r.stack) - 1; index >= 0; index-- {
			r.cyclic[r.stack[index]] = true
			if r.stack[index] == name {
				break
			}
		}
		return nil
	}
	if len(r.stack) >= maxCSSVariableDepth {
		return cssDiagnostic(DiagnosticBudget, 0, "CSS variable dependency depth limit exceeded")
	}
	r.state[name] = 1
	r.stack = append(r.stack, name)
	for _, token := range cssVariableReferences(r.declared[name]) {
		if _, own := r.declared[token]; own {
			if err := r.findCycles(token); err != nil {
				return err
			}
		}
	}
	r.stack = r.stack[:len(r.stack)-1]
	r.state[name] = 2
	return nil
}

// Every var reference contributes an edge, including references in fallbacks.
func cssVariableReferences(tokens []cssToken) []string {
	var names []string
	for index, token := range tokens {
		if !cssVarFunction(token) {
			continue
		}
		end := cssBoundary(tokens[index+1:], csssyntax.RightParenthesisToken)
		name, _, _, valid := cssVariableArguments(tokens[index+1 : index+1+end])
		if valid {
			names = append(names, name)
		}
	}
	return names
}

// Invalid var syntax invalidates its declaration even in an unused fallback.
func validateCSSVariableSyntax(tokens []cssToken) *Diagnostic {
	for index, token := range tokens {
		if !cssVarFunction(token) {
			continue
		}
		end := cssBoundary(tokens[index+1:], csssyntax.RightParenthesisToken)
		if index+end+1 >= len(tokens) {
			return cssDiagnostic(DiagnosticSyntax, token.offset, "unterminated CSS variable function")
		}
		_, fallback, _, valid := cssVariableArguments(tokens[index+1 : index+end+1])
		if !valid {
			return cssDiagnostic(DiagnosticSyntax, token.offset, "CSS variable function requires a custom property name")
		}
		if diagnostic := validateCSSDeclarationValue(fallback); diagnostic != nil {
			return diagnostic
		}
	}
	return nil
}

func (r *cssVariableResolver) resolve(name string, depth int) ([]cssToken, bool, error) {
	if value, exists := r.computed[name]; exists {
		return value, true, nil
	}
	if r.state[name] == 2 || r.cyclic[name] {
		return nil, false, nil
	}
	if depth >= maxCSSVariableDepth {
		return nil, false, cssDiagnostic(DiagnosticBudget, 0, "CSS variable substitution depth limit exceeded")
	}
	value, own := r.declared[name]
	if !own {
		return nil, false, nil
	}
	r.state[name] = 2
	switch cssWideKeyword(value) {
	case "initial":
		return nil, false, nil
	case "inherit", "unset":
		value, valid := r.parent[name]
		if valid {
			r.computed[name] = value
		}
		return value, valid, nil
	}
	return r.compute(name, value, depth)
}

func (r *cssVariableResolver) compute(name string, value []cssToken, depth int) ([]cssToken, bool, error) {
	resolved, valid, err := substituteCSSVariables(value, func(reference string) ([]cssToken, bool, error) {
		return r.resolve(reference, depth+1)
	}, depth)
	if err != nil || !valid {
		return nil, false, err
	}
	switch cssWideKeyword(resolved) {
	case "initial":
		return nil, false, nil
	case "inherit", "unset":
		value, valid := r.parent[name]
		if valid {
			r.computed[name] = value
		}
		return value, valid, nil
	case "revert", "revert-layer":
		return nil, false, &cssVariableRollback{name: name, keyword: cssWideKeyword(resolved)}
	}
	r.bytes += len(cssTokenText(resolved))
	if r.bytes > maxCSSVariableBytes {
		return nil, false, cssDiagnostic(DiagnosticBudget, 0, "CSS variable aggregate byte limit exceeded")
	}
	r.computed[name] = resolved
	return resolved, true, nil
}

func substituteCSSVariables(tokens []cssToken, lookup func(string) ([]cssToken, bool, error), depth int) ([]cssToken, bool, error) {
	if depth >= maxCSSVariableDepth {
		return nil, false, cssDiagnostic(DiagnosticBudget, 0, "CSS variable fallback depth limit exceeded")
	}
	var result []cssToken
	bytes := 0
	for index := 0; index < len(tokens); index++ {
		current := []cssToken{tokens[index]}
		if cssVarFunction(tokens[index]) {
			end := cssBoundary(tokens[index+1:], csssyntax.RightParenthesisToken)
			if index+1+end >= len(tokens) {
				return nil, false, nil
			}
			name, fallback, hasFallback, valid := cssVariableArguments(tokens[index+1 : index+1+end])
			if !valid {
				return nil, false, nil
			}
			value, exists, err := lookup(name)
			if err != nil {
				return nil, false, err
			}
			if !exists && hasFallback {
				value, exists, err = substituteCSSVariables(fallback, lookup, depth+1)
			}
			if err != nil || !exists {
				return nil, false, err
			}
			current = value
			index += end + 1
		}
		for _, token := range current {
			bytes += len(token.text)
		}
		if len(result)+len(current) > maxCSSTokens || bytes > maxCSSValueBytes {
			return nil, false, cssDiagnostic(DiagnosticBudget, 0, "CSS variable expansion limit exceeded")
		}
		result = append(result, current...)
	}
	return trimCSSWhitespace(result), true, nil
}

func cssVarFunction(token cssToken) bool {
	return token.kind == csssyntax.FunctionToken && cssASCIILower(decodeCSSIdentifier(strings.TrimSuffix(token.text, "("))) == "var"
}

func cssVariableArguments(tokens []cssToken) (string, []cssToken, bool, bool) {
	comma := cssBoundary(tokens, csssyntax.CommaToken)
	name := trimCSSTokens(tokens[:comma])
	if len(name) != 1 || name[0].kind != csssyntax.CustomPropertyNameToken {
		return "", nil, false, false
	}
	decoded := decodeCSSIdentifier(name[0].text)
	if !strings.HasPrefix(decoded, "--") || decoded == "--" {
		return "", nil, false, false
	}
	if comma == len(tokens) {
		return decoded, nil, false, true
	}
	return decoded, tokens[comma+1:], true, true
}

func cssWideKeyword(tokens []cssToken) string {
	tokens = trimCSSTokens(tokens)
	if len(tokens) != 1 || tokens[0].kind != csssyntax.IdentToken {
		return ""
	}
	keyword := cssASCIILower(decodeCSSIdentifier(tokens[0].text))
	switch keyword {
	case "initial", "inherit", "unset", "revert", "revert-layer":
		return keyword
	default:
		return ""
	}
}
