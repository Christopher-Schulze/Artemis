package parser

import (
	"strings"
	"testing"
)

func TestParseHTMLBasic(t *testing.T) {
	src := `<!doctype html><html><head><title>T</title></head><body><h1>Hi</h1><p>Hello <b>world</b>.</p></body></html>`
	doc, err := ParseHTML(strings.NewReader(src), "https://example.test/")
	if err != nil {
		t.Fatalf("ParseHTML: %v", err)
	}
	if doc.URL() != "https://example.test/" {
		t.Errorf("URL = %q, want https://example.test/", doc.URL())
	}
	if got := doc.Title(); got != "T" {
		t.Errorf("Title = %q, want T", got)
	}
	if doc.Body() == nil {
		t.Fatal("Body() == nil")
	}
	h1, err := doc.QuerySelector("h1")
	if err != nil || h1 == nil {
		t.Fatalf("h1 not found: %v", err)
	}
	if got := h1.Text(); got != "Hi" {
		t.Errorf("h1 text = %q, want Hi", got)
	}
}

func TestParseHTMLBrokenStillParses(t *testing.T) {
	doc, err := ParseHTML(strings.NewReader(`<p>unclosed<p>chained`), "")
	if err != nil {
		t.Fatalf("ParseHTML: %v", err)
	}
	ps, err := doc.QuerySelectorAll("p")
	if err != nil {
		t.Fatalf("qsa: %v", err)
	}
	if len(ps) != 2 {
		t.Errorf("p count = %d, want 2 (parser auto-closes)", len(ps))
	}
}
