package parser

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestTextHandlerText(t *testing.T) {
	h := NewTextHandler("hello")
	if got := h.Text(); got != "hello" {
		t.Fatalf("Text() = %q, want %q", got, "hello")
	}
}

func TestTextHandlerHasText(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"hello", true},
		{"  hello  ", true},
		{"", false},
		{"   ", false},
		{"\n\t", false},
	}
	for _, c := range cases {
		h := NewTextHandler(c.in)
		if got := h.HasText(); got != c.want {
			t.Errorf("HasText(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestTextHandlerNilSafe(t *testing.T) {
	var h *TextHandler
	if h.Text() != "" {
		t.Fatal("nil TextHandler.Text() must be empty")
	}
	if h.HasText() {
		t.Fatal("nil TextHandler.HasText() must be false")
	}
}

func TestTextHandlersAll(t *testing.T) {
	h := NewTextHandlers([]string{"a", "b", "c"})
	want := []string{"a", "b", "c"}
	if got := h.All(); !reflect.DeepEqual(got, want) {
		t.Fatalf("All() = %v, want %v", got, want)
	}
}

func TestTextHandlersFirstLast(t *testing.T) {
	h := NewTextHandlers([]string{"a", "b", "c"})
	if h.First() != "a" {
		t.Fatalf("First() = %q, want %q", h.First(), "a")
	}
	if h.Last() != "c" {
		t.Fatalf("Last() = %q, want %q", h.Last(), "c")
	}
}

func TestTextHandlersEmptyFirstLast(t *testing.T) {
	h := NewTextHandlers(nil)
	if h.First() != "" {
		t.Fatal("empty First() must be empty string")
	}
	if h.Last() != "" {
		t.Fatal("empty Last() must be empty string")
	}
}

func TestTextHandlersLen(t *testing.T) {
	h := NewTextHandlers([]string{"a", "b", "c"})
	if h.Len() != 3 {
		t.Fatalf("Len() = %d, want 3", h.Len())
	}
	if NewTextHandlers(nil).Len() != 0 {
		t.Fatal("nil Len() must be 0")
	}
}

func TestTextHandlersGet(t *testing.T) {
	h := NewTextHandlers([]string{"a", "b", "c"})
	if h.Get(0) != "a" {
		t.Fatalf("Get(0) = %q", h.Get(0))
	}
	if h.Get(2) != "c" {
		t.Fatalf("Get(2) = %q", h.Get(2))
	}
	if h.Get(-1) != "c" {
		t.Fatalf("Get(-1) = %q, want c", h.Get(-1))
	}
	if h.Get(-3) != "a" {
		t.Fatalf("Get(-3) = %q, want a", h.Get(-3))
	}
	if h.Get(3) != "" {
		t.Fatal("out-of-range Get(3) must be empty")
	}
	if h.Get(-4) != "" {
		t.Fatal("out-of-range Get(-4) must be empty")
	}
}

func TestAttributesHandlerGet(t *testing.T) {
	a := NewAttributesHandler(map[string]string{"href": "/x", "class": "foo"})
	v, ok := a.Get("href")
	if !ok || v != "/x" {
		t.Fatalf("Get(href) = (%q,%v), want (/x,true)", v, ok)
	}
	if _, ok := a.Get("missing"); ok {
		t.Fatal("Get(missing) must be false")
	}
}

func TestAttributesHandlerAll(t *testing.T) {
	src := map[string]string{"href": "/x", "class": "foo"}
	a := NewAttributesHandler(src)
	all := a.All()
	if !reflect.DeepEqual(all, src) {
		t.Fatalf("All() = %v, want %v", all, src)
	}
	// Mutating the returned map must not affect the handler.
	all["href"] = "mutated"
	if v, _ := a.Get("href"); v != "/x" {
		t.Fatalf("handler mutated via All(): href=%q", v)
	}
	// Mutating the source map after construction must not affect the handler.
	src["href"] = "source-mutated"
	if v, _ := a.Get("href"); v != "/x" {
		t.Fatalf("handler mutated via source map: href=%q", v)
	}
}

func TestAttributesHandlerHas(t *testing.T) {
	a := NewAttributesHandler(map[string]string{"href": "/x"})
	if !a.Has("href") {
		t.Fatal("Has(href) must be true")
	}
	if a.Has("class") {
		t.Fatal("Has(class) must be false")
	}
}

func TestAttributesHandlerNames(t *testing.T) {
	a := NewAttributesHandler(map[string]string{"class": "foo", "href": "/x", "id": "bar"})
	names := a.Names()
	want := []string{"class", "href", "id"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("Names() = %v, want %v (sorted)", names, want)
	}
	if !sort.StringsAreSorted(names) {
		t.Fatal("Names() must be sorted")
	}
}

func TestSelectorTagTextAttrib(t *testing.T) {
	s := NewSelector("a", "click me", map[string]string{"href": "/x", "class": "btn"})
	if s.Tag() != "a" {
		t.Fatalf("Tag() = %q", s.Tag())
	}
	if s.Text() != "click me" {
		t.Fatalf("Text() = %q", s.Text())
	}
	v, ok := s.Attrib("href")
	if !ok || v != "/x" {
		t.Fatalf("Attrib(href) = (%q,%v)", v, ok)
	}
	if _, ok := s.Attrib("missing"); ok {
		t.Fatal("Attrib(missing) must be false")
	}
}

func TestSelectorGetAllTextFallback(t *testing.T) {
	s := NewSelector("p", "hello", nil)
	if s.GetAllText() != "hello" {
		t.Fatalf("GetAllText() fallback = %q, want hello", s.GetAllText())
	}
}

func TestSelectorGetAllTextFull(t *testing.T) {
	s := NewSelectorFull("div", "direct", "directdescendant", map[string]string{"id": "x"}, "<div>direct<span>descendant</span></div>")
	if s.GetAllText() != "directdescendant" {
		t.Fatalf("GetAllText() = %q, want directdescendant", s.GetAllText())
	}
}

func TestSelectorAttributes(t *testing.T) {
	s := NewSelector("a", "x", map[string]string{"href": "/x"})
	ah := s.Attributes()
	v, ok := ah.Get("href")
	if !ok || v != "/x" {
		t.Fatalf("Attributes().Get(href) = (%q,%v)", v, ok)
	}
}

func TestSelectorHTMLSynthesized(t *testing.T) {
	s := NewSelector("a", "click", map[string]string{"href": "/x", "class": "btn"})
	got := s.HTML()
	// Attributes are sorted; href before class.
	want := `<a class="btn" href="/x">click</a>`
	if got != want {
		t.Fatalf("HTML() = %q, want %q", got, want)
	}
}

func TestSelectorHTMLEscaping(t *testing.T) {
	s := NewSelector("a", `say "hi"`, map[string]string{"title": `a & b`})
	got := s.HTML()
	if !strings.Contains(got, `title="a &amp; b"`) {
		t.Fatalf("HTML() must escape ampersand in attr: %q", got)
	}
	if !strings.Contains(got, `>say "hi"</a>`) {
		t.Fatalf("HTML() must keep text as-is: %q", got)
	}
}

func TestSelectorHTMLFull(t *testing.T) {
	outer := `<div id="x">direct<span>desc</span></div>`
	s := NewSelectorFull("div", "direct", "directdesc", map[string]string{"id": "x"}, outer)
	if s.HTML() != outer {
		t.Fatalf("HTML() = %q, want %q", s.HTML(), outer)
	}
}

func TestSelectorsLenFirstLastGet(t *testing.T) {
	sel := NewSelectors([]Selector{
		*NewSelector("a", "1", nil),
		*NewSelector("b", "2", nil),
		*NewSelector("c", "3", nil),
	})
	if sel.Len() != 3 {
		t.Fatalf("Len() = %d", sel.Len())
	}
	first, ok := sel.First()
	if !ok || first.Tag() != "a" {
		t.Fatalf("First() = (%q,%v)", first.Tag(), ok)
	}
	last, ok := sel.Last()
	if !ok || last.Tag() != "c" {
		t.Fatalf("Last() = (%q,%v)", last.Tag(), ok)
	}
	g, ok := sel.Get(1)
	if !ok || g.Tag() != "b" {
		t.Fatalf("Get(1) = (%q,%v)", g.Tag(), ok)
	}
	if _, ok := sel.Get(5); ok {
		t.Fatal("Get(5) must be false")
	}
}

func TestSelectorsEmptyFirstLast(t *testing.T) {
	sel := NewSelectors(nil)
	if _, ok := sel.First(); ok {
		t.Fatal("empty First() must be false")
	}
	if _, ok := sel.Last(); ok {
		t.Fatal("empty Last() must be false")
	}
}

func TestSelectorsAll(t *testing.T) {
	src := []Selector{
		*NewSelector("a", "1", nil),
		*NewSelector("b", "2", nil),
	}
	sel := NewSelectors(src)
	all := sel.All()
	if len(all) != 2 {
		t.Fatalf("All() len = %d", len(all))
	}
	// Mutating the returned slice must not affect the handler.
	all[0] = Selector{}
	if g, _ := sel.Get(0); g.Tag() != "a" {
		t.Fatalf("handler mutated via All(): tag=%q", g.Tag())
	}
}

func TestSelectorsTextsAndTags(t *testing.T) {
	sel := NewSelectors([]Selector{
		*NewSelector("a", "1", nil),
		*NewSelector("b", "2", nil),
	})
	if got := sel.Texts(); !reflect.DeepEqual(got, []string{"1", "2"}) {
		t.Fatalf("Texts() = %v", got)
	}
	if got := sel.Tags(); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("Tags() = %v", got)
	}
}

func TestResultJSONSelector(t *testing.T) {
	s := NewSelector("a", "click", map[string]string{"href": "/x", "class": "btn"})
	out, err := ResultJSON(s)
	if err != nil {
		t.Fatal(err)
	}
	var got selectorJSON
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got.Tag != "a" || got.Text != "click" {
		t.Fatalf("decoded = %+v", got)
	}
	if got.Attributes["href"] != "/x" || got.Attributes["class"] != "btn" {
		t.Fatalf("attributes = %v", got.Attributes)
	}
}

func TestResultJSONSelectors(t *testing.T) {
	sel := NewSelectors([]Selector{
		*NewSelector("a", "1", map[string]string{"href": "/1"}),
		*NewSelector("b", "2", nil),
	})
	out, err := ResultJSON(sel)
	if err != nil {
		t.Fatal(err)
	}
	var got []selectorJSON
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d", len(got))
	}
	if got[0].Tag != "a" || got[1].Tag != "b" {
		t.Fatalf("tags = %v %v", got[0].Tag, got[1].Tag)
	}
}

func TestResultJSONNilSelector(t *testing.T) {
	out, err := ResultJSON((*Selector)(nil))
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "null" {
		t.Fatalf("nil selector JSON = %q, want null", out)
	}
}

func TestResultJSONEmptySelectors(t *testing.T) {
	sel := NewSelectors(nil)
	out, err := ResultJSON(sel)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "[]" {
		t.Fatalf("empty selectors JSON = %q, want []", out)
	}
}

func TestResultJSONUnsupportedType(t *testing.T) {
	if _, err := ResultJSON(42); err == nil {
		t.Fatal("expected error for unsupported type")
	}
}

func TestEmptyHandlers(t *testing.T) {
	// Cross-check all empty/nil handlers behave consistently.
	if NewTextHandlers(nil).Len() != 0 {
		t.Fatal("empty TextHandlers Len must be 0")
	}
	ah := NewAttributesHandler(nil)
	if len(ah.All()) != 0 {
		t.Fatal("empty AttributesHandler All must be empty map")
	}
	if len(ah.Names()) != 0 {
		t.Fatal("empty AttributesHandler Names must be empty")
	}
	sel := NewSelectors(nil)
	if sel.Len() != 0 {
		t.Fatal("empty Selectors Len must be 0")
	}
	if len(sel.Texts()) != 0 {
		t.Fatal("empty Selectors Texts must be empty")
	}
}
