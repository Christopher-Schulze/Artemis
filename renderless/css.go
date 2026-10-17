package renderless

import (
	"fmt"

	"github.com/Christopher-Schulze/Artemis/css"
	"golang.org/x/net/html"
)

// css.go (spec L4022: renderless/css.go - CSS parse/cascade/computed
// style).
//
// In-process no-render JS browser path: CSS parse/cascade/computed
// style. Parses CSS rules, applies cascade, and computes final
// styles for DOM elements.
//
// Ref: research/artemis/css/

// CSSRule represents a CSS rule
// (spec L4022: CSS parse/cascade/computed style).
type CSSRule struct {
	css.Rule
	Source string `json:"source"`
}

// CSSParser parses CSS text into rules
// (spec L4022: CSS parse/cascade/computed style).
type CSSParser struct{}

// NewCSSParser creates a new CSSParser
// (spec L4022: CSS parse/cascade/computed style).
func NewCSSParser() *CSSParser {
	return &CSSParser{}
}

// Parse parses CSS text into rules
// (spec L4022: CSS parse/cascade/computed style).
func (p *CSSParser) Parse(source string) ([]CSSRule, error) {
	sheet := css.ParseStylesheet(source)
	if err := sheet.Err(); err != nil {
		return nil, err
	}
	rules := make([]CSSRule, 0, len(sheet.Rules))
	for _, rule := range sheet.Rules {
		rules = append(rules, CSSRule{Rule: rule})
	}
	return rules, nil
}

// ComputedStyle computes the final style for an element
// (spec L4022: CSS parse/cascade/computed style).
type ComputedStyle struct {
	sheet *css.Stylesheet
}

// NewComputedStyle creates a new ComputedStyle
// (spec L4022: CSS parse/cascade/computed style).
func NewComputedStyle(rules []CSSRule) *ComputedStyle {
	sheet := &css.Stylesheet{Rules: make([]css.Rule, 0, len(rules))}
	for _, rule := range rules {
		sheet.Rules = append(sheet.Rules, rule.Rule)
	}
	return &ComputedStyle{sheet: sheet}
}

// Compute resolves one property on the actual owning DOM element.
// (spec L4022: CSS parse/cascade/computed style).
func (c *ComputedStyle) Compute(node *html.Node, inline, property string) (string, error) {
	if c == nil || c.sheet == nil {
		return "", &css.Diagnostic{Code: css.DiagnosticSyntax, Reason: "computed stylesheet required"}
	}
	style, err := css.Cascade([]*css.Stylesheet{c.sheet}, node, inline)
	if err != nil {
		return "", err
	}
	return style.Value(property)
}

// RuleCount returns the number of CSS rules
// (spec L4022: CSS parse/cascade/computed style).
func (c *ComputedStyle) RuleCount() int {
	if c == nil || c.sheet == nil {
		return 0
	}
	return len(c.sheet.Rules)
}

// String returns a diagnostic summary.
func (r CSSRule) String() string {
	return fmt.Sprintf("CSSRule{selector:%s declarations:%d}", r.SelectorRaw, len(r.Declarations))
}
