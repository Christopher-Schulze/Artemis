package css

import (
	"math"
	"strconv"
	"strings"

	"github.com/tdewolff/parse/v2"
	csssyntax "github.com/tdewolff/parse/v2/css"
)

var cssBoxSides = [...]string{"top", "right", "bottom", "left"}
var cssBackgroundLonghands = [...]string{"background-image", "background-position-x", "background-position-y", "background-size", "background-repeat", "background-attachment", "background-origin", "background-clip", "background-color"}

func expandCSSCascadeDeclaration(declaration Declaration) []Declaration {
	properties := []string{declaration.Property}
	switch declaration.Property {
	case "margin", "padding":
		for _, side := range cssBoxSides {
			properties = append(properties, declaration.Property+"-"+side)
		}
	case "background":
		properties = append(properties, cssBackgroundLonghands[:]...)
	}
	result := make([]Declaration, 0, len(properties))
	for _, property := range properties {
		current := declaration
		current.Property = property
		if property != declaration.Property {
			current.Shorthand = declaration.Property
		}
		result = append(result, current)
	}
	return result
}

func cssBoxSideValue(tokens []cssToken, property, shorthand string) ([]cssToken, error) {
	var parts [][]cssToken
	for index := 0; index < len(tokens); {
		if cssTrivia(tokens[index]) {
			index++
			continue
		}
		start := index
		switch tokens[index].kind {
		case csssyntax.FunctionToken, csssyntax.LeftParenthesisToken, csssyntax.LeftBracketToken, csssyntax.LeftBraceToken:
			end := cssBoundary(tokens[index+1:], cssClosingToken(tokens[index].kind))
			index += end + 2
		default:
			index++
		}
		parts = append(parts, tokens[start:index])
		if len(parts) > 4 {
			return nil, cssDiagnostic(DiagnosticSyntax, 0, "box shorthand has more than four components")
		}
	}
	if len(parts) == 0 {
		return nil, cssDiagnostic(DiagnosticSyntax, 0, "box shorthand requires a value")
	}
	for _, part := range parts {
		value, err := cssPixelLength(part, nil, nil)
		if err != nil {
			return nil, err
		}
		if shorthand == "padding" && value < 0 {
			return nil, cssDiagnostic(DiagnosticSyntax, 0, "padding shorthand requires nonnegative lengths")
		}
	}
	indices := [4][4]int{{0, 0, 0, 0}, {0, 1, 0, 1}, {0, 1, 2, 1}, {0, 1, 2, 3}}
	for side, name := range cssBoxSides {
		if property == shorthand+"-"+name {
			return parts[indices[len(parts)-1][side]], nil
		}
	}
	return nil, cssDiagnostic(DiagnosticUnsupported, 0, "unsupported box shorthand longhand")
}

func cssBackgroundDefault(property string) string {
	switch property {
	case "background-color":
		return "transparent"
	case "background-image":
		return "none"
	case "background-position-x", "background-position-y":
		return "0%"
	case "background-size":
		return "auto"
	case "background-repeat":
		return "repeat"
	case "background-attachment":
		return "scroll"
	case "background-origin":
		return "padding-box"
	case "background-clip":
		return "border-box"
	}
	return ""
}

func cssShorthandTokens(declaration Declaration, property string, tokens []cssToken) ([]cssToken, error) {
	if cssWideKeyword(tokens) != "" || declaration.Shorthand == "" {
		return tokens, nil
	}
	if declaration.Shorthand == "background" {
		if _, err := parseCSSColor(tokens); err != nil {
			return nil, err
		}
		if property == "background-color" {
			return tokens, nil
		}
		value, diagnostic := tokenizeCSS(cssBackgroundDefault(property))
		if diagnostic != nil {
			return nil, diagnostic
		}
		return value, nil
	}
	return cssBoxSideValue(tokens, property, declaration.Shorthand)
}

func (s *ComputedStyle) shorthandValue(property string) (string, error) {
	if property == "background" {
		values := make(map[string]string, len(cssBackgroundLonghands))
		for _, name := range cssBackgroundLonghands {
			value, err := s.Value(name)
			if err != nil {
				return "", err
			}
			values[name] = value
		}
		return values["background-color"] + " " + values["background-image"] + " " + values["background-repeat"] + " " + values["background-attachment"] + " " + values["background-position-x"] + " " + values["background-position-y"] + " / " + values["background-size"] + " " + values["background-origin"] + " " + values["background-clip"], nil
	}
	var values [4]string
	for index, side := range cssBoxSides {
		value, err := s.Value(property + "-" + side)
		if err != nil {
			return "", err
		}
		values[index] = value
	}
	return cssBoxShorthandValue(values), nil
}

func cssBoxShorthandValue(values [4]string) string {
	count := 4
	if values[1] == values[3] {
		count = 3
		if values[0] == values[2] {
			count = 2
			if values[0] == values[1] {
				count = 1
			}
		}
	}
	return strings.Join(values[:count], " ")
}

func cssBoxLonghand(property string) bool {
	for _, prefix := range []string{"margin-", "padding-"} {
		for _, side := range cssBoxSides {
			if property == prefix+side {
				return true
			}
		}
	}
	return false
}

func (s *ComputedStyle) boxLength(property string, tokens []cssToken) (string, error) {
	var fontSize, rootSize *float64
	trimmed := trimCSSTokens(tokens)
	if len(trimmed) == 1 && trimmed[0].kind == csssyntax.DimensionToken {
		text := trimmed[0].text
		unit := cssASCIILower(decodeCSSIdentifier(text[parse.Number([]byte(text)):]))
		if unit == "em" || unit == "rem" {
			owner := s
			if unit == "rem" {
				for owner.parent != nil {
					owner = owner.parent
				}
			}
			if _, err := owner.Value("font-size"); err != nil {
				return "", err
			}
			fontSize, rootSize = owner.fontPixels, owner.fontPixels
		}
	}
	value, err := cssPixelLength(tokens, fontSize, rootSize)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(property, "padding-") && value < 0 {
		return "", cssDiagnostic(DiagnosticSyntax, 0, "padding requires a nonnegative length")
	}
	if math.Abs(value) > 1<<20 || math.Trunc(value*64) != value*64 {
		return "", cssDiagnostic(DiagnosticUnsupported, 0, "box length requires unproven layout quantization or range semantics")
	}
	if value == 0 {
		value = 0
	}
	return strconv.FormatFloat(value, 'g', 6, 64) + "px", nil
}
