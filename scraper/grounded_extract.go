package scraper

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/Christopher-Schulze/Artemis/security"
)

// grounded_extract.go is the source-grounded form of deterministic
// structured extraction (spec L4029 SourceSnapshot -> ExtractedPage,
// L4375-L4448 Scrapling rebuild). Every schema field yields evidence: the
// source it came from, the selector or JSON pointer, the DOM path of the
// node and a bounded excerpt. Missing, conflicting, truncated, invalid and
// withheld fields stay distinct; none of them becomes a value. No inference
// is involved.

const (
	maxGroundedExcerpt    = 512
	maxGroundedCandidates = 8
	maxGroundedFields     = 4096
)

// FieldStatus is the evidence state of one extracted field.
type FieldStatus string

const (
	FieldFound     FieldStatus = "found"
	FieldMissing   FieldStatus = "missing"
	FieldConflict  FieldStatus = "conflict"
	FieldTruncated FieldStatus = "truncated"
	FieldInvalid   FieldStatus = "invalid"
	FieldWithheld  FieldStatus = "withheld"
)

// Extraction source kinds.
const (
	SourceDOM          = "dom"
	SourceJSONResponse = "json_response"
)

// ExtractionSource identifies the captured document a field came from.
type ExtractionSource struct {
	URL         string                `json:"url"`
	Kind        string                `json:"kind"`
	CapturedAt  time.Time             `json:"capturedAt"`
	ContentHash string                `json:"contentHash"`
	Bytes       int                   `json:"bytes"`
	Truncated   bool                  `json:"truncated,omitempty"`
	Trust       security.ContentTrust `json:"trust"`
}

// FieldEvidence grounds one field: Selector is the CSS selector or JSON
// pointer, NodePath the DOM path of the source node, Excerpt the bounded
// source text. Candidates lists the distinct values of a conflict.
type FieldEvidence struct {
	Path       string      `json:"path"`
	Status     FieldStatus `json:"status"`
	Value      any         `json:"value,omitempty"`
	Selector   string      `json:"selector"`
	NodePath   string      `json:"nodePath,omitempty"`
	Excerpt    string      `json:"excerpt,omitempty"`
	Matches    int         `json:"matches"`
	Candidates []any       `json:"candidates,omitempty"`
	Reason     string      `json:"reason,omitempty"`
	Source     int         `json:"source"`
}

// FieldCounts summarizes the field states of an extraction.
type FieldCounts struct {
	Found     int `json:"found"`
	Missing   int `json:"missing"`
	Conflict  int `json:"conflict"`
	Truncated int `json:"truncated"`
	Invalid   int `json:"invalid"`
	Withheld  int `json:"withheld"`
}

// GroundedExtraction is a structured value with per-field evidence. It is
// Complete only when every required field was found and no field is in
// conflict, invalid, truncated or withheld.
type GroundedExtraction struct {
	Sources    []ExtractionSource `json:"sources"`
	Value      any                `json:"value"`
	Fields     []FieldEvidence    `json:"fields"`
	Counts     FieldCounts        `json:"counts"`
	Complete   bool               `json:"complete"`
	StopReason string             `json:"stopReason,omitempty"`
}

type groundedWalk struct {
	source    int
	truncated bool
	baseURL   string
	fields    []FieldEvidence
	required  map[string]bool
}

// ExtractGroundedHTML runs a validated declarative schema against htmlDoc and
// returns the value with evidence for every field. source describes the
// capture; its hash and size are computed here.
func ExtractGroundedHTML(htmlDoc string, schema *StructuredSchema, source ExtractionSource) (*GroundedExtraction, error) {
	if err := ValidateSchema(schema); err != nil {
		return nil, fmt.Errorf("grounded extract: validate schema: %w", err)
	}
	document, err := html.Parse(strings.NewReader(htmlDoc))
	if err != nil {
		return nil, fmt.Errorf("grounded extract: parse html: %w", err)
	}
	source.Kind = SourceDOM
	source.ContentHash, source.Bytes = contentHash([]byte(htmlDoc)), len(htmlDoc)
	walk := &groundedWalk{truncated: source.Truncated, baseURL: source.URL, required: map[string]bool{}}
	value := walk.node(schema, document, "$", schema.IsRequired)
	result := &GroundedExtraction{Sources: []ExtractionSource{source}, Value: value, Fields: walk.fields}
	result.summarize(walk.required)
	return result, nil
}

func (w *groundedWalk) add(field FieldEvidence, required bool) {
	if len(w.fields) == maxGroundedFields {
		return
	}
	field.Source = w.source
	if required {
		w.required[field.Path] = true
	}
	w.fields = append(w.fields, field)
}

// absent records a field without a source node; in a truncated source
// absence is not evidence, so the field is truncated rather than missing.
func (w *groundedWalk) absent(path, selector string, required bool) {
	status, reason := FieldMissing, "selector matched nothing"
	if w.truncated {
		status, reason = FieldTruncated, "source was truncated before the field could be observed"
	}
	w.add(FieldEvidence{Path: path, Status: status, Selector: selector, Reason: reason}, required)
}

func (w *groundedWalk) node(schema *StructuredSchema, parent *html.Node, path string, required bool) any {
	matches, err := selectStructuredNodes(parent, schema.Selector)
	if err != nil {
		w.add(FieldEvidence{Path: path, Status: FieldInvalid, Selector: schema.Selector, Reason: err.Error()}, required)
		return nil
	}
	if len(matches) == 0 {
		w.absent(path, schema.Selector, required)
		return nil
	}
	switch schema.Kind {
	case KindObject:
		if len(matches) > 1 {
			w.add(FieldEvidence{Path: path, Status: FieldConflict, Selector: schema.Selector, Matches: len(matches), NodePath: domPath(matches[0]), Reason: "object selector matched several containers"}, required)
			return nil
		}
		return w.object(schema, matches[0], path)
	case KindList:
		values := make([]any, 0, len(matches))
		for index, match := range matches {
			if value := w.node(schema.Item, match, fmt.Sprintf("%s[%d]", path, index), schema.Item.IsRequired); value != nil {
				values = append(values, value)
			}
		}
		return values
	default:
		return w.scalar(schema, matches, path, required)
	}
}

func (w *groundedWalk) object(schema *StructuredSchema, container *html.Node, path string) map[string]any {
	names := make([]string, 0, len(schema.FieldsMap))
	for name := range schema.FieldsMap {
		names = append(names, name)
	}
	sort.Strings(names)
	value := make(map[string]any, len(names))
	for _, name := range names {
		field := schema.FieldsMap[name]
		if fieldValue := w.node(field, container, path+"."+name, field.IsRequired); fieldValue != nil {
			value[name] = fieldValue
		}
	}
	return value
}

// scalar keeps one value only when every match agrees (or the schema joins
// them); distinct values are a conflict, never a silent first pick.
func (w *groundedWalk) scalar(schema *StructuredSchema, matches []*html.Node, path string, required bool) any {
	type candidate struct {
		value any
		node  *html.Node
	}
	var found []candidate
	var invalid []string
	for _, node := range matches {
		value, err := extractStructuredScalarValue(schema, node, w.baseURL)
		if err != nil {
			invalid = append(invalid, err.Error())
			continue
		}
		if schema.Kind != KindNumber && strings.TrimSpace(fmt.Sprint(value)) == "" {
			continue
		}
		found = append(found, candidate{value, node})
	}
	evidence := FieldEvidence{Path: path, Selector: schema.Selector, Matches: len(matches)}
	if len(found) == 0 {
		if len(invalid) > 0 {
			evidence.Status, evidence.Reason, evidence.NodePath = FieldInvalid, invalid[0], domPath(matches[0])
			w.add(evidence, required)
			return nil
		}
		evidence.Status, evidence.Reason, evidence.NodePath = FieldMissing, "matched nodes are empty", domPath(matches[0])
		if w.truncated {
			evidence.Status = FieldTruncated
		}
		w.add(evidence, required)
		return nil
	}
	evidence.NodePath, evidence.Excerpt = domPath(found[0].node), excerpt(nodeText(found[0].node))
	if schema.Join != "" && schema.Kind != KindNumber && schema.Coerce != "number" {
		parts := make([]string, 0, len(found))
		for _, item := range found {
			parts = append(parts, fmt.Sprint(item.value))
		}
		evidence.Status, evidence.Value = FieldFound, strings.Join(parts, schema.Join)
		w.add(evidence, required)
		return evidence.Value
	}
	distinct := []any{}
	seen := map[string]bool{}
	for _, item := range found {
		key := fmt.Sprintf("%T:%v", item.value, item.value)
		if !seen[key] {
			seen[key] = true
			if len(distinct) < maxGroundedCandidates {
				distinct = append(distinct, item.value)
			}
		}
	}
	if len(seen) > 1 {
		evidence.Status, evidence.Candidates, evidence.Reason = FieldConflict, distinct, fmt.Sprintf("%d distinct values", len(seen))
		w.add(evidence, required)
		return nil
	}
	evidence.Status, evidence.Value = FieldFound, found[0].value
	w.add(evidence, required)
	return found[0].value
}

func (r *GroundedExtraction) summarize(required map[string]bool) {
	r.Counts = FieldCounts{}
	complete := true
	for _, field := range r.Fields {
		switch field.Status {
		case FieldFound:
			r.Counts.Found++
		case FieldMissing:
			r.Counts.Missing++
			if required[field.Path] {
				complete = false
			}
		case FieldConflict:
			r.Counts.Conflict++
			complete = false
		case FieldTruncated:
			r.Counts.Truncated++
			complete = false
		case FieldInvalid:
			r.Counts.Invalid++
			complete = false
		case FieldWithheld:
			r.Counts.Withheld++
			complete = false
		}
	}
	for _, source := range r.Sources {
		if source.Truncated {
			complete = false
		}
	}
	r.Complete = complete && len(r.Fields) < maxGroundedFields
}

// domPath renders the element path from the document root as
// tag:nth-of-type steps, stable for the captured document.
func domPath(node *html.Node) string {
	var steps []string
	for current := node; current != nil && current.Type == html.ElementNode; current = current.Parent {
		index := 1
		for sibling := current.PrevSibling; sibling != nil; sibling = sibling.PrevSibling {
			if sibling.Type == html.ElementNode && sibling.Data == current.Data {
				index++
			}
		}
		steps = append(steps, current.Data+":nth-of-type("+strconv.Itoa(index)+")")
	}
	for left, right := 0, len(steps)-1; left < right; left, right = left+1, right-1 {
		steps[left], steps[right] = steps[right], steps[left]
	}
	return strings.Join(steps, " > ")
}

func excerpt(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) <= maxGroundedExcerpt {
		return text
	}
	cut := maxGroundedExcerpt
	for cut > 0 && !utf8Start(text[cut]) {
		cut--
	}
	return text[:cut]
}

func utf8Start(b byte) bool {
	return b&0xC0 != 0x80
}

func contentHash(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// JSONExtractField maps one field to a JSON pointer (RFC 6901) relative to
// the item (or document) and a kind: string, number, boolean, url or any.
type JSONExtractField struct {
	Pointer  string `json:"pointer"`
	Kind     string `json:"kind,omitempty"`
	Required bool   `json:"required,omitempty"`
}

// JSONExtractSchema extracts Fields from the document, or from every element
// of the array at Items when set.
type JSONExtractSchema struct {
	Items  string                      `json:"items,omitempty"`
	Fields map[string]JSONExtractField `json:"fields"`
}

var credentialKey = regexp.MustCompile(`(?i)(pass(word|wd)?|secret|token|auth|cookie|session|api[_-]?key|credential|bearer|csrf|xsrf)`)

// Validate checks pointers, kinds and that no field reads a credential-like
// key.
func (s JSONExtractSchema) Validate() error {
	if len(s.Fields) == 0 || len(s.Fields) > 256 {
		return fmt.Errorf("json extract: 1..256 fields required")
	}
	if s.Items != "" && !strings.HasPrefix(s.Items, "/") {
		return fmt.Errorf("json extract: items must be a JSON pointer")
	}
	for name, field := range s.Fields {
		if name == "" || !strings.HasPrefix(field.Pointer, "/") && field.Pointer != "" {
			return fmt.Errorf("json extract: field %q needs a JSON pointer", name)
		}
		switch field.Kind {
		case "", "string", "number", "boolean", "url", "any":
		default:
			return fmt.Errorf("json extract: field %q has unknown kind %q", name, field.Kind)
		}
	}
	return nil
}

// ExtractGroundedJSON extracts fields from a JSON response body. Fields whose
// pointer passes through a credential-like key are withheld, so tokens and
// session values never reach the result or a model.
func ExtractGroundedJSON(body []byte, schema JSONExtractSchema, source ExtractionSource) (*GroundedExtraction, error) {
	if err := schema.Validate(); err != nil {
		return nil, err
	}
	source.Kind = SourceJSONResponse
	source.ContentHash, source.Bytes = contentHash(body), len(body)
	result := &GroundedExtraction{Sources: []ExtractionSource{source}}
	required := map[string]bool{}
	names := make([]string, 0, len(schema.Fields))
	for name := range schema.Fields {
		names = append(names, name)
		if schema.Fields[name].Required {
			required[name] = true
		}
	}
	sort.Strings(names)
	var document any
	if err := json.Unmarshal(body, &document); err != nil {
		status, reason := FieldInvalid, "response is not JSON: "+err.Error()
		if source.Truncated {
			status, reason = FieldTruncated, "response was truncated"
		}
		for _, name := range names {
			result.Fields = append(result.Fields, FieldEvidence{Path: "$." + name, Status: status, Selector: schema.Fields[name].Pointer, Reason: reason})
		}
		result.summarize(prefixRequired(required, "$."))
		return result, nil
	}
	if schema.Items == "" {
		value := map[string]any{}
		extractJSONFields(document, "", "$", names, schema, source, value, &result.Fields)
		result.Value = value
		result.summarize(prefixRequired(required, "$."))
		return result, nil
	}
	items, ok := jsonPointer(document, schema.Items)
	array, isArray := items.([]any)
	if !ok || !isArray {
		result.Fields = append(result.Fields, FieldEvidence{Path: "$", Status: FieldMissing, Selector: schema.Items, Reason: "items pointer does not name an array"})
		result.summarize(map[string]bool{"$": true})
		return result, nil
	}
	values := make([]any, 0, len(array))
	allRequired := map[string]bool{}
	for index, item := range array {
		value := map[string]any{}
		prefix := fmt.Sprintf("$[%d]", index)
		extractJSONFields(item, schema.Items+"/"+strconv.Itoa(index), prefix, names, schema, source, value, &result.Fields)
		for name := range required {
			allRequired[prefix+"."+name] = true
		}
		values = append(values, value)
	}
	result.Value = values
	result.summarize(allRequired)
	return result, nil
}

func prefixRequired(required map[string]bool, prefix string) map[string]bool {
	out := make(map[string]bool, len(required))
	for name := range required {
		out[prefix+name] = true
	}
	return out
}

func extractJSONFields(document any, basePointer, prefix string, names []string, schema JSONExtractSchema, source ExtractionSource, value map[string]any, fields *[]FieldEvidence) {
	for _, name := range names {
		field := schema.Fields[name]
		evidence := FieldEvidence{Path: prefix + "." + name, Selector: basePointer + field.Pointer}
		if credentialKey.MatchString(schema.Items + field.Pointer) {
			evidence.Status, evidence.Reason = FieldWithheld, "credential-like key is never extracted"
			*fields = append(*fields, evidence)
			continue
		}
		raw, ok := jsonPointer(document, field.Pointer)
		if !ok || raw == nil {
			evidence.Status, evidence.Reason = FieldMissing, "pointer names no value"
			if source.Truncated {
				evidence.Status = FieldTruncated
			}
			*fields = append(*fields, evidence)
			continue
		}
		evidence.Matches = 1
		if encoded, err := json.Marshal(raw); err == nil {
			evidence.Excerpt = excerpt(string(encoded))
		}
		coerced, err := coerceJSONValue(raw, field.Kind, source.URL)
		if err != nil {
			evidence.Status, evidence.Reason = FieldInvalid, err.Error()
			*fields = append(*fields, evidence)
			continue
		}
		evidence.Status, evidence.Value = FieldFound, coerced
		value[name] = coerced
		*fields = append(*fields, evidence)
	}
}

func coerceJSONValue(raw any, kind, baseURL string) (any, error) {
	switch kind {
	case "", "any":
		return raw, nil
	case "string":
		if text, ok := raw.(string); ok {
			return text, nil
		}
		return nil, fmt.Errorf("expected a string, got %T", raw)
	case "number":
		switch typed := raw.(type) {
		case float64:
			return typed, nil
		case string:
			return parseStructuredNumber(typed)
		}
		return nil, fmt.Errorf("expected a number, got %T", raw)
	case "boolean":
		if flag, ok := raw.(bool); ok {
			return flag, nil
		}
		return nil, fmt.Errorf("expected a boolean, got %T", raw)
	case "url":
		text, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("expected a URL string, got %T", raw)
		}
		return resolveStructuredURL(text, baseURL)
	}
	return nil, fmt.Errorf("unknown kind %q", kind)
}

// jsonPointer resolves an RFC 6901 pointer; "" names the document.
func jsonPointer(document any, pointer string) (any, bool) {
	if pointer == "" {
		return document, true
	}
	current := document
	for _, token := range strings.Split(pointer[1:], "/") {
		token = strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
		switch typed := current.(type) {
		case map[string]any:
			next, ok := typed[token]
			if !ok {
				return nil, false
			}
			current = next
		case []any:
			index, err := strconv.Atoi(token)
			if err != nil || index < 0 || index >= len(typed) {
				return nil, false
			}
			current = typed[index]
		default:
			return nil, false
		}
	}
	return current, true
}

// MergeGroundedPages concatenates page extractions in order. Field sources
// are re-indexed; list values are flattened. The merge is complete only when
// every page is complete and the caller's stop reason proves no page was
// skipped.
func MergeGroundedPages(pages []*GroundedExtraction, stopReason string, exhausted bool) *GroundedExtraction {
	merged := &GroundedExtraction{StopReason: stopReason, Fields: []FieldEvidence{}}
	var values []any
	complete := exhausted
	for _, page := range pages {
		offset := len(merged.Sources)
		merged.Sources = append(merged.Sources, page.Sources...)
		for _, field := range page.Fields {
			field.Source += offset
			merged.Fields = append(merged.Fields, field)
		}
		if list, ok := page.Value.([]any); ok {
			values = append(values, list...)
		} else if page.Value != nil {
			values = append(values, page.Value)
		}
		merged.Counts.Found += page.Counts.Found
		merged.Counts.Missing += page.Counts.Missing
		merged.Counts.Conflict += page.Counts.Conflict
		merged.Counts.Truncated += page.Counts.Truncated
		merged.Counts.Invalid += page.Counts.Invalid
		merged.Counts.Withheld += page.Counts.Withheld
		complete = complete && page.Complete
	}
	merged.Value = values
	merged.Complete = complete
	return merged
}
