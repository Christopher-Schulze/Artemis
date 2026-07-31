package scraper

import (
	"strings"
	"testing"
)

func TestExtractStructuredHTMLNestedObjectAndList(t *testing.T) {
	schema := &StructuredSchema{
		Kind:     KindObject,
		Selector: "article.product",
		FieldsMap: map[string]*StructuredSchema{
			"name": {Kind: KindText, Selector: "h1", IsRequired: true, Trim: true},
			"tags": {
				Kind:     KindList,
				Selector: "ul.tags > li",
				Item:     &StructuredSchema{Kind: KindText, Selector: "li", Trim: true},
			},
			"offer": {
				Kind:     KindObject,
				Selector: "div.offer",
				FieldsMap: map[string]*StructuredSchema{
					"price": {Kind: KindNumber, Selector: "span.price", IsRequired: true},
					"link":  {Kind: KindURL, Selector: "a", Attr: "href", IsRequired: true},
				},
			},
		},
	}
	htmlDoc := `<article class="product"><h1>  Widget </h1><ul class="tags"><li>one</li><li>two</li></ul><div class="offer"><span class="price">€1,234.50</span><a href="/buy">buy</a></div></article>`
	result, err := ExtractStructuredHTML(htmlDoc, schema, "https://example.test/catalog")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if result.MatchedCount != 1 {
		t.Fatalf("matched count = %d, want 1", result.MatchedCount)
	}
	if len(result.EvidenceRefs) != 1 || !strings.HasPrefix(result.EvidenceRefs[0], "schema:") {
		t.Fatalf("schema evidence = %#v", result.EvidenceRefs)
	}
	value, ok := result.ExtractedValue.(map[string]interface{})
	if !ok {
		t.Fatalf("value type = %T", result.ExtractedValue)
	}
	if value["name"] != "Widget" {
		t.Fatalf("name = %#v", value["name"])
	}
	tags, ok := value["tags"].([]interface{})
	if !ok || len(tags) != 2 {
		t.Fatalf("tags = %#v", value["tags"])
	}
	if tags[0] != "one" || tags[1] != "two" {
		t.Fatalf("tag extraction = %#v, want [one two]", tags)
	}
	offer, ok := value["offer"].(map[string]interface{})
	if !ok || offer["price"] != 1234.50 || offer["link"] != "https://example.test/buy" {
		t.Fatalf("offer = %#v", offer)
	}
}

func TestExtractStructuredHTMLListItems(t *testing.T) {
	schema := &StructuredSchema{
		Kind:       KindList,
		Selector:   "ul.items > li",
		IsRequired: true,
		Item: &StructuredSchema{
			Kind:     KindObject,
			Selector: "li",
			FieldsMap: map[string]*StructuredSchema{
				"title": {Kind: KindText, Selector: ".title", IsRequired: true, Trim: true},
			},
		},
	}
	result, err := ExtractStructured(`<ul class="items"><li><span class="title">A</span></li><li><span class="title">B</span></li></ul>`, schema)
	if err != nil {
		t.Fatalf("extract list: %v", err)
	}
	if result.MatchedCount != 2 {
		t.Fatalf("root matched count = %d, want 2", result.MatchedCount)
	}
	values, ok := result.ExtractedValue.([]interface{})
	if !ok || len(values) != 2 {
		t.Fatalf("list value = %#v", result.ExtractedValue)
	}
	first, firstOK := values[0].(map[string]interface{})
	second, secondOK := values[1].(map[string]interface{})
	if !firstOK || !secondOK || first["title"] != "A" || second["title"] != "B" {
		t.Fatalf("list items = %#v", values)
	}
}

func TestExtractStructuredHTMLRejectsInvalidCoercion(t *testing.T) {
	schema := &StructuredSchema{
		Kind:     KindObject,
		Selector: ".product",
		FieldsMap: map[string]*StructuredSchema{
			"price": {Kind: KindNumber, Selector: ".price", IsRequired: true},
		},
	}
	result, err := ExtractStructured(`<div class="product"><span class="price">12 dollars</span></div>`, schema)
	if err == nil || result == nil || !strings.Contains(err.Error(), "invalid number") {
		t.Fatalf("invalid number must fail closed: result=%#v err=%v", result, err)
	}
}

func TestExtractStructuredHTMLRequiredAndSelectorValidation(t *testing.T) {
	missing := &StructuredSchema{Kind: KindObject, Selector: ".product", FieldsMap: map[string]*StructuredSchema{
		"name": {Kind: KindText, Selector: "h1", IsRequired: true},
	}}
	result, err := ExtractStructured(`<div class="product"></div>`, missing)
	if err == nil || result == nil || !strings.Contains(err.Error(), "required selector") {
		t.Fatalf("missing required field must fail: result=%#v err=%v", result, err)
	}
	badSelector := &StructuredSchema{Kind: KindObject, Selector: ".product{bad}", FieldsMap: map[string]*StructuredSchema{
		"name": {Kind: KindText, Selector: "h1"},
	}}
	if err := ValidateSchema(badSelector); err == nil {
		t.Fatal("forbidden selector syntax must fail validation")
	}
}

func TestResolveStructuredURLRejectsUnsafeScheme(t *testing.T) {
	if _, err := resolveStructuredURL("javascript:alert(1)", ""); err == nil {
		t.Fatal("javascript URL must be rejected")
	}
}

func TestExtractStructuredHTMLReturnsInnerHTML(t *testing.T) {
	schema := &StructuredSchema{Kind: KindObject, Selector: ".card", FieldsMap: map[string]*StructuredSchema{
		"body": {Kind: KindHTML, Selector: ".content", IsRequired: true},
	}}
	result, err := ExtractStructured(`<div class="card"><div class="content"><strong>inside</strong></div></div>`, schema)
	if err != nil {
		t.Fatal(err)
	}
	value, ok := result.ExtractedValue.(map[string]interface{})
	if !ok || value["body"] != "<strong>inside</strong>" {
		t.Fatalf("inner HTML = %#v", result.ExtractedValue)
	}
}
