package scraper

import (
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/parser"
)

func TestFinderCSS(t *testing.T) {
	html := `<html><body><button id="ok">OK</button></body></html>`
	doc, err := parser.ParseHTML(strings.NewReader(html), "http://example.com/")
	if err != nil {
		t.Fatal(err)
	}
	f := NewFinder()
	r, err := f.Find(doc, "#ok")
	if err != nil {
		t.Fatal(err)
	}
	if r.Strategy != "css" {
		t.Errorf("expected strategy css, got %s", r.Strategy)
	}
	if r.Node.Tag() != "button" {
		t.Errorf("expected button, got %s", r.Node.Tag())
	}
}

func TestFinderXPath(t *testing.T) {
	html := `<html><body><input name="q" value="hello"></body></html>`
	doc, err := parser.ParseHTML(strings.NewReader(html), "http://example.com/")
	if err != nil {
		t.Fatal(err)
	}
	f := NewFinder()
	r, err := f.Find(doc, "//input[@name='q']")
	if err != nil {
		t.Fatal(err)
	}
	if r.Strategy != "xpath" {
		t.Errorf("expected strategy xpath, got %s", r.Strategy)
	}
	if r.Node.Tag() != "input" {
		t.Errorf("expected input, got %s", r.Node.Tag())
	}
}

func TestFinderText(t *testing.T) {
	html := `<html><body><a href="/x">Click me</a></body></html>`
	doc, err := parser.ParseHTML(strings.NewReader(html), "http://example.com/")
	if err != nil {
		t.Fatal(err)
	}
	f := NewFinder()
	r, err := f.Find(doc, "Click me")
	if err != nil {
		t.Fatal(err)
	}
	if r.Strategy != "text" {
		t.Errorf("expected strategy text, got %s", r.Strategy)
	}
	if r.Node.Tag() != "a" {
		t.Errorf("expected a, got %s", r.Node.Tag())
	}
}

func TestFinderCache(t *testing.T) {
	html := `<html><body><button>Go</button></body></html>`
	doc, err := parser.ParseHTML(strings.NewReader(html), "http://example.com/")
	if err != nil {
		t.Fatal(err)
	}
	f := NewFinder()
	r1, err := f.Find(doc, "Go")
	if err != nil {
		t.Fatal(err)
	}
	r2, err := f.Find(doc, "Go")
	if err != nil {
		t.Fatal(err)
	}
	if r1.Node != r2.Node {
		t.Error("cache returned different nodes for same query")
	}
}

func TestFinderNotFound(t *testing.T) {
	html := `<html><body></body></html>`
	doc, err := parser.ParseHTML(strings.NewReader(html), "http://example.com/")
	if err != nil {
		t.Fatal(err)
	}
	f := NewFinder()
	_, err = f.Find(doc, "nonexistent")
	if err == nil {
		t.Error("expected error for missing element")
	}
}
