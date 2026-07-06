// Package parser exposes HTML parsing as a thin facade over
// golang.org/x/net/html. This file implements the Scrapling parser
// result types (spec L4389): TextHandler, TextHandlers,
// AttributesHandler, Selector, Selectors and the ResultJSON helper.
//
// The Go types mirror the Python originals in
// research/webstack/Scrapling-main/scrapling/core/custom_types.py and
// scrapling/parser.py without depending on the HTML parser at runtime:
// callers populate the structs via the New* constructors (typically from
// an x/net/html unit tree) and the result types provide read-only,
// allocation-friendly accessors used by the webapi/selectors layer.
package parser

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// TextHandler wraps a single text unit. It is the Go analogue of
// Scrapling's TextHandler (a str subclass) and exposes the small read
// surface required by the selector API.
type TextHandler struct {
	value string
}

// NewTextHandler returns a TextHandler wrapping the given text.
func NewTextHandler(text string) *TextHandler {
	return &TextHandler{value: text}
}

// Text returns the wrapped text.
func (t *TextHandler) Text() string {
	if t == nil {
		return ""
	}
	return t.value
}

// HasText reports whether the wrapped text is non-empty after trimming
// surrounding whitespace. This matches the Scrapling behaviour where a
// "text" result is only considered present when there is visible
// content.
func (t *TextHandler) HasText() bool {
	if t == nil {
		return false
	}
	return strings.TrimSpace(t.value) != ""
}

// TextHandlers wraps multiple text units (a slice of TextHandler). It is
// the Go analogue of Scrapling's TextHandlers (List[TextHandler]).
type TextHandlers struct {
	items []*TextHandler
}

// NewTextHandlers returns a TextHandlers wrapping the given texts. Each
// string becomes its own TextHandler.
func NewTextHandlers(texts []string) *TextHandlers {
	items := make([]*TextHandler, 0, len(texts))
	for _, s := range texts {
		items = append(items, NewTextHandler(s))
	}
	return &TextHandlers{items: items}
}

// All returns every wrapped text as a slice of strings. The returned
// slice is a copy and may be modified by the caller without affecting
// the handler.
func (h *TextHandlers) All() []string {
	if h == nil {
		return nil
	}
	out := make([]string, 0, len(h.items))
	for _, t := range h.items {
		out = append(out, t.Text())
	}
	return out
}

// First returns the first text or "" when the handler is empty.
func (h *TextHandlers) First() string {
	if h == nil || len(h.items) == 0 {
		return ""
	}
	return h.items[0].Text()
}

// Last returns the last text or "" when the handler is empty.
func (h *TextHandlers) Last() string {
	if h == nil || len(h.items) == 0 {
		return ""
	}
	return h.items[len(h.items)-1].Text()
}

// Len returns the number of wrapped text units.
func (h *TextHandlers) Len() int {
	if h == nil {
		return 0
	}
	return len(h.items)
}

// Get returns the text at index i or "" when i is out of range. A
// negative index counts from the end of the slice (-1 is the last
// element), matching Python's negative-index semantics.
func (h *TextHandlers) Get(i int) string {
	if h == nil {
		return ""
	}
	n := len(h.items)
	if n == 0 {
		return ""
	}
	if i < 0 {
		i += n
	}
	if i < 0 || i >= n {
		return ""
	}
	return h.items[i].Text()
}

// AttributesHandler wraps an element's attributes (map[string]string).
// It is the Go analogue of Scrapling's Attributes wrapper used by
// Selector.attrib.
type AttributesHandler struct {
	attrs map[string]string
}

// NewAttributesHandler returns an AttributesHandler wrapping a copy of
// the given attribute map so callers retain ownership of their map.
func NewAttributesHandler(attrs map[string]string) *AttributesHandler {
	copied := make(map[string]string, len(attrs))
	for k, v := range attrs {
		copied[k] = v
	}
	return &AttributesHandler{attrs: copied}
}

// Get returns the attribute value for name and whether it was present.
func (a *AttributesHandler) Get(name string) (string, bool) {
	if a == nil {
		return "", false
	}
	v, ok := a.attrs[name]
	return v, ok
}

// All returns a copy of the wrapped attribute map. Mutating the returned
// map does not affect the handler.
func (a *AttributesHandler) All() map[string]string {
	if a == nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(a.attrs))
	for k, v := range a.attrs {
		out[k] = v
	}
	return out
}

// Has reports whether an attribute named name is present.
func (a *AttributesHandler) Has(name string) bool {
	if a == nil {
		return false
	}
	_, ok := a.attrs[name]
	return ok
}

// Names returns the attribute names sorted lexicographically. A stable
// order is required so callers can compare attribute sets deterministically
// and so JSON marshaling is reproducible.
func (a *AttributesHandler) Names() []string {
	if a == nil {
		return nil
	}
	out := make([]string, 0, len(a.attrs))
	for k := range a.attrs {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Selector wraps a single element selection. It is the Go analogue of
// Scrapling's Selector (Adaptor/Adaptors subset) carrying the minimal
// data required by the webapi layer: tag, direct text, all-descendant
// text, attributes and outer HTML.
type Selector struct {
	tag       string
	text      string
	allText   string
	attrs     map[string]string
	outerHTML string
}

// NewSelector returns a Selector populated with the given tag, direct
// text and attributes. GetAllText falls back to the direct text and
// HTML falls back to a synthesized tag when the caller does not supply
// them, so a Selector constructed with only the required fields is still
// usable.
func NewSelector(tag string, text string, attrs map[string]string) *Selector {
	copied := make(map[string]string, len(attrs))
	for k, v := range attrs {
		copied[k] = v
	}
	return &Selector{
		tag:       tag,
		text:      text,
		allText:   text,
		attrs:     copied,
		outerHTML: synthesizeHTML(tag, text, copied),
	}
}

// NewSelectorFull returns a Selector with every field populated. It is
// used by the HTML/webapi layer when the full unit tree is available so
// that GetAllText and HTML return the real descendant text and outer
// HTML rather than the synthesized fallback.
func NewSelectorFull(tag, text, allText string, attrs map[string]string, outerHTML string) *Selector {
	copied := make(map[string]string, len(attrs))
	for k, v := range attrs {
		copied[k] = v
	}
	html := outerHTML
	if html == "" {
		html = synthesizeHTML(tag, text, copied)
	}
	all := allText
	if all == "" {
		all = text
	}
	return &Selector{
		tag:       tag,
		text:      text,
		allText:   all,
		attrs:     copied,
		outerHTML: html,
	}
}

// Tag returns the element tag name (lowercase per HTML convention).
func (s *Selector) Tag() string {
	if s == nil {
		return ""
	}
	return s.tag
}

// Text returns the direct text of the element (not including
// descendants).
func (s *Selector) Text() string {
	if s == nil {
		return ""
	}
	return s.text
}

// Attrib returns the attribute value for name and whether it was present.
func (s *Selector) Attrib(name string) (string, bool) {
	if s == nil {
		return "", false
	}
	v, ok := s.attrs[name]
	return v, ok
}

// GetAllText returns the concatenated text of the element and all of its
// descendants. When the Selector was constructed without descendant
// information this is equivalent to Text().
func (s *Selector) GetAllText() string {
	if s == nil {
		return ""
	}
	return s.allText
}

// Attributes returns an AttributesHandler wrapping a copy of the
// element's attributes.
func (s *Selector) Attributes() AttributesHandler {
	if s == nil {
		return AttributesHandler{attrs: map[string]string{}}
	}
	copied := make(map[string]string, len(s.attrs))
	for k, v := range s.attrs {
		copied[k] = v
	}
	return AttributesHandler{attrs: copied}
}

// HTML returns the outer HTML of the element (opening tag, content,
// closing tag). When the Selector was constructed without outer HTML a
// minimal representation is synthesized from the tag, text and
// attributes.
func (s *Selector) HTML() string {
	if s == nil {
		return ""
	}
	return s.outerHTML
}

// synthesizeHTML builds a minimal outer-HTML string from tag, text and
// attributes. Attributes are emitted in sorted order for determinism.
func synthesizeHTML(tag string, text string, attrs map[string]string) string {
	if tag == "" {
		return text
	}
	var b strings.Builder
	b.WriteByte('<')
	b.WriteString(tag)
	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b.WriteByte(' ')
		b.WriteString(k)
		b.WriteString(`="`)
		b.WriteString(escapeAttrValue(attrs[k]))
		b.WriteByte('"')
	}
	b.WriteByte('>')
	b.WriteString(text)
	b.WriteString("</")
	b.WriteString(tag)
	b.WriteByte('>')
	return b.String()
}

// escapeAttrValue performs the minimal attribute-value escaping required
// for a syntactically valid HTML attribute: & and the surrounding quote.
func escapeAttrValue(v string) string {
	v = strings.ReplaceAll(v, "&", "&amp;")
	v = strings.ReplaceAll(v, `"`, "&quot;")
	return v
}

// Selectors wraps multiple element selections (a slice of Selector). It
// is the Go analogue of Scrapling's Adaptors (List[Adaptor]).
type Selectors struct {
	items []Selector
}

// NewSelectors returns a Selectors wrapping a copy of the given slice.
func NewSelectors(selectors []Selector) *Selectors {
	items := make([]Selector, len(selectors))
	copy(items, selectors)
	return &Selectors{items: items}
}

// Len returns the number of wrapped selections.
func (s *Selectors) Len() int {
	if s == nil {
		return 0
	}
	return len(s.items)
}

// First returns the first selection and whether one exists.
func (s *Selectors) First() (Selector, bool) {
	if s == nil || len(s.items) == 0 {
		return Selector{}, false
	}
	return s.items[0], true
}

// Last returns the last selection and whether one exists.
func (s *Selectors) Last() (Selector, bool) {
	if s == nil || len(s.items) == 0 {
		return Selector{}, false
	}
	return s.items[len(s.items)-1], true
}

// Get returns the selection at index i and whether it exists. A negative
// index counts from the end of the slice.
func (s *Selectors) Get(i int) (Selector, bool) {
	if s == nil {
		return Selector{}, false
	}
	n := len(s.items)
	if n == 0 {
		return Selector{}, false
	}
	if i < 0 {
		i += n
	}
	if i < 0 || i >= n {
		return Selector{}, false
	}
	return s.items[i], true
}

// All returns a copy of the wrapped selections.
func (s *Selectors) All() []Selector {
	if s == nil {
		return nil
	}
	out := make([]Selector, len(s.items))
	copy(out, s.items)
	return out
}

// Texts returns the direct text of every wrapped selection.
func (s *Selectors) Texts() []string {
	if s == nil {
		return nil
	}
	out := make([]string, 0, len(s.items))
	for i := range s.items {
		out = append(out, s.items[i].Text())
	}
	return out
}

// Tags returns the tag name of every wrapped selection.
func (s *Selectors) Tags() []string {
	if s == nil {
		return nil
	}
	out := make([]string, 0, len(s.items))
	for i := range s.items {
		out = append(out, s.items[i].Tag())
	}
	return out
}

// selectorJSON is the JSON representation of a Selector used by
// ResultJSON. Attribute keys are sorted for deterministic output.
type selectorJSON struct {
	Tag        string            `json:"tag"`
	Text       string            `json:"text"`
	Attributes map[string]string `json:"attributes"`
}

// ResultJSON marshals a Selector or Selectors to JSON. Each element is
// serialized as an object with tag, text and attributes fields. A nil
// Selector and an empty Selectors both marshal to a JSON null.
func ResultJSON(v interface{}) ([]byte, error) {
	switch val := v.(type) {
	case *Selector:
		if val == nil {
			return []byte("null"), nil
		}
		return json.Marshal(selectorJSON{
			Tag:        val.tag,
			Text:       val.text,
			Attributes: copyAttrsSorted(val.attrs),
		})
	case *Selectors:
		if val == nil {
			return []byte("null"), nil
		}
		out := make([]selectorJSON, 0, len(val.items))
		for i := range val.items {
			out = append(out, selectorJSON{
				Tag:        val.items[i].tag,
				Text:       val.items[i].text,
				Attributes: copyAttrsSorted(val.items[i].attrs),
			})
		}
		return json.Marshal(out)
	case Selector:
		return json.Marshal(selectorJSON{
			Tag:        val.tag,
			Text:       val.text,
			Attributes: copyAttrsSorted(val.attrs),
		})
	case Selectors:
		out := make([]selectorJSON, 0, len(val.items))
		for i := range val.items {
			out = append(out, selectorJSON{
				Tag:        val.items[i].tag,
				Text:       val.items[i].text,
				Attributes: copyAttrsSorted(val.items[i].attrs),
			})
		}
		return json.Marshal(out)
	default:
		return nil, fmt.Errorf("parser: ResultJSON: unsupported type %T", v)
	}
}

// copyAttrsSorted returns a copy of attrs. encoding/json sorts map keys
// when marshaling so the copy is sufficient; the helper exists to avoid
// sharing the Selector's internal map with the encoder.
func copyAttrsSorted(attrs map[string]string) map[string]string {
	out := make(map[string]string, len(attrs))
	for k, v := range attrs {
		out[k] = v
	}
	return out
}
