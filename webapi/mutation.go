package webapi

import (
	"fmt"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// SetAttribute sets the attribute on the element node, replacing any
// existing entry with the same name.
func SetAttribute(n *Node, name, value string) {
	if n == nil || n.raw.Type != html.ElementNode {
		return
	}
	for i := range n.raw.Attr {
		if strings.EqualFold(n.raw.Attr[i].Key, name) {
			n.raw.Attr[i].Val = value
			return
		}
	}
	n.raw.Attr = append(n.raw.Attr, html.Attribute{Key: name, Val: value})
}

// RemoveAttribute removes the named attribute if present.
func RemoveAttribute(n *Node, name string) {
	if n == nil || n.raw.Type != html.ElementNode {
		return
	}
	out := n.raw.Attr[:0]
	for _, a := range n.raw.Attr {
		if !strings.EqualFold(a.Key, name) {
			out = append(out, a)
		}
	}
	n.raw.Attr = out
}

// HasAttribute reports whether the named attribute exists.
func HasAttribute(n *Node, name string) bool {
	if n == nil {
		return false
	}
	_, ok := n.Attr(name)
	return ok
}

// AppendChild appends child to parent, detaching it from any previous
// parent first.
func AppendChild(parent, child *Node) error {
	if parent == nil || child == nil {
		return fmt.Errorf("nil node")
	}
	if child.raw.Parent != nil {
		child.raw.Parent.RemoveChild(child.raw)
	}
	parent.raw.AppendChild(child.raw)
	return nil
}

// RemoveChild detaches child from parent. The detached node remains
// usable but is no longer part of the document tree.
func RemoveChild(parent, child *Node) error {
	if parent == nil || child == nil {
		return fmt.Errorf("nil node")
	}
	if child.raw.Parent != parent.raw {
		return fmt.Errorf("child is not a child of parent")
	}
	parent.raw.RemoveChild(child.raw)
	return nil
}

// InsertBefore inserts newChild before refChild under parent. If refChild
// is nil, newChild is appended.
func InsertBefore(parent, newChild, refChild *Node) error {
	if parent == nil || newChild == nil {
		return fmt.Errorf("nil node")
	}
	if newChild.raw.Parent != nil {
		newChild.raw.Parent.RemoveChild(newChild.raw)
	}
	if refChild == nil {
		parent.raw.AppendChild(newChild.raw)
		return nil
	}
	if refChild.raw.Parent != parent.raw {
		return fmt.Errorf("ref is not a child of parent")
	}
	parent.raw.InsertBefore(newChild.raw, refChild.raw)
	return nil
}

// CloneNode returns a copy of n. When deep is true, descendants are
// cloned too. The clone has no parent.
func CloneNode(n *Node, deep bool) *Node {
	if n == nil {
		return nil
	}
	return &Node{raw: cloneRaw(n.raw, deep)}
}

func cloneRaw(n *html.Node, deep bool) *html.Node {
	if n == nil {
		return nil
	}
	c := &html.Node{
		Type:      n.Type,
		DataAtom:  n.DataAtom,
		Data:      n.Data,
		Namespace: n.Namespace,
	}
	c.Attr = append(c.Attr, n.Attr...)
	if deep {
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			c.AppendChild(cloneRaw(ch, true))
		}
	}
	return c
}

// SetInnerHTML replaces the children of n with nodes parsed from src.
// The parsing context is an element of the same tag as n (whatwg spec).
func SetInnerHTML(n *Node, src string) error {
	if n == nil || n.raw.Type != html.ElementNode {
		return fmt.Errorf("setInnerHTML: target is not an element")
	}
	ctx := &html.Node{
		Type:     html.ElementNode,
		Data:     n.raw.Data,
		DataAtom: n.raw.DataAtom,
	}
	parsed, err := html.ParseFragment(strings.NewReader(src), ctx)
	if err != nil {
		return fmt.Errorf("parse fragment: %w", err)
	}
	for c := n.raw.FirstChild; c != nil; {
		next := c.NextSibling
		n.raw.RemoveChild(c)
		c = next
	}
	for _, p := range parsed {
		// ParseFragment returns nodes whose Parent is set to ctx; detach.
		p.Parent = nil
		p.PrevSibling = nil
		p.NextSibling = nil
		n.raw.AppendChild(p)
	}
	return nil
}

// SetTextContent replaces the children of n with a single text node.
func SetTextContent(n *Node, text string) {
	if n == nil {
		return
	}
	for c := n.raw.FirstChild; c != nil; {
		next := c.NextSibling
		n.raw.RemoveChild(c)
		c = next
	}
	if text == "" {
		return
	}
	n.raw.AppendChild(&html.Node{Type: html.TextNode, Data: text})
}

// CreateElement creates a new detached element node with the given tag.
func CreateElement(tag string) *Node {
	lower := strings.ToLower(tag)
	return &Node{raw: &html.Node{
		Type:     html.ElementNode,
		Data:     lower,
		DataAtom: atom.Lookup([]byte(lower)),
	}}
}

// CreateTextNode creates a new detached text node with the given content.
func CreateTextNode(text string) *Node {
	return &Node{raw: &html.Node{
		Type: html.TextNode,
		Data: text,
	}}
}

// GetElementById walks the document subtree under n and returns the
// first element whose id attribute equals id, or nil.
func GetElementById(n *Node, id string) *Node {
	if n == nil {
		return nil
	}
	var found *Node
	walkRaw(n.raw, func(c *Node) WalkAction {
		if c.raw.Type != html.ElementNode {
			return WalkContinue
		}
		if v, ok := c.Attr("id"); ok && v == id {
			found = c
			return WalkStop
		}
		return WalkContinue
	})
	return found
}

// GetElementsByTagName returns all element descendants of n whose tag
// equals tag (case-insensitive). The pseudo-tag "*" matches all.
func GetElementsByTagName(n *Node, tag string) []*Node {
	if n == nil {
		return nil
	}
	var out []*Node
	all := tag == "*"
	want := strings.ToLower(tag)
	walkRaw(n.raw, func(c *Node) WalkAction {
		if c.raw.Type == html.ElementNode {
			if all || c.raw.Data == want {
				out = append(out, c)
			}
		}
		return WalkContinue
	})
	return out
}

// GetElementsByClassName returns all element descendants of n whose
// class attribute contains class (whitespace-separated, case-sensitive).
func GetElementsByClassName(n *Node, class string) []*Node {
	if n == nil || class == "" {
		return nil
	}
	var out []*Node
	walkRaw(n.raw, func(c *Node) WalkAction {
		if c.raw.Type != html.ElementNode {
			return WalkContinue
		}
		v, ok := c.Attr("class")
		if !ok {
			return WalkContinue
		}
		for _, cls := range strings.Fields(v) {
			if cls == class {
				out = append(out, c)
				return WalkContinue
			}
		}
		return WalkContinue
	})
	return out
}
