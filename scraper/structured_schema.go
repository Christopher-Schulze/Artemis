package scraper

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// SchemaKind enumerates the declarative schema kinds
// (spec L4029: kind=text|html|attr|url|number|object|list).
type SchemaKind string

const (
	KindText   SchemaKind = "text"
	KindHTML   SchemaKind = "html"
	KindAttr   SchemaKind = "attr"
	KindURL    SchemaKind = "url"
	KindNumber SchemaKind = "number"
	KindObject SchemaKind = "object"
	KindList   SchemaKind = "list"
)

// ScalarKinds is the set of valid scalar kinds (spec L4029, research
// structured-extractor.ts scalarKinds).
var ScalarKinds = map[SchemaKind]bool{
	KindText:   true,
	KindHTML:   true,
	KindAttr:   true,
	KindURL:    true,
	KindNumber: true,
}

// JoinKinds is the set of scalar kinds that support the join option
// (spec L4029, research structured-extractor.ts joinKinds).
var JoinKinds = map[SchemaKind]bool{
	KindText: true,
	KindAttr: true,
	KindURL:  true,
}

// StructuredSchema is the recursive declarative extraction schema
// (spec L4029: kind=text|html|attr|url|number|object|list). Root must
// be object or list. Scalar kinds have selector+required+trim+join+
// coerce+attr. Object kind has selector+required+fields (recursive).
// List kind has selector+required+item (recursive).
//
// The Fields field is kept for backward compatibility with the
// original thin schema; new code should use Kind+Selector+FieldsMap.
type StructuredSchema struct {
	Kind       SchemaKind                   `json:"kind"`
	Selector   string                       `json:"selector,omitempty"`
	IsRequired bool                         `json:"required,omitempty"`
	Trim       bool                         `json:"trim,omitempty"`
	Join       string                       `json:"join,omitempty"`
	Coerce     string                       `json:"coerce,omitempty"`
	Attr       string                       `json:"attr,omitempty"`
	Fields     []string                     `json:"-"`                // backward compat
	FieldsMap  map[string]*StructuredSchema `json:"fields,omitempty"` // for object kind
	Item       *StructuredSchema            `json:"item,omitempty"`   // for list kind
}

// Required returns the list of required field names (backward compat
// with the original thin schema).
func (s *StructuredSchema) Required() []string {
	if s == nil {
		return nil
	}
	// Backward compat: if Fields is set, use it.
	if len(s.Fields) > 0 {
		out := make([]string, 0, len(s.Fields))
		for _, f := range s.Fields {
			if f != "" {
				out = append(out, f)
			}
		}
		return out
	}
	// New behavior: collect required fields from FieldsMap.
	if s.FieldsMap != nil {
		out := make([]string, 0, len(s.FieldsMap))
		for name, sub := range s.FieldsMap {
			if sub != nil && sub.IsRequired {
				out = append(out, name)
			}
		}
		return out
	}
	return nil
}

// Validate validates the schema recursively (spec L4029: rejects
// unknown-kind + missing-required-selector + invalid-attr-on-non-attr-kind
// + recursive-cycles). Returns an error if the schema is invalid.
func (s *StructuredSchema) Validate() error {
	return s.validateWithVisited(nil)
}

func (s *StructuredSchema) validateWithVisited(visited []string) error {
	if s == nil {
		return fmt.Errorf("schema is nil")
	}
	// Check kind is valid.
	if s.Kind == "" {
		return fmt.Errorf("schema.kind is required")
	}
	if s.Kind != KindObject && s.Kind != KindList && !ScalarKinds[s.Kind] {
		return fmt.Errorf("schema.kind %q is not a valid kind (must be text|html|attr|url|number|object|list)", s.Kind)
	}
	// Selector is required for all kinds.
	if strings.TrimSpace(s.Selector) == "" {
		return fmt.Errorf("schema.selector is required (non-empty)")
	}
	// Validate attr: only supported for kind=attr or kind=url.
	if s.Attr != "" && s.Kind != KindAttr && s.Kind != KindURL {
		return fmt.Errorf("schema.attr is only supported for kind attr and url, not %s", s.Kind)
	}
	// Validate attr is required for kind=attr.
	if s.Kind == KindAttr && s.Attr == "" {
		return fmt.Errorf("schema.attr is required for kind attr")
	}
	// Validate join: only supported for text, attr, url.
	if s.Join != "" && !JoinKinds[s.Kind] {
		return fmt.Errorf("schema.join is only supported for text, attr, and url fields, not %s", s.Kind)
	}
	// Validate coerce: must be "number" or "url".
	if s.Coerce != "" && s.Coerce != "number" && s.Coerce != "url" {
		return fmt.Errorf("schema.coerce must be \"number\" or \"url\", got %q", s.Coerce)
	}
	// Validate object kind: must have FieldsMap.
	if s.Kind == KindObject {
		if len(s.FieldsMap) == 0 {
			return fmt.Errorf("schema.fields is required for kind object")
		}
		for name, sub := range s.FieldsMap {
			path := fmt.Sprintf("fields.%s", name)
			// Check for recursive cycles.
			for _, v := range visited {
				if v == path {
					return fmt.Errorf("recursive cycle detected at %s", path)
				}
			}
			if err := sub.validateWithVisited(append(visited, path)); err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
		}
	}
	// Validate list kind: must have Item.
	if s.Kind == KindList {
		if s.Item == nil {
			return fmt.Errorf("schema.item is required for kind list")
		}
		path := "item"
		for _, v := range visited {
			if v == path {
				return fmt.Errorf("recursive cycle detected at %s", path)
			}
		}
		if err := s.Item.validateWithVisited(append(visited, path)); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	}
	return nil
}

// IsScalar returns true if the schema kind is a scalar kind.
func (s *StructuredSchema) IsScalar() bool {
	if s == nil {
		return false
	}
	return ScalarKinds[s.Kind]
}

// IsContainer returns true if the schema kind is object or list.
func (s *StructuredSchema) IsContainer() bool {
	if s == nil {
		return false
	}
	return s.Kind == KindObject || s.Kind == KindList
}

// Hash returns the SHA-256 hash of the canonical JSON representation
// (spec L4029: xxhash(schema_json) cache-key; we use SHA-256 for
// Go-native determinism without external deps).
func (s *StructuredSchema) Hash() string {
	if s == nil {
		return ""
	}
	data, err := json.Marshal(s)
	if err != nil {
		return ""
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])[:16]
}

// StructuredExtractResult is the extraction result
// (spec L4029: StructuredExtractResult{matched_count, extracted_value,
// errors[], schema_hash, extracted_at, evidence_refs[]}).
type StructuredExtractResult struct {
	MatchedCount   int         `json:"matched_count"`
	ExtractedValue interface{} `json:"extracted_value"`
	Errors         []string    `json:"errors,omitempty"`
	SchemaHash     string      `json:"schema_hash"`
	ExtractedAt    time.Time   `json:"extracted_at"`
	EvidenceRefs   []string    `json:"evidence_refs,omitempty"`
}

// NewStructuredExtractResult creates a new result with the given
// schema hash and extracted value.
func NewStructuredExtractResult(schema *StructuredSchema, value interface{}, matched int) *StructuredExtractResult {
	return &StructuredExtractResult{
		MatchedCount:   matched,
		ExtractedValue: value,
		SchemaHash:     schema.Hash(),
		ExtractedAt:    time.Now(),
	}
}

// AddError appends an error to the result.
func (r *StructuredExtractResult) AddError(msg string) {
	if r == nil {
		return
	}
	r.Errors = append(r.Errors, msg)
}

// AddEvidenceRef appends an evidence reference.
func (r *StructuredExtractResult) AddEvidenceRef(ref string) {
	if r == nil {
		return
	}
	r.EvidenceRefs = append(r.EvidenceRefs, ref)
}

// HasErrors returns true if the result has any errors.
func (r *StructuredExtractResult) HasErrors() bool {
	if r == nil {
		return false
	}
	return len(r.Errors) > 0
}

// ValidateSchema validates a root schema (must be object or list)
// (spec L4029, research structured-extractor.ts validateStructuredExtractSchema).
func ValidateSchema(schema *StructuredSchema) error {
	if schema == nil {
		return fmt.Errorf("schema is nil")
	}
	if schema.Kind != KindObject && schema.Kind != KindList {
		return fmt.Errorf("schema.kind must be object or list at the root, got %s", schema.Kind)
	}
	return schema.Validate()
}

// CompileSchema validates and returns the schema hash, ready for
// cache-key use (spec L4029: idempotent-cacheable-results with
// xxhash(schema_json) cache-key).
func CompileSchema(schema *StructuredSchema) (string, error) {
	if err := ValidateSchema(schema); err != nil {
		return "", fmt.Errorf("compile schema: %w", err)
	}
	return schema.Hash(), nil
}

// LegacyPseudoElementNames is the whitelist of legacy pseudo-element
// names allowed in CSS selectors (spec L4029, research
// structured-extractor.ts legacyPseudoElementNames).
var LegacyPseudoElementNames = map[string]bool{
	"before":       true,
	"after":        true,
	"first-letter": true,
	"first-line":   true,
}

// SimplePseudoClassNames is the whitelist of simple pseudo-class names
// allowed in CSS selectors (spec L4029, research
// structured-extractor.ts simplePseudoClassNames).
var SimplePseudoClassNames = map[string]bool{
	"active":            true,
	"any-link":          true,
	"autofill":          true,
	"blank":             true,
	"checked":           true,
	"default":           true,
	"defined":           true,
	"disabled":          true,
	"empty":             true,
	"enabled":           true,
	"first-child":       true,
	"first-of-type":     true,
	"focus":             true,
	"focus-visible":     true,
	"focus-within":      true,
	"fullscreen":        true,
	"hover":             true,
	"in-range":          true,
	"indeterminate":     true,
	"invalid":           true,
	"last-child":        true,
	"last-of-type":      true,
	"link":              true,
	"modal":             true,
	"only-child":        true,
	"only-of-type":      true,
	"optional":          true,
	"out-of-range":      true,
	"placeholder-shown": true,
	"read-only":         true,
	"read-write":        true,
	"required":          true,
	"root":              true,
	"scope":             true,
	"target":            true,
	"valid":             true,
	"visited":           true,
}

// FunctionalPseudoClassNames is the whitelist of functional pseudo-class
// names allowed in CSS selectors (spec L4029, research
// structured-extractor.ts functionalPseudoClassNames).
var FunctionalPseudoClassNames = map[string]bool{
	"current":      true,
	"dir":          true,
	"has":          true,
	"heading":      true,
	"host":         true,
	"host-context": true,
	"is":           true,
	"lang":         true,
	"not":          true,
	"nth-col":      true,
	"nth-last-col": true,
	"state":        true,
	"where":        true,
}

// IsSupportedPseudoClass checks if a pseudo-class name is supported
// (spec L4029, research structured-extractor.ts isSupportedPseudoClass).
func IsSupportedPseudoClass(name string, hasChildren bool) bool {
	name = strings.ToLower(name)
	if LegacyPseudoElementNames[name] {
		return true
	}
	if hasChildren {
		return FunctionalPseudoClassNames[name] ||
			strings.HasPrefix(name, "nth-child") ||
			strings.HasPrefix(name, "nth-last-child") ||
			strings.HasPrefix(name, "nth-of-type") ||
			strings.HasPrefix(name, "nth-last-of-type")
	}
	return SimplePseudoClassNames[name]
}
