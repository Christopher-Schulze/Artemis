package css

import "testing"

func TestCSSSpecifiedValuesRemainDistinctFromComputedValues(t *testing.T) {
	for _, test := range []struct{ property, source, want string }{
		{"color", "RED", "red"}, {"color", `r\65 d`, "red"}, {"color", "#f00", "rgb(255, 0, 0)"},
		{"color", "currentColor", "currentcolor"}, {"color", "var(--Mixed)", "var(--Mixed)"},
		{"--Mixed", "/* comment */ Value", "Value"},
		{"width", "12PX", "12px"}, {"width", "50%", "50%"}, {"width", "2em", "2em"}, {"width", "0", "0px"},
		{"margin-left", "-2px", "-2px"}, {"margin-left", "auto", "auto"}, {"padding-left", "2rem", "2rem"},
		{"font-size", "medium", "medium"}, {"font-style", "ITALIC", "italic"}, {"font-weight", "700", "700"},
		{"line-height", "1.5", "1.5"}, {"line-height", "120%", "120%"}, {"line-height", "normal", "normal"},
		{"opacity", "2", "2"}, {"opacity", "-1", "-1"}, {"opacity", "120%", "1.2"},
		{"display", "inline-block", "inline-block"}, {"z-index", "+001", "1"}, {"background-position-x", "0%", "0%"},
	} {
		t.Run(test.property+"/"+test.source, func(t *testing.T) {
			tokens, diagnostic := tokenizeCSS(test.source)
			if diagnostic != nil {
				t.Fatal(diagnostic)
			}
			actual, err := cssSpecifiedValue(test.property, tokens)
			if err != nil || actual != test.want {
				t.Fatalf("specified %s=%q expected=%q err=%v", test.property, actual, test.want, err)
			}
		})
	}
}

func TestCSSSpecifiedValuesRejectUnprovenGrammarAndRanges(t *testing.T) {
	for _, test := range []struct{ property, source string }{
		{"unknown-css-property", "value"}, {"width", "-2px"}, {"width", "5"}, {"width", "12garbage"},
		{"width", "calc(1px + 2px)"}, {"padding-left", "-2px"}, {"padding-left", "auto"},
		{"font-weight", "1001"}, {"font-weight", "0"}, {"z-index", "1.5"}, {"z-index", "1e2"},
		{"line-height", "-1"}, {"float", "both"}, {"color", "color(display-p3 1 0 0)"},
	} {
		tokens, diagnostic := tokenizeCSS(test.source)
		if diagnostic != nil {
			t.Fatal(diagnostic)
		}
		if actual, err := cssSpecifiedValue(test.property, tokens); actual != "" || !IsDiagnostic(err) {
			t.Fatalf("unproven specified value accepted: %s=%q err=%v", test.property, actual, err)
		}
	}
}
