package scraper

import (
	"strings"
	"testing"
)

// ==================== parser.go facade tests ====================

// TestParseReaders verifies ParseReaders alias
// (parser.go - HTML parser).
func TestParseReaders(t *testing.T) {
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

// TestNewParserPool verifies NewParserPool
// (parser.go - HTML parser).
func TestNewParserPool(t *testing.T) {
	pool := NewParserPool(4)
	if pool == nil {
		t.Fatal("pool should not be nil")
	}
	if pool.Workers() != 4 {
		t.Errorf("workers: got %d, want 4", pool.Workers())
	}
}

// TestNewSnapshotPool verifies NewSnapshotPool
// (parser.go).
func TestNewSnapshotPool(t *testing.T) {
	pool := NewSnapshotPool(100)
	if pool == nil {
		t.Fatal("pool should not be nil")
	}
	if pool.Cap() != 100 {
		t.Errorf("cap: got %d, want 100", pool.Cap())
	}
}

// ==================== static.go facade tests ====================

// TestStaticTypeAliases verifies type aliases
// (static.go - HTTP-only fetcher).
func TestStaticTypeAliases(t *testing.T) {
	var opts StaticHTTPOpts
	opts.Method = "GET"
	if opts.Method != "GET" {
		t.Error("StaticHTTPOpts alias should work")
	}
}

// TestShouldRetryStatic verifies ShouldRetryStatic
// (static.go - HTTP-only fetcher).
func TestShouldRetryStatic(t *testing.T) {
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

// TestContentTypeAlias verifies ContentType alias
// (static.go - HTTP-only fetcher).
func TestContentTypeAlias(t *testing.T) {
	ct := ClassifyContentType("text/html; charset=utf-8")
	if ct == "" {
		t.Error("content type should not be empty")
	}
}

// ==================== types.go tests ====================

// TestExtractedPage verifies ExtractedPage type
// (result types).
func TestExtractedPage(t *testing.T) {
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

// TestExtractedPageEmpty verifies IsEmpty
// (result types).
func TestExtractedPageEmpty(t *testing.T) {
	page := ExtractedPage{}
	if !page.IsEmpty() {
		t.Error("empty page should be empty")
	}
}

// TestExtractedPageHasStructuredData verifies HasStructuredData
// (result types).
func TestExtractedPageHasStructuredData(t *testing.T) {
	page := ExtractedPage{}
	if page.HasStructuredData() {
		t.Error("page without structured data should not have it")
	}
	page.StructuredData = []StructuredRecord{{Type: "Product"}}
	if !page.HasStructuredData() {
		t.Error("page with structured data should have it")
	}
}

// TestExtractedPageString verifies String method
// (result types).
func TestExtractedPageString(t *testing.T) {
	page := ExtractedPage{URL: "https://example.com", ExtractionMode: "static_fetch"}
	s := page.String()
	if s == "" {
		t.Error("String should not be empty")
	}
}

// TestSourceSnapshot verifies SourceSnapshot type
// (one SourceSnapshot -> ExtractedPage facade).
func TestSourceSnapshot(t *testing.T) {
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

// TestExtractionModeConstants verifies mode constants
// (static_fetch, renderless_js, chromium_cdp, stealth,
// scrape).
func TestExtractionModeConstants(t *testing.T) {
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

// TestIsValidExtractionMode verifies validation
// (static_fetch, renderless_js, chromium_cdp, stealth,
// scrape).
func TestIsValidExtractionMode(t *testing.T) {
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

// TestExtractedLink verifies ExtractedLink type
// (result types - links).
func TestExtractedLink(t *testing.T) {
	link := ExtractedLink{Href: "https://example.com", Text: "Example"}
	if link.Href != "https://example.com" {
		t.Error("href mismatch")
	}
}

// TestExtractedForm verifies ExtractedForm type
// (result types - forms).
func TestExtractedForm(t *testing.T) {
	form := ExtractedForm{
		Action: "/submit",
		Method: "POST",
		Fields: []ExtractedField{{Name: "email", Type: "text"}},
	}
	if len(form.Fields) != 1 {
		t.Error("should have 1 field")
	}
}

// TestExtractedTable verifies ExtractedTable type
// (result types - tables).
func TestExtractedTable(t *testing.T) {
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

// TestExtractedImage verifies ExtractedImage type
// (result types - images).
func TestExtractedImage(t *testing.T) {
	img := ExtractedImage{Src: "https://example.com/img.png", Alt: "Logo"}
	if img.Src != "https://example.com/img.png" {
		t.Error("src mismatch")
	}
}

// ==================== translator.go tests ====================

// TestCSSToXPathTag verifies tag selector
// (CSS -> XPath translation).
func TestCSSToXPathTag(t *testing.T) {
	xpath, err := CSSToXPath("div")
	if err != nil {
		t.Fatalf("CSSToXPath: %v", err)
	}
	if !strings.Contains(xpath, "div") {
		t.Errorf("xpath should contain 'div': got %s", xpath)
	}
}

// TestCSSToXPathClass verifies class selector
// (CSS -> XPath translation).
func TestCSSToXPathClass(t *testing.T) {
	xpath, err := CSSToXPath("div.container")
	if err != nil {
		t.Fatalf("CSSToXPath: %v", err)
	}
	if !strings.Contains(xpath, "@class") {
		t.Errorf("xpath should contain @class: got %s", xpath)
	}
}

// TestCSSToXPathID verifies ID selector
// (CSS -> XPath translation).
func TestCSSToXPathID(t *testing.T) {
	xpath, err := CSSToXPath("div#main")
	if err != nil {
		t.Fatalf("CSSToXPath: %v", err)
	}
	if !strings.Contains(xpath, "@id='main'") {
		t.Errorf("xpath should contain @id='main': got %s", xpath)
	}
}

// TestCSSToXPathAttribute verifies attribute selector
// (CSS -> XPath translation).
func TestCSSToXPathAttribute(t *testing.T) {
	xpath, err := CSSToXPath("a[href='https://example.com']")
	if err != nil {
		t.Fatalf("CSSToXPath: %v", err)
	}
	if !strings.Contains(xpath, "@href='https://example.com'") {
		t.Errorf("xpath should contain @href: got %s", xpath)
	}
}

// TestCSSToXPathEmpty verifies empty selector errors
// (CSS -> XPath translation).
func TestCSSToXPathEmpty(t *testing.T) {
	_, err := CSSToXPath("")
	if err == nil {
		t.Error("empty selector should error")
	}
}

// TestIsXPath verifies XPath detection
// (CSS -> XPath translation).
func TestIsXPath(t *testing.T) {
	if !IsXPath("//div[@id='main']") {
		t.Error("//div should be detected as XPath")
	}
	if IsXPath("div.container") {
		t.Error("div.container should NOT be detected as XPath")
	}
}

// TestIsCSS verifies CSS detection
// (CSS -> XPath translation).
func TestIsCSS(t *testing.T) {
	if !IsCSS("div.container") {
		t.Error("div.container should be detected as CSS")
	}
	if IsCSS("//div[@id='main']") {
		t.Error("//div should NOT be detected as CSS")
	}
}

// TestTranslateSelectorCSS verifies auto-detect CSS
// (CSS -> XPath translation).
func TestTranslateSelectorCSS(t *testing.T) {
	xpath, err := TranslateSelector("div#main")
	if err != nil {
		t.Fatalf("TranslateSelector: %v", err)
	}
	if !strings.Contains(xpath, "@id='main'") {
		t.Errorf("should contain @id='main': got %s", xpath)
	}
}

// TestTranslateSelectorXPath verifies auto-detect XPath
// (CSS -> XPath translation).
func TestTranslateSelectorXPath(t *testing.T) {
	input := "//div[@id='main']"
	xpath, err := TranslateSelector(input)
	if err != nil {
		t.Fatalf("TranslateSelector: %v", err)
	}
	if xpath != input {
		t.Errorf("XPath should pass through: got %s, want %s", xpath, input)
	}
}

// TestTranslateSelectorEmpty verifies empty selector errors.
func TestTranslateSelectorEmpty(t *testing.T) {
	_, err := TranslateSelector("")
	if err == nil {
		t.Error("empty selector should error")
	}
}

// ==================== structured_extract.go facade tests ====================

// TestStructuredExtractSchemaAlias verifies alias
// (structured_extract.go - Structured-Extract).
func TestStructuredExtractSchemaAlias(t *testing.T) {
	var schema StructuredExtractSchema
	schema.Kind = "text"
	if schema.Kind != "text" {
		t.Error("StructuredExtractSchema alias should work")
	}
}

// TestExtractJSONLDFromHTML verifies JSON-LD extraction
// (structured_extract.go - Structured-Extract).
func TestExtractJSONLDFromHTML(t *testing.T) {
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

// TestExtractOpenGraphFromHTML verifies OpenGraph extraction
// (structured_extract.go - Structured-Extract).
func TestExtractOpenGraphFromHTML(t *testing.T) {
	html := `<html><head>
	<meta property="og:title" content="Example Page">
	<meta property="og:url" content="https://example.com">
	</head></html>`
	og := ExtractOpenGraphFromHTML(html)
	if og["og:title"] != "Example Page" {
		t.Errorf("og:title: got %s, want 'Example Page'", og["og:title"])
	}
}

// TestScalarSchemaKinds verifies ScalarSchemaKinds re-export
// (structured_extract.go - scalarKinds).
func TestScalarSchemaKinds(t *testing.T) {
	if !ScalarSchemaKinds["text"] {
		t.Error("text should be a scalar kind")
	}
}

// TestJoinSchemaKinds verifies JoinSchemaKinds re-export
// (structured_extract.go - joinKinds).
func TestJoinSchemaKinds(t *testing.T) {
	if !JoinSchemaKinds["text"] {
		t.Error("text should be a join kind")
	}
}

// ==================== snapshot_extract.go tests ====================

// TestNewSnapshotExtractor verifies creation
// (snapshot_extract.go - renderless DOM snapshot).
func TestNewSnapshotExtractor(t *testing.T) {
	e := NewSnapshotExtractor(ExtractionModeStaticFetch)
	if e == nil {
		t.Fatal("extractor should not be nil")
	}
	if e.Mode() != ExtractionModeStaticFetch {
		t.Error("mode mismatch")
	}
}

// TestSnapshotExtractorExtract verifies extraction
// (one SourceSnapshot -> ExtractedPage facade).
func TestSnapshotExtractorExtract(t *testing.T) {
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

// TestSnapshotExtractorEmptyHTML verifies empty HTML errors
// (snapshot_extract.go).
func TestSnapshotExtractorEmptyHTML(t *testing.T) {
	e := NewSnapshotExtractor(ExtractionModeStaticFetch)
	_, err := e.Extract(SourceSnapshot{URL: "https://example.com"})
	if err == nil {
		t.Error("empty HTML should error")
	}
}

// TestSnapshotExtractorNilSafe verifies nil extractor is safe.
func TestSnapshotExtractorNilSafe(t *testing.T) {
	var e *SnapshotExtractor
	_, err := e.Extract(SourceSnapshot{HTML: "<html></html>"})
	if err == nil {
		t.Error("nil should error")
	}
	if e.Mode() != "" {
		t.Error("nil mode should be empty")
	}
}

// TestSnapshotExtractorMode verifies Mode
// (snapshot_extract.go).
func TestSnapshotExtractorMode(t *testing.T) {
	e := NewSnapshotExtractor(ExtractionModeChromiumCDP)
	if e.Mode() != ExtractionModeChromiumCDP {
		t.Errorf("mode: got %s, want chromium_cdp", e.Mode())
	}
}

// TestExtractTitle verifies title extraction
// (snapshot_extract.go).
func TestExtractTitle(t *testing.T) {
	html := `<html><head><title>My Title</title></head></html>`
	title := extractTitle(html)
	if title != "My Title" {
		t.Errorf("title: got %s, want 'My Title'", title)
	}
}

// TestExtractTitleMissing verifies missing title.
func TestExtractTitleMissing(t *testing.T) {
	html := `<html><head></head></html>`
	title := extractTitle(html)
	if title != "" {
		t.Errorf("title: got %s, want empty", title)
	}
}

// TestExtractText verifies text extraction
// (snapshot_extract.go).
func TestExtractText(t *testing.T) {
	html := `<html><body><p>Hello</p><script>var x=1;</script><p>World</p></body></html>`
	text := extractText(html)
	if strings.Contains(text, "var x=1") {
		t.Error("script content should be stripped")
	}
	if !strings.Contains(text, "Hello") || !strings.Contains(text, "World") {
		t.Error("text should contain Hello and World")
	}
}

// TestExtractLinks verifies link extraction
// (snapshot_extract.go).
func TestExtractLinks(t *testing.T) {
	html := `<html><body><a href="https://a.com">A</a><a href="https://b.com">B</a></body></html>`
	links := extractLinks(html)
	if len(links) != 2 {
		t.Errorf("links: got %d, want 2", len(links))
	}
	if links[0].Href != "https://a.com" {
		t.Errorf("href: got %s, want https://a.com", links[0].Href)
	}
}

// TestExtractImages verifies image extraction
// (snapshot_extract.go).
func TestExtractImages(t *testing.T) {
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

// ==================== parity test ====================

// TestScraperFacadesParity verifies all 6 spec-mandated files
// (parser.go, static.go, types.go, translator.go,
// structured_extract.go, snapshot_extract.go).
func TestScraperFacadesParity(t *testing.T) {
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
