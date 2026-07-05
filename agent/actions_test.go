package agent

import (
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/parser"
)

func TestClickByTextButton(t *testing.T) {
	doc, _ := parser.ParseHTML(strings.NewReader(`<button id="b">Submit Form</button>`), "")
	n, ok := ClickByText(doc, "Submit Form")
	if !ok || n == nil {
		t.Fatal("ClickByText returned nothing")
	}
	if n.Tag() != "button" {
		t.Errorf("tag = %q, want button", n.Tag())
	}
}

func TestClickByTextAnchor(t *testing.T) {
	doc, _ := parser.ParseHTML(strings.NewReader(`<a href="/x">Read more</a>`), "")
	n, ok := ClickByText(doc, "read more") // case-insensitive
	if !ok {
		t.Fatal("not found")
	}
	if n.Tag() != "a" {
		t.Errorf("tag = %q", n.Tag())
	}
}

func TestClickByTextInputSubmit(t *testing.T) {
	doc, _ := parser.ParseHTML(strings.NewReader(`<input type="submit" value="Go">`), "")
	n, ok := ClickByText(doc, "Go")
	if !ok || n.Tag() != "input" {
		t.Errorf("input submit not found: %v", n)
	}
}

func TestClickByTextNotFound(t *testing.T) {
	doc, _ := parser.ParseHTML(strings.NewReader(`<button>Cancel</button>`), "")
	if _, ok := ClickByText(doc, "missing"); ok {
		t.Error("should not be found")
	}
}

func TestTypeInput(t *testing.T) {
	doc, _ := parser.ParseHTML(strings.NewReader(`<input id="q" type="text" value="">`), "")
	if err := Type(doc, "#q", "hello"); err != nil {
		t.Fatalf("Type: %v", err)
	}
	n, _ := doc.QuerySelector("#q")
	if got, _ := n.Attr("value"); got != "hello" {
		t.Errorf("value = %q", got)
	}
}

func TestTypeTextarea(t *testing.T) {
	doc, _ := parser.ParseHTML(strings.NewReader(`<textarea id="b">old</textarea>`), "")
	if err := Type(doc, "#b", "new"); err != nil {
		t.Fatalf("Type: %v", err)
	}
	n, _ := doc.QuerySelector("#b")
	if got := n.Text(); got != "new" {
		t.Errorf("textarea text = %q", got)
	}
}

func TestTypeOnNonInputErrors(t *testing.T) {
	doc, _ := parser.ParseHTML(strings.NewReader(`<div id="d"></div>`), "")
	if err := Type(doc, "#d", "x"); err == nil {
		t.Error("expected error on non-input element")
	}
}
