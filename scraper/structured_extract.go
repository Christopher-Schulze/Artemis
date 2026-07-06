package scraper

// structured_extract.go (spec L4028: scraper/structured_extract.go -
// Structured-Extract Declarative Schema).
//
// This file is the spec-mandated facade for structured extraction.
// The implementation lives in structured.go and structured_schema.go;
// this file re-exports the key types and functions under the
// spec-mandated file name.
//
// Web scraping engine: Structured-Extract Declarative Schema with
// 0-LLM deterministic per-schema-extraction.

// StructuredExtractSchema is the spec-mandated name for StructuredSchema
// (spec L4028: structured_extract.go - Structured-Extract Declarative
// Schema).
type StructuredExtractSchema = StructuredSchema

// StructuredExtractResultAlias is the spec-mandated re-export of
// StructuredExtractResult (spec L4028: structured_extract.go).
type StructuredExtractResultAlias = StructuredExtractResult

// ExtractSchemaKind is the spec-mandated name for SchemaKind
// (spec L4028: structured_extract.go).
type ExtractSchemaKind = SchemaKind

// ExtractJSONLDFromHTML extracts JSON-LD structured data from HTML
// (spec L4028: structured_extract.go - Structured-Extract).
func ExtractJSONLDFromHTML(htmlDoc string) ([]StructuredRecord, error) {
	return ExtractJSONLD(htmlDoc)
}

// ExtractOpenGraphFromHTML extracts OpenGraph metadata from HTML
// (spec L4028: structured_extract.go - Structured-Extract).
func ExtractOpenGraphFromHTML(htmlDoc string) map[string]string {
	return ExtractOpenGraph(htmlDoc)
}

// NewStructuredExtractResultAlias creates a new StructuredExtractResult
// (spec L4028: structured_extract.go).
func NewStructuredExtractResultAlias(schema *StructuredExtractSchema, value interface{}, matched int) *StructuredExtractResult {
	return NewStructuredExtractResult(schema, value, matched)
}

// ScalarSchemaKinds is the spec-mandated re-export of ScalarKinds
// (spec L4028: structured_extract.go - scalarKinds).
var ScalarSchemaKinds = ScalarKinds

// JoinSchemaKinds is the spec-mandated re-export of JoinKinds
// (spec L4028: structured_extract.go - joinKinds).
var JoinSchemaKinds = JoinKinds
