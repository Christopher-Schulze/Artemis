package css

import (
	"github.com/andybalholm/cascadia"
	"golang.org/x/net/html"
)

// inheritedProps is the set of CSS properties that inherit by default
// (per CSS 2.1 / CSS Backgrounds spec). When the element itself has no
// value, Cascade walks up the parent chain to find one.
var inheritedProps = map[string]bool{
	"color": true, "direction": true,
	"font":                true,
	"font-family":         true,
	"font-size":           true,
	"font-style":          true,
	"font-variant":        true,
	"font-weight":         true,
	"font-stretch":        true,
	"letter-spacing":      true,
	"line-height":         true,
	"list-style":          true,
	"list-style-type":     true,
	"list-style-image":    true,
	"list-style-position": true,
	"text-align":          true,
	"text-indent":         true,
	"text-transform":      true,
	"text-shadow":         true,
	"text-rendering":      true,
	"visibility":          true,
	"white-space":         true,
	"word-spacing":        true,
	"word-break":          true,
	"word-wrap":           true,
	"overflow-wrap":       true,
	"cursor":              true,
	"caption-side":        true,
	"empty-cells":         true,
	"quotes":              true,
	"tab-size":            true,
	"hyphens":             true,
	"writing-mode":        true,
}

// Cascade selects declarations and computes inherited custom properties under
// bounded ancestor/work budgets. Value computes each requested ordinary property
// separately and rejects unsupported environment or layout semantics.
func Cascade(sheets []*Stylesheet, node *html.Node, inline string) (*ComputedStyle, error) {
	if node == nil || node.Type != html.ElementNode {
		return nil, cssDiagnostic(DiagnosticSyntax, 0, "CSS cascade requires an element node")
	}
	ancestry, err := cssCascadeAncestry(sheets, node)
	if err != nil {
		return nil, err
	}
	var parent *ComputedStyle
	if err := checkCSSSelectorCase(sheets, node); err != nil {
		return nil, err
	}
	for index := len(ancestry) - 1; index >= 0; index-- {
		current := ancestry[index]
		if current.Type != html.ElementNode {
			continue
		}
		text := inline
		if index != 0 {
			text = cssNodeInlineStyle(current)
		}
		var inherited cssVariables
		if parent != nil {
			inherited = parent.cascade.variables
		}
		cascade, err := cascadeCSSNode(sheets, current, text, inherited)
		if err != nil {
			return nil, err
		}
		parent = &ComputedStyle{node: current, cascade: cascade, parent: parent, cache: make(map[string]string)}
	}
	return parent, nil
}

func cssNodeInlineStyle(node *html.Node) string {
	for _, attribute := range node.Attr {
		if attribute.Namespace == "" && attribute.Key == "style" {
			return attribute.Val
		}
	}
	return ""
}

func cssCascadeAncestry(sheets []*Stylesheet, node *html.Node) ([]*html.Node, error) {
	var ancestry []*html.Node
	for current := node; current != nil; current = current.Parent {
		if len(ancestry) >= maxCSSAncestorDepth {
			return nil, cssDiagnostic(DiagnosticBudget, 0, "CSS ancestor depth limit exceeded")
		}
		ancestry = append(ancestry, current)
	}
	checks := 0
	for _, sheet := range sheets {
		if err := sheet.Err(); err != nil {
			return nil, err
		}
		for _, rule := range sheet.Rules {
			checks += len(rule.Selectors)
			if len(ancestry) > 0 && checks > maxCSSCascadeSelectorChecks/len(ancestry) {
				return nil, cssDiagnostic(DiagnosticBudget, 0, "CSS ancestor selector work limit exceeded")
			}
		}
	}
	return ancestry, nil
}

func cmpSpec(a, b cascadia.Specificity) int {
	if a[0] != b[0] {
		if a[0] < b[0] {
			return -1
		}
		return 1
	}
	if a[1] != b[1] {
		if a[1] < b[1] {
			return -1
		}
		return 1
	}
	if a[2] != b[2] {
		if a[2] < b[2] {
			return -1
		}
		return 1
	}
	return 0
}
