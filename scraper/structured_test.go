package scraper

import (
	"testing"
)

func TestStructuredJSONLD(t *testing.T) {
	html := `<!DOCTYPE html><html><head>
<script type="application/ld+json">{"@type":"Product","name":"Widget","offers":{"price":"9.99"},"aggregateRating":{"ratingValue":"4.5"}}</script>
</head><body></body></html>`
	recs, err := ExtractJSONLD(html)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(recs))
	}
	if recs[0].Name != "Widget" || recs[0].Price != "9.99" || recs[0].Rating != "4.5" {
		t.Fatalf("unexpected record: %+v", recs[0])
	}
	og := ExtractOpenGraph(`<meta property="og:title" content="T">`)
	if og["og:title"] != "T" {
		t.Fatalf("og: %+v", og)
	}
}
