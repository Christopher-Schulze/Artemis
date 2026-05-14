package webapi

import (
	"fmt"

	"github.com/andybalholm/cascadia"
)

// QuerySelector returns the first descendant of n that matches the given
// CSS selector, or nil if no match is found.
func (n *Node) QuerySelector(selector string) (*Node, error) {
	sel, err := cascadia.Compile(selector)
	if err != nil {
		return nil, fmt.Errorf("parse selector %q: %w", selector, err)
	}
	if m := cascadia.Query(n.raw, sel); m != nil {
		return &Node{raw: m}, nil
	}
	return nil, nil
}

// QuerySelectorAll returns all descendants of n that match the given CSS
// selector, in document order.
func (n *Node) QuerySelectorAll(selector string) ([]*Node, error) {
	sel, err := cascadia.Compile(selector)
	if err != nil {
		return nil, fmt.Errorf("parse selector %q: %w", selector, err)
	}
	matches := cascadia.QueryAll(n.raw, sel)
	out := make([]*Node, len(matches))
	for i, m := range matches {
		out[i] = &Node{raw: m}
	}
	return out, nil
}

// QuerySelector on Document is a convenience wrapping the root node.
func (d *Document) QuerySelector(selector string) (*Node, error) {
	return (&Node{raw: d.root}).QuerySelector(selector)
}

// QuerySelectorAll on Document is a convenience wrapping the root node.
func (d *Document) QuerySelectorAll(selector string) ([]*Node, error) {
	return (&Node{raw: d.root}).QuerySelectorAll(selector)
}
