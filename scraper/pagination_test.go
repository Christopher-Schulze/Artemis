package scraper

import (
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/parser"
)

func TestDetectPagination(t *testing.T) {
	html := `<html><head></head><body>
<a href="?page=2" rel="next">Next</a>
<a href="?page=1" rel="prev">Prev</a>
<a href="/page/3">3</a>
</body></html>`
	doc, _ := parser.ParseHTML(strings.NewReader(html), "https://example.com/")
	info := DetectPagination(doc)
	if !info.HasNext {
		t.Fatal("expected has next")
	}
	if info.NextURL != "https://example.com/?page=2" {
		t.Fatalf("expected next url, got %s", info.NextURL)
	}
	if !info.HasPrev {
		t.Fatal("expected has prev")
	}
	if info.TotalPages != 3 {
		t.Fatalf("expected total pages 3, got %d", info.TotalPages)
	}
}

func TestDetectPagination_InfiniteScroll(t *testing.T) {
	html := `<html><head></head><body>
<script>new IntersectionObserver(() => { loadMore(); });</script>
</body></html>`
	doc, _ := parser.ParseHTML(strings.NewReader(html), "https://example.com/")
	info := DetectPagination(doc)
	if !info.IsInfiniteScroll {
		t.Fatal("expected infinite scroll detected")
	}
}

func TestInfiniteScrollDetector_ShouldContinue(t *testing.T) {
	d := NewInfiniteScrollDetector(5)
	if !d.ShouldContinue(1, 10, 20) {
		t.Fatal("expected continue when new items added")
	}
	if d.ShouldContinue(5, 10, 10) {
		t.Fatal("expected stop at max scrolls")
	}
	if d.ShouldContinue(1, 20, 20) {
		t.Fatal("expected stop when no new items")
	}
}

func TestFindNextButton(t *testing.T) {
	html := `<html><head></head><body>
<a href="/page/2" aria-label="next page">Weiter</a>
</body></html>`
	doc, _ := parser.ParseHTML(strings.NewReader(html), "https://example.com/")
	url, ok := FindNextButton(doc)
	if !ok {
		t.Fatal("expected next button found")
	}
	if url != "https://example.com/page/2" {
		t.Fatalf("expected /page/2, got %s", url)
	}
}
