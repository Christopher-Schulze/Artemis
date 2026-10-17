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

func TestSetTextContentOnTextNode(t *testing.T) {
	text := CreateTextNode("test")
	SetTextContent(text, "")
	if text.Text() != "" || text.Data() != "" {
		t.Errorf("empty text content = %q", text.Text())
	}
	SetTextContent(text, "hello")
	if text.Text() != "hello" {
		t.Errorf("text content = %q, want hello", text.Text())
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

func TestInsertionRejectsCyclesBeforeChangingTree(t *testing.T) {
	for _, test := range []struct {
		name, parentID, childID string
		insert                  bool
	}{
		{"append self", "outer", "outer", false},
		{"append ancestor", "inner", "outer", false},
		{"insert self", "inner", "inner", true},
		{"insert ancestor", "inner", "outer", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			doc := parseFix(t, `<div id="outer"><p id="inner"><i id="ref"></i></p></div>`)
			parent := GetElementById(doc.Root(), test.parentID)
			child := GetElementById(doc.Root(), test.childID)
			reference := GetElementById(doc.Root(), "ref")
			beforeParent, beforeChild := parent.raw.Parent, child.raw.Parent
			var err error
			if test.insert {
				err = InsertBefore(parent, child, reference)
			} else {
				err = AppendChild(parent, child)
			}
			if err == nil {
				t.Fatal("cycle-producing insertion succeeded")
			}
			if parent.raw.Parent != beforeParent || child.raw.Parent != beforeChild || reference.raw.Parent != GetElementById(doc.Root(), "inner").raw {
				t.Fatal("rejected insertion changed actual tree ownership")
			}
		})
	}
}

func TestInsertBeforeInvalidReferencePreservesSourceAndSelfReference(t *testing.T) {
	doc := parseFix(t, `<div id="source"><p id="child"></p><i id="after"></i></div><div id="target"><b id="ref"></b></div>`)
	source := GetElementById(doc.Root(), "source")
	target := GetElementById(doc.Root(), "target")
	child := GetElementById(doc.Root(), "child")
	after := GetElementById(doc.Root(), "after")
	reference := GetElementById(doc.Root(), "ref")
	if err := InsertBefore(source, child, reference); err == nil {
		t.Fatal("foreign reference accepted")
	}
	if child.raw.Parent != source.raw || child.raw.NextSibling != after.raw || source.raw.FirstChild != child.raw || reference.raw.Parent != target.raw {
		t.Fatal("invalid reference detached or reordered source child")
	}
	if err := InsertBefore(source, child, child); err != nil {
		t.Fatalf("valid self-reference failed: %v", err)
	}
	if child.raw.Parent != source.raw || child.raw.NextSibling != after.raw || source.raw.FirstChild != child.raw {
		t.Fatal("self-reference reordered tree")
	}
}

func TestChildInsertionRejectsInvalidOwners(t *testing.T) {
	parent, child := CreateElement("div"), CreateElement("span")
	for _, test := range []struct {
		name          string
		parent, child *Node
	}{
		{"nil parent", nil, child},
		{"nil child", parent, nil},
		{"zero parent", &Node{}, child},
		{"zero child", parent, &Node{}},
		{"text parent", CreateTextNode("actual"), child},
		{"document child", parent, Wrap(&html.Node{Type: html.DocumentNode})},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := AppendChild(test.parent, test.child); err == nil {
				t.Fatal("invalid append accepted")
			}
			if err := InsertBefore(test.parent, test.child, nil); err == nil {
				t.Fatal("invalid insertion accepted")
			}
		})
	}
	if parent.FirstChild() != nil || child.Parent() != nil {
		t.Fatal("rejected insertion mutated valid arguments")
	}
}
