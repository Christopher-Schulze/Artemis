package scraper

import (
	"testing"
)

// ==================== SchemaKind tests ====================

// TestSchemaKindConstants verifies the kind constants
// (kind=text|html|attr|url|number|object|list).
func TestSchemaKindConstants(t *testing.T) {
	if KindText != "text" {
		t.Error("KindText mismatch")
	}
	if KindHTML != "html" {
		t.Error("KindHTML mismatch")
	}
	if KindAttr != "attr" {
		t.Error("KindAttr mismatch")
	}
	if KindURL != "url" {
		t.Error("KindURL mismatch")
	}
	if KindNumber != "number" {
		t.Error("KindNumber mismatch")
	}
	if KindObject != "object" {
		t.Error("KindObject mismatch")
	}
	if KindList != "list" {
		t.Error("KindList mismatch")
	}
}

// TestScalarKinds verifies the scalar kinds set
// (research scalarKinds).
func TestScalarKinds(t *testing.T) {
	scalars := []SchemaKind{KindText, KindHTML, KindAttr, KindURL, KindNumber}
	for _, k := range scalars {
		if !ScalarKinds[k] {
			t.Errorf("%s should be a scalar kind", k)
		}
	}
	if ScalarKinds[KindObject] {
		t.Error("object should NOT be scalar")
	}
	if ScalarKinds[KindList] {
		t.Error("list should NOT be scalar")
	}
}

// TestJoinKinds verifies the join kinds set
// (research joinKinds).
func TestJoinKinds(t *testing.T) {
	joinable := []SchemaKind{KindText, KindAttr, KindURL}
	for _, k := range joinable {
		if !JoinKinds[k] {
			t.Errorf("%s should be a join kind", k)
		}
	}
	if JoinKinds[KindHTML] {
		t.Error("html should NOT be joinable")
	}
	if JoinKinds[KindNumber] {
		t.Error("number should NOT be joinable")
	}
}

// ==================== backward compat tests ====================

// TestBackwardCompatFields verifies the old Fields-based
// API still works (backward compat with thin schema).
func TestBackwardCompatFields(t *testing.T) {
	s := &StructuredSchema{Fields: []string{"title", "price"}}
	req := s.Required()
	if len(req) != 2 {
		t.Fatalf("required: got %d, want 2", len(req))
	}
	if req[0] != "title" || req[1] != "price" {
		t.Errorf("required: got %v, want [title price]", req)
	}
}

// TestBackwardCompatEmptyFields verifies empty Fields returns
// empty required list.
func TestBackwardCompatEmptyFields(t *testing.T) {
	s := &StructuredSchema{Fields: []string{}}
	if len(s.Required()) != 0 {
		t.Error("empty Fields should return empty required")
	}
}

// TestRequiredFromFieldsMap verifies Required() collects
// from FieldsMap when Fields is empty.
func TestRequiredFromFieldsMap(t *testing.T) {
	s := &StructuredSchema{
		Kind:     KindObject,
		Selector: ".root",
		FieldsMap: map[string]*StructuredSchema{
			"title": {Kind: KindText, Selector: ".title", IsRequired: true},
			"price": {Kind: KindText, Selector: ".price"},
		},
	}
	req := s.Required()
	if len(req) != 1 || req[0] != "title" {
		t.Errorf("required: got %v, want [title]", req)
	}
}

// ==================== validation tests ====================

// TestValidateValidScalar verifies a valid scalar schema
// passes validation.
func TestValidateValidScalar(t *testing.T) {
	s := &StructuredSchema{
		Kind:       KindText,
		Selector:   ".title",
		IsRequired: true,
	}
	if err := s.Validate(); err != nil {
		t.Errorf("valid scalar should pass: %v", err)
	}
}

// TestValidateValidObject verifies a valid object schema
// with recursive fields passes validation.
func TestValidateValidObject(t *testing.T) {
	s := &StructuredSchema{
		Kind:     KindObject,
		Selector: ".product",
		FieldsMap: map[string]*StructuredSchema{
			"title": {Kind: KindText, Selector: ".title"},
			"price": {Kind: KindNumber, Selector: ".price"},
		},
	}
	if err := s.Validate(); err != nil {
		t.Errorf("valid object should pass: %v", err)
	}
}

// TestValidateValidList verifies a valid list schema
// with item schema passes validation.
func TestValidateValidList(t *testing.T) {
	s := &StructuredSchema{
		Kind:     KindList,
		Selector: ".items",
		Item: &StructuredSchema{
			Kind:     KindObject,
			Selector: "li",
			FieldsMap: map[string]*StructuredSchema{
				"name": {Kind: KindText, Selector: ".name"},
			},
		},
	}
	if err := s.Validate(); err != nil {
		t.Errorf("valid list should pass: %v", err)
	}
}

// TestValidateUnknownKind verifies unknown kind is rejected
// (rejects unknown-kind).
func TestValidateUnknownKind(t *testing.T) {
	s := &StructuredSchema{
		Kind:     SchemaKind("unknown"),
		Selector: ".x",
	}
	if err := s.Validate(); err == nil {
		t.Fatal("unknown kind should fail validation")
	}
}

// TestValidateEmptyKind verifies empty kind is rejected.
func TestValidateEmptyKind(t *testing.T) {
	s := &StructuredSchema{
		Kind:     "",
		Selector: ".x",
	}
	if err := s.Validate(); err == nil {
		t.Fatal("empty kind should fail validation")
	}
}

// TestValidateMissingSelector verifies missing selector is
// rejected (missing-required-selector).
func TestValidateMissingSelector(t *testing.T) {
	s := &StructuredSchema{
		Kind:     KindText,
		Selector: "",
	}
	if err := s.Validate(); err == nil {
		t.Fatal("missing selector should fail validation")
	}
}

// TestValidateWhitespaceSelector verifies whitespace-only
// selector is rejected.
func TestValidateWhitespaceSelector(t *testing.T) {
	s := &StructuredSchema{
		Kind:     KindText,
		Selector: "   ",
	}
	if err := s.Validate(); err == nil {
		t.Fatal("whitespace selector should fail validation")
	}
}

// TestValidateAttrOnNonAttrKind verifies attr is rejected
// on non-attr kind (invalid-attr-on-non-attr-kind).
func TestValidateAttrOnNonAttrKind(t *testing.T) {
	s := &StructuredSchema{
		Kind:     KindText,
		Selector: ".x",
		Attr:     "href",
	}
	if err := s.Validate(); err == nil {
		t.Fatal("attr on text kind should fail validation")
	}
}

// TestValidateAttrOnURLKind verifies attr is allowed on
// url kind.
func TestValidateAttrOnURLKind(t *testing.T) {
	s := &StructuredSchema{
		Kind:     KindURL,
		Selector: "a",
		Attr:     "href",
	}
	if err := s.Validate(); err != nil {
		t.Errorf("attr on url kind should pass: %v", err)
	}
}

// TestValidateAttrRequiredForAttrKind verifies attr is
// required for attr kind.
func TestValidateAttrRequiredForAttrKind(t *testing.T) {
	s := &StructuredSchema{
		Kind:     KindAttr,
		Selector: ".x",
	}
	if err := s.Validate(); err == nil {
		t.Fatal("attr kind without attr field should fail")
	}
}

// TestValidateJoinOnNonJoinKind verifies join is rejected
// on non-join kind (research joinKinds).
func TestValidateJoinOnNonJoinKind(t *testing.T) {
	s := &StructuredSchema{
		Kind:     KindHTML,
		Selector: ".x",
		Join:     ", ",
	}
	if err := s.Validate(); err == nil {
		t.Fatal("join on html kind should fail validation")
	}
}

// TestValidateJoinOnTextKind verifies join is allowed on
// text kind.
func TestValidateJoinOnTextKind(t *testing.T) {
	s := &StructuredSchema{
		Kind:     KindText,
		Selector: ".x",
		Join:     ", ",
	}
	if err := s.Validate(); err != nil {
		t.Errorf("join on text kind should pass: %v", err)
	}
}

// TestValidateInvalidCoerce verifies invalid coerce value
// is rejected.
func TestValidateInvalidCoerce(t *testing.T) {
	s := &StructuredSchema{
		Kind:     KindText,
		Selector: ".x",
		Coerce:   "invalid",
	}
	if err := s.Validate(); err == nil {
		t.Fatal("invalid coerce should fail validation")
	}
}

// TestValidateCoerceNumber verifies coerce=number is valid.
func TestValidateCoerceNumber(t *testing.T) {
	s := &StructuredSchema{
		Kind:     KindText,
		Selector: ".x",
		Coerce:   "number",
	}
	if err := s.Validate(); err != nil {
		t.Errorf("coerce=number should pass: %v", err)
	}
}

// TestValidateObjectWithoutFields verifies object kind
// without FieldsMap is rejected.
func TestValidateObjectWithoutFields(t *testing.T) {
	s := &StructuredSchema{
		Kind:     KindObject,
		Selector: ".x",
	}
	if err := s.Validate(); err == nil {
		t.Fatal("object without fields should fail validation")
	}
}

// TestValidateListWithoutItem verifies list kind without
// Item is rejected.
func TestValidateListWithoutItem(t *testing.T) {
	s := &StructuredSchema{
		Kind:     KindList,
		Selector: ".x",
	}
	if err := s.Validate(); err == nil {
		t.Fatal("list without item should fail validation")
	}
}

// TestValidateRecursiveObject verifies recursive object
// fields are validated (recursive).
func TestValidateRecursiveObject(t *testing.T) {
	s := &StructuredSchema{
		Kind:     KindObject,
		Selector: ".root",
		FieldsMap: map[string]*StructuredSchema{
			"nested": {
				Kind:     KindObject,
				Selector: ".nested",
				FieldsMap: map[string]*StructuredSchema{
					"deep": {Kind: KindText, Selector: ".deep"},
				},
			},
		},
	}
	if err := s.Validate(); err != nil {
		t.Errorf("recursive object should pass: %v", err)
	}
}

// TestValidateInvalidNestedField verifies invalid nested
// field is caught (recursive validation).
func TestValidateInvalidNestedField(t *testing.T) {
	s := &StructuredSchema{
		Kind:     KindObject,
		Selector: ".root",
		FieldsMap: map[string]*StructuredSchema{
			"nested": {
				Kind:     KindText,
				Selector: "", // missing selector
			},
		},
	}
	if err := s.Validate(); err == nil {
		t.Fatal("invalid nested field should fail validation")
	}
}

// TestValidateNilSchema verifies nil schema is rejected.
func TestValidateNilSchema(t *testing.T) {
	var s *StructuredSchema
	if err := s.Validate(); err == nil {
		t.Fatal("nil schema should fail validation")
	}
}

// ==================== IsScalar / IsContainer tests ====================

// TestIsScalar verifies scalar kind detection.
func TestIsScalar(t *testing.T) {
	scalars := []SchemaKind{KindText, KindHTML, KindAttr, KindURL, KindNumber}
	for _, k := range scalars {
		s := &StructuredSchema{Kind: k, Selector: ".x"}
		if !s.IsScalar() {
			t.Errorf("%s should be scalar", k)
		}
		if s.IsContainer() {
			t.Errorf("%s should NOT be container", k)
		}
	}
}

// TestIsContainer verifies container kind detection.
func TestIsContainer(t *testing.T) {
	containers := []SchemaKind{KindObject, KindList}
	for _, k := range containers {
		s := &StructuredSchema{Kind: k, Selector: ".x"}
		if !s.IsContainer() {
			t.Errorf("%s should be container", k)
		}
		if s.IsScalar() {
			t.Errorf("%s should NOT be scalar", k)
		}
	}
}

// TestIsScalarNilSafe verifies nil schema is safe.
func TestIsScalarNilSafe(t *testing.T) {
	var s *StructuredSchema
	if s.IsScalar() {
		t.Error("nil should not be scalar")
	}
	if s.IsContainer() {
		t.Error("nil should not be container")
	}
}

// ==================== Hash tests ====================

// TestHashDeterministic verifies the schema hash is
// deterministic (cache-key).
func TestHashDeterministic(t *testing.T) {
	s := &StructuredSchema{Kind: KindText, Selector: ".title"}
	h1 := s.Hash()
	h2 := s.Hash()
	if h1 != h2 {
		t.Error("hash should be deterministic")
	}
}

// TestHashDifferentSchemas verifies different schemas have
// different hashes (cache-key).
func TestHashDifferentSchemas(t *testing.T) {
	s1 := &StructuredSchema{Kind: KindText, Selector: ".title"}
	s2 := &StructuredSchema{Kind: KindText, Selector: ".price"}
	if s1.Hash() == s2.Hash() {
		t.Error("different schemas should have different hashes")
	}
}

// TestHashLength verifies hash is 16 chars (SHA-256 truncated).
func TestHashLength(t *testing.T) {
	s := &StructuredSchema{Kind: KindText, Selector: ".x"}
	h := s.Hash()
	if len(h) != 16 {
		t.Errorf("hash length: got %d, want 16", len(h))
	}
}

// TestHashNilSafe verifies nil schema hash is empty.
func TestHashNilSafe(t *testing.T) {
	var s *StructuredSchema
	if s.Hash() != "" {
		t.Error("nil hash should be empty")
	}
}

// ==================== ValidateSchema (root) tests ====================

// TestValidateSchemaRootObject verifies root=object passes
// (research validateStructuredExtractSchema).
func TestValidateSchemaRootObject(t *testing.T) {
	s := &StructuredSchema{
		Kind:     KindObject,
		Selector: ".root",
		FieldsMap: map[string]*StructuredSchema{
			"title": {Kind: KindText, Selector: ".title"},
		},
	}
	if err := ValidateSchema(s); err != nil {
		t.Errorf("root object should pass: %v", err)
	}
}

// TestValidateSchemaRootList verifies root=list passes.
func TestValidateSchemaRootList(t *testing.T) {
	s := &StructuredSchema{
		Kind:     KindList,
		Selector: ".items",
		Item:     &StructuredSchema{Kind: KindText, Selector: "li"},
	}
	if err := ValidateSchema(s); err != nil {
		t.Errorf("root list should pass: %v", err)
	}
}

// TestValidateSchemaRootScalar verifies root=scalar is
// rejected (root must be object or list).
func TestValidateSchemaRootScalar(t *testing.T) {
	s := &StructuredSchema{
		Kind:     KindText,
		Selector: ".x",
	}
	if err := ValidateSchema(s); err == nil {
		t.Fatal("root scalar should fail ValidateSchema")
	}
}

// TestValidateSchemaNil verifies nil root is rejected.
func TestValidateSchemaNil(t *testing.T) {
	if err := ValidateSchema(nil); err == nil {
		t.Fatal("nil root should fail ValidateSchema")
	}
}

// ==================== CompileSchema tests ====================

// TestCompileSchemaValid verifies CompileSchema returns a
// hash for a valid schema (cache-key).
func TestCompileSchemaValid(t *testing.T) {
	s := &StructuredSchema{
		Kind:     KindObject,
		Selector: ".root",
		FieldsMap: map[string]*StructuredSchema{
			"title": {Kind: KindText, Selector: ".title"},
		},
	}
	hash, err := CompileSchema(s)
	if err != nil {
		t.Fatalf("CompileSchema: %v", err)
	}
	if len(hash) != 16 {
		t.Errorf("hash length: got %d, want 16", len(hash))
	}
}

// TestCompileSchemaInvalid verifies CompileSchema returns
// an error for invalid schema.
func TestCompileSchemaInvalid(t *testing.T) {
	s := &StructuredSchema{
		Kind:     KindText,
		Selector: ".x",
	}
	_, err := CompileSchema(s)
	if err == nil {
		t.Fatal("invalid schema should fail CompileSchema")
	}
}

// ==================== StructuredExtractResult tests ====================

// TestNewStructuredExtractResult verifies result creation
// (StructuredExtractResult).
func TestNewStructuredExtractResult(t *testing.T) {
	s := &StructuredSchema{Kind: KindText, Selector: ".x"}
	r := NewStructuredExtractResult(s, "test value", 5)
	if r.MatchedCount != 5 {
		t.Errorf("matched: got %d, want 5", r.MatchedCount)
	}
	if r.ExtractedValue != "test value" {
		t.Errorf("value: got %v, want test value", r.ExtractedValue)
	}
	if r.SchemaHash != s.Hash() {
		t.Error("schema hash mismatch")
	}
	if r.ExtractedAt.IsZero() {
		t.Error("extracted_at should be set")
	}
}

// TestResultAddError verifies error appending.
func TestResultAddError(t *testing.T) {
	r := &StructuredExtractResult{}
	r.AddError("error 1")
	r.AddError("error 2")
	if len(r.Errors) != 2 {
		t.Errorf("errors: got %d, want 2", len(r.Errors))
	}
	if !r.HasErrors() {
		t.Error("HasErrors should be true")
	}
}

// TestResultNoErrors verifies HasErrors is false for empty.
func TestResultNoErrors(t *testing.T) {
	r := &StructuredExtractResult{}
	if r.HasErrors() {
		t.Error("HasErrors should be false for empty")
	}
}

// TestResultAddEvidenceRef verifies evidence ref appending.
func TestResultAddEvidenceRef(t *testing.T) {
	r := &StructuredExtractResult{}
	r.AddEvidenceRef("ref1")
	r.AddEvidenceRef("ref2")
	if len(r.EvidenceRefs) != 2 {
		t.Errorf("evidence refs: got %d, want 2", len(r.EvidenceRefs))
	}
}

// TestResultNilSafe verifies nil result is safe.
func TestResultNilSafe(t *testing.T) {
	var r *StructuredExtractResult
	r.AddError("test")
	r.AddEvidenceRef("test")
	if r.HasErrors() {
		t.Error("nil HasErrors should be false")
	}
}

// ==================== pseudo-class tests ====================

// TestIsSupportedPseudoClassSimple verifies simple pseudo-classes
// (research isSupportedPseudoClass).
func TestIsSupportedPseudoClassSimple(t *testing.T) {
	simple := []string{"hover", "focus", "active", "checked", "disabled"}
	for _, name := range simple {
		if !IsSupportedPseudoClass(name, false) {
			t.Errorf("%s should be supported (simple)", name)
		}
	}
}

// TestIsSupportedPseudoClassFunctional verifies functional
// pseudo-classes.
func TestIsSupportedPseudoClassFunctional(t *testing.T) {
	functional := []string{"not", "is", "where", "has", "lang", "dir"}
	for _, name := range functional {
		if !IsSupportedPseudoClass(name, true) {
			t.Errorf("%s should be supported (functional)", name)
		}
	}
}

// TestIsSupportedPseudoClassLegacy verifies legacy pseudo-elements
// (research legacyPseudoElementNames).
func TestIsSupportedPseudoClassLegacy(t *testing.T) {
	legacy := []string{"before", "after", "first-letter", "first-line"}
	for _, name := range legacy {
		if !IsSupportedPseudoClass(name, false) {
			t.Errorf("%s should be supported (legacy)", name)
		}
	}
}

// TestIsSupportedPseudoClassUnsupported verifies unsupported
// pseudo-classes are rejected.
func TestIsSupportedPseudoClassUnsupported(t *testing.T) {
	unsupported := []string{"unknown-pseudo", "fake-class"}
	for _, name := range unsupported {
		if IsSupportedPseudoClass(name, false) {
			t.Errorf("%s should NOT be supported", name)
		}
	}
}

// TestIsSupportedPseudoClassNthChild verifies nth-child
// variants are supported.
func TestIsSupportedPseudoClassNthChild(t *testing.T) {
	nthVariants := []string{"nth-child", "nth-last-child", "nth-of-type", "nth-last-of-type"}
	for _, name := range nthVariants {
		if !IsSupportedPseudoClass(name, true) {
			t.Errorf("%s should be supported (functional)", name)
		}
	}
}

// TestIsSupportedPseudoClassCaseInsensitive verifies
// case-insensitive matching.
func TestIsSupportedPseudoClassCaseInsensitive(t *testing.T) {
	if !IsSupportedPseudoClass("HOVER", false) {
		t.Error("HOVER should be supported (case-insensitive)")
	}
	if !IsSupportedPseudoClass("Hover", false) {
		t.Error("Hover should be supported (case-insensitive)")
	}
}

// ==================== parity test ====================

// TestStructuredSchemaParity verifies full parity.
func TestStructuredSchemaParity(t *testing.T) {
	// 1. All 7 kinds exist
	kinds := []SchemaKind{KindText, KindHTML, KindAttr, KindURL, KindNumber, KindObject, KindList}
	for _, k := range kinds {
		if string(k) == "" {
			t.Errorf("kind %s is empty", k)
		}
	}

	// 2. Scalar kinds
	if len(ScalarKinds) != 5 {
		t.Errorf("expected 5 scalar kinds, got %d", len(ScalarKinds))
	}

	// 3. Valid object schema
	objSchema := &StructuredSchema{
		Kind:     KindObject,
		Selector: ".product",
		FieldsMap: map[string]*StructuredSchema{
			"title": {Kind: KindText, Selector: ".title"},
			"link":  {Kind: KindURL, Selector: "a", Attr: "href"},
			"price": {Kind: KindNumber, Selector: ".price"},
		},
	}
	if err := ValidateSchema(objSchema); err != nil {
		t.Fatalf("valid object schema: %v", err)
	}

	// 4. Valid list schema
	listSchema := &StructuredSchema{
		Kind:     KindList,
		Selector: ".items",
		Item: &StructuredSchema{
			Kind:     KindObject,
			Selector: "li",
			FieldsMap: map[string]*StructuredSchema{
				"name": {Kind: KindText, Selector: ".name"},
			},
		},
	}
	if err := ValidateSchema(listSchema); err != nil {
		t.Fatalf("valid list schema: %v", err)
	}

	// 5. Schema hash
	h1 := objSchema.Hash()
	if len(h1) != 16 {
		t.Error("hash should be 16 chars")
	}

	// 6. Compile schema
	hash, err := CompileSchema(objSchema)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if hash != h1 {
		t.Error("compile hash should match schema hash")
	}

	// 7. Extract result
	r := NewStructuredExtractResult(objSchema, "value", 3)
	if r.MatchedCount != 3 || r.SchemaHash != h1 {
		t.Error("extract result mismatch")
	}

	// 8. Validation rejects unknown kind
	bad := &StructuredSchema{Kind: SchemaKind("unknown"), Selector: ".x"}
	if err := bad.Validate(); err == nil {
		t.Error("unknown kind should be rejected")
	}

	// 9. Validation rejects missing selector
	bad2 := &StructuredSchema{Kind: KindText, Selector: ""}
	if err := bad2.Validate(); err == nil {
		t.Error("missing selector should be rejected")
	}

	// 10. Validation rejects attr on non-attr kind
	bad3 := &StructuredSchema{Kind: KindText, Selector: ".x", Attr: "href"}
	if err := bad3.Validate(); err == nil {
		t.Error("attr on text kind should be rejected")
	}
}
