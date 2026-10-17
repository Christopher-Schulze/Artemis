package css

import (
	"strings"
	"testing"

	csssyntax "github.com/tdewolff/parse/v2/css"
)

func TestCSSTokensPreserveQuotedEscapedAndFunctionalBoundaries(t *testing.T) {
	for _, source := range []string{
		`a[data-name="x,}("] { content: "a;{}:/*literal*/"; color: rgb(1, 2, 3); }`,
		`a { background: url(data:image/svg+xml;utf8,<svg>{}</svg>); --A: var(--B, func(1;2)); }`,
		`a { content: "escaped\"quote"; } /* complete */`,
		`a { content: '\''; }`,
	} {
		t.Run(source, func(t *testing.T) {
			tokens, err := tokenizeCSS(source)
			if err != nil || cssTokenText(tokens) != source {
				t.Fatalf("actual lexer changed CSS bytes: %v", err)
			}
			for _, token := range tokens {
				if token.offset < 0 || token.offset+len(token.text) > len(source) || source[token.offset:token.offset+len(token.text)] != token.text {
					t.Fatal("token byte offset does not identify its actual source")
				}
			}
		})
	}
	tokens, err := tokenizeCSS(`content: "a;b"; color: rgb(1;2);`)
	if err != nil {
		t.Fatal(err)
	}
	first := cssBoundary(tokens, csssyntax.SemicolonToken)
	if cssTokenText(tokens[:first]) != `content: "a;b"` {
		t.Fatal("a string delimiter split the declaration")
	}
	tokens = trimCSSTokens(tokens[first+1:])
	second := cssBoundary(tokens, csssyntax.SemicolonToken)
	if cssTokenText(tokens[:second]) != `color: rgb(1;2)` {
		t.Fatal("a function delimiter split the declaration")
	}
}

func TestCSSTokensRejectMalformedEOFAndDelimiterState(t *testing.T) {
	for _, source := range []string{
		`a { color: red`, `a { color: red; ] }`, `a { color: rgb(1, 2, 3; }`,
		`/*`, `/*/`, `/* not closed`, `"not closed`, `"escaped\"`,
		`url(not-closed`, `url("not-closed)`, `url(escaped\)`,
		"a { content: 'raw\nnewline'; }", "a\x00{}", string([]byte{0xff}),
	} {
		t.Run(source, func(t *testing.T) {
			tokens, err := tokenizeCSS(source)
			if err == nil || err.Code != DiagnosticSyntax || tokens != nil {
				t.Fatalf("malformed CSS acquired a complete token stream: %v", err)
			}
			if strings.Contains(err.Error(), source) && len(source) > 8 {
				t.Fatal("diagnostic projected source values")
			}
		})
	}
}

func TestCSSTokensEnforceActualCountNestingAndByteBudgets(t *testing.T) {
	exact := strings.Repeat("x;", maxCSSTokens/2)
	if tokens, err := tokenizeCSS(exact); err != nil || len(tokens) != maxCSSTokens {
		t.Fatalf("exact token count rejected: %v", err)
	}
	nested := strings.Repeat("(", maxCSSNesting) + "x" + strings.Repeat(")", maxCSSNesting)
	if _, err := tokenizeCSS(nested); err != nil {
		t.Fatal(err)
	}
	comment := "/*" + strings.Repeat("x", maxCSSValueBytes-4) + "*/"
	source := strings.Repeat(comment, MaxStylesheetBytes/maxCSSValueBytes)
	if len(source) != MaxStylesheetBytes {
		t.Fatal("exact source control does not reach the actual source bound")
	}
	if _, err := tokenizeCSS(source); err != nil {
		t.Fatalf("valid exact source bound rejected: %v", err)
	}
	for _, source := range []string{
		exact + "x", "(" + nested + ")", strings.Repeat("x", maxCSSValueBytes+1), strings.Repeat(" ", MaxStylesheetBytes+1),
	} {
		tokens, err := tokenizeCSS(source)
		if err == nil || err.Code != DiagnosticBudget || tokens != nil {
			t.Fatalf("excess CSS bypassed actual quota: %v", err)
		}
	}
}
