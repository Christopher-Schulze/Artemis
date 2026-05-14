// Package agent implements the agent-facing extraction layer: HTML and
// text serialization in Phase 1, with structured-data, semantic tree,
// links, forms, and actions added in their respective TASKs.
package agent

import (
	"strings"

	"golang.org/x/net/html"

	"artemis/internal/pool"
	"artemis/webapi"
)

// HTML serializes the document to HTML using golang.org/x/net/html's
// canonical render.
func HTML(d *webapi.Document) string {
	if d == nil || d.RawRoot() == nil {
		return ""
	}
	b := pool.GetBuilder()
	defer pool.PutBuilder(b)
	if err := html.Render(b, d.RawRoot()); err != nil {
		return ""
	}
	return strings.Clone(b.String())
}

// Text returns the visible text of the document, skipping <script>,
// <style>, <noscript>, <template>, and the <head> subtree. Whitespace is
// collapsed.
func Text(d *webapi.Document) string {
	if d == nil || d.RawRoot() == nil {
		return ""
	}
	b := pool.GetBuilder()
	defer pool.PutBuilder(b)
	collectVisibleText(d.RawRoot(), b)
	return collapseWhitespace(b.String())
}

var blockElements = map[string]bool{
	"address": true, "article": true, "aside": true, "blockquote": true,
	"body": true, "br": true, "div": true, "dl": true, "dd": true, "dt": true,
	"fieldset": true, "figcaption": true, "figure": true, "footer": true,
	"form": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"header": true, "hr": true, "li": true, "main": true, "nav": true,
	"ol": true, "p": true, "pre": true, "section": true, "table": true,
	"tr": true, "ul": true, "td": true, "th": true,
}

func collectVisibleText(n *html.Node, b *strings.Builder) {
	if n == nil {
		return
	}
	switch n.Type {
	case html.ElementNode:
		switch n.Data {
		case "script", "style", "noscript", "template", "head":
			return
		}
		if blockElements[n.Data] {
			b.WriteByte(' ')
		}
	case html.TextNode:
		b.WriteString(n.Data)
		return
	case html.CommentNode:
		return
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		collectVisibleText(c, b)
	}
	if n.Type == html.ElementNode && blockElements[n.Data] {
		b.WriteByte(' ')
	}
}

func collapseWhitespace(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := true
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		b.WriteRune(r)
		prevSpace = false
	}
	return strings.TrimSpace(b.String())
}
