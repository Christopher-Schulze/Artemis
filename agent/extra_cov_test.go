package agent

import (
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/parser"
	"github.com/Christopher-Schulze/Artemis/webapi"
)

func mustDoc(t *testing.T, html string) *webapi.Document {
	t.Helper()
	d, err := parser.ParseHTML(strings.NewReader(html), "https://example.test/")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return d
}

func TestTitle(t *testing.T) {
	if got := Title(nil); got != "" {
		t.Fatalf("nil doc: %q", got)
	}
	d := mustDoc(t, `<html><head><title>  Hello World  </title></head></html>`)
	if got := Title(d); got != "Hello World" {
		t.Fatalf("got %q", got)
	}
}

func TestFindForm(t *testing.T) {
	if FindForm(nil, "form") != nil {
		t.Fatal("nil doc must return nil")
	}
	d := mustDoc(t, `<html><body><form id="f"><input name="u"></form></body></html>`)
	if FindForm(d, "#missing") != nil {
		t.Fatal("missing selector must return nil")
	}
	if f := FindForm(d, "#f"); f == nil {
		t.Fatal("form not found by id")
	}
	// descendant selector climbs to the form ancestor
	if f := FindForm(d, "input"); f == nil {
		t.Fatal("must climb to form ancestor")
	}
}
