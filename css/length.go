package css

import (
	"math"
	"strconv"

	"github.com/tdewolff/parse/v2"
	csssyntax "github.com/tdewolff/parse/v2/css"
)

// cssPixelLength converts only lengths with an actual absolute or supplied
// font basis. Percentages, layout/viewport/font metrics and math functions
// require a different owner; no default font or viewport is fabricated.
func cssPixelLength(tokens []cssToken, fontSize, rootFontSize *float64) (float64, error) {
	tokens = trimCSSTokens(tokens)
	if len(tokens) != 1 {
		return 0, cssDiagnostic(DiagnosticSyntax, 0, "one CSS length token required")
	}
	token := tokens[0]
	if token.kind != csssyntax.NumberToken && token.kind != csssyntax.DimensionToken {
		return 0, cssDiagnostic(DiagnosticUnsupported, token.offset, "CSS length requires layout, metric or functional computation")
	}
	boundary := parse.Number([]byte(token.text))
	value, err := strconv.ParseFloat(token.text[:boundary], 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, cssDiagnostic(DiagnosticBudget, token.offset, "CSS length exceeds finite numeric precision")
	}
	if token.kind == csssyntax.NumberToken {
		if value != 0 {
			return 0, cssDiagnostic(DiagnosticSyntax, token.offset, "nonzero CSS length requires a unit")
		}
		return 0, nil
	}
	scale, err := cssLengthScale(cssASCIILower(decodeCSSIdentifier(token.text[boundary:])), fontSize, rootFontSize)
	if err != nil {
		return 0, err
	}
	value *= scale
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, cssDiagnostic(DiagnosticBudget, token.offset, "computed CSS length exceeds finite numeric precision")
	}
	return value, nil
}

func cssLengthScale(unit string, fontSize, rootFontSize *float64) (float64, error) {
	switch unit {
	case "px":
		return 1, nil
	case "in":
		return 96, nil
	case "cm":
		return 96 / 2.54, nil
	case "mm":
		return 96 / 25.4, nil
	case "q":
		return 96 / 101.6, nil
	case "pt":
		return 96.0 / 72, nil
	case "pc":
		return 16, nil
	case "em":
		if fontSize != nil && *fontSize >= 0 && !math.IsInf(*fontSize, 0) && !math.IsNaN(*fontSize) {
			return *fontSize, nil
		}
	case "rem":
		if rootFontSize != nil && *rootFontSize >= 0 && !math.IsInf(*rootFontSize, 0) && !math.IsNaN(*rootFontSize) {
			return *rootFontSize, nil
		}
	}
	return 0, cssDiagnostic(DiagnosticUnsupported, 0, "CSS length unit requires a known font or browser metric")
}
