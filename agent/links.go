package agent

import (
	"net/url"
	"strings"

	"golang.org/x/net/html"

	"artemis/webapi"
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
	webapi.Walk(root, func(n *webapi.Node) webapi.WalkAction {
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

func skipHref(raw string) bool {
	if raw == "" {
		return true
	}
	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "#") {
		return true
	}
	for _, p := range []string{"javascript:", "mailto:", "tel:", "data:"} {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	return false
}

// hint to the linter that the html package is used by sibling files;
// keeps imports stable.
var _ = html.ElementNode
