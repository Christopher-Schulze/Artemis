package agent

import (
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/parser"
)

func TestStructuredJSONLDSingle(t *testing.T) {
	src := `<html><head><title>P</title>
<script type="application/ld+json">{"@context":"https://schema.org","@type":"Product","name":"Widget","price":"9.99"}</script>
</head></html>`
	doc, _ := parser.ParseHTML(strings.NewReader(src), "")
	s := Structured(doc)
	if len(s.JSONLD) != 1 {
		t.Fatalf("JSONLD count = %d", len(s.JSONLD))
	}
	if s.JSONLD[0]["name"] != "Widget" {
		t.Errorf("name = %v", s.JSONLD[0]["name"])
	}
	if s.Title != "P" {
		t.Errorf("title = %q", s.Title)
	}
}

func TestStructuredJSONLDArray(t *testing.T) {
	src := `<script type="application/ld+json">[{"@type":"A"},{"@type":"B"}]</script>`
	doc, _ := parser.ParseHTML(strings.NewReader(src), "")
	s := Structured(doc)
	if len(s.JSONLD) != 2 {
		t.Errorf("array len = %d", len(s.JSONLD))
	}
}

func TestStructuredOpenGraphAndTwitter(t *testing.T) {
	src := `<html><head>
		<meta property="og:title" content="A Title">
		<meta property="og:image" content="https://e.test/i.png">
		<meta name="twitter:card" content="summary">
		<meta name="twitter:site" content="@x">
		<meta name="description" content="some desc">
	</head></html>`
	doc, _ := parser.ParseHTML(strings.NewReader(src), "")
	s := Structured(doc)
	if s.OpenGraph["title"] != "A Title" {
		t.Errorf("og:title = %q", s.OpenGraph["title"])
	}
	if s.OpenGraph["image"] != "https://e.test/i.png" {
		t.Errorf("og:image missing")
	}
	if s.Twitter["card"] != "summary" {
		t.Errorf("twitter:card = %q", s.Twitter["card"])
	}
	if s.Twitter["site"] != "@x" {
		t.Errorf("twitter:site = %q", s.Twitter["site"])
	}
	if s.Meta["description"] != "some desc" {
		t.Errorf("meta description = %q", s.Meta["description"])
	}
}

func TestStructuredMicrodata(t *testing.T) {
	src := `<html><head></head><body>
<div itemscope itemtype="https://schema.org/Product">
  <span itemprop="name">Widget</span>
  <span itemprop="price">9.99</span>
</div>
</body></html>`
	doc, _ := parser.ParseHTML(strings.NewReader(src), "")
	s := Structured(doc)
	if len(s.Microdata) != 1 {
		t.Fatalf("expected 1 microdata item, got %d", len(s.Microdata))
	}
	if s.Microdata[0].Type != "https://schema.org/Product" {
		t.Errorf("type = %q", s.Microdata[0].Type)
	}
	if s.Microdata[0].Props["name"] != "Widget" {
		t.Errorf("name = %q", s.Microdata[0].Props["name"])
	}
}

func TestStructuredRDFa(t *testing.T) {
	src := `<html><head></head><body>
<div typeof="schema:Product">
  <span property="schema:name">Gadget</span>
</div>
</body></html>`
	doc, _ := parser.ParseHTML(strings.NewReader(src), "")
	s := Structured(doc)
	found := false
	for _, r := range s.RDFa {
		if r.Property["schema:name"] == "Gadget" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected RDFa property schema:name=Gadget, got %+v", s.RDFa)
	}
}

func TestStructuredHandlesMalformedJSON(t *testing.T) {
	src := `<script type="application/ld+json">{"broken</script>`
	doc, _ := parser.ParseHTML(strings.NewReader(src), "")
	s := Structured(doc)
	if len(s.JSONLD) != 0 {
		t.Errorf("expected no JSONLD on malformed body, got %d", len(s.JSONLD))
	}
}
