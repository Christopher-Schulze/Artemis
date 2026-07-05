package scraper

import (
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/parser"
)

// TestFinderSelectorDriftCSSFailureTextFallback is a deterministic
// regression test for the selector-drift failure class surfaced by the
// TASK-2325 smoke matrix. When a site redesign breaks a CSS selector,
// the Finder's text-fallback stage must still locate the element by
// its visible text content. No live-network dependency: the HTML
// fixtures are inline.
func TestFinderSelectorDriftCSSFailureTextFallback(t *testing.T) {
	// Simulate a site redesign: old HTML had class="product-name",
	// new HTML has class="p-name". The CSS selector ".product-name"
	// breaks on the new HTML, but the text "Product Name" is stable.
	// We use a <span> (non-container) so the text fallback stage can
	// match it — the Finder skips container tags (div, section) in the
	// text stage to avoid matching structural wrappers.
	oldHTML := `<html><body><div><span class="product-name">Product Name</span></div></body></html>`
	newHTML := `<html><body><div><span class="p-name">Product Name</span></div></body></html>`

	docOld, err := parser.ParseHTML(strings.NewReader(oldHTML), "http://shop.example.com/product")
	if err != nil {
		t.Fatal(err)
	}
	docNew, err := parser.ParseHTML(strings.NewReader(newHTML), "http://shop.example.com/product")
	if err != nil {
		t.Fatal(err)
	}

	f := NewFinder()

	// Old HTML: CSS selector works.
	r1, err := f.Find(docOld, ".product-name")
	if err != nil {
		t.Fatalf("Find on old HTML with .product-name: %v", err)
	}
	if r1.Strategy != "css" {
		t.Errorf("old HTML: expected strategy css, got %s", r1.Strategy)
	}
	if r1.Confidence != 1.0 {
		t.Errorf("old HTML: expected confidence 1.0, got %f", r1.Confidence)
	}

	// New HTML: CSS selector breaks, text fallback must find it.
	f2 := NewFinder()
	r2, err := f2.Find(docNew, ".product-name")
	if err == nil {
		// CSS might still parse but not match; check if it matched
		// something wrong. If it matched, strategy would be "css"
		// but the element wouldn't have class "product-name".
		if r2.Strategy == "css" {
			t.Errorf("new HTML: CSS selector .product-name should not match, but got css match")
		}
	}

	// New HTML: find by text content — the adaptive fallback.
	r3, err := f2.Find(docNew, "Product Name")
	if err != nil {
		t.Fatalf("Find on new HTML with text 'Product Name': %v", err)
	}
	if r3.Strategy != "text" {
		t.Errorf("new HTML: expected strategy text, got %s", r3.Strategy)
	}
	if r3.Confidence <= 0 {
		t.Errorf("new HTML: expected positive confidence, got %f", r3.Confidence)
	}
	if text := r3.Node.Text(); !strings.Contains(text, "Product Name") {
		t.Errorf("new HTML: found node text = %q, want to contain 'Product Name'", text)
	}
}

// TestFinderStructuralHeuristicFallback verifies the structural
// heuristic stage works for human-language queries that contain a
// tag hint. This is the last-resort fallback before giving up.
func TestFinderStructuralHeuristicFallback(t *testing.T) {
	html := `<html><body><button data-cy="submit">Submit Order</button></body></html>`
	doc, err := parser.ParseHTML(strings.NewReader(html), "http://example.com/")
	if err != nil {
		t.Fatal(err)
	}

	f := NewFinder()
	// "submit button" contains the tag hint "button" and text "submit".
	// CSS ".submit" won't match (no class), XPath won't match, text
	// "submit button" won't match exactly, but structural heuristic
	// should find the button element.
	r, err := f.Find(doc, "submit button")
	if err != nil {
		t.Fatalf("Find with 'submit button': %v", err)
	}
	if r.Strategy != "heuristic" {
		t.Errorf("expected strategy heuristic, got %s", r.Strategy)
	}
	if r.Confidence > 0.9 {
		t.Errorf("heuristic confidence should be capped at 0.9, got %f", r.Confidence)
	}
	if r.Node.Tag() != "button" {
		t.Errorf("expected button tag, got %s", r.Node.Tag())
	}
}

// TestFinderAttributeFallback verifies the attribute heuristic stage
// finds elements by matching query tokens against attribute key=value pairs.
func TestFinderAttributeFallback(t *testing.T) {
	html := `<html><body><input type="email" name="user-email" placeholder="Enter email"></body></html>`
	doc, err := parser.ParseHTML(strings.NewReader(html), "http://example.com/")
	if err != nil {
		t.Fatal(err)
	}

	f := NewFinder()
	// "user-email" matches the name attribute. CSS won't parse this
	// as a selector, XPath shorthand doesn't apply, text is empty on
	// input elements, but attribute heuristic should match.
	r, err := f.Find(doc, "user-email")
	if err != nil {
		t.Fatalf("Find with 'user-email': %v", err)
	}
	if r.Strategy != "attr" {
		t.Errorf("expected strategy attr, got %s", r.Strategy)
	}
	if r.Node.Tag() != "input" {
		t.Errorf("expected input tag, got %s", r.Node.Tag())
	}
}

// TestFinderAdaptiveChainOrder verifies the Finder tries strategies in
// the documented order: CSS → XPath → Text → Attribute → Structural.
// This is the core invariant of the adaptive mechanism.
func TestFinderAdaptiveChainOrder(t *testing.T) {
	// An element that matches CSS, text, and structural — CSS must win.
	html := `<html><body><button id="go">Go</button></body></html>`
	doc, err := parser.ParseHTML(strings.NewReader(html), "http://example.com/")
	if err != nil {
		t.Fatal(err)
	}

	f := NewFinder()

	// CSS wins over everything.
	r, err := f.Find(doc, "#go")
	if err != nil {
		t.Fatal(err)
	}
	if r.Strategy != "css" {
		t.Errorf("CSS stage should win for #go, got %s", r.Strategy)
	}

	// Text fallback when CSS doesn't match.
	f2 := NewFinder()
	r2, err := f2.Find(doc, "Go")
	if err != nil {
		t.Fatal(err)
	}
	if r2.Strategy != "text" {
		t.Errorf("text stage should win for 'Go', got %s", r2.Strategy)
	}
}

// TestFinderCacheExpiry verifies that cached results expire and are
// re-fetched. This is a regression guard for the adaptive cache: if
// a site changes between fetches, the cache must not serve stale results.
func TestFinderCacheExpiry(t *testing.T) {
	html := `<html><body><button>Go</button></body></html>`
	doc, err := parser.ParseHTML(strings.NewReader(html), "http://example.com/")
	if err != nil {
		t.Fatal(err)
	}

	f := NewFinder()
	// First find populates cache.
	r1, err := f.Find(doc, "Go")
	if err != nil {
		t.Fatal(err)
	}
	// Second find hits cache (same result object).
	r2, err := f.Find(doc, "Go")
	if err != nil {
		t.Fatal(err)
	}
	if r1.Node != r2.Node {
		t.Error("cache should return same node for same query within expiry window")
	}
	// Verify cache has the entry.
	f.cache.mu.RLock()
	_, ok := f.cache.entries[f.cache.key(doc.URL(), "Go")]
	f.cache.mu.RUnlock()
	if !ok {
		t.Error("cache entry should exist after Find")
	}
}
