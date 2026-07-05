package agent

import (
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/parser"
)

func TestHTMLRoundTripContainsContent(t *testing.T) {
	src := `<!doctype html><html><head><title>x</title></head><body><p>Hello <b>world</b>.</p></body></html>`
	d, err := parser.ParseHTML(strings.NewReader(src), "")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out := HTML(d)
	for _, want := range []string{"<title>x</title>", "<p>Hello <b>world</b>.</p>"} {
		if !strings.Contains(out, want) {
			t.Errorf("HTML missing %q in:\n%s", want, out)
		}
	}
}

func TestTextSkipsScriptStyle(t *testing.T) {
	src := `<html><head><title>HEADTITLE</title><style>x{color:red}</style></head><body>Hello <script>var x=1</script><p>World</p></body></html>`
	d, err := parser.ParseHTML(strings.NewReader(src), "")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := Text(d)
	if !strings.Contains(got, "Hello") || !strings.Contains(got, "World") {
		t.Errorf("Text missing visible content: %q", got)
	}
	if strings.Contains(got, "var x=1") {
		t.Errorf("Text leaked script content: %q", got)
	}
	if strings.Contains(got, "color:red") {
		t.Errorf("Text leaked style content: %q", got)
	}
	if strings.Contains(got, "HEADTITLE") {
		t.Errorf("Text leaked head title: %q", got)
	}
}

func TestTextCollapsesWhitespace(t *testing.T) {
	src := "<p>  one\t\n   two   three  </p>"
	d, err := parser.ParseHTML(strings.NewReader(src), "")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := Text(d)
	if strings.Contains(got, "  ") {
		t.Errorf("Text contains double-space: %q", got)
	}
	if !strings.Contains(got, "one two three") {
		t.Errorf("Text = %q, want 'one two three'", got)
	}
}

func TestHTMLNilSafe(t *testing.T) {
	if HTML(nil) != "" {
		t.Error("HTML(nil) should be empty string")
	}
	if Text(nil) != "" {
		t.Error("Text(nil) should be empty string")
	}
}
