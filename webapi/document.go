package webapi

import (
	"strings"

	"golang.org/x/net/html"
)

// Document is the root of a parsed HTML document.
type Document struct {
	root *html.Node
	url  string
}

// NewDocument wraps a *html.Node of type DocumentNode. The url is stored
// for later use as the base for resolving relative references.
func NewDocument(root *html.Node, url string) *Document {
	return &Document{root: root, url: url}
}

// URL returns the URL associated with the document, or "" when not set.
func (d *Document) URL() string { return d.url }

// Root returns the document root as a Node.
func (d *Document) Root() *Node { return wrap(d.root) }

// RawRoot returns the underlying html.Node root for integration with
// libraries that operate on html.Node directly.
func (d *Document) RawRoot() *html.Node { return d.root }

// HTMLElement returns the <html> element, or nil if absent.
func (d *Document) HTMLElement() *Node {
	for c := d.root.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == "html" {
			return wrap(c)
		}
	}
	return nil
}

// Head returns the <head> element, or nil if absent.
func (d *Document) Head() *Node {
	h := d.HTMLElement()
	if h == nil {
		return nil
	}
	for c := h.raw.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == "head" {
			return wrap(c)
		}
	}
	return nil
}

// Body returns the <body> element, or nil if absent.
func (d *Document) Body() *Node {
	h := d.HTMLElement()
	if h == nil {
		return nil
	}
	for c := h.raw.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == "body" {
			return wrap(c)
		}
	}
	return nil
}

// HTMLTitleElement returns the first HTML title in document order. SVG titles
// inside an HTML document do not supply its document title.
func (d *Document) HTMLTitleElement() *Node {
	if d == nil || d.root == nil {
		return nil
	}
	var title *Node
	WalkValue(d.Root(), func(node Node) WalkAction {
		if node.Type() == NodeElement && node.Tag() == "title" && node.Raw().Namespace == "" {
			title = wrap(node.Raw())
			return WalkStop
		}
		return WalkContinue
	})
	return title
}

// Title returns the HTML title's child text with ASCII whitespace collapsed.
func (d *Document) Title() string {
	title := d.HTMLTitleElement()
	if title == nil {
		return ""
	}
	var value strings.Builder
	for child := title.raw.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.TextNode {
			value.WriteString(child.Data)
		}
	}
	return strings.Join(strings.FieldsFunc(value.String(), func(character rune) bool {
		return character == ' ' || character == '\t' || character == '\n' || character == '\r' || character == '\f'
	}), " ")
}
