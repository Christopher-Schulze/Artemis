package css

import (
	"errors"
	"strings"
	"testing"
)

func TestCSSDeclarationsPreserveOrderPriorityCaseAndQuotedValues(t *testing.T) {
	block := ParseDeclarations(`--Mixed: 1; --mixed: 2; content: "a;{}: !important"; COLOR: red !/**/\69mportant; color: blue; --empty: ;`)
	if err := block.Err(); err != nil || len(block.Declarations) != 6 {
		t.Fatalf("complete declarations rejected: %+v %v", block.Diagnostics, err)
	}
	values, priorities := cssDeclarationMaps(block.Declarations)
	if values["--Mixed"] != "1" || values["--mixed"] != "2" || values["color"] != "red" || !priorities["color"] || priorities["content"] || values["content"] != `"a;{}: !important"` {
		t.Fatalf("actual declaration semantics changed: %v %v", values, priorities)
	}
	if value, exists := values["--empty"]; !exists || value != "" {
		t.Fatal("valid empty custom property became absent")
	}
	if block.Declarations[3].Property != "color" || !block.Declarations[3].Important || block.Declarations[4].Value != "blue" || block.Declarations[3].Offset >= block.Declarations[4].Offset {
		t.Fatal("source order or per-declaration priority lost")
	}
	if value := ParseDeclarations(`\63 olor: r\65 d; --\46 oo: value`); value.Err() != nil || value.Declarations[0].Property != "color" || value.Declarations[1].Property != "--Foo" {
		t.Fatalf("CSS escaped identifier changed its actual name: %+v", value)
	}
}

func TestCSSDeclarationDiagnosticsRejectMalformedAndBoundTheirOutput(t *testing.T) {
	for _, source := range []string{"color", ": red", "--: value", "color:", "color: /**/", "color: !important", "div { color: red }", "@import url(example.css)"} {
		block := ParseDeclarations(source)
		var diagnostic Diagnostic
		if !errors.As(block.Err(), &diagnostic) || len(block.Diagnostics) == 0 {
			t.Fatalf("malformed block acquired complete parsing proof: %q %+v", source, block)
		}
	}
	block := ParseDeclarations(strings.Repeat("broken;", maxCSSDiagnostics+2))
	if len(block.Diagnostics) != maxCSSDiagnostics || block.Diagnostics[len(block.Diagnostics)-1].Code != DiagnosticBudget {
		t.Fatal("diagnostic saturation lost its bounded failure")
	}
}

func TestCSSCustomDeclarationsRetainAuthorCommentsAndTokenSpelling(t *testing.T) {
	block := ParseDeclarations(`--y: /* baz */; --x: /* foo */ var(--y) /* bar */; --uuid: 12345678-12e3-8d9b-a456-426614174000; color: red !important /* priority comment */; --z: /* value comment */ !important;`)
	if err := block.Err(); err != nil {
		t.Fatal(err)
	}
	values, priorities := cssDeclarationMaps(block.Declarations)
	if values["--y"] != "/* baz */" || values["--x"] != "/* foo */ var(--y) /* bar */" || values["--uuid"] != "12345678-12e3-8d9b-a456-426614174000" || values["--z"] != "/* value comment */" || !priorities["--z"] || !priorities["color"] || values["color"] != "red" {
		t.Fatalf("CSS author data changed during declaration admission: %v %v", values, priorities)
	}
}

func TestCSSVariableSyntaxRejectsInvalidUnusedFallbacks(t *testing.T) {
	for _, source := range []string{
		`--base: red; --x: var(--base, var(color, blue))`,
		`color: var(color, red)`, `color: var()`, `color: var(--, red)`,
		`color: var(--a --b, red)`, `--x: var(--a, var(--b, var()))`,
		`--base: red; --x: var(--base, !)`, `--x: var(--missing, red; blue)`,
		`--x: !`, `--x: red !wrong`, `color: red !wrong`,
	} {
		block := ParseDeclarations(source)
		var diagnostic Diagnostic
		if !errors.As(block.Err(), &diagnostic) || diagnostic.Code != DiagnosticSyntax {
			t.Fatalf("invalid variable syntax acquired declaration proof: %q %v", source, block.Err())
		}
	}
	if block := ParseDeclarations(`--x: "var(color)"; --y: var(--missing,); --z: var(--missing, red, blue)`); block.Err() != nil || len(block.Declarations) != 3 {
		t.Fatalf("quoted text, empty or comma-containing fallback rejected: %v", block.Err())
	}
}

func TestCSSStylesheetsExposeUnsupportedWithoutPartialSelectorLists(t *testing.T) {
	for _, source := range []string{
		`@media (min-width: 1px) { .a { color: red; } } body { color: blue; }`,
		`a, :not-a-browser-pseudo { color: red; }`, `a:contains(secret) { color: red; }`,
		`a:hover { color: red; }`, `a::before { color: red; }`, `a, { color: red; }`,
		`a[data-name!=value] { color: red; }`, `a[data-name#=pattern] { color: red; }`,
		`input[type="TEXT"] { color:red }`, `p[lang=en] { color:red }`,
	} {
		sheet := ParseStylesheet(source)
		if sheet.Err() == nil || len(sheet.Diagnostics) == 0 {
			t.Fatalf("unsupported CSS acquired a complete stylesheet claim: %q", source)
		}
		if strings.HasPrefix(source, "a,") && len(sheet.Rules) != 0 {
			t.Fatal("invalid unforgiving selector list applied its valid prefix")
		}
	}
}

func TestCSSAttributeCaseAdmissionRetainsExplicitAndExactSemantics(t *testing.T) {
	node := parseAndFind(t, `<input id="x" type="text" data-name="Value">`, "#x")
	for _, test := range []struct {
		selector string
		matches  bool
	}{
		{`input[type="TEXT" i]`, true}, {`input[TYPE]`, true},
		{`[data-name="Value"]`, true}, {`[data-name="value"]`, false},
		{`[id="X"]`, false}, {`[type=i]`, false},
	} {
		sheet := ParseStylesheet(test.selector + `{color:red}`)
		if test.selector == `[type=i]` {
			if !IsDiagnostic(sheet.Err()) {
				t.Fatal("attribute value i was mistaken for a case flag")
			}
			continue
		}
		if sheet.Err() != nil || len(sheet.Rules) != 1 || sheet.Rules[0].Selectors[0].Match(node) != test.matches {
			t.Fatalf("attribute admission changed real matching: %s err=%v", test.selector, sheet.Err())
		}
	}
}

func TestCSSSelectorCommentsCannotInventDescendantOrMergedNames(t *testing.T) {
	sheet := ParseStylesheet(`.a/**/.b { color: red; }`)
	if sheet.Err() != nil || len(sheet.Rules) != 1 {
		t.Fatalf("actual compound selector rejected: %v", sheet.Err())
	}
	for _, test := range []struct {
		document string
		match    bool
	}{
		{`<div id="x" class="a b"></div>`, true},
		{`<div class="a"><span id="x" class="b"></span></div>`, false},
	} {
		node := parseAndFind(t, test.document, "#x")
		if node == nil || sheet.Rules[0].Selectors[0].Match(node) != test.match {
			t.Fatalf("comment changed actual DOM matching: %s", test.document)
		}
	}
	for _, source := range []string{`di/**/v { color: red; }`, `:not/**/(.a) { color: red; }`} {
		if sheet := ParseStylesheet(source); sheet.Err() == nil || len(sheet.Rules) != 0 {
			t.Fatal("comment merged independent CSS tokens into a different selector")
		}
	}
	selectors := strings.Repeat("a,", maxCSSSelectors) + "a{color:red}"
	if sheet := ParseStylesheet(selectors); sheet.Err() == nil || len(sheet.Rules) != 0 || sheet.Diagnostics[0].Code != DiagnosticBudget {
		t.Fatal("selector list budget did not fail before selector compilation")
	}
	quoted := ParseStylesheet(`[data-text="#="] {color: red}`)
	if quoted.Err() != nil || len(quoted.Rules) != 1 {
		t.Fatalf("literal attribute data became a selector operator: %v", quoted.Err())
	}
}

func TestCSSStylesheetRuleSelectorAndValueBounds(t *testing.T) {
	if sheet := ParseStylesheet(strings.Repeat("a{}", maxCSSRules)); sheet.Err() != nil || len(sheet.Rules) != maxCSSRules {
		t.Fatalf("exact CSS rule bound rejected: %v", sheet.Err())
	}
	if sheet := ParseStylesheet(strings.Repeat("a{}", maxCSSRules+1)); sheet.Err() == nil || len(sheet.Rules) != maxCSSRules || sheet.Diagnostics[0].Code != DiagnosticBudget {
		t.Fatal("excess CSS rules acquired complete parsing proof")
	}
	selector := "." + strings.Repeat("x", maxCSSSelectorBytes-1)
	if sheet := ParseStylesheet(selector + "{}"); sheet.Err() != nil || len(sheet.Rules) != 1 {
		t.Fatalf("exact CSS selector bound rejected: %v", sheet.Err())
	}
	if sheet := ParseStylesheet(selector + "x{}"); sheet.Err() == nil || sheet.Diagnostics[0].Code != DiagnosticBudget {
		t.Fatal("excess selector bytes bypassed actual selector admission")
	}
	value := strings.Repeat(`"x" `, maxCSSValueBytes/4+1)
	if block := ParseDeclarations("--value:" + value); block.Err() == nil || block.Diagnostics[0].Code != DiagnosticBudget || len(block.Declarations) != 0 {
		t.Fatal("aggregate declaration value bypassed its bound through small tokens")
	}
}
