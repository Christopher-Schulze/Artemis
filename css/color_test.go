package css

import (
	"errors"
	"testing"
)

func TestCSSColorComputesLegacySRGBAndAlpha(t *testing.T) {
	for _, test := range []struct{ source, want string }{
		{"red", "rgb(255, 0, 0)"}, {`r\65 d`, "rgb(255, 0, 0)"},
		{"rebeccapurple", "rgb(102, 51, 153)"}, {"transparent", "rgba(0, 0, 0, 0)"},
		{"#f0a", "rgb(255, 0, 170)"}, {"#F0A8", "rgba(255, 0, 170, 0.533)"},
		{"#010203", "rgb(1, 2, 3)"}, {"#01020380", "rgba(1, 2, 3, 0.5)"},
		{"rgb(1, 2, 3)", "rgb(1, 2, 3)"}, {"rgb(100%, 0%, 50%)", "rgb(255, 0, 128)"},
		{"rgba(1, 2, 3, 0.5)", "rgba(1, 2, 3, 0.5)"}, {"rgba(1, 2, 3, 50%)", "rgba(1, 2, 3, 0.5)"},
		{"rgb(-20, 300, 127.5)", "rgb(0, 255, 128)"}, {"rgba(1, 2, 3, 4)", "rgb(1, 2, 3)"},
		{"rgba(1, 2, 3, -1)", "rgba(1, 2, 3, 0)"}, {"RGB(1,2,3)", "rgb(1, 2, 3)"},
	} {
		t.Run(test.source, func(t *testing.T) {
			tokens, diagnostic := tokenizeCSS(test.source)
			if diagnostic != nil {
				t.Fatal(diagnostic)
			}
			value, err := parseCSSColor(tokens)
			if err != nil || value.serialize() != test.want {
				t.Fatalf("color=%q err=%v; want=%q", value.serialize(), err, test.want)
			}
		})
	}
}

func TestCSSColorCannotFabricateUnsupportedOrMalformedValues(t *testing.T) {
	for _, source := range []string{"currentColor", "CanvasText", "not-a-color", "hsl(0,100%,50%)", "color(display-p3 1 0 0)", "rgb(1 2 3)", "#ff", "#xxff00", "rgb(1%,2,3)", "rgb(1,2)", "rgba(1,2,3,4,5)", "rgb(1/**/2,3,4)", "rgb(1e999,2,3)"} {
		tokens, diagnostic := tokenizeCSS(source)
		if diagnostic != nil {
			t.Fatal(diagnostic)
		}
		_, err := parseCSSColor(tokens)
		var failure *Diagnostic
		if !errors.As(err, &failure) {
			t.Fatalf("unsupported or malformed color fabricated a result: %q %v", source, err)
		}
	}
}

func TestCSSColorAlphaSerializationRoundTripsEveryStoredByte(t *testing.T) {
	for alpha := 0; alpha <= 255; alpha++ {
		original := cssRGBA{1, 2, 3, uint8(alpha)}
		tokens, diagnostic := tokenizeCSS(original.serialize())
		if diagnostic != nil {
			t.Fatal(diagnostic)
		}
		parsed, err := parseCSSColor(tokens)
		if err != nil || parsed != original {
			t.Fatalf("alpha byte %d lost through serialization %q: %+v %v", alpha, original.serialize(), parsed, err)
		}
	}
}
