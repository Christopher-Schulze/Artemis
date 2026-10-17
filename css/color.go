package css

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	csssyntax "github.com/tdewolff/parse/v2/css"
	"golang.org/x/image/colornames"
)

// Legacy sRGB uses an explicit eight-bit representation. Context-dependent,
// modern color spaces and unimplemented functional forms require escalation.
type cssRGBA struct{ red, green, blue, alpha uint8 }

func parseCSSColor(tokens []cssToken) (cssRGBA, error) {
	tokens = trimCSSTokens(tokens)
	if len(tokens) == 1 {
		token := tokens[0]
		if token.kind == csssyntax.HashToken {
			return parseCSSHexColor(decodeCSSIdentifier(strings.TrimPrefix(token.text, "#")))
		}
		if token.kind == csssyntax.IdentToken {
			name := cssASCIILower(decodeCSSIdentifier(token.text))
			if name == "transparent" {
				return cssRGBA{}, nil
			}
			if name == "rebeccapurple" {
				return cssRGBA{102, 51, 153, 255}, nil
			}
			if color, found := colornames.Map[name]; found {
				return cssRGBA{color.R, color.G, color.B, color.A}, nil
			}
			return cssRGBA{}, cssDiagnostic(DiagnosticUnsupported, token.offset, "color keyword requires browser or element context")
		}
	}
	if len(tokens) > 2 && tokens[0].kind == csssyntax.FunctionToken && tokens[len(tokens)-1].kind == csssyntax.RightParenthesisToken {
		name := cssASCIILower(decodeCSSIdentifier(strings.TrimSuffix(tokens[0].text, "(")))
		if name == "rgb" || name == "rgba" {
			return parseCSSLegacyRGB(tokens[1 : len(tokens)-1])
		}
		return cssRGBA{}, cssDiagnostic(DiagnosticUnsupported, tokens[0].offset, "color function requires browser computation")
	}
	return cssRGBA{}, cssDiagnostic(DiagnosticSyntax, 0, "CSS color value required")
}

func parseCSSHexColor(value string) (cssRGBA, error) {
	if len(value) == 3 || len(value) == 4 {
		var expanded strings.Builder
		for _, digit := range value {
			expanded.WriteRune(digit)
			expanded.WriteRune(digit)
		}
		value = expanded.String()
	}
	if len(value) == 6 {
		value += "ff"
	}
	if len(value) != 8 {
		return cssRGBA{}, cssDiagnostic(DiagnosticSyntax, 0, "CSS hex color requires three, four, six or eight digits")
	}
	parsed, err := strconv.ParseUint(value, 16, 32)
	if err != nil {
		return cssRGBA{}, cssDiagnostic(DiagnosticSyntax, 0, "CSS hex color contains invalid digits")
	}
	return cssRGBA{uint8(parsed >> 24), uint8(parsed >> 16), uint8(parsed >> 8), uint8(parsed)}, nil
}

func parseCSSLegacyRGB(tokens []cssToken) (cssRGBA, error) {
	if cssBoundary(tokens, csssyntax.CommaToken) == len(tokens) {
		return cssRGBA{}, cssDiagnostic(DiagnosticUnsupported, 0, "space-separated RGB requires additional conformance proof")
	}
	var parts [][]cssToken
	for {
		comma := cssBoundary(tokens, csssyntax.CommaToken)
		parts = append(parts, trimCSSTokens(tokens[:comma]))
		if comma == len(tokens) {
			break
		}
		tokens = tokens[comma+1:]
		if len(parts) >= 4 {
			return cssRGBA{}, cssDiagnostic(DiagnosticSyntax, 0, "RGB component count exceeded")
		}
	}
	if len(parts) != 3 && len(parts) != 4 {
		return cssRGBA{}, cssDiagnostic(DiagnosticSyntax, 0, "RGB requires three components and optional alpha")
	}
	var components [4]uint8
	components[3] = 255
	percentage := len(parts[0]) == 1 && parts[0][0].kind == csssyntax.PercentageToken
	for index, part := range parts {
		if len(part) != 1 || index < 3 && (part[0].kind == csssyntax.PercentageToken) != percentage {
			return cssRGBA{}, cssDiagnostic(DiagnosticSyntax, 0, "legacy RGB requires uniform numeric or percentage components")
		}
		component, err := cssColorComponent(part[0], index == 3)
		if err != nil {
			return cssRGBA{}, err
		}
		components[index] = component
	}
	return cssRGBA{components[0], components[1], components[2], components[3]}, nil
}

func cssColorComponent(token cssToken, alpha bool) (uint8, error) {
	if token.kind != csssyntax.NumberToken && token.kind != csssyntax.PercentageToken {
		return 0, cssDiagnostic(DiagnosticUnsupported, token.offset, "RGB component requires supported numeric semantics")
	}
	value, err := strconv.ParseFloat(strings.TrimSuffix(token.text, "%"), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, cssDiagnostic(DiagnosticBudget, token.offset, "RGB numeric component exceeds finite precision")
	}
	limit := 255.0
	if alpha {
		limit = 1
	}
	if token.kind == csssyntax.PercentageToken {
		limit = 100
	}
	value = math.Min(math.Max(value, 0), limit)
	return uint8(math.Floor(value/limit*255 + 0.5)), nil
}

func (color cssRGBA) serialize() string {
	if color.alpha == 255 {
		return fmt.Sprintf("rgb(%d, %d, %d)", color.red, color.green, color.blue)
	}
	alpha := ""
	for percentage := 0; percentage <= 100; percentage++ {
		if (percentage*255+50)/100 == int(color.alpha) {
			alpha = strconv.FormatFloat(float64(percentage)/100, 'f', -1, 64)
			break
		}
	}
	if alpha == "" {
		rounded := (int(color.alpha)*1000 + 127) / 255
		alpha = strconv.FormatFloat(float64(rounded)/1000, 'f', -1, 64)
	}
	return fmt.Sprintf("rgba(%d, %d, %d, %s)", color.red, color.green, color.blue, alpha)
}
