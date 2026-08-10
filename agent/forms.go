package agent

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/Christopher-Schulze/Artemis/webapi"
	"golang.org/x/net/html"
)

// FormField is one input/select/textarea inside a form.
type FormField struct {
	Name    string
	Type    string
	Value   string
	Checked bool
	Options []string
	node    *webapi.Node
}

// Node returns the underlying DOM node. Mutations via webapi helpers
// reflect in `Form.Submit()` since fields are scanned at submit time.
func (f *FormField) Node() *webapi.Node { return f.node }

// Form is a parsed HTML form.
type Form struct {
	Action  string
	Method  string
	EncType string
	doc     *webapi.Document
	node    *webapi.Node
}

// Node returns the underlying form DOM node.
func (f *Form) Node() *webapi.Node { return f.node }

// FormSubmission is the resolved request that submitting the form would
// produce. Callers feed it to `engine.Page.Submit` or build their own
// HTTP request with these fields.
type FormSubmission struct {
	URL         string
	Method      string
	ContentType string
	Body        []byte
}

const (
	FormEncodingURLEncoded = "application/x-www-form-urlencoded"
	FormEncodingMultipart  = "multipart/form-data"
	FormEncodingText       = "text/plain"
)

// ErrFormSubmissionRequiresBrowser classifies form submissions that need the
// governed Chromium action runtime rather than renderless HTTP encoding.
var ErrFormSubmissionRequiresBrowser = errors.New("form submission requires browser execution")

// FormSubmissionUnsupportedError is a value-free escalation result. It never
// includes field contents or local file paths.
type FormSubmissionUnsupportedError struct {
	FieldName string
	EncType   string
	Reason    string
}

func (e *FormSubmissionUnsupportedError) Error() string {
	if e == nil {
		return ErrFormSubmissionRequiresBrowser.Error()
	}
	return fmt.Sprintf("%s: field=%q enctype=%q reason=%s", ErrFormSubmissionRequiresBrowser, e.FieldName, e.EncType, e.Reason)
}

func (e *FormSubmissionUnsupportedError) Unwrap() error {
	return ErrFormSubmissionRequiresBrowser
}

// Forms returns every <form> on the document.
func Forms(d *webapi.Document) []*Form {
	if d == nil {
		return nil
	}
	nodes, err := d.QuerySelectorAll("form")
	if err != nil {
		return nil
	}
	out := make([]*Form, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, formFromNode(d, n))
	}
	return out
}

// FindForm returns the first form matching the CSS selector, or nil.
// The selector is matched against the document, then the result must
// itself be a <form> or contain a single form ancestor.
func FindForm(d *webapi.Document, selector string) *Form {
	if d == nil {
		return nil
	}
	n, err := d.QuerySelector(selector)
	if err != nil || n == nil {
		return nil
	}
	if n.Tag() != "form" {
		// climb to nearest form ancestor
		for p := n.Parent(); p != nil; p = p.Parent() {
			if p.Tag() == "form" {
				n = p
				break
			}
		}
		if n.Tag() != "form" {
			return nil
		}
	}
	return formFromNode(d, n)
}

func formFromNode(d *webapi.Document, n *webapi.Node) *Form {
	method := "GET"
	switch asciiLower(n.AttrOrEmpty("method")) {
	case "post":
		method = "POST"
	case "dialog":
		method = "DIALOG"
	default:
		method = "GET"
	}
	encType := asciiLower(n.AttrOrEmpty("enctype"))
	switch encType {
	case FormEncodingMultipart, FormEncodingText:
	default:
		encType = FormEncodingURLEncoded
	}
	f := &Form{
		doc:     d,
		node:    n,
		Action:  n.AttrOrEmpty("action"),
		Method:  method,
		EncType: encType,
	}
	return f
}

// Fields returns the current state of every named input/select/textarea
// inside the form. Read-after-write reflects mutations done via Set or
// Toggle.
func (f *Form) Fields() []FormField {
	if f == nil || f.node == nil {
		return nil
	}
	var out []FormField
	for _, sel := range []string{"input", "select", "textarea"} {
		nodes, _ := f.node.QuerySelectorAll(sel)
		for _, n := range nodes {
			name := n.AttrOrEmpty("name")
			if name == "" {
				continue
			}
			ff := FormField{Name: name, Type: strings.ToLower(n.AttrOrEmpty("type")), node: n}
			switch sel {
			case "input":
				if ff.Type == "" {
					ff.Type = "text"
				}
				ff.Value = n.AttrOrEmpty("value")
				if ff.Type == "checkbox" || ff.Type == "radio" {
					_, ff.Checked = n.Attr("checked")
				}
			case "textarea":
				ff.Value = n.Text()
			case "select":
				opts, _ := n.QuerySelectorAll("option")
				for _, o := range opts {
					ff.Options = append(ff.Options, o.AttrOrEmpty("value"))
					if _, sel := o.Attr("selected"); sel {
						ff.Value = o.AttrOrEmpty("value")
					}
				}
				if ff.Value == "" && len(ff.Options) > 0 {
					ff.Value = ff.Options[0]
				}
			}
			out = append(out, ff)
		}
	}
	return out
}

// Set updates the named field's value in the underlying DOM. For select
// it sets the `selected` attribute on the matching option. For radio
// groups it ensures only the matching value is checked.
func (f *Form) Set(name, value string) error {
	if f == nil || f.node == nil {
		return fmt.Errorf("nil form")
	}
	// inputs (text, hidden, etc.) and radio groups
	inputs, _ := f.node.QuerySelectorAll("input")
	var matched bool
	for _, n := range inputs {
		if n.AttrOrEmpty("name") != name {
			continue
		}
		t := strings.ToLower(n.AttrOrEmpty("type"))
		if t == "radio" {
			if n.AttrOrEmpty("value") == value {
				webapi.SetAttribute(n, "checked", "checked")
			} else {
				webapi.RemoveAttribute(n, "checked")
			}
			matched = true
			continue
		}
		webapi.SetAttribute(n, "value", value)
		matched = true
	}
	if matched {
		return nil
	}
	// textareas
	tas, _ := f.node.QuerySelectorAll("textarea")
	for _, n := range tas {
		if n.AttrOrEmpty("name") == name {
			webapi.SetTextContent(n, value)
			return nil
		}
	}
	// selects
	selects, _ := f.node.QuerySelectorAll("select")
	for _, n := range selects {
		if n.AttrOrEmpty("name") != name {
			continue
		}
		opts, _ := n.QuerySelectorAll("option")
		for _, o := range opts {
			if o.AttrOrEmpty("value") == value {
				webapi.SetAttribute(o, "selected", "selected")
			} else {
				webapi.RemoveAttribute(o, "selected")
			}
		}
		return nil
	}
	return fmt.Errorf("form has no field named %q", name)
}

// Toggle sets or clears the checked state on a checkbox/radio.
func (f *Form) Toggle(name string, checked bool) error {
	if f == nil || f.node == nil {
		return fmt.Errorf("nil form")
	}
	inputs, _ := f.node.QuerySelectorAll("input")
	for _, n := range inputs {
		if n.AttrOrEmpty("name") != name {
			continue
		}
		if checked {
			webapi.SetAttribute(n, "checked", "checked")
		} else {
			webapi.RemoveAttribute(n, "checked")
		}
		return nil
	}
	return fmt.Errorf("form has no checkbox/radio named %q", name)
}

// Submit returns the FormSubmission representing a submission without an
// activated submitter. File controls require the governed Chromium upload
// runtime and fail closed before a request can be emitted.
func (f *Form) Submit() (FormSubmission, error) {
	if f == nil || f.node == nil || f.doc == nil {
		return FormSubmission{}, fmt.Errorf("nil form")
	}
	if f.Method == "DIALOG" {
		return FormSubmission{}, &FormSubmissionUnsupportedError{EncType: f.EncType, Reason: "dialog submission has no renderless HTTP equivalent"}
	}
	controls, err := f.successfulControls()
	if err != nil {
		return FormSubmission{}, err
	}
	target, err := f.resolveAction()
	if err != nil {
		return FormSubmission{}, err
	}
	if f.Method == "GET" {
		return encodeFormGET(target, controls)
	}
	switch f.EncType {
	case FormEncodingMultipart:
		body, contentType, err := encodeMultipartControls(controls, "")
		if err != nil {
			return FormSubmission{}, err
		}
		return FormSubmission{URL: target, Method: "POST", ContentType: contentType, Body: body}, nil
	case FormEncodingText:
		return FormSubmission{URL: target, Method: "POST", ContentType: FormEncodingText, Body: encodeTextControls(controls)}, nil
	default:
		return FormSubmission{URL: target, Method: "POST", ContentType: FormEncodingURLEncoded, Body: []byte(encodeURLEncodedControls(controls))}, nil
	}
}

type formControl struct {
	name  string
	value string
}

func (f *Form) successfulControls() ([]formControl, error) {
	controls := make([]formControl, 0)
	var collectErr error
	explicitAssociationAllowed := f.isFirstFormWithID()
	webapi.WalkDocument(f.doc, func(node *webapi.Node) webapi.WalkAction {
		if collectErr != nil {
			return webapi.WalkStop
		}
		if !f.ownsControl(node, explicitAssociationAllowed) || controlDisabled(node) || hasAncestorTag(node, "datalist") {
			return webapi.WalkContinue
		}
		name := node.AttrOrEmpty("name")
		if name == "" {
			return webapi.WalkContinue
		}
		if strings.ContainsRune(name, '\x00') {
			collectErr = &FormSubmissionUnsupportedError{FieldName: name, EncType: f.EncType, Reason: "NUL in field name is unsupported"}
			return webapi.WalkStop
		}
		values, err := successfulControlValues(node, f.EncType)
		if err != nil {
			collectErr = err
			return webapi.WalkStop
		}
		for _, value := range values {
			controls = append(controls, formControl{name: name, value: value})
		}
		return webapi.WalkContinue
	})
	return controls, collectErr
}

func (f *Form) ownsControl(node *webapi.Node, explicitAssociationAllowed bool) bool {
	if node == nil || (node.Tag() != "input" && node.Tag() != "select" && node.Tag() != "textarea") {
		return false
	}
	if formID, explicit := node.Attr("form"); explicit {
		return explicitAssociationAllowed && formID == f.node.AttrOrEmpty("id")
	}
	for parent := node.Parent(); parent != nil; parent = parent.Parent() {
		if parent.Tag() == "form" {
			return parent.Raw() == f.node.Raw()
		}
	}
	return false
}

func (f *Form) isFirstFormWithID() bool {
	formID := f.node.AttrOrEmpty("id")
	if formID == "" {
		return false
	}
	isFirst := false
	webapi.WalkDocument(f.doc, func(node *webapi.Node) webapi.WalkAction {
		if node.Tag() != "form" || node.AttrOrEmpty("id") != formID {
			return webapi.WalkContinue
		}
		isFirst = node.Raw() == f.node.Raw()
		return webapi.WalkStop
	})
	return isFirst
}

func hasAncestorTag(node *webapi.Node, tag string) bool {
	for parent := node.Parent(); parent != nil; parent = parent.Parent() {
		if parent.Tag() == tag {
			return true
		}
	}
	return false
}

func successfulControlValues(node *webapi.Node, encType string) ([]string, error) {
	if dirname := node.AttrOrEmpty("dirname"); dirname != "" {
		return nil, &FormSubmissionUnsupportedError{FieldName: node.AttrOrEmpty("name"), EncType: encType, Reason: "dirname requires computed browser directionality"}
	}
	switch node.Tag() {
	case "textarea":
		return []string{node.Text()}, nil
	case "select":
		return selectedControlValues(node), nil
	}
	controlType := canonicalInputType(node.AttrOrEmpty("type"))
	switch controlType {
	case "reset", "button", "submit", "image":
		return nil, nil
	case "file":
		return nil, &FormSubmissionUnsupportedError{FieldName: node.AttrOrEmpty("name"), EncType: encType, Reason: "file control requires governed upload"}
	case "checkbox", "radio":
		if _, checked := node.Attr("checked"); !checked {
			return nil, nil
		}
		if value, present := node.Attr("value"); present {
			return []string{value}, nil
		}
		return []string{"on"}, nil
	case "date", "month", "week", "time", "datetime-local", "number", "range", "color":
		return nil, &FormSubmissionUnsupportedError{FieldName: node.AttrOrEmpty("name"), EncType: encType, Reason: "input value requires browser sanitization"}
	default:
		if controlType == "hidden" && asciiEqualFold(node.AttrOrEmpty("name"), "_charset_") {
			return []string{"UTF-8"}, nil
		}
		value := node.AttrOrEmpty("value")
		if controlType != "hidden" {
			value = stripNewlines(value)
		}
		if controlType == "url" || controlType == "email" {
			value = strings.Trim(value, "\t\n\f\r ")
		}
		return []string{value}, nil
	}
}

func canonicalInputType(value string) string {
	value = asciiLower(value)
	switch value {
	case "hidden", "text", "search", "tel", "url", "email", "password", "date", "month", "week", "time", "datetime-local", "number", "range", "color", "checkbox", "radio", "file", "submit", "image", "reset", "button":
		return value
	default:
		return "text"
	}
}

func asciiLower(value string) string {
	var lower strings.Builder
	lower.Grow(len(value))
	for index := 0; index < len(value); index++ {
		char := value[index]
		if char >= 'A' && char <= 'Z' {
			char += 'a' - 'A'
		}
		lower.WriteByte(char)
	}
	return lower.String()
}

func asciiEqualFold(left, right string) bool {
	return asciiLower(left) == asciiLower(right)
}

func stripNewlines(value string) string {
	value = strings.ReplaceAll(value, "\r", "")
	return strings.ReplaceAll(value, "\n", "")
}

func selectedControlValues(node *webapi.Node) []string {
	options, _ := node.QuerySelectorAll("option")
	selected := make([]string, 0, len(options))
	firstEnabled := ""
	hasEnabled := false
	for _, option := range options {
		if optionDisabled(option) {
			continue
		}
		value, present := option.Attr("value")
		if !present {
			value = optionTextValue(option)
		}
		if !hasEnabled {
			firstEnabled, hasEnabled = value, true
		}
		if _, ok := option.Attr("selected"); ok {
			selected = append(selected, value)
		}
	}
	if _, multiple := node.Attr("multiple"); multiple {
		return selected
	}
	if len(selected) > 0 {
		return selected[len(selected)-1:]
	}
	displaySize, err := strconv.Atoi(strings.Trim(node.AttrOrEmpty("size"), "\t\n\f\r "))
	if err == nil && displaySize > 1 {
		return nil
	}
	if hasEnabled {
		return []string{firstEnabled}
	}
	return nil
}

func optionTextValue(option *webapi.Node) string {
	var text strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if child.Type == html.ElementNode && child.Data == "script" {
				continue
			}
			if child.Type == html.TextNode {
				text.WriteString(child.Data)
			}
			walk(child)
		}
	}
	walk(option.Raw())
	return collapseASCIIWhitespace(text.String())
}

func collapseASCIIWhitespace(value string) string {
	var collapsed strings.Builder
	pendingSpace := false
	for _, char := range value {
		if char == '\t' || char == '\n' || char == '\f' || char == '\r' || char == ' ' {
			if collapsed.Len() > 0 {
				pendingSpace = true
			}
			continue
		}
		if pendingSpace {
			collapsed.WriteByte(' ')
			pendingSpace = false
		}
		collapsed.WriteRune(char)
	}
	return collapsed.String()
}

func controlDisabled(node *webapi.Node) bool {
	if _, disabled := node.Attr("disabled"); disabled {
		return true
	}
	for parent := node.Parent(); parent != nil; parent = parent.Parent() {
		if parent.Tag() != "fieldset" {
			continue
		}
		if _, disabled := parent.Attr("disabled"); !disabled {
			continue
		}
		if legend := firstLegend(parent); legend != nil && nodeDescendsFrom(node, legend) {
			continue
		}
		return true
	}
	return false
}

func firstLegend(fieldset *webapi.Node) *webapi.Node {
	for child := fieldset.FirstChild(); child != nil; child = child.NextSibling() {
		if child.Tag() == "legend" {
			return child
		}
	}
	return nil
}

func nodeDescendsFrom(node, ancestor *webapi.Node) bool {
	for parent := node.Parent(); parent != nil; parent = parent.Parent() {
		if parent.Raw() == ancestor.Raw() {
			return true
		}
	}
	return false
}

func optionDisabled(option *webapi.Node) bool {
	if _, disabled := option.Attr("disabled"); disabled {
		return true
	}
	parent := option.Parent()
	if parent == nil || parent.Tag() != "optgroup" {
		return false
	}
	_, disabled := parent.Attr("disabled")
	return disabled
}

func (f *Form) resolveAction() (string, error) {
	base, err := url.Parse(f.doc.URL())
	if err != nil {
		return "", fmt.Errorf("parse document URL %q: %w", f.doc.URL(), err)
	}
	if f.Action == "" {
		return base.String(), nil
	}
	reference, err := url.Parse(strings.Trim(f.Action, "\t\n\f\r "))
	if err != nil {
		return "", fmt.Errorf("parse action %q: %w", f.Action, err)
	}
	return base.ResolveReference(reference).String(), nil
}

func encodeFormGET(target string, controls []formControl) (FormSubmission, error) {
	u, err := url.Parse(target)
	if err != nil {
		return FormSubmission{}, fmt.Errorf("parse action %q: %w", target, err)
	}
	u.RawQuery = encodeURLEncodedControls(controls)
	u.ForceQuery = u.RawQuery == ""
	return FormSubmission{URL: u.String(), Method: "GET"}, nil
}

func encodeURLEncodedControls(controls []formControl) string {
	parts := make([]string, len(controls))
	for index, control := range controls {
		parts[index] = encodeFormComponent(normalizeFormLineBreaks(control.name)) + "=" + encodeFormComponent(normalizeFormLineBreaks(control.value))
	}
	return strings.Join(parts, "&")
}

func encodeFormComponent(value string) string {
	const hexDigits = "0123456789ABCDEF"
	var encoded strings.Builder
	encoded.Grow(len(value))
	for _, char := range []byte(value) {
		switch {
		case char == ' ':
			encoded.WriteByte('+')
		case (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || strings.ContainsRune("*-._", rune(char)):
			encoded.WriteByte(char)
		default:
			encoded.WriteByte('%')
			encoded.WriteByte(hexDigits[char>>4])
			encoded.WriteByte(hexDigits[char&15])
		}
	}
	return encoded.String()
}

func encodeTextControls(controls []formControl) []byte {
	var body strings.Builder
	for _, control := range controls {
		body.WriteString(normalizeFormLineBreaks(control.name))
		body.WriteByte('=')
		body.WriteString(normalizeFormLineBreaks(control.value))
		body.WriteString("\r\n")
	}
	return []byte(body.String())
}

func encodeMultipartControls(controls []formControl, boundary string) ([]byte, string, error) {
	if boundary == "" {
		random := make([]byte, 24)
		if _, err := rand.Read(random); err != nil {
			return nil, "", fmt.Errorf("generate multipart boundary: %w", err)
		}
		boundary = "ArtemisFormBoundary" + hex.EncodeToString(random)
	}
	if !validMultipartBoundary(boundary) {
		return nil, "", fmt.Errorf("invalid multipart boundary")
	}
	var body bytes.Buffer
	for _, control := range controls {
		name := multipartFieldName(normalizeFormLineBreaks(control.name))
		body.WriteString("--")
		body.WriteString(boundary)
		body.WriteString("\r\nContent-Disposition: form-data; name=\"")
		body.WriteString(name)
		body.WriteString("\"\r\n\r\n")
		body.WriteString(normalizeFormLineBreaks(control.value))
		body.WriteString("\r\n")
	}
	body.WriteString("--")
	body.WriteString(boundary)
	body.WriteString("--\r\n")
	return body.Bytes(), FormEncodingMultipart + "; boundary=" + boundary, nil
}

func validMultipartBoundary(boundary string) bool {
	if len(boundary) == 0 || len(boundary) > 70 {
		return false
	}
	for _, char := range boundary {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') {
			continue
		}
		if !strings.ContainsRune("'()+_,-./:=?", char) {
			return false
		}
	}
	return true
}

func multipartFieldName(name string) string {
	replacer := strings.NewReplacer("\r", "%0D", "\n", "%0A", "\"", "%22")
	return replacer.Replace(name)
}

func normalizeFormLineBreaks(value string) string {
	value = strings.ToValidUTF8(value, "\uFFFD")
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	return strings.ReplaceAll(value, "\n", "\r\n")
}
