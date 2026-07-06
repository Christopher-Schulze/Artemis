package scraper

import (
	"strings"
	"testing"
	"time"
)

func TestParseSitemap(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url>
    <loc>https://example.com/page1</loc>
    <lastmod>2026-01-01</lastmod>
    <changefreq>weekly</changefreq>
    <priority>0.8</priority>
  </url>
  <url>
    <loc>https://example.com/page2</loc>
  </url>
</urlset>`
	sm, err := ParseSitemap(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("parse sitemap: %v", err)
	}
	if len(sm.URLs) != 2 {
		t.Fatalf("expected 2 urls, got %d", len(sm.URLs))
	}
	if sm.URLs[0].Loc != "https://example.com/page1" {
		t.Fatalf("expected page1, got %s", sm.URLs[0].Loc)
	}
}

func TestParseSitemapIndex(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <sitemap>
    <loc>https://example.com/sitemap1.xml</loc>
    <lastmod>2026-01-01</lastmod>
  </sitemap>
</sitemapindex>`
	si, err := ParseSitemapIndex(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("parse sitemap index: %v", err)
	}
	if len(si.Sitemaps) != 1 {
		t.Fatalf("expected 1 sitemap, got %d", len(si.Sitemaps))
	}
}

func TestResolveSitemapURLs(t *testing.T) {
	urls := []SitemapURL{{Loc: "/page1"}, {Loc: "https://other.com/page2"}}
	resolved, err := ResolveSitemapURLs("https://example.com/", urls)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved[0].Loc != "https://example.com/page1" {
		t.Fatalf("expected resolved url, got %s", resolved[0].Loc)
	}
	if resolved[1].Loc != "https://other.com/page2" {
		t.Fatalf("expected absolute url, got %s", resolved[1].Loc)
	}
}

func TestParseRobotsTxt(t *testing.T) {
	body := `User-agent: *
Disallow: /private
Allow: /public
Crawl-delay: 2
Sitemap: https://example.com/sitemap.xml

User-agent: badbot
Disallow: /
`
	policy := ParseRobotsTxt(body, "artemis")
	if !policy.IsURLAllowed("/public/page") {
		t.Fatal("expected /public allowed")
	}
	if policy.IsURLAllowed("/private/page") {
		t.Fatal("expected /private disallowed")
	}
	if policy.CrawlDelay != 2*time.Second {
		t.Fatalf("expected 2s crawl delay, got %v", policy.CrawlDelay)
	}
	if len(policy.Sitemaps) != 1 {
		t.Fatalf("expected 1 sitemap, got %d", len(policy.Sitemaps))
	}
}

func TestRobotsCacheEntry_IsExpired(t *testing.T) {
	e := &RobotsCacheEntry{FetchedAt: time.Now().Add(-25 * time.Hour), TTL: 24 * time.Hour}
	if !e.IsExpired() {
		t.Fatal("expected expired")
	}
	e2 := &RobotsCacheEntry{FetchedAt: time.Now(), TTL: 24 * time.Hour}
	if e2.IsExpired() {
		t.Fatal("expected not expired")
	}
}
