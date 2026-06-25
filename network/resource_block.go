package network

// resource_block.go (spec L4368: Resource Blocking).
//
// Scrapling blocks a fixed set of resource types (EXTRA_RESOURCES in
// scrapling/engines/constants.py) to speed up page loads and avoid
// leaking fingerprintable fonts/media. This module implements the
// full resource + domain blocking logic for the Go engine, including
// automatic subdomain matching and an allowlist override, and emits
// CDP Network.setBlockedURLs patterns.

import (
	"strings"
)

// ResourceType identifies a CDP/Playwright resource type (spec L4368).
type ResourceType string

const (
	// ResourceFont covers @font-face and font preload requests.
	ResourceFont ResourceType = "font"
	// ResourceImage covers <img>, CSS background-image, favicon.
	ResourceImage ResourceType = "image"
	// ResourceMedia covers <audio>, <video>.
	ResourceMedia ResourceType = "media"
	// ResourceBeacon covers navigator.sendBeacon.
	ResourceBeacon ResourceType = "beacon"
	// ResourceObject covers <object>, <embed>.
	ResourceObject ResourceType = "object"
	// ResourceImageset covers <picture>/srcset image requests.
	ResourceImageset ResourceType = "imageset"
	// ResourceTexttrack covers <track> VTT subtitle files.
	ResourceTexttrack ResourceType = "texttrack"
	// ResourceWebsocket covers WebSocket upgrade requests.
	ResourceWebsocket ResourceType = "websocket"
	// ResourceCSPReport covers Content-Security-Policy report uploads.
	ResourceCSPReport ResourceType = "csp_report"
	// ResourceStylesheet covers <link rel="stylesheet"> and CSS @import.
	ResourceStylesheet ResourceType = "stylesheet"
)

// allBlockedResourceTypes is the full set of resource types blocked
// by default (spec L4368 / Scrapling EXTRA_RESOURCES).
var allBlockedResourceTypes = []ResourceType{
	ResourceFont,
	ResourceImage,
	ResourceMedia,
	ResourceBeacon,
	ResourceObject,
	ResourceImageset,
	ResourceTexttrack,
	ResourceWebsocket,
	ResourceCSPReport,
	ResourceStylesheet,
}

// BlockedResources configures resource-type and domain blocking
// (spec L4368). ResourceTypes is the set of blocked types; domains
// in BlockedDomains are blocked with automatic subdomain matching;
// AllowedDomains overrides domain blocking for specific hosts.
type BlockedResources struct {
	// ResourceTypes is the set of blocked resource types. Defaults
	// to all 10 EXTRA_RESOURCES types.
	ResourceTypes map[ResourceType]bool
	// BlockedDomains are domains whose requests are blocked. A
	// blocked domain also blocks all subdomains ("example.com"
	// blocks "sub.example.com").
	BlockedDomains []string
	// AllowedDomains are domains that are never blocked, even if
	// they would match a BlockedDomains pattern or a blocked
	// resource type. Subdomain matching applies identically.
	AllowedDomains []string
}

// DefaultBlockedResources returns a configuration with all 10
// EXTRA_RESOURCES resource types blocked and no domain rules
// (spec L4368).
func DefaultBlockedResources() BlockedResources {
	types := make(map[ResourceType]bool, len(allBlockedResourceTypes))
	for _, rt := range allBlockedResourceTypes {
		types[rt] = true
	}
	return BlockedResources{
		ResourceTypes:  types,
		BlockedDomains: nil,
		AllowedDomains: nil,
	}
}

// domainMatches reports whether candidate matches pattern or is a
// subdomain of pattern (spec L4368: auto-match subdomains). Matching
// is case-insensitive. "example.com" matches "example.com",
// "sub.example.com", and "a.b.example.com" but not "notexample.com".
func domainMatches(candidate, pattern string) bool {
	c := strings.ToLower(strings.TrimSpace(candidate))
	p := strings.ToLower(strings.TrimSpace(pattern))
	if c == "" || p == "" {
		return false
	}
	if c == p {
		return true
	}
	return strings.HasSuffix(c, "."+p)
}

// IsDomainBlocked reports whether domain matches any BlockedDomains
// pattern (with automatic subdomain matching) and is not in the
// AllowedDomains allowlist (spec L4368).
func (b BlockedResources) IsDomainBlocked(domain string) bool {
	if domain == "" {
		return false
	}
	// Allowlist overrides blocked-domain matches.
	for _, allowed := range b.AllowedDomains {
		if domainMatches(domain, allowed) {
			return false
		}
	}
	for _, blocked := range b.BlockedDomains {
		if domainMatches(domain, blocked) {
			return true
		}
	}
	return false
}

// IsBlocked reports whether a request for the given resource type
// from the given domain should be blocked (spec L4368). A request is
// blocked when:
//   - its resource type is in the blocked set, AND
//   - its domain is not in the allowlist, AND
//   - either its domain matches a blocked-domain pattern OR no
//     domain blocking is configured (type-only blocking).
//
// When BlockedDomains is empty, type-based blocking applies to all
// domains except those in the allowlist.
func (b BlockedResources) IsBlocked(resourceType ResourceType, domain string) bool {
	// Allowlist always wins, regardless of type.
	for _, allowed := range b.AllowedDomains {
		if domainMatches(domain, allowed) {
			return false
		}
	}
	if !b.ResourceTypes[resourceType] {
		return false
	}
	// No domain rules: type-based blocking applies globally.
	if len(b.BlockedDomains) == 0 {
		return true
	}
	// Domain rules present: block only matching domains.
	return b.IsDomainBlocked(domain)
}

// AddBlockedDomain adds a domain to the blocked-domains list if it
// is not already present (case-insensitive). Returns true if added.
func (b *BlockedResources) AddBlockedDomain(domain string) bool {
	d := strings.ToLower(strings.TrimSpace(domain))
	if d == "" {
		return false
	}
	for _, existing := range b.BlockedDomains {
		if existing == d {
			return false
		}
	}
	b.BlockedDomains = append(b.BlockedDomains, d)
	return true
}

// AddAllowedDomain adds a domain to the allowlist if it is not
// already present (case-insensitive). Returns true if added.
func (b *BlockedResources) AddAllowedDomain(domain string) bool {
	d := strings.ToLower(strings.TrimSpace(domain))
	if d == "" {
		return false
	}
	for _, existing := range b.AllowedDomains {
		if existing == d {
			return false
		}
	}
	b.AllowedDomains = append(b.AllowedDomains, d)
	return true
}

// ToCDPPattern returns the URL patterns for CDP
// Network.setBlockedURLs (spec L4368). For domain blocking it emits
// wildcard patterns of the form "*://*.<domain>/*" which match the
// domain and all subdomains over any scheme. When no domains are
// configured it returns an empty slice (type-based blocking is
// applied via Network.setRequestInterception / resource-type rules,
// not URL patterns).
func (b BlockedResources) ToCDPPattern() []string {
	patterns := make([]string, 0, len(b.BlockedDomains))
	for _, d := range b.BlockedDomains {
		domain := strings.ToLower(strings.TrimSpace(d))
		if domain == "" {
			continue
		}
		patterns = append(patterns, "*://*."+domain+"/*")
	}
	return patterns
}
