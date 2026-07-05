package engine

import (
	"context"
	"fmt"
	"time"

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
// It clears the field first, then types the text with default keystroke
// timing (50ms delay, 20ms variance). Returns the TypeResult from the
// underlying actions.TypeAction (TASK-2343).
func (p *Page) Type(ctx context.Context, selector, text string) (actions.TypeResult, error) {
	if p == nil || p.document == nil {
		return actions.TypeResult{}, fmt.Errorf("page: nil page or document")
	}
	if selector == "" {
		return actions.TypeResult{}, fmt.Errorf("page.Type: empty selector")
	}
	node, err := p.document.QuerySelector(selector)
	if err != nil {
		return actions.TypeResult{}, fmt.Errorf("page.Type: query %q: %w", selector, err)
	}
	if node == nil {
		return actions.TypeResult{}, fmt.Errorf("page.Type: no element matches %q", selector)
	}
	action := actions.NewTypeAction(selector, text)
	result := action.Execute(ctx)
	if !result.Success {
		return result, fmt.Errorf("page.Type: %s", result.Error)
	}
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
	node, err := p.document.QuerySelector(selector)
	if err != nil {
		return actions.TypeResult{}, fmt.Errorf("page.TypeWithDelay: query %q: %w", selector, err)
	}
	if node == nil {
		return actions.TypeResult{}, fmt.Errorf("page.TypeWithDelay: no element matches %q", selector)
	}
	action := actions.TypeAction{
		Ref:        selector,
		Text:       text,
		Delay:      delay,
		Variance:   variance,
		ClearFirst: true,
	}
	result := action.Execute(ctx)
	if !result.Success {
		return result, fmt.Errorf("page.TypeWithDelay: %s", result.Error)
	}
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
	formNode, err := p.document.QuerySelector(formSelector)
	if err != nil {
		return nil, fmt.Errorf("page.Form: query %q: %w", formSelector, err)
	}
	if formNode == nil {
		return nil, fmt.Errorf("page.Form: no form matches %q", formSelector)
	}
	if len(fields) == 0 {
		return nil, fmt.Errorf("page.Form: no fields to fill")
	}

	var allActions []actions.FormAction
	allFieldsFound := true
	for selector, value := range fields {
		fieldNode, fieldErr := p.document.QuerySelector(selector)
		if fieldErr != nil || fieldNode == nil {
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

	results := actions.FormBatch(ctx, allActions)

	// Check for errors.
	var firstErr error
	for _, r := range results {
		if !r.Success {
			firstErr = fmt.Errorf("page.Form: %s", r.Error)
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
	formNode, err := p.document.QuerySelector(formSelector)
	if err != nil {
		return actions.FormResult{}, fmt.Errorf("page.FormSubmit: query %q: %w", formSelector, err)
	}
	if formNode == nil {
		return actions.FormResult{}, fmt.Errorf("page.FormSubmit: no form matches %q", formSelector)
	}
	action := actions.NewFormSubmit(formSelector)
	result := action.Execute(ctx)
	if !result.Success {
		return result, fmt.Errorf("page.FormSubmit: %s", result.Error)
	}
	return result, nil
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
	node, err := p.document.QuerySelector(selector)
	if err != nil {
		return fmt.Errorf("page.ClickSelector: query %q: %w", selector, err)
	}
	if node == nil {
		return fmt.Errorf("page.ClickSelector: no element matches %q", selector)
	}
	return p.Click(ctx, node)
}
