// Package webapi implements the DOM and Web API surface that Artemis
// exposes both internally and to JavaScript code running inside V8.
//
// Phase 1 covers a static DOM tree wrapping golang.org/x/net/html.Node.
// Subsequent TASKs add Event, Fetch, XHR, MutationObserver, and the
// remaining WebAPI types incrementally.
package webapi

import (
	"strings"

	"golang.org/x/net/html"
)

// NodeType identifies the kind of DOM node.
type NodeType int

const (
	NodeError NodeType = iota
	NodeText
	NodeDocument
	NodeElement
	NodeComment
	NodeDoctype
	NodeRaw
)

// String returns a human-readable name for the node type.
func (t NodeType) String() string {
	switch t {
	case NodeText:
		return "text"
	case NodeDocument:
		return "document"
	case NodeElement:
		return "element"
	case NodeComment:
		return "comment"
	case NodeDoctype:
		return "doctype"
	case NodeRaw:
		return "raw"
	default:
		return "error"
	}
}

// Node is a handle into a parsed DOM tree. It is a thin wrapper around the
// underlying golang.org/x/net/html node and holds no state of its own;
// multiple Node values may reference the same underlying node.
type Node struct {
	raw *html.Node
}

// Wrap creates a Node from a raw html.Node. Exported so sibling
// packages (scraper, bridge, etc.) can integrate with cascadia and
// other html.Node consumers.
func Wrap(n *html.Node) *Node {
	if n == nil {
		return nil
	}
	return &Node{raw: n}
}

func wrap(n *html.Node) *Node { return Wrap(n) }

// Raw returns the underlying golang.org/x/net/html node. Intended for
// integrating with libraries that operate on html.Node directly.
func (n *Node) Raw() *html.Node { return n.raw }

// Type reports the kind of node.
func (n *Node) Type() NodeType {
	switch n.raw.Type {
	case html.ErrorNode:
		return NodeError
	case html.TextNode:
		return NodeText
	case html.DocumentNode:
		return NodeDocument
	case html.ElementNode:
		return NodeElement
	case html.CommentNode:
		return NodeComment
	case html.DoctypeNode:
		return NodeDoctype
	case html.RawNode:
		return NodeRaw
	default:
		return NodeError
	}
}

// Parent returns the parent node, or nil at the document root.
func (n *Node) Parent() *Node { return wrap(n.raw.Parent) }

// FirstChild returns the first child, or nil if the node has none.
func (n *Node) FirstChild() *Node { return wrap(n.raw.FirstChild) }

// LastChild returns the last child, or nil if the node has none.
func (n *Node) LastChild() *Node { return wrap(n.raw.LastChild) }

// NextSibling returns the next sibling, or nil if there is none.
func (n *Node) NextSibling() *Node { return wrap(n.raw.NextSibling) }

// PrevSibling returns the previous sibling, or nil if there is none.
func (n *Node) PrevSibling() *Node { return wrap(n.raw.PrevSibling) }

// Children returns a slice of direct children in document order.
func (n *Node) Children() []*Node {
	var out []*Node
	for c := n.raw.FirstChild; c != nil; c = c.NextSibling {
		out = append(out, &Node{raw: c})
	}
	return out
}

// Tag returns the lowercase tag name. It is empty for non-element nodes.
//
// Optimization (TASK-2344): the HTML parser (golang.org/x/net/html)
// stores DataAtom for known tags and sets Data to the atom's string
// (already lowercase). For unknown tags, Data is the raw token text
// which may not be lowercase. We fast-path the common case (known
// atom) by checking DataAtom != 0 and returning Data directly; only
// for unknown tags (DataAtom == 0) do we call strings.ToLower.
func (n *Node) Tag() string {
	if n.raw.Type != html.ElementNode {
		return ""
	}
	if n.raw.DataAtom != 0 {
		return n.raw.Data // already lowercase (atom string)
	}
	return strings.ToLower(n.raw.Data)
}

// Data returns the raw Data field: tag name for elements, text content for
// text nodes, comment body for comment nodes, and so on.
func (n *Node) Data() string { return n.raw.Data }

// Attr returns the value of the named attribute and whether it was
// present. Attribute names are matched case-insensitively per HTML
// semantics.
//
// Optimization (TASK-2344): the HTML tokenizer (golang.org/x/net/html)
// lowercases all attribute keys, so a fast-path exact match on the
// lowercased name covers the common case. The EqualFold fallback
// handles attributes injected by user code (mutation APIs) that may
// not be lowercased.
func (n *Node) Attr(name string) (string, bool) {
	// Fast path: exact match on lowercased key (parser-normalized).
	for _, a := range n.raw.Attr {
		if a.Key == name {
			return a.Val, true
		}
	}
	// Slow path: case-insensitive match for non-parser-normalized keys.
	for _, a := range n.raw.Attr {
		if strings.EqualFold(a.Key, name) {
			return a.Val, true
		}
	}
	return "", false
}

// AttrOrEmpty returns the attribute value or "" if the attribute is
// absent.
func (n *Node) AttrOrEmpty(name string) string {
	v, _ := n.Attr(name)
	return v
}

// Attrs returns all attributes as a map. If duplicate keys appear, later
// values overwrite earlier ones.
func (n *Node) Attrs() map[string]string {
	out := make(map[string]string, len(n.raw.Attr))
	for _, a := range n.raw.Attr {
		out[a.Key] = a.Val
	}
	return out
}

// Text returns the concatenated text content of the node and all
// descendants, mirroring DOM Node.textContent. It does not collapse
// whitespace and does not skip script or style content; agent.Text
// provides reader-friendly extraction.
func (n *Node) Text() string {
	var b strings.Builder
	collectText(n.raw, &b)
	return b.String()
}

func collectText(n *html.Node, b *strings.Builder) {
	if n == nil {
		return
	}
	if n.Type == html.TextNode {
		b.WriteString(n.Data)
		return
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		collectText(c, b)
	}
}
