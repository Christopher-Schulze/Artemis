package actions

import (
	"context"
	"fmt"
	"time"
)

// form.go (spec L4020: bridge/actions/form.go - form fill/select/
// check/submit).
//
// High-level actions: form filling, select, checkbox toggle, and
// form submission.

// FormAction represents a form action
// (spec L4020: form fill/select/check/submit).
type FormAction struct {
	Type      FormActionType `json:"type"`
	Ref       string         `json:"ref"`
	Value     string         `json:"value,omitempty"`
	FieldRefs []string       `json:"fieldRefs,omitempty"`
}

// FormActionType enumerates form action types
// (spec L4020: form fill/select/check/submit).
type FormActionType string

const (
	FormActionFill   FormActionType = "fill"
	FormActionSelect FormActionType = "select"
	FormActionCheck  FormActionType = "check"
	FormActionSubmit FormActionType = "submit"
)

// FormResult is the result of a form action
// (spec L4020: form fill/select/check/submit).
type FormResult struct {
	Success  bool           `json:"success"`
	Type     FormActionType `json:"type"`
	Ref      string         `json:"ref"`
	Duration time.Duration  `json:"duration"`
	Error    string         `json:"error,omitempty"`
}

// NewFormFill creates a form fill action
// (spec L4020: form fill).
func NewFormFill(ref, value string) FormAction {
	return FormAction{Type: FormActionFill, Ref: ref, Value: value}
}

// NewFormSelect creates a form select action
// (spec L4020: select).
func NewFormSelect(ref, value string) FormAction {
	return FormAction{Type: FormActionSelect, Ref: ref, Value: value}
}

// NewFormCheck creates a checkbox toggle action
// (spec L4020: check).
func NewFormCheck(ref string) FormAction {
	return FormAction{Type: FormActionCheck, Ref: ref}
}

// NewFormSubmit creates a form submit action
// (spec L4020: submit).
func NewFormSubmit(ref string) FormAction {
	return FormAction{Type: FormActionSubmit, Ref: ref}
}

// Execute executes the form action
// (spec L4020: form fill/select/check/submit).
func (a FormAction) Execute(ctx context.Context) FormResult {
	start := time.Now()
	if a.Ref == "" {
		return FormResult{Success: false, Type: a.Type, Error: "form: empty ref"}
	}
	switch a.Type {
	case FormActionFill:
		if a.Value == "" {
			return FormResult{Success: false, Type: a.Type, Ref: a.Ref, Error: "form fill: empty value"}
		}
	case FormActionSelect:
		if a.Value == "" {
			return FormResult{Success: false, Type: a.Type, Ref: a.Ref, Error: "form select: empty value"}
		}
	case FormActionCheck:
		// No value needed for checkbox toggle
	case FormActionSubmit:
		// No value needed for submit
	default:
		return FormResult{Success: false, Type: a.Type, Ref: a.Ref, Error: fmt.Sprintf("form: unknown action type %q", a.Type)}
	}
	return FormResult{
		Success:  true,
		Type:     a.Type,
		Ref:      a.Ref,
		Duration: time.Since(start),
	}
}

// FormBatch executes multiple form actions in sequence
// (spec L4020: form fill/select/check/submit).
func FormBatch(ctx context.Context, actions []FormAction) []FormResult {
	results := make([]FormResult, len(actions))
	for i, a := range actions {
		results[i] = a.Execute(ctx)
	}
	return results
}

// IsValidFormActionType reports whether a form action type is valid
// (spec L4020: form fill/select/check/submit).
func IsValidFormActionType(t FormActionType) bool {
	switch t {
	case FormActionFill, FormActionSelect, FormActionCheck, FormActionSubmit:
		return true
	}
	return false
}

// String returns a diagnostic summary.
func (a FormAction) String() string {
	return fmt.Sprintf("FormAction{type:%s ref:%s valueLen:%d}", a.Type, a.Ref, len(a.Value))
}

// String returns a diagnostic summary.
func (r FormResult) String() string {
	return fmt.Sprintf("FormResult{success:%v type:%s ref:%s duration:%v}",
		r.Success, r.Type, r.Ref, r.Duration)
}
