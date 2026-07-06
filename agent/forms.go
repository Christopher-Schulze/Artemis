package agent

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/Christopher-Schulze/Artemis/webapi"
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
	f := &Form{
		doc:     d,
		node:    n,
		Action:  n.AttrOrEmpty("action"),
		Method:  strings.ToUpper(n.AttrOrEmpty("method")),
		EncType: strings.ToLower(n.AttrOrEmpty("enctype")),
	}
	if f.Method == "" {
		f.Method = "GET"
	}
	if f.EncType == "" {
		f.EncType = "application/x-www-form-urlencoded"
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

// Submit returns the FormSubmission representing what an actual submit
// would request. URL-encoded enctype is implemented; multipart and JSON
// bodies belong to a later TASK.
func (f *Form) Submit() (FormSubmission, error) {
	if f == nil || f.node == nil {
		return FormSubmission{}, fmt.Errorf("nil form")
	}
	values := url.Values{}
	for _, ff := range f.Fields() {
		switch ff.Type {
		case "checkbox", "radio":
			if !ff.Checked {
				continue
			}
			v := ff.Value
			if v == "" {
				v = "on"
			}
			values.Add(ff.Name, v)
		case "submit", "button", "image", "file":
			// submit/button: only the activated control would be sent;
			// agents activating the form via Submit() include none.
			// file: not supported (multipart) - skip silently.
			continue
		default:
			values.Add(ff.Name, ff.Value)
		}
	}

	target := f.Action
	if target == "" {
		target = f.doc.URL()
	} else if base, err := url.Parse(f.doc.URL()); err == nil && base != nil {
		if ref, err := url.Parse(target); err == nil {
			target = base.ResolveReference(ref).String()
		}
	}

	switch f.Method {
	case "POST":
		body := []byte(values.Encode())
		return FormSubmission{
			URL:         target,
			Method:      "POST",
			ContentType: "application/x-www-form-urlencoded",
			Body:        body,
		}, nil
	default: // GET
		u, err := url.Parse(target)
		if err != nil {
			return FormSubmission{}, fmt.Errorf("parse action %q: %w", target, err)
		}
		q := u.Query()
		for k, vs := range values {
			for _, v := range vs {
				q.Add(k, v)
			}
		}
		u.RawQuery = q.Encode()
		return FormSubmission{
			URL:    u.String(),
			Method: "GET",
		}, nil
	}
}
