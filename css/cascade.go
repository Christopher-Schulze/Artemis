package css

import (
	"sort"

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

// Cascade computes the resolved declarations for the given DOM node by
// walking every Rule in every Stylesheet, finding selectors that match,
// sorting by specificity ascending (so later wins), then applying inline
// style on top. !important entries beat non-important ones at the same
// specificity. Inline style outranks stylesheet rules at the same
// importance level. Inherited properties fall back to the nearest
// ancestor that has a value.
func Cascade(sheets []*Stylesheet, n *html.Node, inlineStyle string) map[string]string {
	if n == nil || n.Type != html.ElementNode {
		return map[string]string{}
	}
	type hit struct {
		decls       map[string]string
		important   map[string]bool
		specificity cascadia.Specificity
		order       int // declaration order: later sheets win ties
	}
	var hits []hit
	order := 0
	for _, sheet := range sheets {
		if sheet == nil {
			continue
		}
		for _, r := range sheet.Rules {
			matched := false
			var best cascadia.Specificity
			for _, sel := range r.Selectors {
				if sel.Match(n) {
					sp := sel.Specificity()
					if !matched || cmpSpec(sp, best) > 0 {
						best = sp
					}
					matched = true
				}
			}
			if matched {
				order++
				hits = append(hits, hit{
					decls: r.Decls, important: r.Important,
					specificity: best, order: order,
				})
			}
		}
	}
	// Sort hits by (important, specificity, order). We split the cascade
	// into two phases below; sort here is stable on order.
	sort.SliceStable(hits, func(i, j int) bool {
		if c := cmpSpec(hits[i].specificity, hits[j].specificity); c != 0 {
			return c < 0
		}
		return hits[i].order < hits[j].order
	})

	out := map[string]string{}
	// Phase 1: non-important from sheets
	for _, h := range hits {
		for k, v := range h.decls {
			if h.important[k] {
				continue
			}
			out[k] = v
		}
	}
	// Phase 2: inline style (non-important wins over sheet non-important)
	inlineDecls, inlineImp := parseDeclBlock(inlineStyle)
	for k, v := range inlineDecls {
		if inlineImp[k] {
			continue
		}
		out[k] = v
	}
	// Phase 3: !important from sheets (beats non-important + inline)
	for _, h := range hits {
		for k, v := range h.decls {
			if !h.important[k] {
				continue
			}
			out[k] = v
		}
	}
	// Phase 4: !important inline (top of cascade)
	for k, v := range inlineDecls {
		if inlineImp[k] {
			out[k] = v
		}
	}
	// Phase 5: inheritance. Compute the parent element's cascade ONCE
	// (it already includes its own inherited values transitively), then
	// fill missing inherited properties from there.
	var parentStyle map[string]string
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Type != html.ElementNode {
			continue
		}
		parentInline := ""
		for _, a := range p.Attr {
			if a.Key == "style" {
				parentInline = a.Val
				break
			}
		}
		parentStyle = Cascade(sheets, p, parentInline)
		break
	}
	if parentStyle != nil {
		for prop := range inheritedProps {
			if _, has := out[prop]; has {
				continue
			}
			if v, ok := parentStyle[prop]; ok {
				out[prop] = v
			}
		}
	}
	return out
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
