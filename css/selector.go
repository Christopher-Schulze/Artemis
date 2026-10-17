package css

import (
	"strings"

	"github.com/tdewolff/parse/v2"
	csssyntax "github.com/tdewolff/parse/v2/css"
)

// Cascadia treats comments as descendant whitespace and accepts extensions
// absent from browser CSS. Token admission prevents both semantic changes.
func cssSelectorText(tokens []cssToken) (string, *Diagnostic) {
	if err := validateCSSSelectorTokens(tokens); err != nil {
		return "", err
	}
	var result strings.Builder
	comment := false
	var previous cssToken
	for index := 0; index < len(tokens); index++ {
		token := tokens[index]
		if token.kind == csssyntax.CommentToken {
			comment = true
			continue
		}
		if comment && previous.text != "" && !cssTokenBoundaryStable(previous, token) {
			return "", cssDiagnostic(DiagnosticSyntax, token.offset, "CSS selector comment cannot merge tokens")
		}
		comment = false
		if token.kind == csssyntax.ColonToken {
			next := index + 1
			for next < len(tokens) && tokens[next].kind == csssyntax.CommentToken {
				next++
			}
			if next < len(tokens) && tokens[next].kind == csssyntax.IdentToken && cssASCIILower(decodeCSSIdentifier(tokens[next].text)) == "empty" {
				// Cascadia's :empty incorrectly ignores whitespace text. Its
				// internal predicates express the actual no-element/no-text
				// contract, retaining exactly one pseudo-class of specificity.
				result.WriteString(":not(:has(*)):not(:matchesown((?s).))")
				index, previous = next, tokens[next]
				continue
			}
		}
		result.WriteString(token.text)
		previous = token
	}
	if result.Len() > maxCSSSelectorBytes {
		return "", cssDiagnostic(DiagnosticBudget, 0, "compiled selector expansion byte limit exceeded")
	}
	return strings.TrimSpace(result.String()), nil
}

func cssTokenBoundaryStable(left, right cssToken) bool {
	lexer := csssyntax.NewLexer(parse.NewInputString(left.text + right.text))
	first, firstData := lexer.Next()
	second, secondData := lexer.Next()
	return first == left.kind && string(firstData) == left.text && second == right.kind && string(secondData) == right.text
}

func validateCSSSelectorTokens(tokens []cssToken) *Diagnostic {
	attributes := 0
	for index, token := range tokens {
		if token.kind == csssyntax.LeftBracketToken {
			end := index + 1
			for end < len(tokens) && tokens[end].kind != csssyntax.RightBracketToken {
				end++
			}
			if diagnostic := validateCSSAttributeCase(tokens[index+1 : end]); diagnostic != nil {
				return diagnostic
			}
			attributes++
		} else if token.kind == csssyntax.RightBracketToken {
			attributes--
		}
		if attributes > 0 && token.kind == csssyntax.DelimToken && (token.text == "!" || token.text == "#") {
			remaining := trimCSSTokens(tokens[index+1:])
			if len(remaining) > 0 && remaining[0].kind == csssyntax.DelimToken && remaining[0].text == "=" {
				return cssDiagnostic(DiagnosticUnsupported, token.offset, "non-browser CSS attribute operator denied")
			}
		}
		if token.kind != csssyntax.ColonToken || attributes != 0 {
			continue
		}
		remaining := trimCSSTokens(tokens[index+1:])
		if len(remaining) == 0 || remaining[0].kind != csssyntax.IdentToken && remaining[0].kind != csssyntax.FunctionToken {
			return cssDiagnostic(DiagnosticUnsupported, token.offset, "CSS pseudo-element or malformed pseudo-class requires browser semantics")
		}
		name := decodeCSSIdentifier(strings.TrimSuffix(remaining[0].text, "("))
		switch cssASCIILower(name) {
		case "has":
			return cssDiagnostic(DiagnosticUnsupported, token.offset, "relational selector requires bounded anchor-aware browser matching")
		case "root", "empty", "first-child", "last-child", "only-child", "first-of-type", "last-of-type", "only-of-type",
			"not", "is", "where", "nth-child", "nth-last-child", "nth-of-type", "nth-last-of-type":
		default:
			return cssDiagnostic(DiagnosticUnsupported, token.offset, "CSS pseudo-class lacks admitted renderless semantics")
		}
	}
	return nil
}

// Cascadia has explicit ASCII-insensitive matching but no HTML enumerated
// attribute defaults. Require that missing semantic explicitly rather than
// interpreting e.g. input[type=TEXT] as case-sensitive.
func validateCSSAttributeCase(tokens []cssToken) *Diagnostic {
	var fields []cssToken
	for _, token := range tokens {
		if !cssTrivia(token) {
			fields = append(fields, token)
		}
	}
	if len(fields) <= 1 {
		return nil
	}
	if len(fields) >= 4 && fields[len(fields)-1].kind == csssyntax.IdentToken && cssASCIILower(decodeCSSIdentifier(fields[len(fields)-1].text)) == "i" {
		return nil
	}
	name := cssASCIILower(decodeCSSIdentifier(fields[0].text))
	if strings.HasPrefix(name, "data-") || strings.HasPrefix(name, "aria-") {
		return nil
	}
	switch name {
	case "id", "class", "href", "src", "title", "name", "value", "for", "style":
		return nil
	}
	return cssDiagnostic(DiagnosticUnsupported, fields[0].offset, "attribute value matching requires HTML case-default semantics or an explicit i flag")
}
