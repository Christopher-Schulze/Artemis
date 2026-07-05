package agent

import (
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/parser"
)

func TestLinksAbsoluteAndFiltered(t *testing.T) {
	src := `<html><body>
		<a href="/foo">internal</a>
		<a href="https://other.test/x">external</a>
		<a href="javascript:alert(1)">js</a>
		<a href="mailto:x@y.test">mail</a>
		<a href="#hash">hash</a>
		<a href="">empty</a>
		<a href="https://other.test/y" rel="noopener" title="T">titled</a>
	</body></html>`
	doc, err := parser.ParseHTML(strings.NewReader(src), "https://base.test/here/")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	links := Links(doc)
	if len(links) != 3 {
		t.Fatalf("got %d links, want 3 (filtered): %+v", len(links), links)
	}
	if links[0].Href != "https://base.test/foo" {
		t.Errorf("absolute = %q", links[0].Href)
	}
	if links[0].Text != "internal" {
		t.Errorf("text = %q", links[0].Text)
	}
	if links[2].Rel != "noopener" || links[2].Title != "T" {
		t.Errorf("rel/title = %q/%q", links[2].Rel, links[2].Title)
	}
}

func TestLinksAllIncludesEverything(t *testing.T) {
	src := `<a href="javascript:x()">a</a><a href="mailto:y">b</a><a href="/z">c</a>`
	doc, _ := parser.ParseHTML(strings.NewReader(src), "https://e.test/")
	all := LinksAll(doc)
	if len(all) != 3 {
		t.Errorf("LinksAll = %d, want 3", len(all))
	}
}
