package scraper

import (
	"io"
)

// parser.go (spec L4028: scraper/parser.go - HTML parser (goquery) +
// CSS/XPath selectors).
//
// This file is the spec-mandated facade for HTML parsing. The
// implementation lives in tee_parse.go and parse_pool.go; this file
// re-exports the key types and functions under the spec-mandated
// file name.
//
// Web scraping engine: HTML parser (goquery) + CSS/XPath selectors.

// ParseReaders is the spec-mandated name for TeeParseReaders
// (spec L4028: parser.go - HTML parser).
// It creates a tee that allows parsing the same content with two
// independent readers (e.g., goquery for CSS and a custom parser
// for XPath).
func ParseReaders(primary io.Reader, secondary io.Reader) (io.Reader, io.Reader, error) {
	return TeeParseReaders(primary, secondary)
}

// ParserPool is the spec-mandated name for ParseWorkerPool
// (spec L4028: parser.go - HTML parser).
type ParserPool = ParseWorkerPool

// NewParserPool creates a new parser worker pool
// (spec L4028: parser.go - HTML parser).
func NewParserPool(workers int) *ParserPool {
	return NewParseWorkerPool(workers)
}

// SnapshotPool is the spec-mandated name for SnapshotBuilderPool
// (spec L4028: parser.go - HTML parser).
type SnapshotPool = SnapshotBuilderPool

// NewSnapshotPool creates a new snapshot builder pool
// (spec L4028: parser.go).
func NewSnapshotPool(cap int) *SnapshotPool {
	return NewSnapshotBuilderPool(cap)
}
