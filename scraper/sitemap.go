// Package scraper implements sitemap and robots.txt parsing (spec ss28.12b.9).
package scraper

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"
)

// SitemapURL is a single URL entry in a sitemap.
type SitemapURL struct {
	Loc        string `xml:"loc"`
	LastMod    string `xml:"lastmod"`
	ChangeFreq string `xml:"changefreq"`
	Priority   string `xml:"priority"`
}

// Sitemap is a parsed sitemap.xml.
type Sitemap struct {
	URLs []SitemapURL `xml:"url"`
}

// SitemapIndex is a parsed sitemap index file.
type SitemapIndex struct {
	Sitemaps []SitemapURL `xml:"sitemap"`
}

// ParseSitemap parses a sitemap XML body.
func ParseSitemap(r io.Reader) (*Sitemap, error) {
	var sm Sitemap
	dec := xml.NewDecoder(r)
	if err := dec.Decode(&sm); err != nil {
		return nil, fmt.Errorf("sitemap parse: %w", err)
	}
	return &sm, nil
}

// ParseSitemapIndex parses a sitemap index XML body.
func ParseSitemapIndex(r io.Reader) (*SitemapIndex, error) {
	var si SitemapIndex
	dec := xml.NewDecoder(r)
	if err := dec.Decode(&si); err != nil {
		return nil, fmt.Errorf("sitemap index parse: %w", err)
	}
	return &si, nil
}

// ResolveSitemapURLs resolves relative sitemap URLs against a base URL.
func ResolveSitemapURLs(base string, urls []SitemapURL) ([]SitemapURL, error) {
	baseU, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	var out []SitemapURL
	for _, u := range urls {
		u2, err := url.Parse(u.Loc)
		if err != nil {
			continue
		}
		resolved := baseU.ResolveReference(u2).String()
		out = append(out, SitemapURL{Loc: resolved, LastMod: u.LastMod, ChangeFreq: u.ChangeFreq, Priority: u.Priority})
	}
	return out, nil
}

// RobotsPolicy is a simple robots.txt policy.
type RobotsPolicy struct {
	Disallowed []string
	Allowed    []string
	CrawlDelay time.Duration
	Sitemaps   []string
}

// ParseRobotsTxt parses a robots.txt body for a given user-agent.
func ParseRobotsTxt(body string, userAgent string) *RobotsPolicy {
	policy := &RobotsPolicy{}
	lines := strings.Split(body, "\n")
	var inBlock bool
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(strings.ToLower(parts[0]))
		val := strings.TrimSpace(parts[1])
		switch key {
		case "user-agent":
			inBlock = val == "*" || strings.Contains(strings.ToLower(userAgent), strings.ToLower(val))
		case "disallow":
			if inBlock && val != "" {
				policy.Disallowed = append(policy.Disallowed, val)
			}
		case "allow":
			if inBlock && val != "" {
				policy.Allowed = append(policy.Allowed, val)
			}
		case "crawl-delay":
			if inBlock {
				if d, err := time.ParseDuration(val + "s"); err == nil {
					policy.CrawlDelay = d
				}
			}
		case "sitemap":
			policy.Sitemaps = append(policy.Sitemaps, val)
		}
	}
	return policy
}

// IsURLAllowed checks if a path is allowed by the policy.
func (p *RobotsPolicy) IsURLAllowed(path string) bool {
	for _, a := range p.Allowed {
		if strings.HasPrefix(path, a) {
			return true
		}
	}
	for _, d := range p.Disallowed {
		if strings.HasPrefix(path, d) {
			return false
		}
	}
	return true
}

// RobotsCacheEntry stores a cached robots policy.
type RobotsCacheEntry struct {
	Policy    *RobotsPolicy
	FetchedAt time.Time
	TTL       time.Duration
}

// IsExpired checks if the cache entry has expired.
func (e *RobotsCacheEntry) IsExpired() bool {
	return time.Since(e.FetchedAt) > e.TTL
}
