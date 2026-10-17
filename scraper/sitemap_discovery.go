package scraper

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/Christopher-Schulze/Artemis/network"
)

// SitemapLimits bounds recursive sitemap discovery (spec L4416). Zero values
// select the sitemap-protocol defaults.
type SitemapLimits struct {
	MaxDepth    int   // sitemap-index nesting, default 3
	MaxSitemaps int   // fetched sitemap documents, default 50
	MaxURLs     int   // collected page URLs, default 50,000
	MaxBytes    int64 // per document after decompression, default 50 MiB
}

// SitemapFailure records one sitemap document that could not be used.
type SitemapFailure struct {
	URL   string `json:"url"`
	Error string `json:"error"`
}

// SitemapDiscovery is the deduplicated same-host URL set with every failure.
type SitemapDiscovery struct {
	URLs      []SitemapURL     `json:"urls"`
	Sitemaps  []string         `json:"sitemaps"`
	Failures  []SitemapFailure `json:"failures,omitempty"`
	Truncated bool             `json:"truncated,omitempty"`
}

const (
	defaultSitemapDepth    = 3
	defaultSitemapCount    = 50
	defaultSitemapURLs     = 50_000
	defaultSitemapMaxBytes = 50 << 20
)

// sitemapDocument decodes either root form; XMLName tells which one it was.
type sitemapDocument struct {
	XMLName  xml.Name
	URLs     []SitemapURL `xml:"url"`
	Sitemaps []SitemapURL `xml:"sitemap"`
}

type sitemapTarget struct {
	url   string
	depth int
}

// DiscoverSitemapURLs loads robots.txt Sitemap directives plus /sitemap.xml
// and /sitemap_index.xml for origin, follows sitemap indexes recursively and
// returns the same-host page URLs with lastmod/changefreq/priority. Every
// fetch goes through the client's policy, proxy and accounting.
func DiscoverSitemapURLs(ctx context.Context, client *network.HTTPClient, origin string, limits SitemapLimits) (SitemapDiscovery, error) {
	if ctx == nil || client == nil {
		return SitemapDiscovery{}, errors.New("sitemap discovery: context and client required")
	}
	base, err := url.Parse(origin)
	if err != nil || base.Host == "" || (base.Scheme != "http" && base.Scheme != "https") {
		return SitemapDiscovery{}, fmt.Errorf("sitemap discovery: invalid origin %q", origin)
	}
	limits = limits.withDefaults()
	root := &url.URL{Scheme: base.Scheme, Host: base.Host}
	var result SitemapDiscovery
	queue := make([]sitemapTarget, 0, 4)
	if robots, err := client.FetchRobots(ctx, root); err != nil {
		result.Failures = append(result.Failures, SitemapFailure{URL: root.String() + "/robots.txt", Error: err.Error()})
	} else {
		for _, directive := range robots.Sitemaps {
			queue = append(queue, sitemapTarget{url: directive})
		}
	}
	queue = append(queue, sitemapTarget{url: root.String() + "/sitemap.xml"}, sitemapTarget{url: root.String() + "/sitemap_index.xml"})
	seenSitemaps, seenURLs := map[string]bool{}, map[string]bool{}
	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		target := queue[0]
		queue = queue[1:]
		location, ok := sameHostURL(root, target.url)
		if !ok || seenSitemaps[location] {
			continue
		}
		if len(result.Sitemaps) >= limits.MaxSitemaps {
			result.Truncated = true
			break
		}
		seenSitemaps[location] = true
		document, found, err := fetchSitemapDocument(ctx, client, location, limits.MaxBytes)
		if err != nil {
			result.Failures = append(result.Failures, SitemapFailure{URL: location, Error: err.Error()})
			continue
		}
		if !found {
			continue
		}
		result.Sitemaps = append(result.Sitemaps, location)
		switch document.XMLName.Local {
		case "sitemapindex":
			if target.depth+1 > limits.MaxDepth {
				result.Truncated = true
				continue
			}
			for _, child := range document.Sitemaps {
				queue = append(queue, sitemapTarget{url: strings.TrimSpace(child.Loc), depth: target.depth + 1})
			}
		case "urlset":
			for _, entry := range document.URLs {
				page, ok := sameHostURL(root, strings.TrimSpace(entry.Loc))
				if !ok || seenURLs[page] {
					continue
				}
				if len(result.URLs) >= limits.MaxURLs {
					result.Truncated = true
					break
				}
				seenURLs[page] = true
				entry.Loc = page
				result.URLs = append(result.URLs, entry)
			}
		default:
			result.Failures = append(result.Failures, SitemapFailure{URL: location, Error: fmt.Sprintf("unexpected root element %q", document.XMLName.Local)})
		}
	}
	return result, nil
}

func (l SitemapLimits) withDefaults() SitemapLimits {
	if l.MaxDepth <= 0 {
		l.MaxDepth = defaultSitemapDepth
	}
	if l.MaxSitemaps <= 0 {
		l.MaxSitemaps = defaultSitemapCount
	}
	if l.MaxURLs <= 0 {
		l.MaxURLs = defaultSitemapURLs
	}
	if l.MaxBytes <= 0 {
		l.MaxBytes = defaultSitemapMaxBytes
	}
	return l
}

// sameHostURL resolves raw against root and keeps it only on root's host, as
// the sitemap protocol requires for sitemap-listed locations.
func sameHostURL(root *url.URL, raw string) (string, bool) {
	if raw == "" {
		return "", false
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	resolved := root.ResolveReference(parsed)
	if !strings.EqualFold(resolved.Host, root.Host) || (resolved.Scheme != "http" && resolved.Scheme != "https") {
		return "", false
	}
	resolved.Fragment = ""
	return resolved.String(), true
}

// fetchSitemapDocument returns found=false for a missing document (4xx).
func fetchSitemapDocument(ctx context.Context, client *network.HTTPClient, location string, maxBytes int64) (sitemapDocument, bool, error) {
	response, err := client.DoTarget(ctx, network.Request{Method: http.MethodGet, URL: location, MaxBodyBytes: maxBytes}, network.TargetSubresource)
	if err != nil {
		return sitemapDocument{}, false, err
	}
	if response.StatusCode >= 400 && response.StatusCode < 500 {
		return sitemapDocument{}, false, nil
	}
	if response.StatusCode != http.StatusOK {
		return sitemapDocument{}, false, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	var body io.Reader = bytes.NewReader(response.Body)
	if strings.HasSuffix(strings.ToLower(location), ".gz") || isGzip(response.Body) {
		reader, err := gzip.NewReader(bytes.NewReader(response.Body))
		if err != nil {
			return sitemapDocument{}, false, fmt.Errorf("gzip: %w", err)
		}
		defer reader.Close()
		body = reader
	}
	limited := &io.LimitedReader{R: body, N: maxBytes + 1}
	var document sitemapDocument
	if err := xml.NewDecoder(limited).Decode(&document); err != nil {
		return sitemapDocument{}, false, fmt.Errorf("parse: %w", err)
	}
	if limited.N <= 0 {
		return sitemapDocument{}, false, fmt.Errorf("document exceeds %d bytes", maxBytes)
	}
	return document, true, nil
}

func isGzip(body []byte) bool {
	return len(body) >= 2 && body[0] == 0x1f && body[1] == 0x8b
}
