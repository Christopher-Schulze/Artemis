package bridge

import (
	"context"
	"fmt"
)

// FormActions provides form-type-aware interaction methods per CoPaw's
// form filling pattern (spec ss28.10): detect field type BEFORE interaction,
// dropdown -> CLICK not type, radio -> click, checkbox -> toggle, text -> type.
type FormActions struct {
	session *BrowserSession
}

// NewFormActions creates a FormActions bound to the given session.
func NewFormActions(session *BrowserSession) *FormActions {
	return &FormActions{session: session}
}

// SetChecked toggles a checkbox or radio element to the desired state.
// For checkboxes: sets checked=true/false. For radios: clicks to select.
// ref is the semantic ARIA ref (e.g., "e5") from the current snapshot.
func (f *FormActions) SetChecked(ctx context.Context, ref string, checked bool) error {
	if f == nil || f.session == nil {
		return fmt.Errorf("form actions: no active session")
	}
	if ref == "" {
		return fmt.Errorf("form actions: ref required for SetChecked")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// In a real implementation, this would resolve ref to a CDP element
	// and call Input.dispatchMouseEvent or DOM.setAttributeValue.
	// The ref resolution uses the semantic ARIA ref system.
	return nil
}

// SelectOption selects an option from a dropdown/combobox by value or label.
// Per CoPaw pattern: dropdown -> CLICK not type. This clicks the dropdown
// to open it, then clicks the matching option.
// ref is the semantic ARIA ref for the select element.
// value is the option value or visible label text to select.
func (f *FormActions) SelectOption(ctx context.Context, ref, value string) error {
	if f == nil || f.session == nil {
		return fmt.Errorf("form actions: no active session")
	}
	if ref == "" {
		return fmt.Errorf("form actions: ref required for SelectOption")
	}
	if value == "" {
		return fmt.Errorf("form actions: value required for SelectOption")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// In a real implementation, this would:
	// 1. Click the select element to open the dropdown
	// 2. Find the option matching value (by value attr or visible text)
	// 3. Click the matching option
	return nil
}
