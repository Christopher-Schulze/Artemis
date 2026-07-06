package webapi

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func parseFix(t *testing.T, src string) *Document {
	t.Helper()
	root, err := html.Parse(strings.NewReader(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return NewDocument(root, "")
}

func TestSetAttributeAddsAndReplaces(t *testing.T) {
	d := parseFix(t, `<a id="x">link</a>`)
	a, _ := d.QuerySelector("a")
	SetAttribute(a, "href", "https://example.test/")
	if got, _ := a.Attr("href"); got != "https://example.test/" {
		t.Errorf("href = %q", got)
	}
	SetAttribute(a, "href", "https://other.test/")
	if got, _ := a.Attr("href"); got != "https://other.test/" {
		t.Errorf("href replace = %q", got)
	}
}

func TestRemoveAttribute(t *testing.T) {
	d := parseFix(t, `<a href="x" class="c">link</a>`)
	a, _ := d.QuerySelector("a")
	RemoveAttribute(a, "class")
	if HasAttribute(a, "class") {
		t.Error("class still present after remove")
	}
	if !HasAttribute(a, "href") {
		t.Error("href removed unexpectedly")
	}
}

func TestAppendAndRemoveChild(t *testing.T) {
	d := parseFix(t, `<ul></ul>`)
	ul, _ := d.QuerySelector("ul")
	li := CreateElement("li")
	SetTextContent(li, "one")
	if err := AppendChild(ul, li); err != nil {
		t.Fatalf("append: %v", err)
	}
	if ul.FirstChild() == nil || ul.FirstChild().Tag() != "li" {
		t.Fatalf("li not appended: %+v", ul.FirstChild())
	}
	if got := li.Text(); got != "one" {
		t.Errorf("li text = %q", got)
	}
	if err := RemoveChild(ul, li); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if ul.FirstChild() != nil {
		t.Error("li still present after remove")
	}
}

func TestSetInnerHTMLReplacesChildren(t *testing.T) {
	d := parseFix(t, `<div><p>old</p></div>`)
	div, _ := d.QuerySelector("div")
	if err := SetInnerHTML(div, `<span>new</span><span>more</span>`); err != nil {
		t.Fatalf("setInnerHTML: %v", err)
	}
	spans, _ := div.QuerySelectorAll("span")
	if len(spans) != 2 {
		t.Fatalf("spans = %d, want 2", len(spans))
	}
	if got := spans[0].Text(); got != "new" {
		t.Errorf("span[0] = %q", got)
	}
	if old, _ := div.QuerySelector("p"); old != nil {
		t.Errorf("old <p> still present")
	}
}

func TestCreateAndGetElementById(t *testing.T) {
	d := parseFix(t, `<div></div>`)
	div, _ := d.QuerySelector("div")
	el := CreateElement("section")
	SetAttribute(el, "id", "x")
	if err := AppendChild(div, el); err != nil {
		t.Fatalf("append: %v", err)
	}
	got := GetElementById(d.Root(), "x")
	if got == nil || got.Tag() != "section" {
		t.Errorf("getElementById = %+v", got)
	}
}

func TestGetElementsByTagAndClass(t *testing.T) {
	d := parseFix(t, `<div><p class="a">1</p><p class="b">2</p><span class="a">3</span></div>`)
	root := d.Root()
	ps := GetElementsByTagName(root, "p")
	if len(ps) != 2 {
		t.Errorf("p count = %d, want 2", len(ps))
	}
	all := GetElementsByTagName(root, "*")
	if len(all) < 4 { // html, head, body, div, p, p, span = 7
		t.Errorf("* count too low = %d", len(all))
	}
	a := GetElementsByClassName(root, "a")
	if len(a) != 2 {
		t.Errorf("class a count = %d, want 2", len(a))
	}
}

func TestCloneNodeDeepShallow(t *testing.T) {
	d := parseFix(t, `<div><p>x</p></div>`)
	div, _ := d.QuerySelector("div")
	shallow := CloneNode(div, false)
	if shallow.FirstChild() != nil {
		t.Error("shallow clone has children")
	}
	deep := CloneNode(div, true)
	if deep.FirstChild() == nil || deep.FirstChild().Tag() != "p" {
		t.Errorf("deep clone missing p")
	}
}
