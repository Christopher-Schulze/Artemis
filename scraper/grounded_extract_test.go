package scraper

import (
	"strings"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/security"
)

const groundedProduct = `<!doctype html><html><body>
<div class="product"><h1 class="name"> Lamp  Deluxe </h1>
<span class="sku">L-1</span><span class="sku">L-1</span>
<span class="price">$1,299.00</span><span class="price">$1,199.00</span>
<span class="stock">many</span>
<a class="link" href="/lamp">details</a></div>
<table><tr class="row"><td class="cell">a</td></tr><tr class="row"><td class="cell">b</td></tr></table>
</body></html>`

func groundedSource() ExtractionSource {
	return ExtractionSource{URL: "https://shop.example/p/1", CapturedAt: time.Unix(1700000000, 0).UTC(), Trust: security.ContentTrust{Origin: "https://shop.example", Source: "test", Trust: security.TrustUntrusted}}
}

func fieldByPath(t *testing.T, result *GroundedExtraction, path string) FieldEvidence {
	t.Helper()
	for _, field := range result.Fields {
		if field.Path == path {
			return field
		}
	}
	t.Fatalf("no field %s in %+v", path, result.Fields)
	return FieldEvidence{}
}

func TestGroundedHTMLKeepsEvidenceAndSeparatesMissingConflictInvalid(t *testing.T) {
	schema := &StructuredSchema{Kind: KindObject, Selector: ".product", FieldsMap: map[string]*StructuredSchema{
		"name":   {Kind: KindText, Selector: ".name", Trim: true, IsRequired: true},
		"sku":    {Kind: KindText, Selector: ".sku", IsRequired: true},
		"price":  {Kind: KindNumber, Selector: ".price", IsRequired: true},
		"stock":  {Kind: KindNumber, Selector: ".stock"},
		"rating": {Kind: KindText, Selector: ".rating", IsRequired: true},
		"link":   {Kind: KindURL, Selector: ".link"},
	}}
	result, err := ExtractGroundedHTML(groundedProduct, schema, groundedSource())
	if err != nil {
		t.Fatal(err)
	}
	source := result.Sources[0]
	if source.Kind != SourceDOM || source.Bytes != len(groundedProduct) || len(source.ContentHash) != 64 || source.Trust.Origin != "https://shop.example" || !source.CapturedAt.Equal(time.Unix(1700000000, 0)) {
		t.Fatalf("source evidence incomplete: %+v", source)
	}
	name := fieldByPath(t, result, "$.name")
	if name.Status != FieldFound || name.Value != "Lamp  Deluxe" || name.Selector != ".name" || !strings.HasSuffix(name.NodePath, "h1:nth-of-type(1)") || name.Excerpt != "Lamp Deluxe" {
		t.Fatalf("found field lacks grounding: %+v", name)
	}
	if sku := fieldByPath(t, result, "$.sku"); sku.Status != FieldFound || sku.Value != "L-1" || sku.Matches != 2 {
		t.Fatalf("agreeing duplicates: %+v", sku)
	}
	price := fieldByPath(t, result, "$.price")
	if price.Status != FieldConflict || price.Value != nil || len(price.Candidates) != 2 || price.Candidates[0] != 1299.0 || price.Candidates[1] != 1199.0 {
		t.Fatalf("conflicting values were not exposed: %+v", price)
	}
	if stock := fieldByPath(t, result, "$.stock"); stock.Status != FieldInvalid || !strings.Contains(stock.Reason, "invalid number") {
		t.Fatalf("coercion failure: %+v", stock)
	}
	if rating := fieldByPath(t, result, "$.rating"); rating.Status != FieldMissing || rating.Value != nil {
		t.Fatalf("missing field: %+v", rating)
	}
	if link := fieldByPath(t, result, "$.link"); link.Value != "https://shop.example/lamp" {
		t.Fatalf("URL not resolved against the source: %+v", link)
	}
	value := result.Value.(map[string]any)
	if _, guessed := value["price"]; guessed {
		t.Fatalf("conflict became a value: %+v", value)
	}
	if _, guessed := value["rating"]; guessed {
		t.Fatalf("missing field became a value: %+v", value)
	}
	if result.Complete || result.Counts != (FieldCounts{Found: 3, Missing: 1, Conflict: 1, Invalid: 1}) {
		t.Fatalf("counts/completeness = %+v %v", result.Counts, result.Complete)
	}

	optional := &StructuredSchema{Kind: KindObject, Selector: ".product", FieldsMap: map[string]*StructuredSchema{
		"name":   {Kind: KindText, Selector: ".name", Trim: true, IsRequired: true},
		"rating": {Kind: KindText, Selector: ".rating"},
	}}
	if ok, err := ExtractGroundedHTML(groundedProduct, optional, groundedSource()); err != nil || !ok.Complete || ok.Counts.Missing != 1 {
		t.Fatalf("optional absence must not break completeness: %+v %v", ok, err)
	}
	truncated := groundedSource()
	truncated.Truncated = true
	cut, err := ExtractGroundedHTML(groundedProduct[:120], optional, truncated)
	if err != nil {
		t.Fatal(err)
	}
	if rating := fieldByPath(t, cut, "$.rating"); rating.Status != FieldTruncated || cut.Complete {
		t.Fatalf("absence in a truncated source was claimed as missing: %+v %v", rating, cut.Complete)
	}
}

func TestGroundedHTMLListsAndAmbiguousContainers(t *testing.T) {
	rows := &StructuredSchema{Kind: KindList, Selector: ".row", Item: &StructuredSchema{Kind: KindObject, Selector: ".row", FieldsMap: map[string]*StructuredSchema{"cell": {Kind: KindText, Selector: ".cell", IsRequired: true}}}}
	result, err := ExtractGroundedHTML(groundedProduct, rows, groundedSource())
	if err != nil {
		t.Fatal(err)
	}
	if values := result.Value.([]any); len(values) != 2 || fieldByPath(t, result, "$[1].cell").Value != "b" || !result.Complete {
		t.Fatalf("list extraction = %+v", result)
	}
	ambiguous := &StructuredSchema{Kind: KindObject, Selector: "span", FieldsMap: map[string]*StructuredSchema{"text": {Kind: KindText, Selector: "span"}}}
	conflict, err := ExtractGroundedHTML(groundedProduct, ambiguous, groundedSource())
	if err != nil {
		t.Fatal(err)
	}
	if root := fieldByPath(t, conflict, "$"); root.Status != FieldConflict || conflict.Value != nil || conflict.Complete {
		t.Fatalf("ambiguous container was resolved silently: %+v", conflict)
	}
	if _, err := ExtractGroundedHTML(groundedProduct, &StructuredSchema{Kind: KindText, Selector: "h1"}, groundedSource()); err == nil {
		t.Fatal("invalid root schema accepted")
	}
}

func TestGroundedJSONWithholdsCredentialsAndCoercesSchemaKinds(t *testing.T) {
	body := []byte(`{"token":"secret-value","data":{"items":[{"name":"Lamp","price":"12.50","url":"/l","stock":true},{"name":"Desk","price":7,"url":"/d"}]}}`)
	schema := JSONExtractSchema{Items: "/data/items", Fields: map[string]JSONExtractField{
		"name":  {Pointer: "/name", Kind: "string", Required: true},
		"price": {Pointer: "/price", Kind: "number", Required: true},
		"url":   {Pointer: "/url", Kind: "url"},
		"stock": {Pointer: "/stock", Kind: "boolean", Required: true},
	}}
	source := groundedSource()
	result, err := ExtractGroundedJSON(body, schema, source)
	if err != nil {
		t.Fatal(err)
	}
	if price := fieldByPath(t, result, "$[0].price"); price.Status != FieldFound || price.Value != 12.5 || price.Selector != "/data/items/0/price" || price.Excerpt != `"12.50"` {
		t.Fatalf("coerced JSON field = %+v", price)
	}
	if link := fieldByPath(t, result, "$[1].url"); link.Value != "https://shop.example/d" {
		t.Fatalf("relative URL = %+v", link)
	}
	if stock := fieldByPath(t, result, "$[1].stock"); stock.Status != FieldMissing || result.Complete {
		t.Fatalf("missing required JSON field = %+v complete=%v", stock, result.Complete)
	}
	if result.Sources[0].Kind != SourceJSONResponse || result.Sources[0].Bytes != len(body) {
		t.Fatalf("JSON source = %+v", result.Sources[0])
	}

	secrets := JSONExtractSchema{Fields: map[string]JSONExtractField{"token": {Pointer: "/token"}, "first": {Pointer: "/data/items/0/name"}}}
	withheld, err := ExtractGroundedJSON(body, secrets, source)
	if err != nil {
		t.Fatal(err)
	}
	if token := fieldByPath(t, withheld, "$.token"); token.Status != FieldWithheld || token.Value != nil || token.Excerpt != "" {
		t.Fatalf("credential value was extracted: %+v", token)
	}
	if strings.Contains(strings.Join([]string{withheld.Fields[0].Excerpt, withheld.Fields[1].Excerpt}, ""), "secret-value") {
		t.Fatal("credential value leaked into an excerpt")
	}

	cut := source
	cut.Truncated = true
	truncated, err := ExtractGroundedJSON(body[:40], schema, cut)
	if err != nil {
		t.Fatal(err)
	}
	if truncated.Counts.Truncated != 4 || truncated.Complete {
		t.Fatalf("truncated response = %+v", truncated.Counts)
	}
	if _, err := ExtractGroundedJSON(body, JSONExtractSchema{Fields: map[string]JSONExtractField{"x": {Pointer: "name"}}}, source); err == nil {
		t.Fatal("relative pointer accepted")
	}
}

func TestMergeGroundedPagesRequiresExhaustedPagination(t *testing.T) {
	rows := &StructuredSchema{Kind: KindList, Selector: ".row", Item: &StructuredSchema{Kind: KindObject, Selector: ".row", FieldsMap: map[string]*StructuredSchema{"cell": {Kind: KindText, Selector: ".cell"}}}}
	first, err := ExtractGroundedHTML(groundedProduct, rows, groundedSource())
	if err != nil {
		t.Fatal(err)
	}
	second, err := ExtractGroundedHTML(groundedProduct, rows, groundedSource())
	if err != nil {
		t.Fatal(err)
	}
	merged := MergeGroundedPages([]*GroundedExtraction{first, second}, "no_next_page", true)
	if len(merged.Value.([]any)) != 4 || len(merged.Sources) != 2 || merged.Fields[len(merged.Fields)-1].Source != 1 || !merged.Complete || merged.Counts.Found != 4 {
		t.Fatalf("merged pages = %+v", merged)
	}
	if capped := MergeGroundedPages([]*GroundedExtraction{first}, "max_pages", false); capped.Complete {
		t.Fatal("a capped pagination claimed completeness")
	}
}
