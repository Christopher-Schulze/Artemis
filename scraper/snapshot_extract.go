package scraper

import (
	"fmt"
	"strings"
	"time"
)

// snapshot_extract.go (spec L4028: scraper/snapshot_extract.go -
// renderless DOM snapshot, CDP live-DOM snapshot).
//
// Web scraping engine: snapshot extraction from DOM snapshots.
// This file provides the snapshot extraction facade that converts
// a SourceSnapshot into an ExtractedPage.

// SnapshotExtractor extracts structured data from DOM snapshots
// (spec L4028: snapshot_extract.go - renderless DOM snapshot, CDP
// live-DOM snapshot).
type SnapshotExtractor struct {
	mode ExtractionMode
}

// NewSnapshotExtractor creates a new SnapshotExtractor for the given
// extraction mode (spec L4028: snapshot_extract.go).
func NewSnapshotExtractor(mode ExtractionMode) *SnapshotExtractor {
	return &SnapshotExtractor{mode: mode}
}

// Extract extracts an ExtractedPage from a SourceSnapshot
// (spec L4028: one SourceSnapshot -> ExtractedPage facade).
func (e *SnapshotExtractor) Extract(snap SourceSnapshot) (ExtractedPage, error) {
	if e == nil {
		return ExtractedPage{}, fmt.Errorf("snapshot_extract: nil extractor")
	}
	if snap.HTML == "" {
		return ExtractedPage{}, fmt.Errorf("snapshot_extract: empty HTML in snapshot")
	}

	page := ExtractedPage{
		URL:            snap.URL,
		ExtractionMode: string(e.mode),
		ExtractedAt:    time.Now(),
	}

	// Extract title
	page.Title = extractTitle(snap.HTML)

	// Extract text (simplified: strip HTML tags)
	page.Text = extractText(snap.HTML)

	// Extract links
	page.Links = extractLinks(snap.HTML)

	// Extract images
	page.Images = extractImages(snap.HTML)

	// Extract structured data (JSON-LD)
	if records, err := ExtractJSONLD(snap.HTML); err == nil {
		page.StructuredData = records
	}

	// Extract OpenGraph
	page.OpenGraph = ExtractOpenGraph(snap.HTML)

	return page, nil
}

// extractTitle extracts the <title> from HTML
// (spec L4028: snapshot_extract.go).
func extractTitle(html string) string {
	start := strings.Index(strings.ToLower(html), "<title>")
	if start < 0 {
		return ""
	}
	start += 7
	end := strings.Index(strings.ToLower(html[start:]), "</title>")
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(html[start : start+end])
}

// extractText extracts visible text from HTML (simplified)
// (spec L4028: snapshot_extract.go).
func extractText(html string) string {
	// Remove script and style tags
	lower := strings.ToLower(html)
	for _, tag := range []string{"script", "style", "noscript"} {
		for {
			start := strings.Index(lower, "<"+tag)
			if start < 0 {
				break
			}
			end := strings.Index(lower[start:], "</"+tag+">")
			if end < 0 {
				break
			}
			html = html[:start] + html[start+end+len("</"+tag+">"):]
			lower = strings.ToLower(html)
		}
	}
	// Strip remaining HTML tags
	var result strings.Builder
	inTag := false
	for _, ch := range html {
		if ch == '<' {
			inTag = true
			continue
		}
		if ch == '>' {
			inTag = false
			continue
		}
		if !inTag {
			result.WriteRune(ch)
		}
	}
	return strings.TrimSpace(result.String())
}

// extractLinks extracts hyperlinks from HTML
// (spec L4028: snapshot_extract.go).
func extractLinks(html string) []ExtractedLink {
	var links []ExtractedLink
	lower := strings.ToLower(html)
	idx := 0
	for {
		pos := strings.Index(lower[idx:], "<a ")
		if pos < 0 {
			break
		}
		pos += idx
		end := strings.Index(lower[pos:], ">")
		if end < 0 {
			break
		}
		tag := html[pos : pos+end+1]
		href := extractAttr(tag, "href")
		if href != "" {
			// Extract text content
			textStart := pos + end + 1
			textEnd := strings.Index(lower[textStart:], "</a>")
			text := ""
			if textEnd >= 0 {
				text = strings.TrimSpace(extractText(html[textStart : textStart+textEnd]))
			}
			links = append(links, ExtractedLink{Href: href, Text: text})
		}
		idx = pos + end + 1
	}
	return links
}

// extractImages extracts image sources from HTML
// (spec L4028: snapshot_extract.go).
func extractImages(html string) []ExtractedImage {
	var images []ExtractedImage
	lower := strings.ToLower(html)
	idx := 0
	for {
		pos := strings.Index(lower[idx:], "<img ")
		if pos < 0 {
			break
		}
		pos += idx
		end := strings.Index(lower[pos:], ">")
		if end < 0 {
			break
		}
		tag := html[pos : pos+end+1]
		src := extractAttr(tag, "src")
		alt := extractAttr(tag, "alt")
		if src != "" {
			images = append(images, ExtractedImage{Src: src, Alt: alt})
		}
		idx = pos + end + 1
	}
	return images
}

// extractAttr extracts an attribute value from an HTML tag
// (spec L4028: snapshot_extract.go).
func extractAttr(tag, attr string) string {
	lower := strings.ToLower(tag)
	key := attr + "=\""
	idx := strings.Index(lower, key)
	if idx < 0 {
		key = attr + "='"
		idx = strings.Index(lower, key)
	}
	if idx < 0 {
		return ""
	}
	start := idx + len(key)
	end := strings.IndexAny(tag[start:], "\"'")
	if end < 0 {
		return ""
	}
	return tag[start : start+end]
}

// Mode returns the extraction mode
// (spec L4028: snapshot_extract.go).
func (e *SnapshotExtractor) Mode() ExtractionMode {
	if e == nil {
		return ""
	}
	return e.mode
}
