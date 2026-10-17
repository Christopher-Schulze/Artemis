package css

import (
	"math"
	"testing"
)

func TestCSSLengthsUseAbsoluteRatiosAndActualFontBases(t *testing.T) {
	font, root := 20.0, 16.0
	for _, test := range []struct {
		source string
		want   float64
	}{
		{"0", 0}, {"-0", 0}, {"12px", 12}, {"1in", 96}, {"2.54cm", 96}, {"25.4mm", 96},
		{"101.6Q", 96}, {"72pt", 96}, {"6pc", 96}, {"1.5em", 30}, {"2rem", 32},
		{"-2px", -2}, {"1e2PX", 100}, {`12p\78`, 12},
	} {
		t.Run(test.source, func(t *testing.T) {
			tokens, diagnostic := tokenizeCSS(test.source)
			if diagnostic != nil {
				t.Fatal(diagnostic)
			}
			actual, err := cssPixelLength(tokens, &font, &root)
			if err != nil || math.Abs(actual-test.want) > 1e-9 {
				t.Fatalf("length=%g err=%v; want=%g", actual, err, test.want)
			}
		})
	}
	zero := 0.0
	tokens, diagnostic := tokenizeCSS("2em")
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}
	if actual, err := cssPixelLength(tokens, &zero, nil); err != nil || actual != 0 {
		t.Fatalf("known zero font was treated as unknown: %g %v", actual, err)
	}
}

func TestCSSLengthsNeverInventLayoutOrMergeVariableTokens(t *testing.T) {
	for _, source := range []string{"1em", "1rem", "50%", "10vw", "1ch", "1ex", "calc(1px + 2px)", "auto", "4", "1e999px", "1/**/px"} {
		tokens, diagnostic := tokenizeCSS(source)
		if diagnostic != nil {
			t.Fatal(diagnostic)
		}
		if _, err := cssPixelLength(tokens, nil, nil); !IsDiagnostic(err) {
			t.Fatalf("length acquired fabricated metric/value: %q %v", source, err)
		}
	}
	variables, err := resolveCSSVariables(nil, cssTestVariables(t, `--n: 10; --length: var(--n)px`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cssPixelLength(variables["--length"], nil, nil); !IsDiagnostic(err) {
		t.Fatal("variable number/ident seam became a valid dimension")
	}
}
