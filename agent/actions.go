package agent

import (
	"fmt"
	"strings"

	"github.com/Christopher-Schulze/Artemis/webapi"
)

// ClickByText returns the first <button>, <a>, or input[type=button|submit]
// whose normalized text content equals text (case-insensitive). Returns
// (nil, false) if not found. The caller dispatches the click via
// `engine.Page.Click(node)`.
func ClickByText(d *webapi.Document, text string) (*webapi.Node, bool) {
	if d == nil {
		return nil, false
	}
	target := strings.ToLower(strings.TrimSpace(text))
	if target == "" {
		return nil, false
	}
	root := d.Root()
	if root == nil {
		return nil, false
	}
	var found *webapi.Node
	webapi.Walk(root, func(n *webapi.Node) webapi.WalkAction {
		if n.Type() != webapi.NodeElement {
			return webapi.WalkContinue
		}
		switch n.Tag() {
		case "button", "a":
			candidate := strings.ToLower(strings.TrimSpace(collapseInline(n.Text())))
			if candidate == target {
				found = n
				return webapi.WalkStop
			}
		case "input":
			t := strings.ToLower(n.AttrOrEmpty("type"))
			if t != "button" && t != "submit" && t != "reset" {
				return webapi.WalkContinue
			}
			candidate := strings.ToLower(strings.TrimSpace(n.AttrOrEmpty("value")))
			if candidate == target {
				found = n
				return webapi.WalkStop
			}
		}
		return webapi.WalkContinue
	})
	return found, found != nil
}

// Type sets the value of the input matching selector. It is a value
// mutation only: input/change events are not dispatched here. For
// React-style controlled inputs that watch input events, dispatch the
// event from JS via `page.Eval(...)` after Type.
func Type(d *webapi.Document, selector, text string) error {
	if d == nil {
		return fmt.Errorf("nil document")
	}
	n, err := d.QuerySelector(selector)
	if err != nil {
		return fmt.Errorf("query %q: %w", selector, err)
	}
	if n == nil {
		return fmt.Errorf("selector %q matched no element", selector)
	}
	switch n.Tag() {
	case "input":
		webapi.SetAttribute(n, "value", text)
	case "textarea":
		webapi.SetTextContent(n, text)
	default:
		return fmt.Errorf("Type: element %q is not an input/textarea", n.Tag())
	}
	return nil
}
