package css

import (
	"fmt"
	"strings"
	"testing"
)

func TestCSSComputedValuesUseActualParentsAndTokenSubstitution(t *testing.T) {
	sheet := ParseStylesheet(`html {font-size:16px;color:red;--base:red;--alias:var(--base)} body {font-size:2em} #x {font-size:1.5em;--base:blue;color:var(--alias);background-color:currentColor;opacity:150%;visibility:hidden} #y {font-size:inherit;color:inherit}`)
	for _, test := range []struct{ id, property, want string }{
		{"x", "font-size", "48px"}, {"y", "font-size", "48px"},
		{"x", "color", "rgb(255, 0, 0)"}, {"y", "color", "rgb(255, 0, 0)"},
		{"x", "background-color", "rgb(255, 0, 0)"}, {"x", "opacity", "1"},
		{"x", "visibility", "hidden"}, {"y", "visibility", "hidden"},
		{"x", "--base", "blue"}, {"x", "--alias", "red"},
	} {
		t.Run(test.id+"/"+test.property, func(t *testing.T) {
			node := parseAndFind(t, `<html><body><div id="x"><p id="y"></p></div></body></html>`, "#"+test.id)
			style, err := Cascade([]*Stylesheet{sheet}, node, "")
			if err != nil {
				t.Fatal(err)
			}
			for attempt := 0; attempt < 2; attempt++ {
				value, err := style.Value(test.property)
				if err != nil || value != test.want {
					t.Fatalf("computed %s=%q; want=%q error=%v", test.property, value, test.want, err)
				}
			}
		})
	}
}

func TestCSSComputedInvalidVariablesDefaultWithoutResurrectingEarlierDeclarations(t *testing.T) {
	node := parseAndFind(t, `<div id="x"></div>`, "#x")
	sheet := ParseStylesheet(`html {font-size:16px;color:red} #x {--number:10; --dimension:var(--number)px; color:blue; color:var(--missing); font-size:40px; font-size:var(--dimension)}`)
	style, err := Cascade([]*Stylesheet{sheet}, node, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ property, want string }{{"color", "rgb(255, 0, 0)"}, {"font-size", "16px"}, {"--dimension", "10/**/px"}} {
		actual, err := style.Value(test.property)
		if err != nil || actual != test.want {
			t.Fatalf("computed invalid value revived an older declaration or merged tokens: %s=%q err=%v", test.property, actual, err)
		}
	}
}

func TestCSSComputedFontBasesAndKeywords(t *testing.T) {
	node := parseAndFind(t, `<div id="x"></div>`, "#x")
	for _, test := range []struct{ value, want string }{{"150%", "30px"}, {"2rem", "32px"}, {`1\65m`, "20px"}, {"1in", "96px"}, {"unset", "20px"}} {
		style, err := Cascade([]*Stylesheet{ParseStylesheet(`html {font-size:16px} body {font-size:20px}`)}, node, "font-size:"+test.value)
		if err != nil {
			t.Fatal(err)
		}
		actual, err := style.Value("font-size")
		if err != nil || actual != test.want {
			t.Fatalf("font basis %q became %q; want %q err=%v", test.value, actual, test.want, err)
		}
	}
}

func TestCSSComputedUnknownEnvironmentAndLayoutAreExplicit(t *testing.T) {
	node := parseAndFind(t, `<div id="x"></div>`, "#x")
	for _, test := range []struct{ inline, property string }{
		{"color:initial", "color"}, {"font-size:initial", "font-size"},
		{"font-size:2em", "font-size"}, {"font-size:12vw", "font-size"},
		{"width:20px", "width"}, {"color:CanvasText", "color"},
	} {
		style, err := Cascade(nil, node, test.inline)
		if err != nil {
			t.Fatal(err)
		}
		if value, err := style.Value(test.property); value != "" || !IsDiagnostic(err) {
			t.Fatalf("unknown metric/environment fabricated %s=%q err=%v", test.property, value, err)
		}
	}
	deep := parseAndFind(t, strings.Repeat("<div>", maxCSSAncestorDepth)+`<p id="x"></p>`, "#x")
	if _, err := Cascade(nil, deep, ""); !IsDiagnostic(err) {
		t.Fatal("unbounded ancestor chain accepted")
	}
}

func TestCSSComputedOverlappingDeclarationsCannotDisappear(t *testing.T) {
	node := parseAndFind(t, `<div id="x"></div>`, "#x")
	for _, test := range []struct{ inline, property string }{
		{"color:red;all:initial", "color"},
		{"font-size:16px;font:20px serif", "FONT-SIZE"},
		{"line-height:2;font:20px serif", "line-height"},
		{"padding:2px;padding-inline-start:4px", "padding-left"},
		{"margin:2px;margin-block:4px", "margin"},
		{"color:red;transition:color 1s", "color"},
		{"opacity:1;animation-name:unproved", "opacity"},
	} {
		style, err := Cascade(nil, node, test.inline)
		if err != nil {
			t.Fatal(err)
		}
		if value, err := style.Value(test.property); value != "" || !IsDiagnostic(err) {
			t.Fatalf("overlapping declaration disappeared: %s=%q error=%v", test.property, value, err)
		}
	}
}

func TestCSSComputedUnownedElementDefaultsCannotPretendToInherit(t *testing.T) {
	for _, test := range []struct{ markup, property string }{
		{`<h1 id="x"></h1>`, "font-size"},
		{`<em id="x"></em>`, "font-style"},
		{`<strong id="x"></strong>`, "font-weight"},
		{`<a id="x" href="/">link</a>`, "color"},
		{`<input id="x">`, "color"},
		{`<body id="x" text="blue"></body>`, "color"},
	} {
		node := parseAndFind(t, test.markup, "#x")
		style, err := Cascade([]*Stylesheet{ParseStylesheet(`html{color:red;font-size:16px;font-weight:400;font-style:normal}`)}, node, "")
		if err != nil {
			t.Fatal(err)
		}
		if value, err := style.Value(test.property); value != "" || !IsDiagnostic(err) {
			t.Fatalf("unowned UA defaults fabricated %s=%q error=%v", test.property, value, err)
		}
	}
}

func TestCSSComputedSerializationPreservesIndependentTokenIdentities(t *testing.T) {
	for _, source := range []string{`--a: 10; --b: px; --out: var(--a)var(--b)`} {
		variables, err := resolveCSSVariables(nil, cssTestVariables(t, source))
		if err != nil {
			t.Fatal(err)
		}
		serialized, err := cssComputedTokenText(variables["--out"])
		if err != nil {
			t.Fatal(err)
		}
		actual, diagnostic := tokenizeCSS(serialized)
		if diagnostic != nil {
			t.Fatal(diagnostic)
		}
		var significant []cssToken
		for _, token := range actual {
			if !cssTrivia(token) {
				significant = append(significant, token)
			}
		}
		var expected []cssToken
		for _, token := range variables["--out"] {
			if !cssTrivia(token) {
				expected = append(expected, token)
			}
		}
		if len(significant) != len(expected) {
			t.Fatalf("serialization changed token count: %q", serialized)
		}
		for index, token := range expected {
			if significant[index].kind != token.kind || significant[index].text != token.text {
				t.Fatal("computed serialization changed token kind or value")
			}
		}
	}
}

func TestCSSComputedSerializationCannotMergeIndependentCDOTokens(t *testing.T) {
	var tokens []cssToken
	for _, source := range []string{"<", "!", "--x"} {
		part, diagnostic := tokenizeCSS(source)
		if diagnostic != nil || len(part) != 1 {
			t.Fatalf("lexical seam input rejected: %q %v", source, diagnostic)
		}
		tokens = append(tokens, part[0])
	}
	serialized, err := cssComputedTokenText(tokens)
	if err != nil {
		t.Fatal(err)
	}
	actual, diagnostic := tokenizeCSS(serialized)
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}
	index := 0
	for _, token := range actual {
		if cssTrivia(token) {
			continue
		}
		if index >= len(tokens) || token.kind != tokens[index].kind || token.text != tokens[index].text {
			t.Fatalf("independent tokens became CDO syntax: %q", serialized)
		}
		index++
	}
	if index != len(tokens) {
		t.Fatal("serialization lost a lexical token")
	}
}

func TestCSSComputedSerializationBoundsInsertedSeparators(t *testing.T) {
	var source strings.Builder
	source.WriteString("--a:1;--b:x;--v0:var(--a)var(--b);")
	for index := 1; index <= 14; index++ {
		fmt.Fprintf(&source, "--v%d:var(--v%d)var(--v%d);", index, index-1, index-1)
	}
	variables, err := resolveCSSVariables(nil, cssTestVariables(t, source.String()))
	if err != nil {
		t.Fatal(err)
	}
	if actual, err := cssComputedTokenText(variables["--v14"]); actual != "" || !IsDiagnostic(err) {
		t.Fatalf("separator insertion escaped the computed value byte budget: bytes=%d err=%v", len(actual), err)
	}
}

func TestCSSComputedFontNormalizationAndUnroundedParentBasis(t *testing.T) {
	for _, test := range []struct {
		input float64
		want  string
	}{
		{1000000, "10000px"}, {10000, "10000px"}, {0.0000001, "0px"},
		{cssFontFloatEpsilon, "1.19209e-07px"}, {12.3456789, "12.3457px"},
	} {
		style := &ComputedStyle{}
		actual, err := style.storeFontPixels(test.input)
		if err != nil || actual != test.want || style.fontPixels == nil {
			t.Fatalf("font normalization input=%g actual=%q expected=%q err=%v", test.input, actual, test.want, err)
		}
	}
	parent := requireCascade(t, nil, parseAndFind(t, `<div id="parent"></div>`, "#parent"), "font-size:1px")
	serialized, err := parent.storeFontPixels(96 / 2.54)
	if err != nil {
		t.Fatal(err)
	}
	parent.cache["font-size"] = serialized
	child := &ComputedStyle{parent: parent}
	basis, err := child.fontBasis("em")
	if err != nil || basis != float64(float32(96/2.54)) || basis == 37.7953 {
		t.Fatalf("relative font basis was reconstructed from rounded CSSOM text: %.16g %v", basis, err)
	}
}
