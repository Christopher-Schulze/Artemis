package css

import (
	"math"
	"strconv"
	"strings"

	"github.com/tdewolff/parse/v2"
	csssyntax "github.com/tdewolff/parse/v2/css"
)

func cssSpecifiedValue(property string, tokens []cssToken) (string, error) {
	if strings.HasPrefix(property, "--") {
		return cssTokenText(trimCSSTokens(tokens)), nil
	}
	if !cssSpecifiedProperty(property) {
		return "", cssDiagnostic(DiagnosticUnsupported, 0, "CSSOM property lacks admitted specified-value semantics")
	}
	if keyword := cssWideKeyword(tokens); keyword != "" {
		return keyword, nil
	}
	if len(cssVariableReferences(tokens)) > 0 {
		return cssTokenText(tokens), nil
	}
	if initial := cssBackgroundDefault(property); initial != "" && property != "background-color" && cssTokenText(trimCSSTokens(tokens)) == initial {
		return initial, nil
	}
	if property == "color" || property == "background-color" {
		return cssSpecifiedColor(tokens)
	}
	if keywords := cssSpecifiedKeywords(property); keywords != "" {
		value := cssIdentValue(tokens)
		for _, keyword := range strings.Fields(keywords) {
			if value == keyword {
				return value, nil
			}
		}
	}
	if property == "opacity" || property == "font-weight" || property == "z-index" || property == "line-height" {
		trimmed := trimCSSTokens(tokens)
		if len(trimmed) == 1 && (trimmed[0].kind == csssyntax.NumberToken || property == "opacity" && trimmed[0].kind == csssyntax.PercentageToken) {
			return cssSpecifiedNumber(property, trimmed[0])
		}
	}
	if cssSpecifiedLengthProperty(property) {
		return cssSpecifiedLength(property, tokens)
	}
	return "", cssDiagnostic(DiagnosticUnsupported, 0, "CSSOM specified value requires additional grammar semantics")
}

func cssSpecifiedColor(tokens []cssToken) (string, error) {
	if cssIdentValue(tokens) == "currentcolor" {
		return "currentcolor", nil
	}
	color, err := parseCSSColor(tokens)
	if err != nil {
		return "", err
	}
	if keyword := cssIdentValue(tokens); keyword != "" {
		return keyword, nil
	}
	return color.serialize(), nil
}

func cssSpecifiedProperty(property string) bool {
	if cssBoxLonghand(property) || cssBackgroundDefault(property) != "" {
		return true
	}
	switch property {
	case "color", "font-size", "font-style", "font-weight", "line-height", "opacity", "visibility", "display",
		"width", "height", "min-width", "min-height", "max-width", "max-height", "top", "right", "bottom", "left",
		"position", "float", "clear", "overflow", "overflow-x", "overflow-y", "box-sizing", "z-index", "text-align", "text-transform", "white-space":
		return true
	}
	return false
}

func cssSpecifiedLengthProperty(property string) bool {
	if cssBoxLonghand(property) {
		return true
	}
	switch property {
	case "font-size", "line-height", "width", "height", "min-width", "min-height", "max-width", "max-height", "top", "right", "bottom", "left":
		return true
	}
	return false
}

func cssSpecifiedKeywords(property string) string {
	switch property {
	case "display":
		return "none block inline inline-block flex inline-flex grid inline-grid contents table inline-table list-item flow-root"
	case "visibility":
		return "visible hidden collapse"
	case "font-style":
		return "normal italic oblique"
	case "font-weight":
		return "normal bold bolder lighter"
	case "font-size":
		return "xx-small x-small small medium large x-large xx-large xxx-large smaller larger math"
	case "line-height":
		return "normal"
	case "text-align":
		return "start end left right center justify match-parent"
	case "text-transform":
		return "none capitalize uppercase lowercase full-width full-size-kana"
	case "white-space":
		return "normal pre nowrap pre-wrap pre-line break-spaces"
	}
	return cssSpecifiedBoxKeywords(property)
}

func cssSpecifiedBoxKeywords(property string) string {
	switch property {
	case "position":
		return "static relative absolute fixed sticky"
	case "float":
		return "none left right inline-start inline-end"
	case "clear":
		return "none left right inline-start inline-end both"
	case "overflow", "overflow-x", "overflow-y":
		return "visible hidden clip scroll auto"
	case "box-sizing":
		return "content-box border-box"
	case "z-index":
		return "auto"
	}
	if cssBoxLonghand(property) && strings.HasPrefix(property, "margin-") {
		return "auto"
	}
	switch property {
	case "width", "height", "min-width", "min-height":
		return "auto min-content max-content fit-content stretch"
	case "max-width", "max-height":
		return "none min-content max-content fit-content stretch"
	case "top", "right", "bottom", "left":
		return "auto"
	}
	if value := cssBackgroundDefault(property); value != "" && property != "background-color" {
		return value
	}
	return ""
}

func cssSpecifiedNumber(property string, token cssToken) (string, error) {
	value, err := strconv.ParseFloat(strings.TrimSuffix(token.text, "%"), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return "", cssDiagnostic(DiagnosticBudget, token.offset, "specified numeric value exceeds finite precision")
	}
	if property == "z-index" && (math.Trunc(value) != value || strings.ContainsAny(token.text, ".eE")) || property == "font-weight" && (value < 1 || value > 1000) || property == "line-height" && value < 0 {
		return "", cssDiagnostic(DiagnosticSyntax, token.offset, "specified numeric value violates property grammar")
	}
	if value == 0 {
		value = 0
	}
	result := strconv.FormatFloat(value, 'g', 6, 64)
	if token.kind == csssyntax.PercentageToken {
		if property == "opacity" {
			result = strconv.FormatFloat(value/100, 'g', 6, 64)
		} else {
			result += "%"
		}
	}
	return result, nil
}

func cssSpecifiedLength(property string, tokens []cssToken) (string, error) {
	tokens = trimCSSTokens(tokens)
	if len(tokens) != 1 {
		return "", cssDiagnostic(DiagnosticUnsupported, 0, "specified length requires supported single-value grammar")
	}
	token := tokens[0]
	if token.kind != csssyntax.NumberToken && token.kind != csssyntax.DimensionToken && token.kind != csssyntax.PercentageToken {
		return "", cssDiagnostic(DiagnosticUnsupported, token.offset, "specified length requires additional functional grammar")
	}
	boundary := parse.Number([]byte(token.text))
	value, err := strconv.ParseFloat(token.text[:boundary], 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return "", cssDiagnostic(DiagnosticBudget, token.offset, "specified length exceeds finite precision")
	}
	negativeAllowed := strings.HasPrefix(property, "margin-") || property == "top" || property == "right" || property == "bottom" || property == "left"
	if value < 0 && !negativeAllowed || token.kind == csssyntax.NumberToken && value != 0 {
		return "", cssDiagnostic(DiagnosticSyntax, token.offset, "specified length violates range or unit grammar")
	}
	unit := cssASCIILower(decodeCSSIdentifier(token.text[boundary:]))
	if token.kind == csssyntax.NumberToken {
		unit = "px"
	}
	allowed := false
	for _, candidate := range strings.Fields("% px in cm mm q pt pc em rem ex rex cap rcap ch rch ic ric lh rlh vw vh vi vb vmin vmax svw svh svi svb svmin svmax lvw lvh lvi lvb lvmin lvmax dvw dvh dvi dvb dvmin dvmax") {
		allowed = allowed || unit == candidate
	}
	if !allowed {
		return "", cssDiagnostic(DiagnosticUnsupported, token.offset, "specified CSS unit lacks admitted semantics")
	}
	if value == 0 {
		value = 0
	}
	return strconv.FormatFloat(value, 'g', 6, 64) + unit, nil
}
