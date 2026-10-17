// Package scraper implements sitemap and robots.txt parsing (spec ss28.12b.9).
package scraper

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/Christopher-Schulze/Artemis/network"
)

// SitemapURL is a single URL entry in a sitemap.
type SitemapURL struct {
	Loc        string `xml:"loc" json:"loc"`
	LastMod    string `xml:"lastmod" json:"lastmod,omitempty"`
	ChangeFreq string `xml:"changefreq" json:"changefreq,omitempty"`
	Priority   string `xml:"priority" json:"priority,omitempty"`
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
	ParseError error `json:"-"`
}

// ParseRobotsTxt parses a robots.txt body for a given user-agent.
func ParseRobotsTxt(body string, userAgent string) *RobotsPolicy {
	policy := &RobotsPolicy{}
	parsed, err := network.ParseRobots(strings.NewReader(body))
	if err != nil {
		return &RobotsPolicy{Disallowed: []string{"/"}, ParseError: err}
	}
	for _, rule := range parsed.RulesFor(userAgent) {
		if rule.Allow {
			policy.Allowed = append(policy.Allowed, rule.Path)
		} else {
			policy.Disallowed = append(policy.Disallowed, rule.Path)
		}
	}
	policy.CrawlDelay = parsed.CrawlDelayFor(userAgent)
	policy.Sitemaps = append([]string(nil), parsed.Sitemaps...)
	return policy
}

// IsURLAllowed checks if a path is allowed by the policy.
func (p *RobotsPolicy) IsURLAllowed(path string) bool {
	if p == nil || p.ParseError != nil {
		return false
	}
	rules := make([]network.RobotsRule, 0, len(p.Allowed)+len(p.Disallowed))
	for _, pattern := range p.Allowed {
		rules = append(rules, network.RobotsRule{Allow: true, Path: pattern})
	}
	for _, pattern := range p.Disallowed {
		rules = append(rules, network.RobotsRule{Allow: false, Path: pattern})
	}
	canonical := network.RobotsPolicy{Groups: map[string][]network.RobotsRule{"*": rules}}
	return canonical.Allowed("*", path)
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
