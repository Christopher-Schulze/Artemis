package css

import (
	"errors"
	"strings"
	"unicode/utf8"

	csssyntax "github.com/tdewolff/parse/v2/css"
)

// Declaration retains source order and priority independently of map order.
type Declaration struct {
	Property  string
	Value     string
	Important bool
	Offset    int
	tokens    []cssToken
	// Shorthand identifies the source shorthand of a pending longhand value.
	Shorthand string
}

// DeclarationBlock retains diagnostics beside the complete admitted declarations.
type DeclarationBlock struct {
	Declarations []Declaration
	Diagnostics  []Diagnostic
}

// Err rejects using a partial declaration block as complete CSS evidence.
func (b DeclarationBlock) Err() error { return cssDiagnosticsError(b.Diagnostics) }

// ParseDeclarations parses one inline or rule declaration block through the
// same bounded token owner used by stylesheets and variable substitution.
func ParseDeclarations(source string) DeclarationBlock {
	tokens, err := tokenizeCSS(source)
	if err != nil {
		return DeclarationBlock{Diagnostics: []Diagnostic{*err}}
	}
	return parseCSSDeclarations(tokens)
}

func parseCSSDeclarations(tokens []cssToken) DeclarationBlock {
	var block DeclarationBlock
	for len(tokens) > 0 {
		boundary := cssBoundary(tokens, csssyntax.SemicolonToken)
		current := trimCSSWhitespace(tokens[:boundary])
		if boundary == len(tokens) {
			tokens = nil
		} else {
			tokens = tokens[boundary+1:]
		}
		if len(trimCSSTokens(current)) == 0 {
			continue
		}
		declaration, err := parseCSSDeclaration(current)
		if err != nil {
			block.Diagnostics = appendCSSDiagnostic(block.Diagnostics, *err)
			continue
		}
		block.Declarations = append(block.Declarations, declaration)
	}
	return block
}

func parseCSSDeclaration(tokens []cssToken) (Declaration, *Diagnostic) {
	colon := cssBoundary(tokens, csssyntax.ColonToken)
	name := trimCSSTokens(tokens[:colon])
	if colon == len(tokens) || len(name) != 1 || name[0].kind != csssyntax.IdentToken && name[0].kind != csssyntax.CustomPropertyNameToken {
		return Declaration{}, cssDiagnostic(DiagnosticSyntax, tokens[0].offset, "CSS declaration name and colon required")
	}
	property := decodeCSSIdentifier(name[0].text)
	if property == "--" {
		return Declaration{}, cssDiagnostic(DiagnosticSyntax, tokens[0].offset, "custom property name required after dashes")
	}
	if !strings.HasPrefix(property, "--") {
		property = cssASCIILower(property)
	}
	value, important := cssDeclarationPriority(trimCSSWhitespace(tokens[colon+1:]))
	if diagnostic := validateCSSDeclarationValue(value); diagnostic != nil {
		return Declaration{}, diagnostic
	}
	if diagnostic := validateCSSVariableSyntax(value); diagnostic != nil {
		return Declaration{}, diagnostic
	}
	if len(trimCSSTokens(value)) == 0 && !strings.HasPrefix(property, "--") {
		return Declaration{}, cssDiagnostic(DiagnosticSyntax, tokens[0].offset, "CSS declaration value required")
	}
	text := cssTokenText(value)
	if len(text) > maxCSSValueBytes {
		return Declaration{}, cssDiagnostic(DiagnosticBudget, tokens[0].offset, "CSS declaration value byte limit exceeded")
	}
	return Declaration{Property: property, Value: text, Important: important, Offset: tokens[0].offset, tokens: value}, nil
}

func validateCSSDeclarationValue(tokens []cssToken) *Diagnostic {
	for len(tokens) > 0 {
		boundary := cssBoundary(tokens, csssyntax.DelimToken, csssyntax.SemicolonToken)
		if boundary == len(tokens) {
			break
		}
		token := tokens[boundary]
		if token.kind == csssyntax.SemicolonToken || token.text == "!" {
			return cssDiagnostic(DiagnosticSyntax, token.offset, "CSS value contains a forbidden top-level delimiter")
		}
		tokens = tokens[boundary+1:]
	}
	return nil
}

func cssDeclarationPriority(tokens []cssToken) ([]cssToken, bool) {
	last := len(tokens) - 1
	for last >= 0 && cssTrivia(tokens[last]) {
		last--
	}
	if last < 1 || tokens[last].kind != csssyntax.IdentToken || cssASCIILower(decodeCSSIdentifier(tokens[last].text)) != "important" {
		return tokens, false
	}
	prior := last - 1
	for prior >= 0 && cssTrivia(tokens[prior]) {
		prior--
	}
	if prior < 0 || tokens[prior].kind != csssyntax.DelimToken || tokens[prior].text != "!" {
		return tokens, false
	}
	return trimCSSWhitespace(tokens[:prior]), true
}

func cssDeclarationMaps(declarations []Declaration) (map[string]string, map[string]bool) {
	values, priorities := make(map[string]string), make(map[string]bool)
	for _, declaration := range declarations {
		if priorities[declaration.Property] && !declaration.Important {
			continue
		}
		values[declaration.Property] = declaration.Value
		if declaration.Important {
			priorities[declaration.Property] = true
		}
	}
	return values, priorities
}

func cssASCIILower(value string) string {
	first := strings.IndexAny(value, "ABCDEFGHIJKLMNOPQRSTUVWXYZ")
	if first < 0 {
		return value
	}
	data := []byte(value)
	for index := first; index < len(data); index++ {
		current := data[index]
		if current >= 'A' && current <= 'Z' {
			data[index] = current + ('a' - 'A')
		}
	}
	return string(data)
}

func decodeCSSIdentifier(value string) string {
	var result strings.Builder
	for index := 0; index < len(value); {
		if value[index] != '\\' || index+1 == len(value) {
			result.WriteByte(value[index])
			index++
			continue
		}
		index++
		code, count := cssHexEscape(value[index:])
		index += count
		if count == 0 {
			current, size := utf8.DecodeRuneInString(value[index:])
			result.WriteRune(current)
			index += size
			continue
		}
		if !utf8.ValidRune(code) || code == 0 {
			code = utf8.RuneError
		}
		result.WriteRune(code)
		if index < len(value) && strings.ContainsRune(" \t\r\n\f", rune(value[index])) {
			if value[index] == '\r' && index+1 < len(value) && value[index+1] == '\n' {
				index++
			}
			index++
		}
	}
	return result.String()
}

func cssHexEscape(value string) (rune, int) {
	code, count := rune(0), 0
	for count < len(value) && count < 6 {
		current := value[count]
		if current >= 'A' && current <= 'F' {
			current += 'a' - 'A'
		}
		digit := strings.IndexByte("0123456789abcdef", current)
		if digit < 0 {
			break
		}
		code = code*16 + rune(digit)
		count++
	}
	return code, count
}

const maxCSSDiagnostics = 64

func appendCSSDiagnostic(diagnostics []Diagnostic, diagnostic Diagnostic) []Diagnostic {
	if len(diagnostics) >= maxCSSDiagnostics {
		diagnostics[maxCSSDiagnostics-1] = Diagnostic{Code: DiagnosticBudget, Offset: diagnostic.Offset, Reason: "CSS diagnostic limit exceeded"}
		return diagnostics
	}
	return append(diagnostics, diagnostic)
}

func cssDiagnosticsError(diagnostics []Diagnostic) error {
	var result error
	for _, diagnostic := range diagnostics {
		result = errors.Join(result, diagnostic)
	}
	return result
}
