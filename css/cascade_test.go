package css

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func parseAndFind(t *testing.T, src, selector string) *html.Node {
	t.Helper()
	root, err := html.Parse(strings.NewReader(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var find func(*html.Node) *html.Node
	find = func(n *html.Node) *html.Node {
		if n == nil {
			return nil
		}
		if n.Type == html.ElementNode {
			for _, a := range n.Attr {
				if a.Key == "id" && a.Val == strings.TrimPrefix(selector, "#") {
					return n
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if r := find(c); r != nil {
				return r
			}
		}
		return nil
	}
	return find(root)
}

func TestCascadeSpecificityIDBeatsClass(t *testing.T) {
	sh := ParseStylesheet(`
		.x { color: red; }
		#a { color: blue; }
	`)
	doc := `<html><body><div id="a" class="x"></div></body></html>`
	n := parseAndFind(t, doc, "#a")
	if n == nil {
		t.Fatal("not found")
	}
	got := Cascade([]*Stylesheet{sh}, n, "")
	if got["color"] != "blue" {
		t.Errorf("color = %q, want blue (id beats class)", got["color"])
	}
}

func TestCascadeImportantBeatsInline(t *testing.T) {
	sh := ParseStylesheet(`.x { color: red !important; }`)
	doc := `<html><body><div id="a" class="x" style="color: green"></div></body></html>`
	n := parseAndFind(t, doc, "#a")
	got := Cascade([]*Stylesheet{sh}, n, "color: green")
	if got["color"] != "red" {
		t.Errorf("color = %q, want red (!important beats inline)", got["color"])
	}
}

func TestCascadeInlineBeatsClass(t *testing.T) {
	sh := ParseStylesheet(`.x { color: red; }`)
	doc := `<html><body><div id="a" class="x" style="color: green"></div></body></html>`
	n := parseAndFind(t, doc, "#a")
	got := Cascade([]*Stylesheet{sh}, n, "color: green")
	if got["color"] != "green" {
		t.Errorf("color = %q, want green (inline beats class)", got["color"])
	}
}

func TestCascadeOrderTiesBreakLast(t *testing.T) {
	sh := ParseStylesheet(`
		.x { color: red; }
		.x { color: blue; }
	`)
	doc := `<html><body><div id="a" class="x"></div></body></html>`
	n := parseAndFind(t, doc, "#a")
	got := Cascade([]*Stylesheet{sh}, n, "")
	if got["color"] != "blue" {
		t.Errorf("color = %q, want blue (last wins on tie)", got["color"])
	}
}

func TestCascadeMultipleProps(t *testing.T) {
	sh := ParseStylesheet(`
		.x { color: red; font-size: 14px; }
		#a { background: blue; }
	`)
	doc := `<html><body><div id="a" class="x"></div></body></html>`
	n := parseAndFind(t, doc, "#a")
	got := Cascade([]*Stylesheet{sh}, n, "padding: 4px")
	if got["color"] != "red" {
		t.Errorf("color = %q", got["color"])
	}
	if got["font-size"] != "14px" {
		t.Errorf("font-size = %q", got["font-size"])
	}
	if got["background"] != "blue" {
		t.Errorf("background = %q", got["background"])
	}
	if got["padding"] != "4px" {
		t.Errorf("padding (inline) = %q", got["padding"])
	}
}
