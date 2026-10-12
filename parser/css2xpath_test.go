package parser

import (
	"strings"
	"testing"
)

func TestCSSToXPathElement(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"div", "//div"},
		{"p", "//p"},
		{"*", "//*"},
	}
	for _, c := range cases {
		got, err := CSSToXPath(c.in)
		if err != nil {
			t.Fatalf("CSSToXPath(%q) error: %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("CSSToXPath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCSSToXPathClass(t *testing.T) {
	got, err := CSSToXPath(".foo")
	if err != nil {
		t.Fatal(err)
	}
	want := "//*[contains(concat(' ', normalize-space(@class), ' '), ' foo ')]"
	if got != want {
		t.Fatalf("CSSToXPath(.foo) = %q, want %q", got, want)
	}
}

func TestCSSToXPathElementClass(t *testing.T) {
	got, err := CSSToXPath("div.foo")
	if err != nil {
		t.Fatal(err)
	}
	want := "//div[contains(concat(' ', normalize-space(@class), ' '), ' foo ')]"
	if got != want {
		t.Fatalf("CSSToXPath(div.foo) = %q, want %q", got, want)
	}
}

func TestCSSToXPathID(t *testing.T) {
	got, err := CSSToXPath("#bar")
	if err != nil {
		t.Fatal(err)
	}
	want := "//*[@id='bar']"
	if got != want {
		t.Fatalf("CSSToXPath(#bar) = %q, want %q", got, want)
	}
}

func TestCSSToXPathAttributePresence(t *testing.T) {
	got, err := CSSToXPath("[href]")
	if err != nil {
		t.Fatal(err)
	}
	want := "//*[@href]"
	if got != want {
		t.Fatalf("CSSToXPath([href]) = %q, want %q", got, want)
	}
}

func TestCSSToXPathAttributeValue(t *testing.T) {
	got, err := CSSToXPath("[type='text']")
	if err != nil {
		t.Fatal(err)
	}
	want := "//*[@type='text']"
	if got != want {
		t.Fatalf("CSSToXPath([type='text']) = %q, want %q", got, want)
	}
}

func TestCSSToXPathAttributeValueDoubleQuote(t *testing.T) {
	got, err := CSSToXPath(`[type="text"]`)
	if err != nil {
		t.Fatal(err)
	}
	want := "//*[@type='text']"
	if got != want {
		t.Fatalf(`CSSToXPath([type="text"]) = %q, want %q`, got, want)
	}
}

func TestCSSToXPathDescendant(t *testing.T) {
	got, err := CSSToXPath("div p")
	if err != nil {
		t.Fatal(err)
	}
	want := "//div//p"
	if got != want {
		t.Fatalf("CSSToXPath(div p) = %q, want %q", got, want)
	}
}

func TestCSSToXPathChild(t *testing.T) {
	got, err := CSSToXPath("div > p")
	if err != nil {
		t.Fatal(err)
	}
	want := "//div/p"
	if got != want {
		t.Fatalf("CSSToXPath(div > p) = %q, want %q", got, want)
	}
}

func TestCSSToXPathChildNoSpaces(t *testing.T) {
	got, err := CSSToXPath("div>p")
	if err != nil {
		t.Fatal(err)
	}
	want := "//div/p"
	if got != want {
		t.Fatalf("CSSToXPath(div>p) = %q, want %q", got, want)
	}
}

func TestCSSToXPathTextPseudo(t *testing.T) {
	got, err := CSSToXPath("div::text")
	if err != nil {
		t.Fatal(err)
	}
	want := "//div/text()"
	if got != want {
		t.Fatalf("CSSToXPath(div::text) = %q, want %q", got, want)
	}
}

func TestCSSToXPathAttrPseudo(t *testing.T) {
	got, err := CSSToXPath("div::attr(href)")
	if err != nil {
		t.Fatal(err)
	}
	want := "//div/@href"
	if got != want {
		t.Fatalf("CSSToXPath(div::attr(href)) = %q, want %q", got, want)
	}
}

func TestCSSToXPathMultipleSelectors(t *testing.T) {
	got, err := CSSToXPath("div, span")
	if err != nil {
		t.Fatal(err)
	}
	want := "//div | //span"
	if got != want {
		t.Fatalf("CSSToXPath(div, span) = %q, want %q", got, want)
	}
}

func TestCSSToXPathCompoundClassAndID(t *testing.T) {
	got, err := CSSToXPath("div.foo#bar")
	if err != nil {
		t.Fatal(err)
	}
	want := "//div[contains(concat(' ', normalize-space(@class), ' '), ' foo ') and @id='bar']"
	if got != want {
		t.Fatalf("CSSToXPath(div.foo#bar) = %q, want %q", got, want)
	}
}

func TestCSSToXPathDescendantWithClass(t *testing.T) {
	got, err := CSSToXPath("div.foo p")
	if err != nil {
		t.Fatal(err)
	}
	want := "//div[contains(concat(' ', normalize-space(@class), ' '), ' foo ')]//p"
	if got != want {
		t.Fatalf("CSSToXPath(div.foo p) = %q, want %q", got, want)
	}
}

func TestCSSToXPathPrefixScrapling(t *testing.T) {
	got, err := CSSToXPathWithPrefix("div", "descendant-or-self::")
	if err != nil {
		t.Fatal(err)
	}
	want := "descendant-or-self::div"
	if got != want {
		t.Fatalf("CSSToXPathWithPrefix(div, scrapling) = %q, want %q", got, want)
	}
}

func TestCSSToXPathPrefixEmpty(t *testing.T) {
	got, err := CSSToXPathWithPrefix("div", "")
	if err != nil {
		t.Fatal(err)
	}
	want := "div"
	if got != want {
		t.Fatalf("CSSToXPathWithPrefix(div, '') = %q, want %q", got, want)
	}
}

func TestCSSToXPathPrefixDescendant(t *testing.T) {
	got, err := CSSToXPathWithPrefix("div p", "descendant-or-self::")
	if err != nil {
		t.Fatal(err)
	}
	want := "descendant-or-self::div//p"
	if got != want {
		t.Fatalf("CSSToXPathWithPrefix(div p, scrapling) = %q, want %q", got, want)
	}
}

func TestCSSToXPathEmptyInput(t *testing.T) {
	cases := []string{"", "   ", "\t\n"}
	for _, in := range cases {
		if _, err := CSSToXPath(in); err == nil {
			t.Fatalf("CSSToXPath(%q) expected error, got nil", in)
		}
	}
}

func TestCSSToXPathInvalidSelector(t *testing.T) {
	cases := []string{
		"::text",       // pseudo without selector
		"div::unknown", // unsupported pseudo
		"div::attr()",  // empty attr name
		"div >",        // trailing child combinator
		"div..foo",     // empty class name
		"div#",         // empty id
		"div@",         // invalid char
	}
	for _, in := range cases {
		if _, err := CSSToXPath(in); err == nil {
			t.Fatalf("CSSToXPath(%q) expected error, got nil", in)
		} else {
			pe, ok := err.(*ParseError)
			if !ok {
				t.Fatalf("CSSToXPath(%q) error type %T, want *ParseError", in, err)
			}
			if !strings.Contains(pe.Error(), "css2xpath") {
				t.Fatalf("ParseError message must mention css2xpath: %q", pe.Error())
			}
		}
	}
}

func TestParseErrorMessage(t *testing.T) {
	e := &ParseError{CSS: "div@", Reason: "invalid compound selector"}
	if !strings.Contains(e.Error(), "div@") {
		t.Fatalf("ParseError.Error() must contain CSS: %q", e.Error())
	}
	if !strings.Contains(e.Error(), "invalid compound selector") {
		t.Fatalf("ParseError.Error() must contain reason: %q", e.Error())
	}
}
