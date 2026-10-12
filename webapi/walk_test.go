package webapi

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func parseFixture(t *testing.T, s string) *Document {
	t.Helper()
	root, err := html.Parse(strings.NewReader(s))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return NewDocument(root, "")
}

func TestNodeAttrAndTag(t *testing.T) {
	d := parseFixture(t, `<a href="x" class="c">link</a>`)
	a, err := d.QuerySelector("a")
	if err != nil {
		t.Fatalf("qs: %v", err)
	}
	if a == nil {
		t.Fatal("a not found")
	}
	if a.Tag() != "a" {
		t.Errorf("Tag = %q, want a", a.Tag())
	}
	if got, _ := a.Attr("HREF"); got != "x" {
		t.Errorf("href = %q, want x (case-insensitive lookup)", got)
	}
	if got := a.AttrOrEmpty("missing"); got != "" {
		t.Errorf("missing = %q, want empty", got)
	}
}

func TestNodeChildrenAndText(t *testing.T) {
	d := parseFixture(t, `<ul><li>one</li><li>two</li><li>three</li></ul>`)
	ul, err := d.QuerySelector("ul")
	if err != nil || ul == nil {
		t.Fatalf("ul not found: %v", err)
	}
	kids := ul.Children()
	if len(kids) != 3 {
		t.Fatalf("children = %d, want 3", len(kids))
	}
	if got := kids[0].Text(); got != "one" {
		t.Errorf("first li text = %q, want one", got)
	}
	if got := kids[2].Text(); got != "three" {
		t.Errorf("third li text = %q, want three", got)
	}
}

func TestQuerySelectorAll(t *testing.T) {
	d := parseFixture(t, `<div><p>1</p><p>2</p><p>3</p></div>`)
	ps, err := d.QuerySelectorAll("p")
	if err != nil {
		t.Fatalf("qsa: %v", err)
	}
	if len(ps) != 3 {
		t.Fatalf("got %d <p>, want 3", len(ps))
	}
	for i, p := range ps {
		want := []string{"1", "2", "3"}[i]
		if got := p.Text(); got != want {
			t.Errorf("p[%d] = %q, want %q", i, got, want)
		}
	}
}

func TestWalkPreOrderAndStop(t *testing.T) {
	d := parseFixture(t, `<a><b/><c><d/></c><e/></a>`)
	root := d.Root()
	var seen []string
	Walk(root, func(n *Node) WalkAction {
		if n.Type() == NodeElement {
			seen = append(seen, n.Tag())
		}
		return WalkContinue
	})
	want := []string{"html", "head", "body", "a", "b", "c", "d", "e"}
	if strings.Join(seen, ",") != strings.Join(want, ",") {
		t.Errorf("walk = %v, want %v", seen, want)
	}

	var stopped []string
	Walk(root, func(n *Node) WalkAction {
		if n.Type() == NodeElement {
			stopped = append(stopped, n.Tag())
			if n.Tag() == "a" {
				return WalkStop
			}
		}
		return WalkContinue
	})
	if len(stopped) == 0 || stopped[len(stopped)-1] != "a" {
		t.Errorf("walk stop = %v, want last element 'a'", stopped)
	}
}

func TestNodeAttrsMap(t *testing.T) {
	d := parseFixture(t, `<input type="text" name="user" value="">`)
	inp, err := d.QuerySelector("input")
	if err != nil || inp == nil {
		t.Fatalf("input not found: %v", err)
	}
	attrs := inp.Attrs()
	cases := map[string]string{"type": "text", "name": "user", "value": ""}
	for k, want := range cases {
		if got, ok := attrs[k]; !ok || got != want {
			t.Errorf("attr %s = %q (ok=%v), want %q", k, got, ok, want)
		}
	}
}
