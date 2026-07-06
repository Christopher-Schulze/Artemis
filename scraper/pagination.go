// Package scraper implements pagination and infinite scroll detection (spec ss28.12b.10).
package scraper

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/Christopher-Schulze/Artemis/webapi"
)

// PaginationInfo describes detected pagination on a page.
type PaginationInfo struct {
	HasNext          bool
	NextURL          string
	HasPrev          bool
	PrevURL          string
	TotalPages       int
	CurrentPage      int
	IsInfiniteScroll bool
}

// DetectPagination scans a document for pagination signals.
func DetectPagination(d *webapi.Document) PaginationInfo {
	var info PaginationInfo
	if d == nil {
		return info
	}

	// Check for rel="next" / rel="prev" links
	links, _ := d.QuerySelectorAll("a[rel], link[rel]")
	for _, l := range links {
		rel, _ := l.Attr("rel")
		href, _ := l.Attr("href")
		switch rel {
		case "next":
			info.HasNext = true
			info.NextURL = resolveURL(d.URL(), href)
		case "prev":
			info.HasPrev = true
			info.PrevURL = resolveURL(d.URL(), href)
		}
	}

	// Check for numbered pagination buttons
	pageLinks, _ := d.QuerySelectorAll("a")
	pagePattern := regexp.MustCompile(`\?page=(\d+)|[/?]page/(\d+)|\?p=(\d+)`)
	maxPage := 1
	for _, l := range pageLinks {
		href, ok := l.Attr("href")
		if !ok {
			continue
		}
		matches := pagePattern.FindStringSubmatch(href)
		if len(matches) > 0 {
			for _, m := range matches[1:] {
				if m != "" {
					if n, err := strconv.Atoi(m); err == nil && n > maxPage {
						maxPage = n
					}
				}
			}
		}
	}
	if maxPage > 1 {
		info.TotalPages = maxPage
	}

	// Detect infinite scroll indicators
	scripts, _ := d.QuerySelectorAll("script")
	for _, s := range scripts {
		text := s.Text()
		if strings.Contains(text, "IntersectionObserver") || strings.Contains(text, "scroll") && strings.Contains(text, "append") {
			info.IsInfiniteScroll = true
		}
	}

	return info
}

// InfiniteScrollDetector detects new content after scrolling.
type InfiniteScrollDetector struct {
	maxScrolls int
}

// NewInfiniteScrollDetector creates a detector.
func NewInfiniteScrollDetector(maxScrolls int) *InfiniteScrollDetector {
	return &InfiniteScrollDetector{maxScrolls: maxScrolls}
}

// ShouldContinue determines if more scrolling is likely to yield content.
func (d *InfiniteScrollDetector) ShouldContinue(scrollCount int, previousItemCount, currentItemCount int) bool {
	if scrollCount >= d.maxScrolls {
		return false
	}
	return currentItemCount > previousItemCount
}

var nextButtonTexts = []string{"next", "weiter", ">", "»", "→", "siguiente", "suivant"}

// FindNextButton searches for a likely "next" pagination button.
func FindNextButton(d *webapi.Document) (string, bool) {
	buttons, _ := d.QuerySelectorAll("a, button")
	for _, b := range buttons {
		text := strings.ToLower(b.Text())
		for _, nt := range nextButtonTexts {
			if strings.Contains(text, nt) {
				href, ok := b.Attr("href")
				if ok {
					return resolveURL(d.URL(), href), true
				}
			}
		}
		aria, _ := b.Attr("aria-label")
		for _, nt := range nextButtonTexts {
			if strings.Contains(strings.ToLower(aria), nt) {
				href, ok := b.Attr("href")
				if ok {
					return resolveURL(d.URL(), href), true
				}
			}
		}
	}
	return "", false
}

func resolveURL(base, ref string) string {
	if strings.HasPrefix(ref, "http") {
		return ref
	}
	b, _ := url.Parse(base)
	r, _ := url.Parse(ref)
	if b != nil && r != nil {
		return b.ResolveReference(r).String()
	}
	return ref
}
