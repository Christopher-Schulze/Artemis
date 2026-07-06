package scraper

import (
	"fmt"
	"time"
)

// types.go (spec L4028: scraper/types.go - result types
// (text/markdown/semantic_tree/structured_data/links/forms/tables/
// images)).
//
// Web scraping engine: result types for extracted page content.

// ExtractedPage is the unified result of a scraping operation
// (spec L4028: one SourceSnapshot -> ExtractedPage facade for
// static_fetch, renderless_js, chromium_cdp, stealth, scrape).
type ExtractedPage struct {
	URL            string             `json:"url"`
	Title          string             `json:"title"`
	Text           string             `json:"text"`
	Markdown       string             `json:"markdown"`
	SemanticTree   string             `json:"semanticTree,omitempty"`
	StructuredData []StructuredRecord `json:"structuredData,omitempty"`
	Links          []ExtractedLink    `json:"links,omitempty"`
	Forms          []ExtractedForm    `json:"forms,omitempty"`
	Tables         []ExtractedTable   `json:"tables,omitempty"`
	Images         []ExtractedImage   `json:"images,omitempty"`
	Metadata       map[string]string  `json:"metadata,omitempty"`
	OpenGraph      map[string]string  `json:"openGraph,omitempty"`
	ExtractedAt    time.Time          `json:"extractedAt"`
	ExtractionMode string             `json:"extractionMode"` // static_fetch, renderless_js, chromium_cdp, stealth, scrape
}

// SourceSnapshot is the input to the scraping pipeline
// (spec L4028: one SourceSnapshot -> ExtractedPage facade).
// Output contract per ss28.3a: render_mode, fallback_reason?,
// unsupported_features[], renderless_webapi_hits[], script_timeout?,
// request_intercepts[].
type SourceSnapshot struct {
	URL                  string             `json:"url"`
	HTML                 string             `json:"html"`
	Screenshot           []byte             `json:"screenshot,omitempty"`
	Mode                 string             `json:"mode"` // static_fetch, renderless_js, chromium_cdp, stealth, scrape
	RenderMode           string             `json:"renderMode,omitempty"`
	FallbackReason       string             `json:"fallbackReason,omitempty"`
	UnsupportedFeatures  []string           `json:"unsupportedFeatures,omitempty"`
	RenderlessWebAPIHits []string           `json:"renderlessWebApiHits,omitempty"`
	ScriptTimeout        *int               `json:"scriptTimeout,omitempty"`
	RequestIntercepts    []RequestIntercept `json:"requestIntercepts,omitempty"`
}

// RequestIntercept records a network request that was intercepted
// during renderless execution (spec L3990: request_intercepts[]).
type RequestIntercept struct {
	URL    string `json:"url"`
	Method string `json:"method"`
	Action string `json:"action"` // continue, fulfill, fail, mock
	Status int    `json:"status,omitempty"`
}

// ExtractedLink is a hyperlink extracted from a page
// (spec L4028: result types - links).
type ExtractedLink struct {
	Href string `json:"href"`
	Text string `json:"text"`
}

// ExtractedForm is a form extracted from a page
// (spec L4028: result types - forms).
type ExtractedForm struct {
	Action string           `json:"action"`
	Method string           `json:"method"`
	Fields []ExtractedField `json:"fields"`
}

// ExtractedField is a form field
// (spec L4028: result types - forms).
type ExtractedField struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Value string `json:"value"`
}

// ExtractedTable is a table extracted from a page
// (spec L4028: result types - tables).
type ExtractedTable struct {
	Headers []string   `json:"headers"`
	Rows    [][]string `json:"rows"`
}

// ExtractedImage is an image extracted from a page
// (spec L4028: result types - images).
type ExtractedImage struct {
	Src    string `json:"src"`
	Alt    string `json:"alt"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

// ExtractionMode enumerates the scraping modes
// (spec L4028: static_fetch, renderless_js, chromium_cdp, stealth,
// scrape).
type ExtractionMode string

const (
	// ExtractionModeStaticFetch is HTTP-only fetch mode.
	ExtractionModeStaticFetch ExtractionMode = "static_fetch"
	// ExtractionModeRenderlessJS is renderless DOM snapshot mode.
	ExtractionModeRenderlessJS ExtractionMode = "renderless_js"
	// ExtractionModeChromiumCDP is CDP live-DOM snapshot mode.
	ExtractionModeChromiumCDP ExtractionMode = "chromium_cdp"
	// ExtractionModeStealth is stealth scraping mode.
	ExtractionModeStealth ExtractionMode = "stealth"
	// ExtractionModeScrape is general scrape mode.
	ExtractionModeScrape ExtractionMode = "scrape"
)

// String returns a diagnostic summary.
func (p ExtractedPage) String() string {
	return fmt.Sprintf("ExtractedPage{url:%s mode:%s links:%d forms:%d tables:%d images:%d}",
		p.URL, p.ExtractionMode, len(p.Links), len(p.Forms), len(p.Tables), len(p.Images))
}

// IsEmpty reports whether the extracted page has no content.
func (p ExtractedPage) IsEmpty() bool {
	return p.Text == "" && p.Markdown == "" && len(p.Links) == 0 && len(p.StructuredData) == 0
}

// HasStructuredData reports whether the page has structured data.
func (p ExtractedPage) HasStructuredData() bool {
	return len(p.StructuredData) > 0
}

// String returns a diagnostic summary.
func (s SourceSnapshot) String() string {
	return fmt.Sprintf("SourceSnapshot{url:%s mode:%s html:%d bytes}", s.URL, s.Mode, len(s.HTML))
}

// IsValidExtractionMode reports whether a mode is valid
// (spec L4028: static_fetch, renderless_js, chromium_cdp, stealth,
// scrape).
func IsValidExtractionMode(mode string) bool {
	switch ExtractionMode(mode) {
	case ExtractionModeStaticFetch,
		ExtractionModeRenderlessJS,
		ExtractionModeChromiumCDP,
		ExtractionModeStealth,
		ExtractionModeScrape:
		return true
	}
	return false
}
