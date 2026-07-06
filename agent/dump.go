// Package agent implements the agent-facing extraction layer: HTML and
// text serialization in Phase 1, with structured-data, semantic tree,
// links, forms, and actions added in their respective TASKs.
package agent

import (
	"strings"

	"golang.org/x/net/html"

	"github.com/Christopher-Schulze/Artemis/internal/pool"
	"github.com/Christopher-Schulze/Artemis/webapi"
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

// collapseWhitespace folds runs of ASCII whitespace into a single space and
// trims the ends. Like collapseInline it scans bytes (multi-byte UTF-8 bytes are
// >= 0x80 and copied verbatim); a string that needs no change is returned as-is,
// allocation free.
func collapseWhitespace(s string) string {
	if !needsWhitespaceCollapse(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := true
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isCollapsibleWS(c) {
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		b.WriteByte(c)
		prevSpace = false
	}
	return strings.TrimSpace(b.String())
}

// needsWhitespaceCollapse reports whether collapseWhitespace would change s:
// leading/trailing whitespace, any tab/CR/LF, or any run of two or more
// whitespace bytes.
func needsWhitespaceCollapse(s string) bool {
	if s == "" {
		return false
	}
	if isCollapsibleWS(s[0]) || isCollapsibleWS(s[len(s)-1]) {
		return true
	}
	prevSpace := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !isCollapsibleWS(c) {
			prevSpace = false
			continue
		}
		if c != ' ' || prevSpace {
			return true
		}
		prevSpace = true
	}
	return false
}
