package engine

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/Christopher-Schulze/Artemis/agent"
	"github.com/Christopher-Schulze/Artemis/bridge/actions"
)

// page_convenience.go (TASK-2343: Page.Type / Page.Form convenience methods).
//
// These are thin convenience wrappers over bridge/actions/ that provide
// ergonomic one-liners for the most common browser interactions. They do
// NOT replace the BrowserAction interface or the actions package; they
// delegate to it. The primary interaction surface remains the actions
// package; these methods are for users who want a quick single-call API
// on the Page object.
//
// The methods accept a CSS selector string (not an eN ref) because the
// engine.Page operates on the parsed DOM, not on a live CDP session. The
// selector is resolved to a DOM element via querySelector, and the action
// is executed against that element.

// Type types text into the first element matching the given CSS selector.
// It performs a renderless DOM value mutation. Browser keystroke timing belongs
// to the canonical live action runtime and is not simulated here.
func (p *Page) Type(ctx context.Context, selector, text string) (actions.TypeResult, error) {
	if p == nil || p.document == nil {
		return actions.TypeResult{}, fmt.Errorf("page: nil page or document")
	}
	if selector == "" {
		return actions.TypeResult{}, fmt.Errorf("page.Type: empty selector")
	}
	if text == "" {
		return actions.TypeResult{}, fmt.Errorf("page.Type: empty text")
	}
	if ctx == nil {
		return actions.TypeResult{}, fmt.Errorf("page.Type: context required")
	}
	if err := p.sessionError(); err != nil {
		return actions.TypeResult{}, err
	}
	element, err := p.document.QuerySelector(selector)
	if err != nil {
		return actions.TypeResult{}, fmt.Errorf("page.Type: query %q: %w", selector, err)
	}
	if element == nil {
		return actions.TypeResult{}, fmt.Errorf("page.Type: no element matches %q", selector)
	}
	start := time.Now()
	if err := agent.Type(p.document, selector, text); err != nil {
		return actions.TypeResult{Ref: selector, Duration: time.Since(start), Error: err.Error()}, fmt.Errorf("page.Type: %w", err)
	}
	result := actions.TypeResult{Success: true, Ref: selector, CharsTyped: len(text), Duration: time.Since(start)}
	return result, nil
}

// TypeWithDelay is like Type but allows customizing the keystroke delay
// and variance (TASK-2343).
func (p *Page) TypeWithDelay(ctx context.Context, selector, text string, delay, variance time.Duration) (actions.TypeResult, error) {
	if p == nil || p.document == nil {
		return actions.TypeResult{}, fmt.Errorf("page: nil page or document")
	}
	if selector == "" {
		return actions.TypeResult{}, fmt.Errorf("page.TypeWithDelay: empty selector")
	}
	if text == "" {
		return actions.TypeResult{}, fmt.Errorf("page.TypeWithDelay: empty text")
	}
	if ctx == nil {
		return actions.TypeResult{}, fmt.Errorf("page.TypeWithDelay: context required")
	}
	if delay != 0 || variance != 0 {
		return actions.TypeResult{}, fmt.Errorf("page.TypeWithDelay: renderless timing is unsupported; use the canonical browser action runtime")
	}
	if err := p.sessionError(); err != nil {
		return actions.TypeResult{}, err
	}
	element, err := p.document.QuerySelector(selector)
	if err != nil {
		return actions.TypeResult{}, fmt.Errorf("page.TypeWithDelay: query %q: %w", selector, err)
	}
	if element == nil {
		return actions.TypeResult{}, fmt.Errorf("page.TypeWithDelay: no element matches %q", selector)
	}
	start := time.Now()
	if err := agent.Type(p.document, selector, text); err != nil {
		return actions.TypeResult{Ref: selector, Duration: time.Since(start), Error: err.Error()}, fmt.Errorf("page.TypeWithDelay: %w", err)
	}
	result := actions.TypeResult{Success: true, Ref: selector, CharsTyped: len(text), Duration: time.Since(start)}
	return result, nil
}

// Form fills and optionally submits a form. The fields map maps CSS
// selectors to values. Each field is filled via actions.NewFormFill.
// If submit is true, the form is submitted via actions.NewFormSubmit
// after all fields are filled (TASK-2343).
//
// Returns the batch of FormResults from all actions (fills + optional
// submit). If any fill fails, the remaining fills are still attempted
// but the submit is skipped.
func (p *Page) Form(ctx context.Context, formSelector string, fields map[string]string, submit bool) ([]actions.FormResult, error) {
	if p == nil || p.document == nil {
		return nil, fmt.Errorf("page: nil page or document")
	}
	if formSelector == "" {
		return nil, fmt.Errorf("page.Form: empty form selector")
	}
	formElement, err := p.document.QuerySelector(formSelector)
	if err != nil {
		return nil, fmt.Errorf("page.Form: query %q: %w", formSelector, err)
	}
	if formElement == nil {
		return nil, fmt.Errorf("page.Form: no form matches %q", formSelector)
	}
	form := agent.FindForm(p.document, formSelector)
	if form == nil {
		return nil, fmt.Errorf("page.Form: selector %q does not resolve to a form", formSelector)
	}
	if len(fields) == 0 {
		return nil, fmt.Errorf("page.Form: no fields to fill")
	}
	if ctx == nil {
		return nil, fmt.Errorf("page.Form: context required")
	}
	if err := p.sessionError(); err != nil {
		return nil, err
	}
	if submit {
		if _, preflightErr := form.Submit(); preflightErr != nil {
			if !errors.Is(preflightErr, agent.ErrFormSubmissionRequiresBrowser) {
				return nil, fmt.Errorf("page.Form: submit preflight: %w", preflightErr)
			}
			result := actions.FormResult{Type: actions.FormActionSubmit, Ref: formSelector, Error: preflightErr.Error()}
			return []actions.FormResult{result}, preflightErr
		}
	}

	var allActions []actions.FormAction
	allFieldsFound := true
	selectors := make([]string, 0, len(fields))
	for selector := range fields {
		selectors = append(selectors, selector)
	}
	sort.Strings(selectors)
	for _, selector := range selectors {
		value := fields[selector]
		fieldElement, fieldErr := p.document.QuerySelector(selector)
		if fieldErr != nil || fieldElement == nil {
			allFieldsFound = false
			// Record a failed fill for the missing field.
			allActions = append(allActions, actions.FormAction{
				Type:  actions.FormActionFill,
				Ref:   "",
				Value: value,
			})
			continue
		}
		allActions = append(allActions, actions.NewFormFill(selector, value))
	}

	if submit && allFieldsFound {
		allActions = append(allActions, actions.NewFormSubmit(formSelector))
	}

	results := make([]actions.FormResult, 0, len(allActions))
	for _, action := range allActions {
		start := time.Now()
		result := actions.FormResult{Type: action.Type, Ref: action.Ref}
		switch action.Type {
		case actions.FormActionFill:
			if action.Ref == "" {
				result.Error = "form: field not found"
			} else if err := agent.Type(p.document, action.Ref, action.Value); err != nil {
				result.Error = err.Error()
			} else {
				result.Success = true
			}
		case actions.FormActionSubmit:
			result.Error = renderlessFormSubmissionError(form.EncType).Error()
		default:
			result.Error = "form: unsupported renderless action"
		}
		result.Duration = time.Since(start)
		results = append(results, result)
	}

	// Check for errors.
	var firstErr error
	for _, r := range results {
		if !r.Success {
			if r.Type == actions.FormActionSubmit {
				firstErr = renderlessFormSubmissionError(form.EncType)
			} else {
				firstErr = fmt.Errorf("page.Form: %s", r.Error)
			}
			break
		}
	}

	return results, firstErr
}

// FormFill is a convenience alias for Form with submit=false.
func (p *Page) FormFill(ctx context.Context, formSelector string, fields map[string]string) ([]actions.FormResult, error) {
	return p.Form(ctx, formSelector, fields, false)
}

// FormSubmit is a convenience method that submits a form without filling
// any fields. This is useful when the form was filled via other means
// (e.g. via JS) and just needs a submit action (TASK-2343).
func (p *Page) FormSubmit(ctx context.Context, formSelector string) (actions.FormResult, error) {
	if p == nil || p.document == nil {
		return actions.FormResult{}, fmt.Errorf("page: nil page or document")
	}
	if formSelector == "" {
		return actions.FormResult{}, fmt.Errorf("page.FormSubmit: empty form selector")
	}
	formElement, err := p.document.QuerySelector(formSelector)
	if err != nil {
		return actions.FormResult{}, fmt.Errorf("page.FormSubmit: query %q: %w", formSelector, err)
	}
	if formElement == nil {
		return actions.FormResult{}, fmt.Errorf("page.FormSubmit: no form matches %q", formSelector)
	}
	form := agent.FindForm(p.document, formSelector)
	if form == nil {
		return actions.FormResult{}, fmt.Errorf("page.FormSubmit: selector %q does not resolve to a form", formSelector)
	}
	if ctx == nil {
		return actions.FormResult{}, fmt.Errorf("page.FormSubmit: context required")
	}
	if sessionErr := p.sessionError(); sessionErr != nil {
		return actions.FormResult{}, sessionErr
	}
	start := time.Now()
	err = renderlessFormSubmissionError(form.EncType)
	return actions.FormResult{Type: actions.FormActionSubmit, Ref: formSelector, Duration: time.Since(start), Error: err.Error()}, err
}

func renderlessFormSubmissionError(encType string) error {
	return &agent.FormSubmissionUnsupportedError{EncType: encType, Reason: "renderless DOM submit requires browser event semantics"}
}

// ClickSelector is a convenience method that clicks the first element
// matching the given CSS selector. It delegates to the page's JS context
// to dispatch a click event (TASK-2343).
func (p *Page) ClickSelector(ctx context.Context, selector string) error {
	if p == nil || p.document == nil {
		return fmt.Errorf("page: nil page or document")
	}
	if selector == "" {
		return fmt.Errorf("page.ClickSelector: empty selector")
	}
	element, err := p.document.QuerySelector(selector)
	if err != nil {
		return fmt.Errorf("page.ClickSelector: query %q: %w", selector, err)
	}
	if element == nil {
		return fmt.Errorf("page.ClickSelector: no element matches %q", selector)
	}
	return p.Click(ctx, element)
}
