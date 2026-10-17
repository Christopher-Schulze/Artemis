package css

import (
	"errors"
	"math"
	"strconv"
	"strings"

	"github.com/tdewolff/parse/v2"
	csssyntax "github.com/tdewolff/parse/v2/css"
	"golang.org/x/net/html"
)

const maxCSSAncestorDepth = 256
const maxCSSCascadeSelectorChecks = 1 << 20
const maxCSSFontPixels = 10000
const cssFontFloatEpsilon = 1.1920928955078125e-7

// ComputedStyle retains cascaded declarations and computed parent scope.
// A caller owns the returned style; its lazy cache is not shared across contexts.
type ComputedStyle struct {
	node       *html.Node
	cascade    *cssCascadeNode
	parent     *ComputedStyle
	cache      map[string]string
	fontPixels *float64
}

// Value returns actual admitted computed/CSSOM semantics or a typed failure.
// Custom property spelling and token boundaries remain case-sensitive.
func (s *ComputedStyle) Value(property string) (string, error) {
	if s == nil || s.cascade == nil {
		return "", cssDiagnostic(DiagnosticUnsupported, 0, "computed style requires an element")
	}
	if err := s.cascade.checkComputedDependencies(property); err != nil {
		return "", err
	}
	if strings.HasPrefix(property, "--") {
		return cssComputedTokenText(s.cascade.variables[property])
	}
	property = cssASCIILower(property)
	if cached, exists := s.cache[property]; exists {
		return cached, nil
	}
	if property == "margin" || property == "padding" || property == "background" {
		value, err := s.shorthandValue(property)
		if err == nil {
			s.cache[property] = value
		}
		return value, err
	}
	value, err := s.resolveComputedValue(property)
	if err == nil {
		if property == "font-size" && s.fontPixels == nil && s.parent != nil {
			s.fontPixels = s.parent.fontPixels
		}
		s.cache[property] = value
	}
	return value, err
}

func (s *ComputedStyle) resolveComputedValue(property string) (string, error) {
	resolved, err := s.cascade.resolveProperty(property)
	if err != nil {
		return "", err
	}
	tokens, valid := resolved.tokens, resolved.valid
	if !valid && len(s.cascade.candidates[property]) == 0 && (cssBoxLonghand(property) || strings.HasPrefix(property, "background-")) {
		return "", cssDiagnostic(DiagnosticUnsupported, 0, "undeclared box or background value requires user-agent style authority")
	}
	keyword := cssWideKeyword(tokens)
	if !valid && len(s.cascade.candidates[property]) == 0 && !s.admitsUserAgentDefault(property) {
		return "", cssDiagnostic(DiagnosticUnsupported, 0, "element default requires user-agent stylesheet authority")
	}
	if !valid || keyword == "unset" {
		keyword = "initial"
		if inheritedProps[property] {
			keyword = "inherit"
		}
	}
	var value string
	if keyword == "inherit" && s.parent != nil {
		value, err = s.parent.Value(property)
	} else if keyword == "initial" || keyword == "inherit" {
		value, err = initialCSSComputedValue(property)
	} else {
		value, err = s.compute(property, tokens)
		var diagnostic *Diagnostic
		if errors.As(err, &diagnostic) && diagnostic.Code == DiagnosticSyntax && resolved.hasVariables {
			if inheritedProps[property] && s.parent != nil {
				value, err = s.parent.Value(property)
			} else {
				value, err = initialCSSComputedValue(property)
			}
		}
	}
	return value, err
}

// Only neutral HTML elements can inherit/default without a supplied UA rule.
// Headings, links, controls and presentational hints must not borrow a generic
// parent's value as if their browser defaults had been evaluated.
func (s *ComputedStyle) admitsUserAgentDefault(property string) bool {
	if s.node == nil || s.node.Namespace != "" {
		return false
	}
	switch s.node.Data {
	case "html", "body", "div", "span", "p":
	case "a":
		if property == "color" {
			for _, attribute := range s.node.Attr {
				if attribute.Namespace == "" && attribute.Key == "href" {
					return false
				}
			}
		}
	default:
		return false
	}
	if property == "color" {
		for _, attribute := range s.node.Attr {
			if attribute.Namespace == "" && attribute.Key == "text" {
				return false
			}
		}
	}
	return true
}

// Cascaded returns the winning declared value before defaulting/computation.
// An inherited value is not a declaration on this element.
func (s *ComputedStyle) Cascaded(property string) (Declaration, bool) {
	if s == nil || s.cascade == nil {
		return Declaration{}, false
	}
	if !strings.HasPrefix(property, "--") {
		property = cssASCIILower(property)
	}
	candidate, exists := s.cascade.winners[property]
	return candidate.declaration, exists
}

func initialCSSComputedValue(property string) (string, error) {
	if cssBoxLonghand(property) {
		return "0px", nil
	}
	if initial := cssBackgroundDefault(property); initial != "" && property != "background-color" {
		return initial, nil
	}
	switch property {
	case "opacity":
		return "1", nil
	case "visibility":
		return "visible", nil
	case "background-color":
		return "rgba(0, 0, 0, 0)", nil
	case "font-style":
		return "normal", nil
	case "font-weight":
		return "400", nil
	}
	return "", cssDiagnostic(DiagnosticUnsupported, 0, "initial computed property requires browser or environment semantics")
}

func (s *ComputedStyle) compute(property string, tokens []cssToken) (string, error) {
	if cssBoxLonghand(property) {
		return s.boxLength(property, tokens)
	}
	if initial := cssBackgroundDefault(property); initial != "" && property != "background-color" {
		if cssTokenText(trimCSSTokens(tokens)) == initial {
			return initial, nil
		}
		return "", cssDiagnostic(DiagnosticUnsupported, 0, "background longhand requires additional computation")
	}
	switch property {
	case "color", "background-color":
		if cssIdentValue(tokens) == "currentcolor" {
			if property != "color" {
				return s.Value("color")
			}
			if s.parent != nil {
				return s.parent.Value("color")
			}
			return initialCSSComputedValue("color")
		}
		color, err := parseCSSColor(tokens)
		if err != nil {
			return "", err
		}
		return color.serialize(), nil
	case "font-size":
		return s.fontSize(tokens)
	case "opacity":
		return cssComputedOpacity(tokens)
	case "visibility":
		value := cssIdentValue(tokens)
		if value == "visible" || value == "hidden" || value == "collapse" {
			return value, nil
		}
	}
	return "", cssDiagnostic(DiagnosticUnsupported, 0, "property computation requires additional browser semantics")
}

func cssIdentValue(tokens []cssToken) string {
	tokens = trimCSSTokens(tokens)
	if len(tokens) != 1 || tokens[0].kind != csssyntax.IdentToken {
		return ""
	}
	return cssASCIILower(decodeCSSIdentifier(tokens[0].text))
}

func cssComputedOpacity(tokens []cssToken) (string, error) {
	tokens = trimCSSTokens(tokens)
	if len(tokens) != 1 || tokens[0].kind != csssyntax.NumberToken && tokens[0].kind != csssyntax.PercentageToken {
		return "", cssDiagnostic(DiagnosticUnsupported, 0, "opacity requires supported numeric computation")
	}
	value, err := strconv.ParseFloat(strings.TrimSuffix(tokens[0].text, "%"), 64)
	if err != nil || math.IsInf(value, 0) || math.IsNaN(value) {
		return "", cssDiagnostic(DiagnosticBudget, tokens[0].offset, "opacity exceeds finite numeric precision")
	}
	if tokens[0].kind == csssyntax.PercentageToken {
		value /= 100
	}
	value = float64(float32(math.Min(math.Max(value, 0), 1)))
	return strconv.FormatFloat(value, 'g', 6, 64), nil
}

func (s *ComputedStyle) fontSize(tokens []cssToken) (string, error) {
	tokens = trimCSSTokens(tokens)
	if len(tokens) != 1 {
		return "", cssDiagnostic(DiagnosticSyntax, 0, "one font size value required")
	}
	var parentSize, rootSize *float64
	unit := ""
	if tokens[0].kind == csssyntax.DimensionToken {
		unit = cssASCIILower(decodeCSSIdentifier(tokens[0].text[parse.Number([]byte(tokens[0].text)):]))
	}
	if unit == "em" || unit == "rem" || tokens[0].kind == csssyntax.PercentageToken {
		basis, err := s.fontBasis(unit)
		if err != nil {
			return "", err
		}
		parentSize, rootSize = &basis, &basis
	}
	if tokens[0].kind == csssyntax.PercentageToken && parentSize != nil {
		value, err := strconv.ParseFloat(strings.TrimSuffix(tokens[0].text, "%"), 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
			return "", cssDiagnostic(DiagnosticBudget, 0, "font percentage exceeds finite numeric precision")
		}
		return s.storeFontPixels(value / 100 * *parentSize)
	}
	value, err := cssPixelLength(tokens, parentSize, rootSize)
	if err != nil {
		return "", err
	}
	return s.storeFontPixels(value)
}

func (s *ComputedStyle) fontBasis(unit string) (float64, error) {
	owner := s.parent
	if unit == "rem" {
		owner = s
		for owner.parent != nil {
			owner = owner.parent
		}
		if owner == s {
			return 0, cssDiagnostic(DiagnosticUnsupported, 0, "root rem font size requires initial font metrics")
		}
	}
	if owner == nil {
		return 0, cssDiagnostic(DiagnosticUnsupported, 0, "relative root font size requires initial font metrics")
	}
	_, err := owner.Value("font-size")
	if err != nil {
		return 0, err
	}
	if owner.fontPixels == nil {
		return 0, cssDiagnostic(DiagnosticUnsupported, 0, "font size basis must be a computed pixel length")
	}
	return *owner.fontPixels, nil
}

func (s *ComputedStyle) storeFontPixels(value float64) (string, error) {
	value = float64(float32(value))
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "", cssDiagnostic(DiagnosticBudget, 0, "font size exceeds finite numeric precision")
	}
	if value < 0 {
		return "", cssDiagnostic(DiagnosticSyntax, 0, "nonnegative font size required")
	}
	// Match Blink's computed font-size normalization: sub-epsilon sizes are
	// zero and the platform font boundary clamps the maximum to 10,000px.
	if value < cssFontFloatEpsilon {
		value = 0
	}
	value = math.Min(value, maxCSSFontPixels)
	s.fontPixels = &value
	return strconv.FormatFloat(value, 'g', 6, 64) + "px", nil
}
