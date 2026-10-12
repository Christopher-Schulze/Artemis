package scraper

import (
	"testing"
)

// ==================== SchemaKind tests ====================

// TestTASK2240_SchemaKindConstants verifies the kind constants
// (spec L4029: kind=text|html|attr|url|number|object|list).
func TestTASK2240_SchemaKindConstants(t *testing.T) {
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

// TestTASK2240_ScalarKinds verifies the scalar kinds set
// (spec L4029, research scalarKinds).
func TestTASK2240_ScalarKinds(t *testing.T) {
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

// TestTASK2240_JoinKinds verifies the join kinds set
// (spec L4029, research joinKinds).
func TestTASK2240_JoinKinds(t *testing.T) {
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

// TestTASK2240_BackwardCompatFields verifies the old Fields-based
// API still works (backward compat with thin schema).
func TestTASK2240_BackwardCompatFields(t *testing.T) {
	s := &StructuredSchema{Fields: []string{"title", "price"}}
	req := s.Required()
	if len(req) != 2 {
		t.Fatalf("required: got %d, want 2", len(req))
	}
	if req[0] != "title" || req[1] != "price" {
		t.Errorf("required: got %v, want [title price]", req)
	}
}

// TestTASK2240_BackwardCompatEmptyFields verifies empty Fields returns
// empty required list.
func TestTASK2240_BackwardCompatEmptyFields(t *testing.T) {
	s := &StructuredSchema{Fields: []string{}}
	if len(s.Required()) != 0 {
		t.Error("empty Fields should return empty required")
	}
}

// TestTASK2240_RequiredFromFieldsMap verifies Required() collects
// from FieldsMap when Fields is empty.
func TestTASK2240_RequiredFromFieldsMap(t *testing.T) {
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

// TestTASK2240_ValidateValidScalar verifies a valid scalar schema
// passes validation (spec L4029).
func TestTASK2240_ValidateValidScalar(t *testing.T) {
	s := &StructuredSchema{
		Kind:       KindText,
		Selector:   ".title",
		IsRequired: true,
	}
	if err := s.Validate(); err != nil {
		t.Errorf("valid scalar should pass: %v", err)
	}
}

// TestTASK2240_ValidateValidObject verifies a valid object schema
// with recursive fields passes validation (spec L4029).
func TestTASK2240_ValidateValidObject(t *testing.T) {
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

// TestTASK2240_ValidateValidList verifies a valid list schema
// with item schema passes validation (spec L4029).
func TestTASK2240_ValidateValidList(t *testing.T) {
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

// TestTASK2240_ValidateUnknownKind verifies unknown kind is rejected
// (spec L4029: rejects unknown-kind).
func TestTASK2240_ValidateUnknownKind(t *testing.T) {
	s := &StructuredSchema{
		Kind:     SchemaKind("unknown"),
		Selector: ".x",
	}
	if err := s.Validate(); err == nil {
		t.Fatal("unknown kind should fail validation")
	}
}

// TestTASK2240_ValidateEmptyKind verifies empty kind is rejected
// (spec L4029).
func TestTASK2240_ValidateEmptyKind(t *testing.T) {
	s := &StructuredSchema{
		Kind:     "",
		Selector: ".x",
	}
	if err := s.Validate(); err == nil {
		t.Fatal("empty kind should fail validation")
	}
}

// TestTASK2240_ValidateMissingSelector verifies missing selector is
// rejected (spec L4029: missing-required-selector).
func TestTASK2240_ValidateMissingSelector(t *testing.T) {
	s := &StructuredSchema{
		Kind:     KindText,
		Selector: "",
	}
	if err := s.Validate(); err == nil {
		t.Fatal("missing selector should fail validation")
	}
}

// TestTASK2240_ValidateWhitespaceSelector verifies whitespace-only
// selector is rejected (spec L4029).
func TestTASK2240_ValidateWhitespaceSelector(t *testing.T) {
	s := &StructuredSchema{
		Kind:     KindText,
		Selector: "   ",
	}
	if err := s.Validate(); err == nil {
		t.Fatal("whitespace selector should fail validation")
	}
}

// TestTASK2240_ValidateAttrOnNonAttrKind verifies attr is rejected
// on non-attr kind (spec L4029: invalid-attr-on-non-attr-kind).
func TestTASK2240_ValidateAttrOnNonAttrKind(t *testing.T) {
	s := &StructuredSchema{
		Kind:     KindText,
		Selector: ".x",
		Attr:     "href",
	}
	if err := s.Validate(); err == nil {
		t.Fatal("attr on text kind should fail validation")
	}
}

// TestTASK2240_ValidateAttrOnURLKind verifies attr is allowed on
// url kind (spec L4029).
func TestTASK2240_ValidateAttrOnURLKind(t *testing.T) {
	s := &StructuredSchema{
		Kind:     KindURL,
		Selector: "a",
		Attr:     "href",
	}
	if err := s.Validate(); err != nil {
		t.Errorf("attr on url kind should pass: %v", err)
	}
}

// TestTASK2240_ValidateAttrRequiredForAttrKind verifies attr is
// required for attr kind (spec L4029).
func TestTASK2240_ValidateAttrRequiredForAttrKind(t *testing.T) {
	s := &StructuredSchema{
		Kind:     KindAttr,
		Selector: ".x",
	}
	if err := s.Validate(); err == nil {
		t.Fatal("attr kind without attr field should fail")
	}
}

// TestTASK2240_ValidateJoinOnNonJoinKind verifies join is rejected
// on non-join kind (spec L4029, research joinKinds).
func TestTASK2240_ValidateJoinOnNonJoinKind(t *testing.T) {
	s := &StructuredSchema{
		Kind:     KindHTML,
		Selector: ".x",
		Join:     ", ",
	}
	if err := s.Validate(); err == nil {
		t.Fatal("join on html kind should fail validation")
	}
}

// TestTASK2240_ValidateJoinOnTextKind verifies join is allowed on
// text kind (spec L4029).
func TestTASK2240_ValidateJoinOnTextKind(t *testing.T) {
	s := &StructuredSchema{
		Kind:     KindText,
		Selector: ".x",
		Join:     ", ",
	}
	if err := s.Validate(); err != nil {
		t.Errorf("join on text kind should pass: %v", err)
	}
}

// TestTASK2240_ValidateInvalidCoerce verifies invalid coerce value
// is rejected (spec L4029).
func TestTASK2240_ValidateInvalidCoerce(t *testing.T) {
	s := &StructuredSchema{
		Kind:     KindText,
		Selector: ".x",
		Coerce:   "invalid",
	}
	if err := s.Validate(); err == nil {
		t.Fatal("invalid coerce should fail validation")
	}
}

// TestTASK2240_ValidateCoerceNumber verifies coerce=number is valid.
func TestTASK2240_ValidateCoerceNumber(t *testing.T) {
	s := &StructuredSchema{
		Kind:     KindText,
		Selector: ".x",
		Coerce:   "number",
	}
	if err := s.Validate(); err != nil {
		t.Errorf("coerce=number should pass: %v", err)
	}
}

// TestTASK2240_ValidateObjectWithoutFields verifies object kind
// without FieldsMap is rejected (spec L4029).
func TestTASK2240_ValidateObjectWithoutFields(t *testing.T) {
	s := &StructuredSchema{
		Kind:     KindObject,
		Selector: ".x",
	}
	if err := s.Validate(); err == nil {
		t.Fatal("object without fields should fail validation")
	}
}

// TestTASK2240_ValidateListWithoutItem verifies list kind without
// Item is rejected (spec L4029).
func TestTASK2240_ValidateListWithoutItem(t *testing.T) {
	s := &StructuredSchema{
		Kind:     KindList,
		Selector: ".x",
	}
	if err := s.Validate(); err == nil {
		t.Fatal("list without item should fail validation")
	}
}

// TestTASK2240_ValidateRecursiveObject verifies recursive object
// fields are validated (spec L4029: recursive).
func TestTASK2240_ValidateRecursiveObject(t *testing.T) {
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

// TestTASK2240_ValidateInvalidNestedField verifies invalid nested
// field is caught (spec L4029: recursive validation).
func TestTASK2240_ValidateInvalidNestedField(t *testing.T) {
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

// TestTASK2240_ValidateNilSchema verifies nil schema is rejected.
func TestTASK2240_ValidateNilSchema(t *testing.T) {
	var s *StructuredSchema
	if err := s.Validate(); err == nil {
		t.Fatal("nil schema should fail validation")
	}
}

// ==================== IsScalar / IsContainer tests ====================

// TestTASK2240_IsScalar verifies scalar kind detection.
func TestTASK2240_IsScalar(t *testing.T) {
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

// TestTASK2240_IsContainer verifies container kind detection.
func TestTASK2240_IsContainer(t *testing.T) {
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

// TestTASK2240_IsScalarNilSafe verifies nil schema is safe.
func TestTASK2240_IsScalarNilSafe(t *testing.T) {
	var s *StructuredSchema
	if s.IsScalar() {
		t.Error("nil should not be scalar")
	}
	if s.IsContainer() {
		t.Error("nil should not be container")
	}
}

// ==================== Hash tests ====================

// TestTASK2240_HashDeterministic verifies the schema hash is
// deterministic (spec L4029: cache-key).
func TestTASK2240_HashDeterministic(t *testing.T) {
	s := &StructuredSchema{Kind: KindText, Selector: ".title"}
	h1 := s.Hash()
	h2 := s.Hash()
	if h1 != h2 {
		t.Error("hash should be deterministic")
	}
}

// TestTASK2240_HashDifferentSchemas verifies different schemas have
// different hashes (spec L4029: cache-key).
func TestTASK2240_HashDifferentSchemas(t *testing.T) {
	s1 := &StructuredSchema{Kind: KindText, Selector: ".title"}
	s2 := &StructuredSchema{Kind: KindText, Selector: ".price"}
	if s1.Hash() == s2.Hash() {
		t.Error("different schemas should have different hashes")
	}
}

// TestTASK2240_HashLength verifies hash is 16 chars (SHA-256 truncated).
func TestTASK2240_HashLength(t *testing.T) {
	s := &StructuredSchema{Kind: KindText, Selector: ".x"}
	h := s.Hash()
	if len(h) != 16 {
		t.Errorf("hash length: got %d, want 16", len(h))
	}
}

// TestTASK2240_HashNilSafe verifies nil schema hash is empty.
func TestTASK2240_HashNilSafe(t *testing.T) {
	var s *StructuredSchema
	if s.Hash() != "" {
		t.Error("nil hash should be empty")
	}
}

// ==================== ValidateSchema (root) tests ====================

// TestTASK2240_ValidateSchemaRootObject verifies root=object passes
// (spec L4029, research validateStructuredExtractSchema).
func TestTASK2240_ValidateSchemaRootObject(t *testing.T) {
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

// TestTASK2240_ValidateSchemaRootList verifies root=list passes
// (spec L4029).
func TestTASK2240_ValidateSchemaRootList(t *testing.T) {
	s := &StructuredSchema{
		Kind:     KindList,
		Selector: ".items",
		Item:     &StructuredSchema{Kind: KindText, Selector: "li"},
	}
	if err := ValidateSchema(s); err != nil {
		t.Errorf("root list should pass: %v", err)
	}
}

// TestTASK2240_ValidateSchemaRootScalar verifies root=scalar is
// rejected (spec L4029: root must be object or list).
func TestTASK2240_ValidateSchemaRootScalar(t *testing.T) {
	s := &StructuredSchema{
		Kind:     KindText,
		Selector: ".x",
	}
	if err := ValidateSchema(s); err == nil {
		t.Fatal("root scalar should fail ValidateSchema")
	}
}

// TestTASK2240_ValidateSchemaNil verifies nil root is rejected.
func TestTASK2240_ValidateSchemaNil(t *testing.T) {
	if err := ValidateSchema(nil); err == nil {
		t.Fatal("nil root should fail ValidateSchema")
	}
}

// ==================== CompileSchema tests ====================

// TestTASK2240_CompileSchemaValid verifies CompileSchema returns a
// hash for a valid schema (spec L4029: cache-key).
func TestTASK2240_CompileSchemaValid(t *testing.T) {
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

// TestTASK2240_CompileSchemaInvalid verifies CompileSchema returns
// an error for invalid schema.
func TestTASK2240_CompileSchemaInvalid(t *testing.T) {
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

// TestTASK2240_NewStructuredExtractResult verifies result creation
// (spec L4029: StructuredExtractResult).
func TestTASK2240_NewStructuredExtractResult(t *testing.T) {
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

// TestTASK2240_ResultAddError verifies error appending.
func TestTASK2240_ResultAddError(t *testing.T) {
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

// TestTASK2240_ResultNoErrors verifies HasErrors is false for empty.
func TestTASK2240_ResultNoErrors(t *testing.T) {
	r := &StructuredExtractResult{}
	if r.HasErrors() {
		t.Error("HasErrors should be false for empty")
	}
}

// TestTASK2240_ResultAddEvidenceRef verifies evidence ref appending.
func TestTASK2240_ResultAddEvidenceRef(t *testing.T) {
	r := &StructuredExtractResult{}
	r.AddEvidenceRef("ref1")
	r.AddEvidenceRef("ref2")
	if len(r.EvidenceRefs) != 2 {
		t.Errorf("evidence refs: got %d, want 2", len(r.EvidenceRefs))
	}
}

// TestTASK2240_ResultNilSafe verifies nil result is safe.
func TestTASK2240_ResultNilSafe(t *testing.T) {
	var r *StructuredExtractResult
	r.AddError("test")
	r.AddEvidenceRef("test")
	if r.HasErrors() {
		t.Error("nil HasErrors should be false")
	}
}

// ==================== pseudo-class tests ====================

// TestTASK2240_IsSupportedPseudoClassSimple verifies simple pseudo-classes
// (spec L4029, research isSupportedPseudoClass).
func TestTASK2240_IsSupportedPseudoClassSimple(t *testing.T) {
	simple := []string{"hover", "focus", "active", "checked", "disabled"}
	for _, name := range simple {
		if !IsSupportedPseudoClass(name, false) {
			t.Errorf("%s should be supported (simple)", name)
		}
	}
}

// TestTASK2240_IsSupportedPseudoClassFunctional verifies functional
// pseudo-classes (spec L4029).
func TestTASK2240_IsSupportedPseudoClassFunctional(t *testing.T) {
	functional := []string{"not", "is", "where", "has", "lang", "dir"}
	for _, name := range functional {
		if !IsSupportedPseudoClass(name, true) {
			t.Errorf("%s should be supported (functional)", name)
		}
	}
}

// TestTASK2240_IsSupportedPseudoClassLegacy verifies legacy pseudo-elements
// (spec L4029, research legacyPseudoElementNames).
func TestTASK2240_IsSupportedPseudoClassLegacy(t *testing.T) {
	legacy := []string{"before", "after", "first-letter", "first-line"}
	for _, name := range legacy {
		if !IsSupportedPseudoClass(name, false) {
			t.Errorf("%s should be supported (legacy)", name)
		}
	}
}

// TestTASK2240_IsSupportedPseudoClassUnsupported verifies unsupported
// pseudo-classes are rejected (spec L4029).
func TestTASK2240_IsSupportedPseudoClassUnsupported(t *testing.T) {
	unsupported := []string{"unknown-pseudo", "fake-class"}
	for _, name := range unsupported {
		if IsSupportedPseudoClass(name, false) {
			t.Errorf("%s should NOT be supported", name)
		}
	}
}

// TestTASK2240_IsSupportedPseudoClassNthChild verifies nth-child
// variants are supported (spec L4029).
func TestTASK2240_IsSupportedPseudoClassNthChild(t *testing.T) {
	nthVariants := []string{"nth-child", "nth-last-child", "nth-of-type", "nth-last-of-type"}
	for _, name := range nthVariants {
		if !IsSupportedPseudoClass(name, true) {
			t.Errorf("%s should be supported (functional)", name)
		}
	}
}

// TestTASK2240_IsSupportedPseudoClassCaseInsensitive verifies
// case-insensitive matching (spec L4029).
func TestTASK2240_IsSupportedPseudoClassCaseInsensitive(t *testing.T) {
	if !IsSupportedPseudoClass("HOVER", false) {
		t.Error("HOVER should be supported (case-insensitive)")
	}
	if !IsSupportedPseudoClass("Hover", false) {
		t.Error("Hover should be supported (case-insensitive)")
	}
}

// ==================== full spec parity test ====================

// TestTASK2240_FullSpecParity verifies full spec parity for L4029
// (spec L4029).
func TestTASK2240_FullSpecParity(t *testing.T) {
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
