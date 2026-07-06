package agent

import (
	"net/url"
	"strings"

	"golang.org/x/net/html"

	"github.com/Christopher-Schulze/Artemis/webapi"
)

// Link represents a single anchor extracted from the document.
type Link struct {
	Href  string
	Text  string
	Rel   string
	Title string
}

// Links returns every <a href> with a navigable URL, with hrefs resolved
// against the document's base URL. Empty hrefs and the
// `javascript:` / `mailto:` / `tel:` / `data:` schemes plus
// fragment-only links are skipped; LinksAll returns the unfiltered list.
func Links(d *webapi.Document) []Link {
	return collectLinks(d, true)
}

// LinksAll returns every <a> with an `href`, including skipped schemes
// and intra-page anchors. Order is document order.
func LinksAll(d *webapi.Document) []Link {
	return collectLinks(d, false)
}

func collectLinks(d *webapi.Document, filter bool) []Link {
	if d == nil {
		return nil
	}
	var base *url.URL
	if u := d.URL(); u != "" {
		if parsed, err := url.Parse(u); err == nil {
			base = parsed
		}
	}
	root := d.Root()
	if root == nil {
		return nil
	}
	var out []Link
	webapi.WalkValue(root, func(n webapi.Node) webapi.WalkAction {
		if n.Type() != webapi.NodeElement || n.Tag() != "a" {
			return webapi.WalkContinue
		}
		raw, _ := n.Attr("href")
		if filter && skipHref(raw) {
			return webapi.WalkContinue
		}
		href := raw
		if base != nil && href != "" {
			if ref, err := url.Parse(href); err == nil {
				href = base.ResolveReference(ref).String()
			}
		}
		text := strings.TrimSpace(collapseInline(n.Text()))
		out = append(out, Link{
			Href:  href,
			Text:  text,
			Rel:   n.AttrOrEmpty("rel"),
			Title: n.AttrOrEmpty("title"),
		})
		return webapi.WalkContinue
	})
	return out
}

// skipPrefixes are the URL schemes that Links() filters out.
// Package-level to avoid per-call slice allocation.
var skipPrefixes = []string{"javascript:", "mailto:", "tel:", "data:"}

func skipHref(raw string) bool {
	if raw == "" {
		return true
	}
	// Fast path: fragment-only links start with '#'
	if raw[0] == '#' {
		return true
	}
	// Case-insensitive prefix check without allocating a lowered copy.
	// ASCII scheme prefixes are all lowercase, so we compare byte-by-byte
	// folding the input to lowercase.
	for _, p := range skipPrefixes {
		if hasPrefixFold(raw, p) {
			return true
		}
	}
	return false
}

// hasPrefixFold reports whether s starts with prefix, comparing ASCII
// case-insensitively without allocating. Non-ASCII bytes are compared
// verbatim (scheme prefixes are ASCII-only).
func hasPrefixFold(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	for i := 0; i < len(prefix); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		if c != prefix[i] {
			return false
		}
	}
	return true
}

// hint to the linter that the html package is used by sibling files;
// keeps imports stable.
var _ = html.ElementNode
