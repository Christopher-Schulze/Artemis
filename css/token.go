package css

import (
	"errors"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/tdewolff/parse/v2"
	csssyntax "github.com/tdewolff/parse/v2/css"
)

type cssToken struct {
	kind   csssyntax.TokenType
	text   string
	offset int
}

// tokenizeCSS owns bounded token data independently of the lexer's input.
func tokenizeCSS(source string) ([]cssToken, *Diagnostic) {
	if len(source) > MaxStylesheetBytes {
		return nil, cssDiagnostic(DiagnosticBudget, 0, "stylesheet byte limit exceeded")
	}
	if !utf8.ValidString(source) || strings.ContainsRune(source, 0) {
		return nil, cssDiagnostic(DiagnosticSyntax, 0, "NUL-free UTF-8 CSS required")
	}
	input := parse.NewInputString(source)
	lexer := csssyntax.NewLexer(input)
	tokens := make([]cssToken, 0, min(len(source)/4, 1024))
	var brackets []csssyntax.TokenType
	for {
		offset := input.Offset()
		kind, data := lexer.Next()
		if kind == csssyntax.ErrorToken {
			if !errors.Is(lexer.Err(), io.EOF) || len(brackets) != 0 {
				return nil, cssDiagnostic(DiagnosticSyntax, offset, "unterminated CSS token or block")
			}
			return tokens, nil
		}
		if len(tokens) >= maxCSSTokens || len(data) > maxCSSValueBytes {
			return nil, cssDiagnostic(DiagnosticBudget, offset, "CSS token count or byte limit exceeded")
		}
		token := cssToken{kind: kind, text: string(data), offset: offset}
		if err := validateCSSToken(token, &brackets); err != nil {
			return nil, err
		}
		tokens = append(tokens, token)
	}
}

func validateCSSToken(token cssToken, brackets *[]csssyntax.TokenType) *Diagnostic {
	switch token.kind {
	case csssyntax.BadStringToken, csssyntax.BadURLToken:
		return cssDiagnostic(DiagnosticSyntax, token.offset, "malformed CSS string or URL")
	case csssyntax.CommentToken:
		if len(token.text) < 4 || !strings.HasSuffix(token.text, "*/") {
			return cssDiagnostic(DiagnosticSyntax, token.offset, "unterminated CSS comment")
		}
	case csssyntax.StringToken:
		if !cssClosedString(token.text) {
			return cssDiagnostic(DiagnosticSyntax, token.offset, "unterminated CSS string")
		}
	case csssyntax.URLToken:
		if !cssClosedURL(token.text) {
			return cssDiagnostic(DiagnosticSyntax, token.offset, "unterminated CSS URL")
		}
	case csssyntax.FunctionToken, csssyntax.LeftParenthesisToken, csssyntax.LeftBracketToken, csssyntax.LeftBraceToken:
		if len(*brackets) >= maxCSSNesting {
			return cssDiagnostic(DiagnosticBudget, token.offset, "CSS nesting limit exceeded")
		}
		*brackets = append(*brackets, cssClosingToken(token.kind))
	case csssyntax.RightParenthesisToken, csssyntax.RightBracketToken, csssyntax.RightBraceToken:
		if len(*brackets) == 0 || (*brackets)[len(*brackets)-1] != token.kind {
			return cssDiagnostic(DiagnosticSyntax, token.offset, "unmatched CSS closing delimiter")
		}
		*brackets = (*brackets)[:len(*brackets)-1]
	}
	return nil
}

func cssEscapedLastByte(value string) bool {
	backslashes := 0
	for index := len(value) - 2; index >= 0 && value[index] == '\\'; index-- {
		backslashes++
	}
	return backslashes%2 != 0
}

func cssClosedString(value string) bool {
	return len(value) >= 2 && value[0] == value[len(value)-1] && !cssEscapedLastByte(value)
}

func cssClosedURL(value string) bool {
	opening := strings.IndexByte(value, '(')
	if opening < 0 || !strings.HasSuffix(value, ")") || cssEscapedLastByte(value) {
		return false
	}
	body := strings.Trim(value[opening+1:len(value)-1], " \t\r\n\f")
	if len(body) > 0 && (body[0] == '\'' || body[0] == '"') {
		return cssClosedString(body)
	}
	return true
}

func cssClosingToken(kind csssyntax.TokenType) csssyntax.TokenType {
	switch kind {
	case csssyntax.LeftBraceToken:
		return csssyntax.RightBraceToken
	case csssyntax.LeftBracketToken:
		return csssyntax.RightBracketToken
	default:
		return csssyntax.RightParenthesisToken
	}
}

func cssTrivia(token cssToken) bool {
	return token.kind == csssyntax.WhitespaceToken || token.kind == csssyntax.CommentToken
}

func trimCSSTokens(tokens []cssToken) []cssToken {
	for len(tokens) > 0 && cssTrivia(tokens[0]) {
		tokens = tokens[1:]
	}
	for len(tokens) > 0 && cssTrivia(tokens[len(tokens)-1]) {
		tokens = tokens[:len(tokens)-1]
	}
	return tokens
}

func trimCSSWhitespace(tokens []cssToken) []cssToken {
	for len(tokens) > 0 && tokens[0].kind == csssyntax.WhitespaceToken {
		tokens = tokens[1:]
	}
	for len(tokens) > 0 && tokens[len(tokens)-1].kind == csssyntax.WhitespaceToken {
		tokens = tokens[:len(tokens)-1]
	}
	return tokens
}

// cssBoundary finds a top-level delimiter in an already balanced token stream.
func cssBoundary(tokens []cssToken, delimiters ...csssyntax.TokenType) int {
	depth := 0
	for index, token := range tokens {
		if depth == 0 {
			for _, delimiter := range delimiters {
				if token.kind == delimiter {
					return index
				}
			}
		}
		switch token.kind {
		case csssyntax.FunctionToken, csssyntax.LeftParenthesisToken, csssyntax.LeftBracketToken, csssyntax.LeftBraceToken:
			depth++
		case csssyntax.RightParenthesisToken, csssyntax.RightBracketToken, csssyntax.RightBraceToken:
			depth--
		}
	}
	return len(tokens)
}

func cssTokenText(tokens []cssToken) string {
	var result strings.Builder
	for _, token := range tokens {
		result.WriteString(token.text)
	}
	return result.String()
}

// Computed substitutions may put unrelated tokens next to each other. Insert
// separators only where re-lexing would change their identity, including CDO.
func cssComputedTokenText(tokens []cssToken) (string, error) {
	var result strings.Builder
	for index, token := range tokens {
		separator := false
		if index > 0 && !cssTrivia(token) && !cssTrivia(tokens[index-1]) {
			cdo := index >= 2 && tokens[index-2].text == "<" && tokens[index-1].text == "!" && strings.HasPrefix(token.text, "--")
			separator = cdo || !cssTokenBoundaryStable(tokens[index-1], token)
		}
		additional := len(token.text)
		if separator {
			additional += 4
		}
		if result.Len()+additional > maxCSSValueBytes {
			return "", cssDiagnostic(DiagnosticBudget, token.offset, "computed CSS serialization byte limit exceeded")
		}
		if separator {
			result.WriteString("/**/")
		}
		result.WriteString(token.text)
	}
	return result.String(), nil
}
