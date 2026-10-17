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

func requireCascade(t *testing.T, sheets []*Stylesheet, node *html.Node, inline string) *ComputedStyle {
	t.Helper()
	style, err := Cascade(sheets, node, inline)
	if err != nil {
		t.Fatal(err)
	}
	return style
}

func declaredValue(t *testing.T, style *ComputedStyle, property string) string {
	t.Helper()
	declaration, exists := style.Cascaded(property)
	if !exists {
		t.Fatalf("winning declaration missing for %s", property)
	}
	return declaration.Value
}

func computedValue(t *testing.T, style *ComputedStyle, property string) string {
	t.Helper()
	value, err := style.Value(property)
	if err != nil {
		t.Fatalf("computed %s: %v", property, err)
	}
	return value
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
	got := requireCascade(t, []*Stylesheet{sh}, n, "")
	if value := declaredValue(t, got, "color"); value != "blue" {
		t.Errorf("color = %q, want blue (id beats class)", value)
	}
}

func TestCascadeImportantBeatsInline(t *testing.T) {
	sh := ParseStylesheet(`.x { color: red !important; }`)
	doc := `<html><body><div id="a" class="x" style="color: green"></div></body></html>`
	n := parseAndFind(t, doc, "#a")
	got := requireCascade(t, []*Stylesheet{sh}, n, "color: green")
	if value := declaredValue(t, got, "color"); value != "red" {
		t.Errorf("color = %q, want red (!important beats inline)", value)
	}
}

func TestCascadeInlineBeatsClass(t *testing.T) {
	sh := ParseStylesheet(`.x { color: red; }`)
	doc := `<html><body><div id="a" class="x" style="color: green"></div></body></html>`
	n := parseAndFind(t, doc, "#a")
	got := requireCascade(t, []*Stylesheet{sh}, n, "color: green")
	if value := declaredValue(t, got, "color"); value != "green" {
		t.Errorf("color = %q, want green (inline beats class)", value)
	}
}

func TestCascadeOrderTiesBreakLast(t *testing.T) {
	sh := ParseStylesheet(`
		.x { color: red; }
		.x { color: blue; }
	`)
	doc := `<html><body><div id="a" class="x"></div></body></html>`
	n := parseAndFind(t, doc, "#a")
	got := requireCascade(t, []*Stylesheet{sh}, n, "")
	if value := declaredValue(t, got, "color"); value != "blue" {
		t.Errorf("color = %q, want blue (last wins on tie)", value)
	}
}

func TestCascadeMultipleProps(t *testing.T) {
	sh := ParseStylesheet(`
		.x { color: red; font-size: 14px; }
		#a { background: blue; }
	`)
	doc := `<html><body><div id="a" class="x"></div></body></html>`
	n := parseAndFind(t, doc, "#a")
	got := requireCascade(t, []*Stylesheet{sh}, n, "padding: 4px")
	if value := declaredValue(t, got, "color"); value != "red" {
		t.Errorf("color = %q", value)
	}
	if value := declaredValue(t, got, "font-size"); value != "14px" {
		t.Errorf("font-size = %q", value)
	}
	if value := declaredValue(t, got, "background"); value != "blue" {
		t.Errorf("background = %q", value)
	}
	if value := declaredValue(t, got, "padding"); value != "4px" {
		t.Errorf("padding (inline) = %q", value)
	}
}
