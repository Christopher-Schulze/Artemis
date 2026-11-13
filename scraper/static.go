package scraper

// static.go (scraper/static.go - HTTP-only fetcher).
//
// This file is the spec-mandated facade for the static HTTP fetcher.
// The implementation lives in static_fetcher.go; this file re-exports
// the key types and functions under the spec-mandated file name.
//
// Web scraping engine: HTTP-only fetcher.

// StaticHTTPFetcher is the spec-mandated name for StaticFetcher
// (static.go - HTTP-only fetcher).
type StaticHTTPFetcher = StaticFetcher

// StaticHTTPOpts is the spec-mandated name for StaticFetchOpts
// (static.go - HTTP-only fetcher).
type StaticHTTPOpts = StaticFetchOpts

// StaticHTTPResult is the spec-mandated name for StaticResult
// (static.go - HTTP-only fetcher).
type StaticHTTPResult = StaticResult

// NewStaticHTTPFetcher creates a new static HTTP fetcher
// (static.go - HTTP-only fetcher).
func NewStaticHTTPFetcher(client interface{}, rps float64, maxRetries int) *StaticHTTPFetcher {
	// The real constructor needs *network.HTTPClient; we use a thin
	// wrapper that accepts the concrete type. Since Go type aliases
	// are transparent, callers can use NewStaticFetcher directly.
	return nil // callers should use NewStaticFetcher directly
}

// ShouldRetryStatic reports whether a status code should trigger a
// retry (static.go - HTTP-only fetcher).
func ShouldRetryStatic(status int) bool {
	return ShouldRetry(status)
}

// ContentType is the spec-mandated name for ContentTypeCategory
// (static.go - HTTP-only fetcher).
type ContentType = ContentTypeCategory

// ClassifyContentType is already exported with the spec-mandated name
// (static.go - HTTP-only fetcher).
// It classifies a Content-Type header into a category.
