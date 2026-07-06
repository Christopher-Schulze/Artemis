package scraper

import (
	"strings"
	"testing"
)

// ==================== parser.go facade tests ====================

// TestTASK2250_ParseReaders verifies ParseReaders alias
// (spec L4028: parser.go - HTML parser).
func TestTASK2250_ParseReaders(t *testing.T) {
	primary := strings.NewReader("primary data")
	secondary := strings.NewReader("secondary data")
	p, s, err := ParseReaders(primary, secondary)
	if err != nil {
		t.Fatalf("ParseReaders: %v", err)
	}
	if p == nil || s == nil {
		t.Fatal("readers should not be nil")
	}
}

// TestTASK2250_NewParserPool verifies NewParserPool
// (spec L4028: parser.go - HTML parser).
func TestTASK2250_NewParserPool(t *testing.T) {
	pool := NewParserPool(4)
	if pool == nil {
		t.Fatal("pool should not be nil")
	}
	if pool.Workers() != 4 {
		t.Errorf("workers: got %d, want 4", pool.Workers())
	}
}

// TestTASK2250_NewSnapshotPool verifies NewSnapshotPool
// (spec L4028: parser.go).
func TestTASK2250_NewSnapshotPool(t *testing.T) {
	pool := NewSnapshotPool(100)
	if pool == nil {
		t.Fatal("pool should not be nil")
	}
	if pool.Cap() != 100 {
		t.Errorf("cap: got %d, want 100", pool.Cap())
	}
}

// ==================== static.go facade tests ====================

// TestTASK2250_StaticTypeAliases verifies type aliases
// (spec L4028: static.go - HTTP-only fetcher).
func TestTASK2250_StaticTypeAliases(t *testing.T) {
	var opts StaticHTTPOpts
	opts.Method = "GET"
	if opts.Method != "GET" {
		t.Error("StaticHTTPOpts alias should work")
	}
}

// TestTASK2250_ShouldRetryStatic verifies ShouldRetryStatic
// (spec L4028: static.go - HTTP-only fetcher).
func TestTASK2250_ShouldRetryStatic(t *testing.T) {
	if !ShouldRetryStatic(429) {
		t.Error("429 should retry")
	}
	if !ShouldRetryStatic(503) {
		t.Error("503 should retry")
	}
	if ShouldRetryStatic(200) {
		t.Error("200 should not retry")
	}
}

// TestTASK2250_ContentTypeAlias verifies ContentType alias
// (spec L4028: static.go - HTTP-only fetcher).
func TestTASK2250_ContentTypeAlias(t *testing.T) {
	ct := ClassifyContentType("text/html; charset=utf-8")
	if ct == "" {
		t.Error("content type should not be empty")
	}
}

// ==================== types.go tests ====================

// TestTASK2250_ExtractedPage verifies ExtractedPage type
// (spec L4028: result types).
func TestTASK2250_ExtractedPage(t *testing.T) {
	page := ExtractedPage{
		URL:            "https://example.com",
		Title:          "Example",
		Text:           "Hello World",
		ExtractionMode: "static_fetch",
	}
	if page.URL != "https://example.com" {
		t.Error("URL mismatch")
	}
	if page.IsEmpty() {
		t.Error("page with text should not be empty")
	}
}

// TestTASK2250_ExtractedPageEmpty verifies IsEmpty
// (spec L4028: result types).
func TestTASK2250_ExtractedPageEmpty(t *testing.T) {
	page := ExtractedPage{}
	if !page.IsEmpty() {
		t.Error("empty page should be empty")
	}
}

// TestTASK2250_ExtractedPageHasStructuredData verifies HasStructuredData
// (spec L4028: result types).
func TestTASK2250_ExtractedPageHasStructuredData(t *testing.T) {
	page := ExtractedPage{}
	if page.HasStructuredData() {
		t.Error("page without structured data should not have it")
	}
	page.StructuredData = []StructuredRecord{{Type: "Product"}}
	if !page.HasStructuredData() {
		t.Error("page with structured data should have it")
	}
}

// TestTASK2250_ExtractedPageString verifies String method
// (spec L4028: result types).
func TestTASK2250_ExtractedPageString(t *testing.T) {
	page := ExtractedPage{URL: "https://example.com", ExtractionMode: "static_fetch"}
	s := page.String()
	if s == "" {
		t.Error("String should not be empty")
	}
}

// TestTASK2250_SourceSnapshot verifies SourceSnapshot type
// (spec L4028: one SourceSnapshot -> ExtractedPage facade).
func TestTASK2250_SourceSnapshot(t *testing.T) {
	snap := SourceSnapshot{
		URL:  "https://example.com",
		HTML: "<html></html>",
		Mode: "static_fetch",
	}
	if snap.URL != "https://example.com" {
		t.Error("URL mismatch")
	}
	s := snap.String()
	if s == "" {
		t.Error("String should not be empty")
	}
}

// TestTASK2250_ExtractionModeConstants verifies mode constants
// (spec L4028: static_fetch, renderless_js, chromium_cdp, stealth,
// scrape).
func TestTASK2250_ExtractionModeConstants(t *testing.T) {
	if ExtractionModeStaticFetch != "static_fetch" {
		t.Error("static_fetch mismatch")
	}
	if ExtractionModeRenderlessJS != "renderless_js" {
		t.Error("renderless_js mismatch")
	}
	if ExtractionModeChromiumCDP != "chromium_cdp" {
		t.Error("chromium_cdp mismatch")
	}
	if ExtractionModeStealth != "stealth" {
		t.Error("stealth mismatch")
	}
	if ExtractionModeScrape != "scrape" {
		t.Error("scrape mismatch")
	}
}

// TestTASK2250_IsValidExtractionMode verifies validation
// (spec L4028: static_fetch, renderless_js, chromium_cdp, stealth,
// scrape).
func TestTASK2250_IsValidExtractionMode(t *testing.T) {
	if !IsValidExtractionMode("static_fetch") {
		t.Error("static_fetch should be valid")
	}
	if !IsValidExtractionMode("stealth") {
		t.Error("stealth should be valid")
	}
	if IsValidExtractionMode("invalid") {
		t.Error("invalid should not be valid")
	}
}

// TestTASK2250_ExtractedLink verifies ExtractedLink type
// (spec L4028: result types - links).
func TestTASK2250_ExtractedLink(t *testing.T) {
	link := ExtractedLink{Href: "https://example.com", Text: "Example"}
	if link.Href != "https://example.com" {
		t.Error("href mismatch")
	}
}

// TestTASK2250_ExtractedForm verifies ExtractedForm type
// (spec L4028: result types - forms).
func TestTASK2250_ExtractedForm(t *testing.T) {
	form := ExtractedForm{
		Action: "/submit",
		Method: "POST",
		Fields: []ExtractedField{{Name: "email", Type: "text"}},
	}
	if len(form.Fields) != 1 {
		t.Error("should have 1 field")
	}
}

// TestTASK2250_ExtractedTable verifies ExtractedTable type
// (spec L4028: result types - tables).
func TestTASK2250_ExtractedTable(t *testing.T) {
	table := ExtractedTable{
		Headers: []string{"Name", "Value"},
		Rows:    [][]string{{"a", "1"}, {"b", "2"}},
	}
	if len(table.Headers) != 2 {
		t.Error("should have 2 headers")
	}
	if len(table.Rows) != 2 {
		t.Error("should have 2 rows")
	}
}

// TestTASK2250_ExtractedImage verifies ExtractedImage type
// (spec L4028: result types - images).
func TestTASK2250_ExtractedImage(t *testing.T) {
	img := ExtractedImage{Src: "https://example.com/img.png", Alt: "Logo"}
	if img.Src != "https://example.com/img.png" {
		t.Error("src mismatch")
	}
}

// ==================== translator.go tests ====================

// TestTASK2250_CSSToXPathTag verifies tag selector
// (spec L4028: CSS -> XPath translation).
func TestTASK2250_CSSToXPathTag(t *testing.T) {
	xpath, err := CSSToXPath("div")
	if err != nil {
		t.Fatalf("CSSToXPath: %v", err)
	}
	if !strings.Contains(xpath, "div") {
		t.Errorf("xpath should contain 'div': got %s", xpath)
	}
}

// TestTASK2250_CSSToXPathClass verifies class selector
// (spec L4028: CSS -> XPath translation).
func TestTASK2250_CSSToXPathClass(t *testing.T) {
	xpath, err := CSSToXPath("div.container")
	if err != nil {
		t.Fatalf("CSSToXPath: %v", err)
	}
	if !strings.Contains(xpath, "@class") {
		t.Errorf("xpath should contain @class: got %s", xpath)
	}
}

// TestTASK2250_CSSToXPathID verifies ID selector
// (spec L4028: CSS -> XPath translation).
func TestTASK2250_CSSToXPathID(t *testing.T) {
	xpath, err := CSSToXPath("div#main")
	if err != nil {
		t.Fatalf("CSSToXPath: %v", err)
	}
	if !strings.Contains(xpath, "@id='main'") {
		t.Errorf("xpath should contain @id='main': got %s", xpath)
	}
}

// TestTASK2250_CSSToXPathAttribute verifies attribute selector
// (spec L4028: CSS -> XPath translation).
func TestTASK2250_CSSToXPathAttribute(t *testing.T) {
	xpath, err := CSSToXPath("a[href='https://example.com']")
	if err != nil {
		t.Fatalf("CSSToXPath: %v", err)
	}
	if !strings.Contains(xpath, "@href='https://example.com'") {
		t.Errorf("xpath should contain @href: got %s", xpath)
	}
}

// TestTASK2250_CSSToXPathEmpty verifies empty selector errors
// (spec L4028: CSS -> XPath translation).
func TestTASK2250_CSSToXPathEmpty(t *testing.T) {
	_, err := CSSToXPath("")
	if err == nil {
		t.Error("empty selector should error")
	}
}

// TestTASK2250_IsXPath verifies XPath detection
// (spec L4028: CSS -> XPath translation).
func TestTASK2250_IsXPath(t *testing.T) {
	if !IsXPath("//div[@id='main']") {
		t.Error("//div should be detected as XPath")
	}
	if IsXPath("div.container") {
		t.Error("div.container should NOT be detected as XPath")
	}
}

// TestTASK2250_IsCSS verifies CSS detection
// (spec L4028: CSS -> XPath translation).
func TestTASK2250_IsCSS(t *testing.T) {
	if !IsCSS("div.container") {
		t.Error("div.container should be detected as CSS")
	}
	if IsCSS("//div[@id='main']") {
		t.Error("//div should NOT be detected as CSS")
	}
}

// TestTASK2250_TranslateSelectorCSS verifies auto-detect CSS
// (spec L4028: CSS -> XPath translation).
func TestTASK2250_TranslateSelectorCSS(t *testing.T) {
	xpath, err := TranslateSelector("div#main")
	if err != nil {
		t.Fatalf("TranslateSelector: %v", err)
	}
	if !strings.Contains(xpath, "@id='main'") {
		t.Errorf("should contain @id='main': got %s", xpath)
	}
}

// TestTASK2250_TranslateSelectorXPath verifies auto-detect XPath
// (spec L4028: CSS -> XPath translation).
func TestTASK2250_TranslateSelectorXPath(t *testing.T) {
	input := "//div[@id='main']"
	xpath, err := TranslateSelector(input)
	if err != nil {
		t.Fatalf("TranslateSelector: %v", err)
	}
	if xpath != input {
		t.Errorf("XPath should pass through: got %s, want %s", xpath, input)
	}
}

// TestTASK2250_TranslateSelectorEmpty verifies empty selector errors.
func TestTASK2250_TranslateSelectorEmpty(t *testing.T) {
	_, err := TranslateSelector("")
	if err == nil {
		t.Error("empty selector should error")
	}
}

// ==================== structured_extract.go facade tests ====================

// TestTASK2250_StructuredExtractSchemaAlias verifies alias
// (spec L4028: structured_extract.go - Structured-Extract).
func TestTASK2250_StructuredExtractSchemaAlias(t *testing.T) {
	var schema StructuredExtractSchema
	schema.Kind = "text"
	if schema.Kind != "text" {
		t.Error("StructuredExtractSchema alias should work")
	}
}

// TestTASK2250_ExtractJSONLDFromHTML verifies JSON-LD extraction
// (spec L4028: structured_extract.go - Structured-Extract).
func TestTASK2250_ExtractJSONLDFromHTML(t *testing.T) {
	html := `<html><head>
	<script type="application/ld+json">{"@type":"Product","name":"Test"}</script>
	</head></html>`
	records, err := ExtractJSONLDFromHTML(html)
	if err != nil {
		t.Fatalf("ExtractJSONLDFromHTML: %v", err)
	}
	if len(records) == 0 {
		t.Error("should extract at least 1 record")
	}
}

// TestTASK2250_ExtractOpenGraphFromHTML verifies OpenGraph extraction
// (spec L4028: structured_extract.go - Structured-Extract).
func TestTASK2250_ExtractOpenGraphFromHTML(t *testing.T) {
	html := `<html><head>
	<meta property="og:title" content="Example Page">
	<meta property="og:url" content="https://example.com">
	</head></html>`
	og := ExtractOpenGraphFromHTML(html)
	if og["og:title"] != "Example Page" {
		t.Errorf("og:title: got %s, want 'Example Page'", og["og:title"])
	}
}

// TestTASK2250_ScalarSchemaKinds verifies ScalarSchemaKinds re-export
// (spec L4028: structured_extract.go - scalarKinds).
func TestTASK2250_ScalarSchemaKinds(t *testing.T) {
	if !ScalarSchemaKinds["text"] {
		t.Error("text should be a scalar kind")
	}
}

// TestTASK2250_JoinSchemaKinds verifies JoinSchemaKinds re-export
// (spec L4028: structured_extract.go - joinKinds).
func TestTASK2250_JoinSchemaKinds(t *testing.T) {
	if !JoinSchemaKinds["text"] {
		t.Error("text should be a join kind")
	}
}

// ==================== snapshot_extract.go tests ====================

// TestTASK2250_NewSnapshotExtractor verifies creation
// (spec L4028: snapshot_extract.go - renderless DOM snapshot).
func TestTASK2250_NewSnapshotExtractor(t *testing.T) {
	e := NewSnapshotExtractor(ExtractionModeStaticFetch)
	if e == nil {
		t.Fatal("extractor should not be nil")
	}
	if e.Mode() != ExtractionModeStaticFetch {
		t.Error("mode mismatch")
	}
}

// TestTASK2250_SnapshotExtractorExtract verifies extraction
// (spec L4028: one SourceSnapshot -> ExtractedPage facade).
func TestTASK2250_SnapshotExtractorExtract(t *testing.T) {
	e := NewSnapshotExtractor(ExtractionModeStaticFetch)
	snap := SourceSnapshot{
		URL:  "https://example.com",
		HTML: `<html><head><title>Test Page</title></head><body><p>Hello World</p><a href="https://link.com">Link</a><img src="img.png" alt="Image"></body></html>`,
		Mode: "static_fetch",
	}
	page, err := e.Extract(snap)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if page.Title != "Test Page" {
		t.Errorf("title: got %s, want 'Test Page'", page.Title)
	}
	if !strings.Contains(page.Text, "Hello World") {
		t.Errorf("text should contain 'Hello World': got %s", page.Text)
	}
	if len(page.Links) == 0 {
		t.Error("should extract links")
	}
	if len(page.Images) == 0 {
		t.Error("should extract images")
	}
}

// TestTASK2250_SnapshotExtractorEmptyHTML verifies empty HTML errors
// (spec L4028: snapshot_extract.go).
func TestTASK2250_SnapshotExtractorEmptyHTML(t *testing.T) {
	e := NewSnapshotExtractor(ExtractionModeStaticFetch)
	_, err := e.Extract(SourceSnapshot{URL: "https://example.com"})
	if err == nil {
		t.Error("empty HTML should error")
	}
}

// TestTASK2250_SnapshotExtractorNilSafe verifies nil extractor is safe.
func TestTASK2250_SnapshotExtractorNilSafe(t *testing.T) {
	var e *SnapshotExtractor
	_, err := e.Extract(SourceSnapshot{HTML: "<html></html>"})
	if err == nil {
		t.Error("nil should error")
	}
	if e.Mode() != "" {
		t.Error("nil mode should be empty")
	}
}

// TestTASK2250_SnapshotExtractorMode verifies Mode
// (spec L4028: snapshot_extract.go).
func TestTASK2250_SnapshotExtractorMode(t *testing.T) {
	e := NewSnapshotExtractor(ExtractionModeChromiumCDP)
	if e.Mode() != ExtractionModeChromiumCDP {
		t.Errorf("mode: got %s, want chromium_cdp", e.Mode())
	}
}

// TestTASK2250_ExtractTitle verifies title extraction
// (spec L4028: snapshot_extract.go).
func TestTASK2250_ExtractTitle(t *testing.T) {
	html := `<html><head><title>My Title</title></head></html>`
	title := extractTitle(html)
	if title != "My Title" {
		t.Errorf("title: got %s, want 'My Title'", title)
	}
}

// TestTASK2250_ExtractTitleMissing verifies missing title.
func TestTASK2250_ExtractTitleMissing(t *testing.T) {
	html := `<html><head></head></html>`
	title := extractTitle(html)
	if title != "" {
		t.Errorf("title: got %s, want empty", title)
	}
}

// TestTASK2250_ExtractText verifies text extraction
// (spec L4028: snapshot_extract.go).
func TestTASK2250_ExtractText(t *testing.T) {
	html := `<html><body><p>Hello</p><script>var x=1;</script><p>World</p></body></html>`
	text := extractText(html)
	if strings.Contains(text, "var x=1") {
		t.Error("script content should be stripped")
	}
	if !strings.Contains(text, "Hello") || !strings.Contains(text, "World") {
		t.Error("text should contain Hello and World")
	}
}

// TestTASK2250_ExtractLinks verifies link extraction
// (spec L4028: snapshot_extract.go).
func TestTASK2250_ExtractLinks(t *testing.T) {
	html := `<html><body><a href="https://a.com">A</a><a href="https://b.com">B</a></body></html>`
	links := extractLinks(html)
	if len(links) != 2 {
		t.Errorf("links: got %d, want 2", len(links))
	}
	if links[0].Href != "https://a.com" {
		t.Errorf("href: got %s, want https://a.com", links[0].Href)
	}
}

// TestTASK2250_ExtractImages verifies image extraction
// (spec L4028: snapshot_extract.go).
func TestTASK2250_ExtractImages(t *testing.T) {
	html := `<html><body><img src="logo.png" alt="Logo"></body></html>`
	images := extractImages(html)
	if len(images) != 1 {
		t.Errorf("images: got %d, want 1", len(images))
	}
	if images[0].Src != "logo.png" {
		t.Errorf("src: got %s, want logo.png", images[0].Src)
	}
	if images[0].Alt != "Logo" {
		t.Errorf("alt: got %s, want Logo", images[0].Alt)
	}
}

// ==================== full spec parity test ====================

// TestTASK2250_FullSpecParity verifies all 6 spec-mandated files
// (spec L4028: parser.go, static.go, types.go, translator.go,
// structured_extract.go, snapshot_extract.go).
func TestTASK2250_FullSpecParity(t *testing.T) {
	// 1. parser.go - HTML parser
	pool := NewParserPool(4)
	if pool == nil {
		t.Error("parser.go: pool should not be nil")
	}

	// 2. static.go - HTTP-only fetcher
	if !ShouldRetryStatic(503) {
		t.Error("static.go: 503 should retry")
	}

	// 3. types.go - result types
	page := ExtractedPage{URL: "https://example.com", Text: "content"}
	if page.IsEmpty() {
		t.Error("types.go: page with content should not be empty")
	}

	// 4. translator.go - CSS -> XPath translation
	xpath, err := CSSToXPath("div#main")
	if err != nil {
		t.Error("translator.go: CSSToXPath should work")
	}
	if !strings.Contains(xpath, "@id") {
		t.Error("translator.go: should contain @id")
	}

	// 5. structured_extract.go - Structured-Extract
	if !ScalarSchemaKinds["text"] {
		t.Error("structured_extract.go: text should be scalar kind")
	}

	// 6. snapshot_extract.go - snapshot extraction
	e := NewSnapshotExtractor(ExtractionModeStaticFetch)
	snap := SourceSnapshot{
		URL:  "https://example.com",
		HTML: `<html><head><title>Test</title></head><body>Hello</body></html>`,
	}
	extracted, err := e.Extract(snap)
	if err != nil || extracted.Title != "Test" {
		t.Error("snapshot_extract.go: extraction should work")
	}
}
