package css

import (
	"strings"

	"github.com/andybalholm/cascadia"
	csssyntax "github.com/tdewolff/parse/v2/css"
)

// Rule is one CSS rule: a (possibly comma-separated) selector list plus
// the declarations parsed from the rule body.
type Rule struct {
	// SelectorRaw is the un-trimmed selector text as it appeared.
	SelectorRaw string
	// Selectors is the cascadia-compiled selector list. Specificity is
	// per individual selector; we keep them split so we can match each
	// against an element and pick the highest specificity.
	Selectors []cascadia.Sel
	// Declarations preserves complete source order for cascade expansion.
	Declarations []Declaration
	// Decls is the last winning value for each literal property name.
	Decls map[string]string
	// Important records which decls were declared `!important`.
	Important   map[string]bool
	identifiers []cssSelectorIdentifier
}

// Stylesheet is a parsed CSS document.
type Stylesheet struct {
	Rules       []Rule
	Diagnostics []Diagnostic
	Origin      Origin
}

// Err rejects using a partial stylesheet as complete CSS evidence.
func (s *Stylesheet) Err() error {
	if s == nil {
		return cssDiagnostic(DiagnosticSyntax, 0, "stylesheet required")
	}
	return cssDiagnosticsError(s.Diagnostics)
}

// ParseStylesheet retains bounded diagnostics for every rejected construct.
// Strings, comments and functional values share the declaration token owner.
func ParseStylesheet(src string) *Stylesheet {
	out := &Stylesheet{}
	tokens, err := tokenizeCSS(src)
	if err != nil {
		out.Diagnostics = []Diagnostic{*err}
		return out
	}
	for tokens = trimCSSTokens(tokens); len(tokens) > 0; tokens = trimCSSTokens(tokens) {
		if tokens[0].kind == csssyntax.CDOToken || tokens[0].kind == csssyntax.CDCToken {
			tokens = tokens[1:]
			continue
		}
		if len(out.Rules) >= maxCSSRules {
			out.Diagnostics = appendCSSDiagnostic(out.Diagnostics, *cssDiagnostic(DiagnosticBudget, tokens[0].offset, "CSS rule count limit exceeded"))
			break
		}
		if tokens[0].kind == csssyntax.AtKeywordToken {
			out.Diagnostics = appendCSSDiagnostic(out.Diagnostics, *cssDiagnostic(DiagnosticUnsupported, tokens[0].offset, "CSS at-rule requires browser semantics"))
			tokens = afterCSSAtRule(tokens)
			continue
		}
		opening := cssBoundary(tokens, csssyntax.LeftBraceToken)
		if opening == len(tokens) {
			out.Diagnostics = appendCSSDiagnostic(out.Diagnostics, *cssDiagnostic(DiagnosticSyntax, tokens[0].offset, "CSS rule block required"))
			break
		}
		body := tokens[opening+1:]
		closing := cssBoundary(body, csssyntax.RightBraceToken)
		rule, diagnostics := parseCSSRule(tokens[:opening], body[:closing])
		for _, diagnostic := range diagnostics {
			out.Diagnostics = appendCSSDiagnostic(out.Diagnostics, diagnostic)
		}
		if len(rule.Selectors) > 0 {
			out.Rules = append(out.Rules, rule)
		}
		tokens = body[closing+1:]
	}
	return out
}

func afterCSSAtRule(tokens []cssToken) []cssToken {
	boundary := cssBoundary(tokens, csssyntax.SemicolonToken, csssyntax.LeftBraceToken)
	if boundary == len(tokens) {
		return nil
	}
	if tokens[boundary].kind == csssyntax.SemicolonToken {
		return tokens[boundary+1:]
	}
	body := tokens[boundary+1:]
	closing := cssBoundary(body, csssyntax.RightBraceToken)
	return body[closing+1:]
}

func parseCSSRule(selectorTokens, body []cssToken) (Rule, []Diagnostic) {
	selectors, err := compileCSSSelectors(trimCSSTokens(selectorTokens))
	if err != nil {
		return Rule{}, []Diagnostic{*err}
	}
	declarations := parseCSSDeclarations(body)
	values, priorities := cssDeclarationMaps(declarations.Declarations)
	return Rule{SelectorRaw: strings.TrimSpace(cssTokenText(selectorTokens)), Selectors: selectors,
		Declarations: declarations.Declarations, Decls: values, Important: priorities,
		identifiers: cssSelectorIdentifiers(selectorTokens)}, declarations.Diagnostics
}

func compileCSSSelectors(tokens []cssToken) ([]cascadia.Sel, *Diagnostic) {
	if len(tokens) == 0 {
		return nil, cssDiagnostic(DiagnosticSyntax, 0, "CSS selector required")
	}
	if len(cssTokenText(tokens)) > maxCSSSelectorBytes {
		return nil, cssDiagnostic(DiagnosticBudget, tokens[0].offset, "CSS selector byte limit exceeded")
	}
	var result []cascadia.Sel
	for len(tokens) > 0 {
		if len(result) >= maxCSSSelectors {
			return nil, cssDiagnostic(DiagnosticBudget, tokens[0].offset, "CSS selector list limit exceeded")
		}
		boundary := cssBoundary(tokens, csssyntax.CommaToken)
		selector, diagnostic := cssSelectorText(tokens[:boundary])
		if diagnostic != nil {
			return nil, diagnostic
		}
		compiled, err := cascadia.Parse(selector)
		if err != nil || compiled == nil {
			return nil, cssDiagnostic(DiagnosticUnsupported, tokens[0].offset, "CSS selector is malformed or unsupported")
		}
		result = append(result, compiled)
		if boundary == len(tokens) {
			return result, nil
		}
		tokens = trimCSSTokens(tokens[boundary+1:])
		if len(tokens) == 0 {
			return nil, cssDiagnostic(DiagnosticSyntax, 0, "CSS selector required after comma")
		}
	}
	return result, nil
}
